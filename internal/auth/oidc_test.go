package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"

	"github.com/phoedox/phoedox/internal/config"
	"github.com/phoedox/phoedox/internal/core"
	"github.com/phoedox/phoedox/internal/store"
)

// fakeIdP is a minimal OpenID provider: discovery, JWKS and a token
// endpoint that returns a signed ID token for codes registered by the test.
type fakeIdP struct {
	*httptest.Server
	key   *rsa.PrivateKey
	mu    sync.Mutex
	codes map[string]map[string]any
}

func newFakeIdP(t *testing.T) *fakeIdP {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	f := &fakeIdP{key: key, codes: map[string]map[string]any{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"issuer": f.URL, "authorization_endpoint": f.URL + "/authorize", "token_endpoint": f.URL + "/token",
			"jwks_uri": f.URL + "/jwks", "id_token_signing_alg_values_supported": []string{"RS256"}, "response_types_supported": []string{"code"}, "subject_types_supported": []string{"public"}})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig"}}})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.PostForm.Get("code_verifier") == "" {
			http.Error(w, `{"error":"invalid_request"}`, 400)
			return
		}
		f.mu.Lock()
		claims, ok := f.codes[r.PostForm.Get("code")]
		f.mu.Unlock()
		if !ok {
			http.Error(w, `{"error":"invalid_grant"}`, 400)
			return
		}
		claims["iss"], claims["aud"] = f.URL, "phx"
		claims["iat"], claims["exp"] = time.Now().Unix(), time.Now().Add(time.Hour).Unix()
		signer, _ := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: jose.JSONWebKey{Key: key, KeyID: "k1"}}, (&jose.SignerOptions{}).WithType("JWT"))
		payload, _ := json.Marshal(claims)
		obj, _ := signer.Sign(payload)
		raw, _ := obj.CompactSerialize()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "at", "token_type": "Bearer", "id_token": raw})
	})
	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Close)
	return f
}

func TestOIDC(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, store.SQLite, filepath.Join(t.TempDir(), "o.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	idp := newFakeIdP(t)
	a := &Auth{DB: db, OIDC: &OIDC{Config: config.OIDC{Issuer: idp.URL, ClientID: "phx", ClientSecret: "s", Scopes: []string{"openid", "email"}, AutoCreate: true, Domains: []string{"example.org"}}}}
	admin, _ := a.CreateUser(ctx, "admin@example.org", "Admin", "sehr-geheimes-passwort", true)
	const base = "https://docs.example.org"

	// signIn runs start + callback with the given claims.
	signIn := func(claims map[string]any, withCookie bool) (*http.Cookie, string, error) {
		t.Helper()
		u, cookie, err := a.OIDCStart(ctx, base, "/p/netz")
		if err != nil {
			t.Fatal(err)
		}
		pu, _ := url.Parse(u)
		q := pu.Query()
		if q.Get("redirect_uri") != base+"/api/v1/auth/oidc/callback" || q.Get("code_challenge_method") != "S256" || q.Get("nonce") == "" {
			t.Fatalf("auth url: %s", u)
		}
		if claims["nonce"] == nil {
			claims["nonce"] = q.Get("nonce")
		}
		code := "code-" + q.Get("state")[:8]
		idp.mu.Lock()
		idp.codes[code] = claims
		idp.mu.Unlock()
		r := httptest.NewRequest("GET", "/api/v1/auth/oidc/callback?code="+code+"&state="+url.QueryEscape(q.Get("state")), nil)
		if withCookie {
			r.AddCookie(cookie)
		}
		return a.OIDCCallback(ctx, base, r)
	}
	yes, no := true, false

	// new user in an allowed domain is created and lands on the return path
	c, ret, err := signIn(map[string]any{"sub": "s1", "email": "Neu@Example.org", "email_verified": yes, "name": "Neu"}, true)
	if err != nil || c == nil || ret != "/p/netz" {
		t.Fatalf("first sign-in: %v %q", err, ret)
	}
	created, err := db.UserByEmail(ctx, "neu@example.org")
	if err != nil || created.Name != "Neu" || created.HasPassword {
		t.Fatalf("created: %v %+v", err, created)
	}
	// same subject again, even with another email: same account
	if _, _, err := signIn(map[string]any{"sub": "s1", "email": "anders@example.org", "email_verified": yes}, true); err != nil {
		t.Fatal(err)
	}
	if n, _ := db.CountUsers(ctx); n != 2 {
		t.Fatalf("users: %d", n)
	}
	// existing account is linked only with a verified email
	if _, _, err := signIn(map[string]any{"sub": "s2", "email": "admin@example.org", "email_verified": no}, true); keyOf(err) != "validation.oidc_email" {
		t.Fatalf("unverified link: %v", err)
	}
	if _, _, err := signIn(map[string]any{"sub": "s2", "email": "admin@example.org"}, true); keyOf(err) != "validation.oidc_email" {
		t.Fatalf("missing email_verified must not link: %v", err)
	}
	if _, _, err := signIn(map[string]any{"sub": "s2", "email": "admin@example.org", "email_verified": yes}, true); err != nil {
		t.Fatal(err)
	}
	if u, err := db.UserByIdentity(ctx, idp.URL, "s2"); err != nil || u.ID != admin.ID {
		t.Fatalf("link: %v", err)
	}
	// foreign domains and disabled accounts are refused
	if _, _, err := signIn(map[string]any{"sub": "s3", "email": "x@fremd.de", "email_verified": yes}, true); keyOf(err) != "validation.oidc_no_account" {
		t.Fatalf("foreign domain: %v", err)
	}
	created.Disabled = true
	_ = db.UpdateUser(ctx, created)
	if _, _, err := signIn(map[string]any{"sub": "s1", "email": "neu@example.org", "email_verified": yes}, true); keyOf(err) != "validation.user_disabled" {
		t.Fatalf("disabled: %v", err)
	}
	// state must be bound to the browser, nonce must match
	if _, _, err := signIn(map[string]any{"sub": "s2", "email": "admin@example.org", "email_verified": yes}, false); keyOf(err) != "validation.oidc_failed" {
		t.Fatalf("missing cookie: %v", err)
	}
	if _, _, err := signIn(map[string]any{"sub": "s2", "email": "admin@example.org", "email_verified": yes, "nonce": "other"}, true); keyOf(err) != "validation.oidc_failed" {
		t.Fatalf("nonce mismatch: %v", err)
	}
	// without auto-create only known users get in
	a.OIDC.Config.AutoCreate = false
	if _, _, err := signIn(map[string]any{"sub": "s4", "email": "neu2@example.org", "email_verified": yes}, true); keyOf(err) != "validation.oidc_no_account" {
		t.Fatalf("no auto-create: %v", err)
	}
	// return paths stay local
	for in, want := range map[string]string{"/p/x": "/p/x", "//evil.example": "/", "https://evil.example": "/", "/\\evil": "/"} {
		if got := SafeReturn(in); got != want {
			t.Errorf("SafeReturn(%q) = %q", in, got)
		}
	}
	_ = core.ErrForbidden
}
