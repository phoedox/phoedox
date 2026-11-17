package core

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/phoedox/phoedox/internal/store"
)

// Reporters signing in with the product's own login (OpenID Connect):
// organizations connect their identity provider to a portal; people sign
// in there and their reports carry who they are in the product (verified),
// without a Phoedox account. The redirect address to register at the
// provider is <instance>/api/v1/feedback/login/callback.

// ReporterLogin is a portal's OpenID Connect login for reporters.
type ReporterLogin struct {
	Issuer   string `json:"issuer"`
	ClientID string `json:"clientId"`
	// ClientSecret is write-only: given when saving, then kept sealed.
	ClientSecret       string `json:"clientSecret,omitempty"`
	ClientSecretSealed string `json:"clientSecretSealed,omitempty"`
	HasSecret          bool   `json:"hasSecret"`
	// Label is the button text, e.g. "Starfall account".
	Label string `json:"label,omitempty"`
	// Required: only signed-in reporters may report (besides keys).
	Required bool `json:"required,omitempty"`
}

// Reporter is a person signed in with a portal's login.
type Reporter struct {
	Portal string `json:"-"`
	Sub    string `json:"sub"`
	Name   string `json:"name"`
	Email  string `json:"email,omitempty"`
}

// reporterSession is what a reporter's sign-in stores.
type reporterSession struct {
	Reporter
	PortalID string `json:"portal"`
}

// ReporterCookie holds a reporter's sign-in for one portal; the path limits
// it to that portal's API.
const ReporterCookie = "phx_reporter"

// ReporterCallbackPath is where the identity provider sends people back.
const ReporterCallbackPath = "/api/v1/feedback/login/callback"

const (
	kindReporterLogin   = "fb_login"
	kindReporterSession = "fb_reporter"
	reporterSessionTTL  = 7 * 24 * time.Hour
)

func (rl *ReporterLogin) validate() error {
	rl.Issuer = strings.TrimRight(strings.TrimSpace(rl.Issuer), "/")
	rl.ClientID = strings.TrimSpace(rl.ClientID)
	rl.Label = excerpt(strings.TrimSpace(rl.Label), 60)
	u, err := url.Parse(rl.Issuer)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || rl.ClientID == "" {
		return invalid("validation.feedback_login", nil, "the login needs the https address of the provider (issuer) and a client id")
	}
	return nil
}

// sealLogin keeps the client secret sealed: a new one replaces the old,
// none keeps what was there.
func (s *Service) sealLogin(rl *ReporterLogin, before *ReporterLogin) error {
	if rl.ClientSecret != "" {
		if s.Box == nil {
			rl.ClientSecretSealed = "plain:" + rl.ClientSecret // tests
		} else {
			sealed, err := s.Box.Seal([]byte(rl.ClientSecret), "feedback-oidc")
			if err != nil {
				return err
			}
			rl.ClientSecretSealed = sealed
		}
	} else if before != nil && before.Issuer == rl.Issuer && before.ClientID == rl.ClientID {
		rl.ClientSecretSealed = before.ClientSecretSealed
	} else {
		rl.ClientSecretSealed = ""
	}
	rl.ClientSecret = ""
	rl.HasSecret = rl.ClientSecretSealed != ""
	return nil
}

func (s *Service) loginSecret(rl *ReporterLogin) string {
	if v, ok := strings.CutPrefix(rl.ClientSecretSealed, "plain:"); ok && s.Box == nil {
		return v
	}
	if s.Box == nil || rl.ClientSecretSealed == "" {
		return ""
	}
	raw, err := s.Box.Open(rl.ClientSecretSealed, "feedback-oidc")
	if err != nil {
		return ""
	}
	return string(raw)
}

// providers caches discovered identity providers per issuer.
var reporterProviders = struct {
	sync.Mutex
	m map[string]providerEntry
}{m: map[string]providerEntry{}}

type providerEntry struct {
	p      *oidc.Provider
	failed time.Time
}

func (s *Service) oidcCtx(ctx context.Context) context.Context {
	return oidc.ClientContext(ctx, s.RequestPolicy.Client())
}

func (s *Service) reporterProvider(ctx context.Context, issuer string) (*oidc.Provider, error) {
	reporterProviders.Lock()
	defer reporterProviders.Unlock()
	e := reporterProviders.m[issuer]
	if e.p != nil {
		return e.p, nil
	}
	if time.Since(e.failed) < time.Minute {
		return nil, errors.New("provider unavailable")
	}
	p, err := oidc.NewProvider(s.oidcCtx(ctx), issuer)
	if err != nil {
		reporterProviders.m[issuer] = providerEntry{failed: time.Now()}
		return nil, err
	}
	reporterProviders.m[issuer] = providerEntry{p: p}
	return p, nil
}

func (s *Service) reporterOAuth(p *oidc.Provider, rl *ReporterLogin) *oauth2.Config {
	return &oauth2.Config{ClientID: rl.ClientID, ClientSecret: s.loginSecret(rl), Endpoint: p.Endpoint(),
		RedirectURL: s.PublicURL + ReporterCallbackPath, Scopes: []string{oidc.ScopeOpenID, "profile", "email"}}
}

func loginFailed(err error) error {
	return invalid("validation.feedback_login_failed", nil, "the sign-in did not work: %v", err)
}

type reporterLoginState struct {
	Portal   string `json:"portal"`
	Handle   string `json:"handle"`
	Nonce    string `json:"nonce"`
	Verifier string `json:"verifier"`
}

// ReporterLoginStart returns the provider address to send the browser to
// and the state that binds the sign-in to this browser (a cookie).
func (s *Service) ReporterLoginStart(ctx context.Context, handle string) (string, string, error) {
	portal, err := s.DB.FeedbackPortalByHandle(ctx, handle)
	if err != nil {
		return "", "", err
	}
	ps := portalSettings(portal)
	rl := ps.ReporterLogin
	if rl == nil || !portal.Enabled {
		return "", "", ErrNotFound
	}
	p, err := s.reporterProvider(ctx, rl.Issuer)
	if err != nil {
		return "", "", loginFailed(err)
	}
	state, nonce := newSecret(""), newSecret("")
	st := reporterLoginState{Portal: portal.ID, Handle: portal.Handle, Nonce: nonce, Verifier: oauth2.GenerateVerifier()}
	data, _ := json.Marshal(st)
	exp := store.Now()
	exp.Time = exp.Time.Add(10 * time.Minute)
	if err := s.DB.CreateChallenge(ctx, store.Challenge{ID: hashSecret(state), Kind: kindReporterLogin, Data: string(data)}, exp); err != nil {
		return "", "", err
	}
	return s.reporterOAuth(p, rl).AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(st.Verifier)), state, nil
}

// ReporterLoginCallback finishes a sign-in: it returns the portal's handle
// and the reporter's session token (cookie value).
func (s *Service) ReporterLoginCallback(ctx context.Context, state, cookieState, code, idpError string) (string, string, error) {
	if state == "" || subtle.ConstantTimeCompare([]byte(state), []byte(cookieState)) != 1 {
		return "", "", loginFailed(errors.New("state mismatch"))
	}
	ch, err := s.DB.GetChallenge(ctx, hashSecret(state), kindReporterLogin)
	if err != nil {
		return "", "", loginFailed(errors.New("sign-in expired"))
	}
	_ = s.DB.DeleteChallenge(ctx, ch.ID)
	var st reporterLoginState
	_ = json.Unmarshal([]byte(ch.Data), &st)
	if idpError != "" {
		return st.Handle, "", loginFailed(errors.New(idpError))
	}
	portal, err := s.DB.FeedbackPortal(ctx, "", st.Portal)
	if err != nil {
		return st.Handle, "", err
	}
	rl := portalSettings(portal).ReporterLogin
	if rl == nil {
		return portal.Handle, "", ErrNotFound
	}
	p, err := s.reporterProvider(ctx, rl.Issuer)
	if err != nil {
		return portal.Handle, "", loginFailed(err)
	}
	octx := s.oidcCtx(ctx)
	tok, err := s.reporterOAuth(p, rl).Exchange(octx, code, oauth2.VerifierOption(st.Verifier))
	if err != nil {
		return portal.Handle, "", loginFailed(err)
	}
	raw, ok := tok.Extra("id_token").(string)
	if !ok {
		return portal.Handle, "", loginFailed(errors.New("no id_token"))
	}
	idt, err := p.Verifier(&oidc.Config{ClientID: rl.ClientID}).Verify(octx, raw)
	if err != nil {
		return portal.Handle, "", loginFailed(err)
	}
	if subtle.ConstantTimeCompare([]byte(idt.Nonce), []byte(st.Nonce)) != 1 {
		return portal.Handle, "", loginFailed(errors.New("nonce mismatch"))
	}
	var c struct {
		Sub           string `json:"sub"`
		Name          string `json:"name"`
		Username      string `json:"preferred_username"`
		Email         string `json:"email"`
		EmailVerified *bool  `json:"email_verified"`
	}
	if err := idt.Claims(&c); err != nil || c.Sub == "" {
		return portal.Handle, "", loginFailed(errors.New("no subject"))
	}
	r := Reporter{Portal: portal.ID, Sub: excerpt(c.Sub, 200), Name: excerpt(firstNonEmpty(c.Name, c.Username), 200)}
	if c.EmailVerified == nil || *c.EmailVerified {
		r.Email = excerpt(c.Email, 254)
	}
	if r.Name == "" {
		r.Name = r.Sub
	}
	session := newSecret("fbr_")
	data, _ := json.Marshal(reporterSession{Reporter: r, PortalID: r.Portal})
	exp := store.Now()
	exp.Time = exp.Time.Add(reporterSessionTTL)
	if err := s.DB.CreateChallenge(ctx, store.Challenge{ID: hashSecret(session), Kind: kindReporterSession, Data: string(data)}, exp); err != nil {
		return portal.Handle, "", err
	}
	return portal.Handle, session, nil
}

// ReporterFor returns the signed-in reporter of a portal (nil when none).
func (s *Service) ReporterFor(ctx context.Context, handle, session string) *Reporter {
	if session == "" {
		return nil
	}
	ch, err := s.DB.GetChallenge(ctx, hashSecret(session), kindReporterSession)
	if err != nil {
		return nil
	}
	var rs reporterSession
	if json.Unmarshal([]byte(ch.Data), &rs) != nil {
		return nil
	}
	r := rs.Reporter
	r.Portal = rs.PortalID
	portal, err := s.DB.FeedbackPortalByHandle(ctx, handle)
	if err != nil || portal.ID != r.Portal || portalSettings(portal).ReporterLogin == nil {
		return nil
	}
	return &r
}

// ReporterLogout ends a reporter's sign-in.
func (s *Service) ReporterLogout(ctx context.Context, session string) {
	if session != "" {
		_ = s.DB.DeleteChallenge(ctx, hashSecret(session))
	}
}
