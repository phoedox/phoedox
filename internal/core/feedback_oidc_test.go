package core

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"

	"github.com/phoedox/phoedox/internal/requests"
)

// testIdP is a minimal OpenID provider over TLS: discovery, JWKS and a token
// endpoint that answers codes the test registered.
type testIdP struct {
	*httptest.Server
	mu    sync.Mutex
	codes map[string]map[string]any
}

func newTestIdP(t *testing.T) *testIdP {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	f := &testIdP{codes: map[string]map[string]any{}}
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
		f.mu.Lock()
		claims, ok := f.codes[r.PostForm.Get("code")]
		f.mu.Unlock()
		if !ok || r.PostForm.Get("code_verifier") == "" {
			http.Error(w, `{"error":"invalid_grant"}`, 400)
			return
		}
		claims["iss"], claims["aud"] = f.URL, "starfall-portal"
		claims["iat"], claims["exp"] = time.Now().Unix(), time.Now().Add(time.Hour).Unix()
		signer, _ := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: jose.JSONWebKey{Key: key, KeyID: "k1"}}, (&jose.SignerOptions{}).WithType("JWT"))
		payload, _ := json.Marshal(claims)
		obj, _ := signer.Sign(payload)
		raw, _ := obj.CompactSerialize()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "at", "token_type": "Bearer", "id_token": raw})
	})
	f.Server = httptest.NewTLSServer(mux)
	t.Cleanup(f.Close)
	return f
}

func TestFeedbackReporterLogin(t *testing.T) {
	ctx := context.Background()
	s, owner, _, _ := setup(t)
	s.PublicURL = "https://docs.example"
	idp := newTestIdP(t)
	s.RequestPolicy = requests.Policy{AllowPrivate: true, RootCAs: idp.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs}

	personal, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Solo"})
	settings := DefaultPortalSettings()
	settings.Anonymous = true
	settings.ReporterLogin = &ReporterLogin{Issuer: idp.URL, ClientID: "starfall-portal", ClientSecret: "s3cret", Label: "Starfall account"}
	if _, err := s.SaveFeedbackPortal(ctx, owner, personal.Slug, "", PortalInput{Handle: "solo", Name: "Solo", Settings: &settings}); invalidKey(err) != "validation.feedback_login_org" {
		t.Fatalf("personal project: %v", err)
	}
	admin := owner
	admin.User.IsAdmin = true
	org, err := s.CreateOrg(ctx, admin, OrgInput{Name: "Starfall Studio"})
	if err != nil {
		t.Fatal(err)
	}
	orgSlug := org.Slug
	p, err := s.CreateProject(ctx, owner, ProjectInput{Name: "Starfall", Org: &orgSlug})
	if err != nil {
		t.Fatal(err)
	}
	bad := settings
	bad.ReporterLogin = &ReporterLogin{Issuer: "http://idp.example", ClientID: "x"}
	if _, err := s.SaveFeedbackPortal(ctx, owner, p.Slug, "", PortalInput{Handle: "starfall", Name: "Starfall", Settings: &bad}); invalidKey(err) != "validation.feedback_login" {
		t.Fatalf("http issuer: %v", err)
	}
	v, err := s.SaveFeedbackPortal(ctx, owner, p.Slug, "", PortalInput{Handle: "starfall", Name: "Starfall", Settings: &settings})
	if err != nil {
		t.Fatal(err)
	}
	rl := v.Settings.ReporterLogin
	if rl == nil || !rl.HasSecret || rl.ClientSecret != "" || rl.ClientSecretSealed != "" {
		t.Fatalf("secret must be sealed and hidden: %+v", rl)
	}
	// Saving again without a secret keeps it.
	again := v.Settings
	again.ReporterLogin.Required = true
	if v, err = s.SaveFeedbackPortal(ctx, owner, p.Slug, v.ID, PortalInput{Settings: &again}); err != nil || !v.Settings.ReporterLogin.HasSecret {
		t.Fatalf("keep secret: %v %+v", err, v.Settings.ReporterLogin)
	}
	pub, _ := s.PublicFeedbackPortal(ctx, "starfall", "en")
	if pub.Login == nil || pub.Login.Label != "Starfall account" || !pub.Login.Required {
		t.Fatalf("public: %+v", pub.Login)
	}
	if _, err := s.SubmitFeedback(ctx, nil, "starfall", ReportInput{Title: "Lag"}, nil, Intake{Via: "form"}); invalidKey(err) != "validation.feedback_login_required" {
		t.Fatalf("login required: %v", err)
	}

	// Sign in: start, the provider answers with a code, callback.
	authURL, state, err := s.ReporterLoginStart(ctx, "starfall")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(authURL)
	q := u.Query()
	if q.Get("client_id") != "starfall-portal" || q.Get("redirect_uri") != "https://docs.example"+ReporterCallbackPath || q.Get("code_challenge") == "" {
		t.Fatalf("auth url: %s", authURL)
	}
	idp.mu.Lock()
	idp.codes["c1"] = map[string]any{"sub": "player-77", "name": "Mira", "email": "mira@example.org", "email_verified": true, "nonce": q.Get("nonce")}
	idp.mu.Unlock()
	if _, _, err := s.ReporterLoginCallback(ctx, state, "other", "c1", ""); invalidKey(err) != "validation.feedback_login_failed" {
		t.Fatalf("state mismatch: %v", err)
	}
	authURL, state, _ = s.ReporterLoginStart(ctx, "starfall")
	u, _ = url.Parse(authURL)
	idp.mu.Lock()
	idp.codes["c1"]["nonce"] = u.Query().Get("nonce")
	idp.mu.Unlock()
	handle, session, err := s.ReporterLoginCallback(ctx, state, state, "c1", "")
	if err != nil || handle != "starfall" || session == "" {
		t.Fatalf("callback: %v %s", err, handle)
	}
	// The state is used up.
	if _, _, err := s.ReporterLoginCallback(ctx, state, state, "c1", ""); err == nil {
		t.Fatal("state reused")
	}
	r := s.ReporterFor(ctx, "starfall", session)
	if r == nil || r.Sub != "player-77" || r.Name != "Mira" || r.Email != "mira@example.org" {
		t.Fatalf("reporter: %+v", r)
	}
	if s.ReporterFor(ctx, "other", session) != nil {
		t.Fatal("session works for another portal")
	}
	rec, err := s.SubmitFeedback(ctx, nil, "starfall", ReportInput{Title: "Lag in the lobby"}, nil, Intake{Via: "form", Reporter: r})
	if err != nil {
		t.Fatal(err)
	}
	topics, _ := s.FeedbackTopics(ctx, owner, p.Slug, TopicQuery{})
	d, _ := s.FeedbackTopic(ctx, owner, p.Slug, topics.Topics[0].ID)
	got := d.Reports[0]
	if got.Context["reporter"] != "player-77" || got.Context["reporter_name"] != "Mira" || got.Context["reporter_verified"] != "login" || got.Email != "mira@example.org" {
		t.Fatalf("report: %+v %s", got.Context, got.Email)
	}
	_ = rec
	s.ReporterLogout(ctx, session)
	if s.ReporterFor(ctx, "starfall", session) != nil {
		t.Fatal("still signed in after logout")
	}
}
