package auth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/phoedox/phoedox/internal/core"
	"github.com/phoedox/phoedox/internal/graph"
	"github.com/phoedox/phoedox/internal/store"
)

// OAuth 2.1 authorization server for MCP clients. Tokens only work for the
// MCP endpoint (resource <base>/mcp) and act with the rights of the user who
// approved the client.
const (
	OAuthScope    = "mcp"
	accessTTL     = time.Hour
	refreshTTL    = 30 * 24 * time.Hour
	codeTTL       = 2 * time.Minute
	accessPrefix  = "phxo_"
	refreshPrefix = "phxr_"
)

// OAuthError is an error response of the token endpoint (RFC 6749 §5.2).
type OAuthError struct {
	Code        string `json:"error"`
	Description string `json:"error_description,omitempty"`
}

func (e *OAuthError) Error() string { return e.Code + ": " + e.Description }

func oauthErr(code, desc string) *OAuthError { return &OAuthError{Code: code, Description: desc} }

// ClientRegistration is the dynamic client registration request (RFC 7591).
type ClientRegistration struct {
	RedirectURIs            []string `json:"redirect_uris"`
	ClientName              string   `json:"client_name,omitempty"`
	ClientURI               string   `json:"client_uri,omitempty"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method,omitempty"`
	GrantTypes              []string `json:"grant_types,omitempty"`
	ResponseTypes           []string `json:"response_types,omitempty"`
	Scope                   string   `json:"scope,omitempty"`
}

// ClientRegistrationResponse is the answer to a registration.
type ClientRegistrationResponse struct {
	ClientRegistration
	ClientID              string `json:"client_id"`
	ClientIDIssuedAt      int64  `json:"client_id_issued_at"`
	ClientSecret          string `json:"client_secret,omitempty"`
	ClientSecretExpiresAt int64  `json:"client_secret_expires_at"`
}

var blockedSchemes = []string{"http", "javascript", "data", "file", "about", "blob", "vbscript", "ftp", "ws", "wss", "mailto", "tel"}

func isLoopback(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// validRedirect allows https, http on loopback and private-use schemes of
// native apps (RFC 8252), never fragments.
func validRedirect(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Fragment != "" || strings.Contains(raw, "#") || u.Scheme == "" {
		return false
	}
	switch s := strings.ToLower(u.Scheme); {
	case s == "https":
		return u.Host != ""
	case s == "http":
		return isLoopback(u.Hostname())
	default:
		return !slices.Contains(blockedSchemes, s)
	}
}

// redirectMatches compares exactly, except that loopback redirects may use
// any port (native apps pick a free port, RFC 8252 §7.3).
func redirectMatches(registered []string, got string) bool {
	if slices.Contains(registered, got) {
		return true
	}
	g, err := url.Parse(got)
	if err != nil || g.Scheme != "http" || !isLoopback(g.Hostname()) {
		return false
	}
	for _, r := range registered {
		if u, err := url.Parse(r); err == nil && u.Scheme == "http" && u.Hostname() == g.Hostname() && u.Path == g.Path && u.RawQuery == g.RawQuery {
			return true
		}
	}
	return false
}

// RegisterClient registers an OAuth client. Registration is open, as MCP
// clients register themselves; the user decides on the consent screen.
func (a *Auth) RegisterClient(ctx context.Context, in ClientRegistration) (ClientRegistrationResponse, *OAuthError) {
	if len(in.RedirectURIs) == 0 || len(in.RedirectURIs) > 10 {
		return ClientRegistrationResponse{}, oauthErr("invalid_redirect_uri", "1 to 10 redirect_uris are required")
	}
	for _, u := range in.RedirectURIs {
		if !validRedirect(u) {
			return ClientRegistrationResponse{}, oauthErr("invalid_redirect_uri", "redirect_uri must be https, http on localhost or an app scheme: "+u)
		}
	}
	for _, g := range in.GrantTypes {
		if g != "authorization_code" && g != "refresh_token" {
			return ClientRegistrationResponse{}, oauthErr("invalid_client_metadata", "unsupported grant type "+g)
		}
	}
	for _, rt := range in.ResponseTypes {
		if rt != "code" {
			return ClientRegistrationResponse{}, oauthErr("invalid_client_metadata", "unsupported response type "+rt)
		}
	}
	name := strings.TrimSpace(in.ClientName)
	if name == "" {
		name = "MCP-Client"
	}
	if len([]rune(name)) > 80 {
		name = string([]rune(name)[:80])
	}
	if in.ClientURI != "" {
		if u, err := url.Parse(in.ClientURI); err != nil || u.Scheme != "https" {
			in.ClientURI = ""
		}
	}
	out := ClientRegistrationResponse{ClientRegistration: in, ClientID: "phxc_" + strings.ToLower(string(graph.NewID()))}
	out.ClientName = name
	out.GrantTypes = []string{"authorization_code", "refresh_token"}
	out.ResponseTypes = []string{"code"}
	out.Scope = OAuthScope
	c := store.OAuthClient{ID: out.ClientID, Name: name, ClientURI: in.ClientURI, RedirectURIs: in.RedirectURIs}
	switch in.TokenEndpointAuthMethod {
	case "", "none":
		out.TokenEndpointAuthMethod = "none"
	case "client_secret_post", "client_secret_basic":
		plain, hash := NewSecret("phxs_")
		out.ClientSecret, c.SecretHash = plain, hash
	default:
		return ClientRegistrationResponse{}, oauthErr("invalid_client_metadata", "unsupported token_endpoint_auth_method")
	}
	created, err := a.DB.CreateOAuthClient(ctx, c)
	if err != nil {
		return ClientRegistrationResponse{}, oauthErr("server_error", "registration failed")
	}
	out.ClientIDIssuedAt = created.CreatedAt.Unix()
	_ = a.DB.AddEvent(ctx, "oauth:"+c.ID, "oauth.client_register", c.ID, map[string]any{"name": name, "redirects": in.RedirectURIs})
	return out, nil
}

// AuthRequest are the parameters of /oauth/authorize.
type AuthRequest struct {
	ResponseType        string `json:"response_type"`
	ClientID            string `json:"client_id"`
	RedirectURI         string `json:"redirect_uri"`
	CodeChallenge       string `json:"code_challenge"`
	CodeChallengeMethod string `json:"code_challenge_method"`
	State               string `json:"state"`
	Scope               string `json:"scope"`
	Resource            string `json:"resource"`
}

// AuthRequestInfo is what the consent screen shows.
type AuthRequestInfo struct {
	ClientName   string `json:"clientName"`
	ClientURI    string `json:"clientUri,omitempty"`
	RedirectHost string `json:"redirectHost"`
	Resource     string `json:"resource"`
}

func oauthInvalid(reason string) error {
	return core.Invalid("validation.oauth_request", map[string]any{"reason": reason}, "invalid authorization request: %s", reason)
}

// resourceFor checks the requested resource (RFC 8707) and returns the
// canonical MCP resource of this instance.
func resourceFor(requested, base string) (string, bool) {
	want := strings.TrimRight(base, "/") + "/mcp"
	if requested == "" || strings.TrimRight(requested, "/") == want {
		return want, true
	}
	return "", false
}

// CheckAuthRequest validates an authorization request before the consent
// screen. Errors here are shown to the user and never redirected, because
// the redirect target is not trustworthy yet.
func (a *Auth) CheckAuthRequest(ctx context.Context, req AuthRequest, base string) (AuthRequestInfo, error) {
	c, err := a.DB.OAuthClientByID(ctx, req.ClientID)
	if errors.Is(err, store.ErrNotFound) {
		return AuthRequestInfo{}, oauthInvalid("unknown client")
	}
	if err != nil {
		return AuthRequestInfo{}, err
	}
	if !redirectMatches(c.RedirectURIs, req.RedirectURI) {
		return AuthRequestInfo{}, oauthInvalid("redirect_uri is not registered")
	}
	if req.ResponseType != "code" {
		return AuthRequestInfo{}, oauthInvalid("response_type must be code")
	}
	if req.CodeChallengeMethod != "S256" || len(req.CodeChallenge) < 43 || len(req.CodeChallenge) > 128 {
		return AuthRequestInfo{}, oauthInvalid("PKCE with S256 is required")
	}
	resource, ok := resourceFor(req.Resource, base)
	if !ok {
		return AuthRequestInfo{}, oauthInvalid("unknown resource")
	}
	host := req.RedirectURI
	if u, err := url.Parse(req.RedirectURI); err == nil && u.Host != "" {
		host = u.Host
	}
	return AuthRequestInfo{ClientName: c.Name, ClientURI: c.ClientURI, RedirectHost: host, Resource: resource}, nil
}

// Authorize records the user's decision and returns where to send the
// browser: the client's redirect URI with a code or an error.
func (a *Auth) Authorize(ctx context.Context, actor core.Actor, req AuthRequest, base string, approve bool, access string, projects []string) (string, error) {
	if actor.Via != "web" {
		return "", core.ErrForbidden // only a signed-in browser may approve
	}
	info, err := a.CheckAuthRequest(ctx, req, base)
	if err != nil {
		return "", err
	}
	q := url.Values{"iss": {base}}
	if req.State != "" {
		q.Set("state", req.State)
	}
	if !approve {
		q.Set("error", "access_denied")
		_ = a.DB.AddEvent(ctx, actor.String(), "oauth.deny", req.ClientID, nil)
		return withQuery(req.RedirectURI, q), nil
	}
	if access == "" {
		access = store.AccessWrite
	}
	g, err := a.DB.UpsertGrant(ctx, store.OAuthGrant{ID: string(graph.NewID()), ClientID: req.ClientID, UserID: actor.User.ID, Scope: OAuthScope, Resource: info.Resource, Access: access, Projects: projects})
	if err != nil {
		return "", err
	}
	plain, hash := NewSecret("")
	exp := store.Now()
	exp.Time = exp.Time.Add(codeTTL)
	if err := a.DB.CreateOAuthCode(ctx, hash, g.ID, req.RedirectURI, req.CodeChallenge, exp); err != nil {
		return "", err
	}
	q.Set("code", plain)
	_ = a.DB.AddEvent(ctx, actor.String(), "oauth.authorize", g.ID, map[string]string{"client": req.ClientID, "name": info.ClientName})
	return withQuery(req.RedirectURI, q), nil
}

func withQuery(raw string, q url.Values) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	merged := u.Query()
	for k, v := range q {
		merged[k] = v
	}
	u.RawQuery = merged.Encode()
	return u.String()
}

// TokenRequest is a request to the token endpoint.
type TokenRequest struct {
	GrantType    string
	Code         string
	RedirectURI  string
	ClientID     string
	ClientSecret string
	CodeVerifier string
	RefreshToken string
	Resource     string
}

// TokenResponse is a successful token response.
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
}

func pkceMatches(verifier, challenge string) bool {
	if len(verifier) < 43 || len(verifier) > 128 {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	return subtle.ConstantTimeCompare([]byte(base64.RawURLEncoding.EncodeToString(sum[:])), []byte(challenge)) == 1
}

func (a *Auth) authenticateClient(ctx context.Context, id, secret string) (store.OAuthClient, *OAuthError) {
	c, err := a.DB.OAuthClientByID(ctx, id)
	if err != nil {
		return c, oauthErr("invalid_client", "unknown client")
	}
	if c.SecretHash != "" && subtle.ConstantTimeCompare([]byte(HashSecret(secret)), []byte(c.SecretHash)) != 1 {
		return c, oauthErr("invalid_client", "client authentication failed")
	}
	return c, nil
}

// Token implements the token endpoint for authorization codes and
// refresh tokens. Reusing a code or a rotated refresh token revokes the
// whole grant.
func (a *Auth) Token(ctx context.Context, req TokenRequest, base string) (TokenResponse, *OAuthError) {
	client, oerr := a.authenticateClient(ctx, req.ClientID, req.ClientSecret)
	if oerr != nil {
		return TokenResponse{}, oerr
	}
	if req.Resource != "" {
		if _, ok := resourceFor(req.Resource, base); !ok {
			return TokenResponse{}, oauthErr("invalid_target", "unknown resource")
		}
	}
	var grant store.OAuthGrant
	switch req.GrantType {
	case "authorization_code":
		code, err := a.DB.UseOAuthCode(ctx, HashSecret(req.Code))
		if err != nil {
			return TokenResponse{}, oauthErr("invalid_grant", "unknown or expired code")
		}
		if code.Used {
			_ = a.DB.RevokeGrant(ctx, "", code.Grant.ID)
			_ = a.DB.AddEvent(ctx, "oauth:"+client.ID, "oauth.code_reuse", code.Grant.ID, nil)
			return TokenResponse{}, oauthErr("invalid_grant", "code was already used")
		}
		if code.Expired || code.Grant.ClientID != client.ID || code.RedirectURI != req.RedirectURI {
			return TokenResponse{}, oauthErr("invalid_grant", "code does not match")
		}
		if !pkceMatches(req.CodeVerifier, code.CodeChallenge) {
			return TokenResponse{}, oauthErr("invalid_grant", "PKCE verification failed")
		}
		grant = code.Grant
	case "refresh_token":
		hash := HashSecret(req.RefreshToken)
		t, err := a.DB.OAuthTokenByHash(ctx, hash)
		if err != nil || t.Kind != "refresh" || t.Grant.ClientID != client.ID {
			return TokenResponse{}, oauthErr("invalid_grant", "unknown refresh token")
		}
		if t.Used || a.DB.UseRefreshToken(ctx, hash) != nil {
			_ = a.DB.RevokeGrant(ctx, "", t.Grant.ID)
			_ = a.DB.AddEvent(ctx, "oauth:"+client.ID, "oauth.refresh_reuse", t.Grant.ID, nil)
			return TokenResponse{}, oauthErr("invalid_grant", "refresh token was already used")
		}
		if !t.ExpiresAt.After(time.Now()) {
			return TokenResponse{}, oauthErr("invalid_grant", "refresh token expired")
		}
		grant = t.Grant
	default:
		return TokenResponse{}, oauthErr("unsupported_grant_type", "use authorization_code or refresh_token")
	}
	if u, err := a.DB.UserByID(ctx, grant.UserID); err != nil || u.Disabled {
		return TokenResponse{}, oauthErr("invalid_grant", "the user is not active")
	}
	access, accessHash := NewSecret(accessPrefix)
	refresh, refreshHash := NewSecret(refreshPrefix)
	now := store.Now()
	accessExp, refreshExp := now, now
	accessExp.Time = now.Time.Add(accessTTL)
	refreshExp.Time = now.Time.Add(refreshTTL)
	if err := a.DB.CreateOAuthToken(ctx, accessHash, grant.ID, "access", accessExp); err != nil {
		return TokenResponse{}, oauthErr("server_error", "")
	}
	if err := a.DB.CreateOAuthToken(ctx, refreshHash, grant.ID, "refresh", refreshExp); err != nil {
		return TokenResponse{}, oauthErr("server_error", "")
	}
	a.DB.TouchGrant(ctx, grant)
	return TokenResponse{AccessToken: access, TokenType: "Bearer", ExpiresIn: int(accessTTL.Seconds()), RefreshToken: refresh, Scope: grant.Scope}, nil
}

// RevokeOAuthToken implements RFC 7009 (unknown tokens are not an error).
func (a *Auth) RevokeOAuthToken(ctx context.Context, token string) {
	_ = a.DB.RevokeOAuthToken(ctx, HashSecret(token))
}

// oauthActor resolves an access token for the MCP endpoint.
func (a *Auth) oauthActor(ctx context.Context, token string) (core.Actor, time.Time, error) {
	t, err := a.DB.OAuthTokenByHash(ctx, HashSecret(token))
	if err != nil || t.Kind != "access" || !t.ExpiresAt.After(time.Now()) {
		return core.Actor{}, time.Time{}, store.ErrNotFound
	}
	u, err := a.DB.UserByID(ctx, t.Grant.UserID)
	if err != nil || u.Disabled {
		return core.Actor{}, time.Time{}, store.ErrNotFound
	}
	return core.Actor{User: u, Via: "mcp", TokenID: "oauth:" + t.Grant.ID, Scope: core.NewScope(t.Grant.Access, t.Grant.Projects)}, t.ExpiresAt.Time, nil
}

// Grants lists the apps the actor connected via OAuth.
func (a *Auth) Grants(ctx context.Context, actor core.Actor) ([]store.OAuthGrant, error) {
	return a.DB.Grants(ctx, actor.User.ID)
}

// RevokeGrant disconnects an app of the actor.
func (a *Auth) RevokeGrant(ctx context.Context, actor core.Actor, id string) error {
	err := a.DB.RevokeGrant(ctx, actor.User.ID, id)
	if err == nil {
		_ = a.DB.AddEvent(ctx, actor.String(), "oauth.revoke", id, nil)
	}
	return err
}
