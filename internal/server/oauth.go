package server

import (
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/go-chi/chi/v5"
	"github.com/modelcontextprotocol/go-sdk/oauthex"

	"github.com/phoedox/phoedox/internal/auth"
	"github.com/phoedox/phoedox/internal/core"
)

// oauthRoutes serves the OAuth 2.1 authorization server for MCP clients.
// GET /oauth/authorize is a page of the web app (consent screen).
func (a *api) oauthRoutes(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(openCORS)
		r.Get("/.well-known/oauth-protected-resource", a.resourceMetadata)
		r.Get("/.well-known/oauth-protected-resource/mcp", a.resourceMetadata)
		r.Get("/.well-known/oauth-authorization-server", a.serverMetadata)
		r.Post("/oauth/register", a.registerClient)
		r.Post("/oauth/token", a.token)
		r.Post("/oauth/revoke", a.revoke)
		for _, p := range []string{"/.well-known/oauth-protected-resource", "/.well-known/oauth-protected-resource/mcp", "/.well-known/oauth-authorization-server", "/oauth/register", "/oauth/token", "/oauth/revoke"} {
			r.Options(p, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
		}
	})
}

// openCORS allows browser-based MCP clients to reach the OAuth endpoints.
// No cookies are involved, so any origin is fine.
func openCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Access-Control-Allow-Origin", "*")
		h.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		h.Set("Access-Control-Allow-Headers", "Content-Type, Authorization, MCP-Protocol-Version")
		next.ServeHTTP(w, r)
	})
}

func (a *api) resourceMetadata(w http.ResponseWriter, r *http.Request) {
	base := a.baseURL(r)
	writeJSON(w, http.StatusOK, &oauthex.ProtectedResourceMetadata{
		Resource:               base + "/mcp",
		AuthorizationServers:   []string{base},
		ScopesSupported:        []string{auth.OAuthScope},
		BearerMethodsSupported: []string{"header"},
		ResourceName:           "Phoedox",
	})
}

func (a *api) serverMetadata(w http.ResponseWriter, r *http.Request) {
	base := a.baseURL(r)
	writeJSON(w, http.StatusOK, &oauthex.AuthServerMeta{
		Issuer:                                     base,
		AuthorizationEndpoint:                      base + "/oauth/authorize",
		TokenEndpoint:                              base + "/oauth/token",
		RegistrationEndpoint:                       base + "/oauth/register",
		RevocationEndpoint:                         base + "/oauth/revoke",
		ScopesSupported:                            []string{auth.OAuthScope},
		ResponseTypesSupported:                     []string{"code"},
		GrantTypesSupported:                        []string{"authorization_code", "refresh_token"},
		TokenEndpointAuthMethodsSupported:          []string{"none", "client_secret_post", "client_secret_basic"},
		CodeChallengeMethodsSupported:              []string{"S256"},
		AuthorizationResponseIssParameterSupported: true,
	})
}

func writeOAuth(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, status, v)
}

func (a *api) registerClient(w http.ResponseWriter, r *http.Request) {
	if !a.regLimiter.allow(clientIP(r)) {
		writeOAuth(w, http.StatusTooManyRequests, &auth.OAuthError{Code: "temporarily_unavailable", Description: "too many registrations"})
		return
	}
	var in auth.ClientRegistration
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&in); err != nil {
		writeOAuth(w, http.StatusBadRequest, &auth.OAuthError{Code: "invalid_client_metadata", Description: "invalid JSON"})
		return
	}
	res, oerr := a.auth.RegisterClient(r.Context(), in)
	if oerr != nil {
		writeOAuth(w, http.StatusBadRequest, oerr)
		return
	}
	writeOAuth(w, http.StatusCreated, res)
}

func (a *api) token(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil {
		writeOAuth(w, http.StatusBadRequest, &auth.OAuthError{Code: "invalid_request", Description: "form body expected"})
		return
	}
	f := r.PostForm
	req := auth.TokenRequest{GrantType: f.Get("grant_type"), Code: f.Get("code"), RedirectURI: f.Get("redirect_uri"),
		ClientID: f.Get("client_id"), ClientSecret: f.Get("client_secret"), CodeVerifier: f.Get("code_verifier"),
		RefreshToken: f.Get("refresh_token"), Resource: f.Get("resource")}
	if id, secret, ok := r.BasicAuth(); ok {
		// client_secret_basic: both parts are form-encoded (RFC 6749 §2.3.1).
		req.ClientID, _ = url.QueryUnescape(id)
		req.ClientSecret, _ = url.QueryUnescape(secret)
	}
	res, oerr := a.auth.Token(r.Context(), req, a.baseURL(r))
	if oerr != nil {
		status := http.StatusBadRequest
		if oerr.Code == "invalid_client" {
			status = http.StatusUnauthorized
		}
		writeOAuth(w, status, oerr)
		return
	}
	writeOAuth(w, http.StatusOK, res)
}

func (a *api) revoke(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := r.ParseForm(); err == nil && r.PostForm.Get("token") != "" {
		a.auth.RevokeOAuthToken(r.Context(), r.PostForm.Get("token"))
	}
	w.WriteHeader(http.StatusOK)
}

// --- API for the consent page and connected apps ---

func authRequestFrom(q url.Values) auth.AuthRequest {
	return auth.AuthRequest{ResponseType: q.Get("response_type"), ClientID: q.Get("client_id"), RedirectURI: q.Get("redirect_uri"),
		CodeChallenge: q.Get("code_challenge"), CodeChallengeMethod: q.Get("code_challenge_method"), State: q.Get("state"),
		Scope: q.Get("scope"), Resource: q.Get("resource")}
}

func (a *api) oauthRequest(w http.ResponseWriter, r *http.Request) {
	if !a.core.AIEnabled(r.Context()) {
		writeErr(w, core.Invalid("validation.ai_disabled", nil, "AI access is disabled on this instance"))
		return
	}
	info, err := a.auth.CheckAuthRequest(r.Context(), authRequestFrom(r.URL.Query()), a.baseURL(r))
	respond(w, info, err)
}

func (a *api) oauthAuthorize(w http.ResponseWriter, r *http.Request) {
	var in struct {
		auth.AuthRequest
		Approve  bool     `json:"approve"`
		Access   string   `json:"access"`
		Projects []string `json:"projects"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	if !a.core.AIEnabled(r.Context()) {
		writeErr(w, core.Invalid("validation.ai_disabled", nil, "AI access is disabled on this instance"))
		return
	}
	access, projects, err := a.core.TokenScope(r.Context(), actorOf(r), in.Access, in.Projects)
	if err != nil {
		writeErr(w, err)
		return
	}
	redirect, err := a.auth.Authorize(r.Context(), actorOf(r), in.AuthRequest, a.baseURL(r), in.Approve, access, projects)
	respond(w, map[string]string{"redirect": redirect}, err)
}

func (a *api) listGrants(w http.ResponseWriter, r *http.Request) {
	gs, err := a.auth.Grants(r.Context(), actorOf(r))
	respond(w, gs, err)
}

func (a *api) revokeGrant(w http.ResponseWriter, r *http.Request) {
	if err := a.auth.RevokeGrant(r.Context(), actorOf(r), chi.URLParam(r, "id")); err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
