package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"

	"github.com/phoedox/phoedox/internal/auth"
	"github.com/phoedox/phoedox/internal/config"
	"github.com/phoedox/phoedox/internal/core"
	"github.com/phoedox/phoedox/internal/mcp"
	"github.com/phoedox/phoedox/internal/store"
)

// httpClient talks to a real test server with a session cookie.
type httpClient struct {
	t      *testing.T
	base   string
	cookie *http.Cookie
}

func (c *httpClient) json(method, path string, body any) (int, map[string]any) {
	c.t.Helper()
	var buf bytes.Buffer
	_ = json.NewEncoder(&buf).Encode(body)
	req, _ := http.NewRequest(method, c.base+path, &buf)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Phoedox", "1")
	if c.cookie != nil {
		req.AddCookie(c.cookie)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer res.Body.Close()
	for _, ck := range res.Cookies() {
		if ck.Name == auth.CookieName {
			c.cookie = ck
		}
	}
	var out map[string]any
	_ = json.NewDecoder(res.Body).Decode(&out)
	return res.StatusCode, out
}

func oauthServer(t *testing.T) (*httptest.Server, *store.DB) {
	t.Helper()
	ctx := context.Background()
	db, err := store.Open(ctx, store.SQLite, filepath.Join(t.TempDir(), "o.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	au := &auth.Auth{DB: db}
	if _, err := au.CreateUser(ctx, "admin@coph.dev", "Admin", "sehr-geheimes-passwort", true); err != nil {
		t.Fatal(err)
	}
	svc := core.New(db)
	cfg := config.Config{}
	srv := httptest.NewServer(New(Deps{Config: cfg, Core: svc, Auth: au, MCP: mcp.Handler(svc, au, "test", BaseURL(cfg))}))
	t.Cleanup(srv.Close)
	return srv, db
}

// TestMCPOAuthWithSDKClient runs the full flow with the MCP SDK's OAuth
// client: 401 with metadata, discovery, dynamic registration, consent in
// the browser (simulated), token exchange with PKCE and a tool call.
func TestMCPOAuthWithSDKClient(t *testing.T) {
	srv, _ := oauthServer(t)
	browser := &httpClient{t: t, base: srv.URL}
	if code, _ := browser.json("POST", "/api/v1/auth/login", map[string]string{"email": "admin@coph.dev", "password": "sehr-geheimes-passwort"}); code != 200 {
		t.Fatalf("login: %d", code)
	}

	// Unauthenticated MCP request points to the resource metadata.
	res, err := http.Post(srv.URL+"/mcp", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 401 || !strings.Contains(res.Header.Get("WWW-Authenticate"), srv.URL+"/.well-known/oauth-protected-resource/mcp") {
		t.Fatalf("401 header: %d %q", res.StatusCode, res.Header.Get("WWW-Authenticate"))
	}

	consented := 0
	fetch := func(ctx context.Context, args *sdkauth.AuthorizationArgs) (*sdkauth.AuthorizationResult, error) {
		u, err := url.Parse(args.URL)
		if err != nil {
			return nil, err
		}
		if code, info := browser.json("GET", "/api/v1/oauth/request?"+u.RawQuery, nil); code != 200 || info["clientName"] != "Testclient" || info["redirectHost"] != "localhost:7777" {
			t.Fatalf("consent info: %d %v", code, info)
		}
		q := u.Query()
		body := map[string]any{"approve": true}
		for k := range q {
			body[k] = q.Get(k)
		}
		code, out := browser.json("POST", "/api/v1/oauth/authorize", body)
		if code != 200 {
			t.Fatalf("authorize: %d %v", code, out)
		}
		consented++
		ru, _ := url.Parse(out["redirect"].(string))
		return &sdkauth.AuthorizationResult{Code: ru.Query().Get("code"), State: ru.Query().Get("state"), Iss: ru.Query().Get("iss")}, nil
	}
	h, err := sdkauth.NewAuthorizationCodeHandler(&sdkauth.AuthorizationCodeHandlerConfig{
		DynamicClientRegistrationConfig: &sdkauth.DynamicClientRegistrationConfig{Metadata: &oauthex.ClientRegistrationMetadata{
			RedirectURIs: []string{"http://localhost:7777/callback"}, ClientName: "Testclient", TokenEndpointAuthMethod: "none"}},
		RedirectURL:              "http://localhost:7777/callback",
		AuthorizationCodeFetcher: fetch,
		RequestRefreshToken:      true,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	client := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "1"}, nil)
	session, err := client.Connect(ctx, &sdk.StreamableClientTransport{Endpoint: srv.URL + "/mcp", OAuthHandler: h, DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatalf("connect with OAuth: %v", err)
	}
	defer session.Close()
	out, err := session.CallTool(ctx, &sdk.CallToolParams{Name: "list_projects", Arguments: map[string]any{}})
	if err != nil || out.IsError || consented != 1 {
		t.Fatalf("tool call: %v %+v consented=%d", err, out, consented)
	}

	// The app shows up as a connected app and can be revoked.
	req, _ := http.NewRequest("GET", srv.URL+"/api/v1/oauth/grants", nil)
	req.AddCookie(browser.cookie)
	res, _ = http.DefaultClient.Do(req)
	var grants []map[string]any
	_ = json.NewDecoder(res.Body).Decode(&grants)
	res.Body.Close()
	if len(grants) != 1 || grants[0]["clientName"] != "Testclient" {
		t.Fatalf("grants: %v", grants)
	}
}

func pkce() (verifier, challenge string) {
	verifier = strings.Repeat("v", 50)
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:])
}

func postForm(t *testing.T, u string, v url.Values) (int, map[string]any) {
	t.Helper()
	res, err := http.PostForm(u, v)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(res.Body).Decode(&out)
	return res.StatusCode, out
}

// TestOAuthSecurity covers the cases the happy path does not.
func TestOAuthSecurity(t *testing.T) {
	srv, _ := oauthServer(t)
	browser := &httpClient{t: t, base: srv.URL}
	browser.json("POST", "/api/v1/auth/login", map[string]string{"email": "admin@coph.dev", "password": "sehr-geheimes-passwort"})

	// registration rejects unsafe redirects
	for _, bad := range []string{"http://evil.example/cb", "javascript:alert(1)", "https://x.example/cb#frag"} {
		code, out := (&httpClient{t: t, base: srv.URL}).json("POST", "/oauth/register", map[string]any{"redirect_uris": []string{bad}})
		if code != 400 || out["error"] != "invalid_redirect_uri" {
			t.Fatalf("redirect %s: %d %v", bad, code, out)
		}
	}
	code, reg := (&httpClient{t: t, base: srv.URL}).json("POST", "/oauth/register", map[string]any{"redirect_uris": []string{"http://127.0.0.1/cb", "app.example.client:/cb"}, "client_name": "Native"})
	if code != 201 || reg["client_id"] == "" || reg["client_secret"] != nil {
		t.Fatalf("register: %d %v", code, reg)
	}
	clientID := reg["client_id"].(string)
	verifier, challenge := pkce()
	params := map[string]any{"response_type": "code", "client_id": clientID, "redirect_uri": "http://127.0.0.1:53111/cb",
		"code_challenge": challenge, "code_challenge_method": "S256", "state": "xyz"}

	// consent: unregistered redirect, missing PKCE and API tokens are refused
	bad := map[string]any{}
	for k, v := range params {
		bad[k] = v
	}
	bad["redirect_uri"] = "https://evil.example/cb"
	if code, out := browser.json("POST", "/api/v1/oauth/authorize", map[string]any{"approve": true, "client_id": clientID, "redirect_uri": "https://evil.example/cb", "response_type": "code", "code_challenge": challenge, "code_challenge_method": "S256"}); code != 400 || out["key"] != "validation.oauth_request" {
		t.Fatalf("foreign redirect: %d %v", code, out)
	}
	if code, out := browser.json("POST", "/api/v1/oauth/authorize", map[string]any{"approve": true, "client_id": clientID, "redirect_uri": "http://127.0.0.1:53111/cb", "response_type": "code"}); code != 400 || out["key"] != "validation.oauth_request" {
		t.Fatalf("missing PKCE: %d %v", code, out)
	}

	// deny → error redirect with state
	params["approve"] = false
	_, out := browser.json("POST", "/api/v1/oauth/authorize", params)
	if r := out["redirect"].(string); !strings.Contains(r, "error=access_denied") || !strings.Contains(r, "state=xyz") {
		t.Fatalf("deny: %s", r)
	}
	// approve (loopback with another port is accepted)
	params["approve"] = true
	_, out = browser.json("POST", "/api/v1/oauth/authorize", params)
	ru, _ := url.Parse(out["redirect"].(string))
	codeValue := ru.Query().Get("code")
	if ru.Host != "127.0.0.1:53111" || codeValue == "" || ru.Query().Get("iss") != srv.URL {
		t.Fatalf("approve redirect: %s", ru)
	}

	tokenURL := srv.URL + "/oauth/token"
	form := url.Values{"grant_type": {"authorization_code"}, "code": {codeValue}, "redirect_uri": {"http://127.0.0.1:53111/cb"}, "client_id": {clientID}}
	form.Set("code_verifier", strings.Repeat("w", 50))
	if code, out := postForm(t, tokenURL, form); code != 400 || out["error"] != "invalid_grant" {
		t.Fatalf("wrong verifier: %d %v", code, out)
	}
	// the code was consumed by the failed attempt; a new consent is needed
	_, out = browser.json("POST", "/api/v1/oauth/authorize", params)
	ru, _ = url.Parse(out["redirect"].(string))
	form.Set("code", ru.Query().Get("code"))
	form.Set("code_verifier", verifier)
	code, tok := postForm(t, tokenURL, form)
	if code != 200 || !strings.HasPrefix(tok["access_token"].(string), "phxo_") || tok["token_type"] != "Bearer" {
		t.Fatalf("token: %d %v", code, tok)
	}
	access, refresh := tok["access_token"].(string), tok["refresh_token"].(string)

	// access tokens work for MCP only, not for the REST API
	req, _ := http.NewRequest("GET", srv.URL+"/api/v1/projects", nil)
	req.Header.Set("Authorization", "Bearer "+access)
	if res, _ := http.DefaultClient.Do(req); res.StatusCode != 401 {
		t.Fatalf("OAuth token on REST: %d", res.StatusCode)
	}

	// code reuse revokes the grant
	if code, out := postForm(t, tokenURL, form); code != 400 || out["error"] != "invalid_grant" {
		t.Fatalf("code reuse: %d %v", code, out)
	}
	if code, out := postForm(t, tokenURL, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}, "client_id": {clientID}}); code != 400 || out["error"] != "invalid_grant" {
		t.Fatalf("grant must be revoked after code reuse: %d %v", code, out)
	}

	// fresh grant: refresh rotation and reuse detection
	_, out = browser.json("POST", "/api/v1/oauth/authorize", params)
	ru, _ = url.Parse(out["redirect"].(string))
	form.Set("code", ru.Query().Get("code"))
	_, tok = postForm(t, tokenURL, form)
	r1 := tok["refresh_token"].(string)
	code, tok2 := postForm(t, tokenURL, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {r1}, "client_id": {clientID}})
	if code != 200 || tok2["refresh_token"] == r1 {
		t.Fatalf("refresh: %d %v", code, tok2)
	}
	if code, _ := postForm(t, tokenURL, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {r1}, "client_id": {clientID}}); code != 400 {
		t.Fatalf("rotated refresh token reused: %d", code)
	}
	if code, _ := postForm(t, tokenURL, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {tok2["refresh_token"].(string)}, "client_id": {clientID}}); code != 400 {
		t.Fatalf("reuse must revoke the whole grant: %d", code)
	}

	// metadata
	res, _ := http.Get(srv.URL + "/.well-known/oauth-authorization-server")
	var meta map[string]any
	_ = json.NewDecoder(res.Body).Decode(&meta)
	res.Body.Close()
	if meta["issuer"] != srv.URL || meta["registration_endpoint"] != srv.URL+"/oauth/register" || res.Header.Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("metadata: %v", meta)
	}
}
