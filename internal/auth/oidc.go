package auth

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/phoedox/phoedox/internal/config"
	"github.com/phoedox/phoedox/internal/core"
	"github.com/phoedox/phoedox/internal/graph"
	"github.com/phoedox/phoedox/internal/store"
)

// OIDCCookie binds the sign-in state to the browser (login CSRF).
const OIDCCookie = "phx_oidc"

const kindOIDC = "oidc"

// OIDC signs users in with an external OpenID Connect provider. Discovery
// runs lazily and is retried after a minute if the provider was down.
type OIDC struct {
	Config config.OIDC
	// HTTPClient is used for discovery and token requests (tests).
	HTTPClient *http.Client

	mu       sync.Mutex
	provider *oidc.Provider
	failedAt time.Time
}

func (o *OIDC) ctx(ctx context.Context) context.Context {
	if o.HTTPClient != nil {
		return oidc.ClientContext(ctx, o.HTTPClient)
	}
	return ctx
}

func (o *OIDC) discover(ctx context.Context) (*oidc.Provider, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.provider != nil {
		return o.provider, nil
	}
	if time.Since(o.failedAt) < time.Minute {
		return nil, errors.New("oidc: provider unavailable")
	}
	p, err := oidc.NewProvider(o.ctx(ctx), o.Config.Issuer)
	if err != nil {
		o.failedAt = time.Now()
		return nil, fmt.Errorf("oidc discovery: %w", err)
	}
	o.provider = p
	return p, nil
}

func (o *OIDC) oauth2(p *oidc.Provider, base string) *oauth2.Config {
	return &oauth2.Config{ClientID: o.Config.ClientID, ClientSecret: o.Config.ClientSecret, Endpoint: p.Endpoint(),
		RedirectURL: base + "/api/v1/auth/oidc/callback", Scopes: o.Config.Scopes}
}

func oidcError(key string, err error) error {
	return core.Invalid(key, nil, "single sign-on failed: %v", err)
}

// SafeReturn accepts only local paths as targets after sign-in.
func SafeReturn(p string) string {
	if !strings.HasPrefix(p, "/") || strings.HasPrefix(p, "//") || strings.HasPrefix(p, "/\\") || strings.ContainsAny(p, "\r\n") {
		return "/"
	}
	return p
}

type oidcState struct {
	Nonce    string `json:"nonce"`
	Verifier string `json:"verifier"`
	Return   string `json:"return"`
}

// OIDCStart returns the provider URL to send the browser to and the cookie
// that binds the state to this browser.
func (a *Auth) OIDCStart(ctx context.Context, base, returnTo string) (string, *http.Cookie, error) {
	if a.OIDC == nil || !a.OIDC.Config.Enabled() {
		return "", nil, store.ErrNotFound
	}
	p, err := a.OIDC.discover(ctx)
	if err != nil {
		return "", nil, oidcError("validation.oidc_failed", err)
	}
	state, _ := NewSecret("")
	nonce, _ := NewSecret("")
	st := oidcState{Nonce: nonce, Verifier: oauth2.GenerateVerifier(), Return: SafeReturn(returnTo)}
	data, _ := json.Marshal(st)
	exp := store.Now()
	exp.Time = exp.Time.Add(10 * time.Minute)
	if err := a.DB.CreateChallenge(ctx, store.Challenge{ID: HashSecret(state), Kind: kindOIDC, Data: string(data)}, exp); err != nil {
		return "", nil, err
	}
	u := a.OIDC.oauth2(p, base).AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(st.Verifier))
	c := &http.Cookie{Name: OIDCCookie, Value: state, Path: "/api/v1/auth/oidc", MaxAge: 600, HttpOnly: true, Secure: a.SecureCookies, SameSite: http.SameSiteLaxMode}
	return u, c, nil
}

type oidcClaims struct {
	Subject       string `json:"sub"`
	Email         string `json:"email"`
	EmailVerified *bool  `json:"email_verified"`
	Name          string `json:"name"`
	Username      string `json:"preferred_username"`
}

// OIDCCallback finishes the sign-in and returns the session cookie and the
// local path to continue at.
func (a *Auth) OIDCCallback(ctx context.Context, base string, r *http.Request) (*http.Cookie, string, error) {
	if a.OIDC == nil || !a.OIDC.Config.Enabled() {
		return nil, "/", store.ErrNotFound
	}
	q := r.URL.Query()
	if e := q.Get("error"); e != "" {
		return nil, "/", oidcError("validation.oidc_failed", errors.New(e))
	}
	cookie, err := r.Cookie(OIDCCookie)
	state := q.Get("state")
	if err != nil || state == "" || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(state)) != 1 {
		return nil, "/", oidcError("validation.oidc_failed", errors.New("state mismatch"))
	}
	ch, err := a.DB.GetChallenge(ctx, HashSecret(state), kindOIDC)
	if err != nil {
		return nil, "/", oidcError("validation.oidc_failed", errors.New("sign-in expired"))
	}
	_ = a.DB.DeleteChallenge(ctx, ch.ID)
	var st oidcState
	_ = json.Unmarshal([]byte(ch.Data), &st)

	p, err := a.OIDC.discover(ctx)
	if err != nil {
		return nil, st.Return, oidcError("validation.oidc_failed", err)
	}
	octx := a.OIDC.ctx(ctx)
	tok, err := a.OIDC.oauth2(p, base).Exchange(octx, q.Get("code"), oauth2.VerifierOption(st.Verifier))
	if err != nil {
		return nil, st.Return, oidcError("validation.oidc_failed", err)
	}
	raw, ok := tok.Extra("id_token").(string)
	if !ok {
		return nil, st.Return, oidcError("validation.oidc_failed", errors.New("no id_token"))
	}
	idt, err := p.Verifier(&oidc.Config{ClientID: a.OIDC.Config.ClientID}).Verify(octx, raw)
	if err != nil {
		return nil, st.Return, oidcError("validation.oidc_failed", err)
	}
	if subtle.ConstantTimeCompare([]byte(idt.Nonce), []byte(st.Nonce)) != 1 {
		return nil, st.Return, oidcError("validation.oidc_failed", errors.New("nonce mismatch"))
	}
	var c oidcClaims
	if err := idt.Claims(&c); err != nil {
		return nil, st.Return, oidcError("validation.oidc_failed", err)
	}
	u, err := a.oidcUser(ctx, idt.Issuer, c)
	if err != nil {
		return nil, st.Return, err
	}
	if claim := a.OIDC.Config.GroupsClaim; claim != "" {
		var all map[string]any
		if idt.Claims(&all) == nil {
			if err := a.DB.SyncOIDCGroups(ctx, u.ID, claimStrings(all[claim])); err != nil {
				return nil, st.Return, err
			}
		}
	}
	session, err := a.startSession(ctx, u)
	if err == nil {
		_ = a.DB.AddEvent(ctx, "user:"+u.ID, "auth.login", u.ID, map[string]string{"method": "oidc"})
	}
	return session, st.Return, err
}

// oidcUser finds the account for an identity: by link, by verified email
// (and links it), or creates one if allowed.
func (a *Auth) oidcUser(ctx context.Context, issuer string, c oidcClaims) (store.User, error) {
	if u, err := a.DB.UserByIdentity(ctx, issuer, c.Subject); err == nil {
		if u.Disabled {
			return u, core.Invalid("validation.user_disabled", nil, "the user is disabled")
		}
		return u, nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return u, err
	}
	email, err := normalizeEmail(c.Email)
	if err != nil {
		return store.User{}, core.Invalid("validation.oidc_email", nil, "the provider sent no usable email address")
	}
	verified := (c.EmailVerified != nil && *c.EmailVerified) || (c.EmailVerified == nil && a.OIDC.Config.TrustEmail)
	if !verified {
		return store.User{}, core.Invalid("validation.oidc_email", nil, "the provider did not confirm the email address")
	}
	u, err := a.DB.UserByEmailAny(ctx, email)
	switch {
	case err == nil:
		if u.Disabled {
			return u, core.Invalid("validation.user_disabled", nil, "the user is disabled")
		}
	case errors.Is(err, store.ErrNotFound):
		domain := email[strings.LastIndex(email, "@")+1:]
		if !a.OIDC.Config.AutoCreate || (len(a.OIDC.Config.Domains) > 0 && !slices.Contains(a.OIDC.Config.Domains, domain)) {
			return store.User{}, core.Invalid("validation.oidc_no_account", nil, "there is no account for %s", email)
		}
		name := strings.TrimSpace(c.Name)
		if name == "" {
			name = c.Username
		}
		if name == "" {
			name, _, _ = strings.Cut(email, "@")
		}
		if u, err = a.DB.CreateUser(ctx, store.User{ID: string(graph.NewID()), Email: email, Name: name}); err != nil {
			return u, err
		}
		_ = a.DB.AddEvent(ctx, "oidc", "user.create", u.ID, map[string]string{"issuer": issuer})
	default:
		return u, err
	}
	if err := a.DB.LinkIdentity(ctx, issuer, c.Subject, u.ID, email); err != nil {
		return u, err
	}
	_ = a.DB.AddEvent(ctx, "user:"+u.ID, "auth.identity_link", u.ID, map[string]string{"issuer": issuer})
	return u, nil
}

// claimStrings reads a claim that is a list of strings or one string.
func claimStrings(v any) []string {
	switch x := v.(type) {
	case string:
		return []string{x}
	case []any:
		out := make([]string, 0, len(x))
		for _, e := range x {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}
