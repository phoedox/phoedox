package auth

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/phoedox/phoedox/internal/core"
	"github.com/phoedox/phoedox/internal/graph"
	"github.com/phoedox/phoedox/internal/store"
)

// Sign-in with Home Assistant (ADR-0052): Home Assistant's own login (its
// IndieAuth-style flow, client_id = Phoedox's address), mainly for the
// Phoedox add-on. Accounts are linked by Home Assistant user id.

// HACookie binds the sign-in state to the browser.
const HACookie = "phx_ha"

const kindHA = "ha"

// HAConfig configures sign-in with Home Assistant.
type HAConfig struct {
	// URL is where browsers reach Home Assistant (PHOEDOX_HA_URL).
	URL string
	// InternalURL is where the server reaches it (PHOEDOX_HA_INTERNAL_URL,
	// default URL), e.g. http://homeassistant:8123 inside the add-on.
	InternalURL string
	// AutoCreate creates accounts on first sign-in (PHOEDOX_HA_AUTO_CREATE,
	// default true). Home Assistant's owner becomes a system admin.
	AutoCreate bool
	// Client is used for server-side calls (tests).
	Client *http.Client
}

// Enabled reports whether sign-in with Home Assistant is configured.
func (c *HAConfig) Enabled() bool { return c != nil && c.URL != "" }

func (c *HAConfig) internal() string {
	if c.InternalURL != "" {
		return strings.TrimRight(c.InternalURL, "/")
	}
	return strings.TrimRight(c.URL, "/")
}

func (c *HAConfig) client() *http.Client {
	if c.Client != nil {
		return c.Client
	}
	return &http.Client{Timeout: 15 * time.Second}
}

// issuer names Home Assistant in linked identities.
func (c *HAConfig) issuer() string { return "homeassistant:" + strings.TrimRight(c.URL, "/") }

// HAStart returns the Home Assistant login URL and the state cookie.
func (a *Auth) HAStart(ctx context.Context, base, returnTo string) (string, *http.Cookie, error) {
	if !a.HA.Enabled() {
		return "", nil, store.ErrNotFound
	}
	state, _ := NewSecret("")
	data, _ := json.Marshal(map[string]string{"return": SafeReturn(returnTo)})
	exp := store.Now()
	exp.Time = exp.Time.Add(10 * time.Minute)
	if err := a.DB.CreateChallenge(ctx, store.Challenge{ID: HashSecret(state), Kind: kindHA, Data: string(data)}, exp); err != nil {
		return "", nil, err
	}
	q := url.Values{"response_type": {"code"}, "client_id": {base + "/"}, "redirect_uri": {base + "/api/v1/auth/ha/callback"}, "state": {state}}
	c := &http.Cookie{Name: HACookie, Value: state, Path: "/api/v1/auth/ha", MaxAge: 600, HttpOnly: true, Secure: a.SecureCookies, SameSite: http.SameSiteLaxMode}
	return strings.TrimRight(a.HA.URL, "/") + "/auth/authorize?" + q.Encode(), c, nil
}

// haUser is Home Assistant's answer to auth/current_user.
type haUser struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	IsOwner bool   `json:"is_owner"`
	IsAdmin bool   `json:"is_admin"`
}

func haError(err error) error {
	return &core.InvalidError{Key: "validation.ha_login_failed", Msg: "sign-in with Home Assistant failed: " + err.Error()}
}

// HACallback finishes the sign-in: exchanges the code, asks Home Assistant
// who signed in, revokes the token again and starts a session.
func (a *Auth) HACallback(ctx context.Context, base string, r *http.Request) (*http.Cookie, string, error) {
	if !a.HA.Enabled() {
		return nil, "/", store.ErrNotFound
	}
	q := r.URL.Query()
	cookie, err := r.Cookie(HACookie)
	state := q.Get("state")
	if err != nil || state == "" || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(state)) != 1 {
		return nil, "/", haError(errors.New("state mismatch"))
	}
	ch, err := a.DB.GetChallenge(ctx, HashSecret(state), kindHA)
	if err != nil {
		return nil, "/", haError(errors.New("sign-in expired"))
	}
	_ = a.DB.DeleteChallenge(ctx, ch.ID)
	var st struct{ Return string }
	_ = json.Unmarshal([]byte(ch.Data), &st)
	if q.Get("code") == "" {
		return nil, st.Return, haError(errors.New("no code"))
	}
	tok, err := a.haToken(ctx, base, q.Get("code"))
	if err != nil {
		return nil, st.Return, haError(err)
	}
	hu, err := a.haCurrentUser(ctx, tok.AccessToken)
	a.haRevoke(ctx, tok.RefreshToken)
	if err != nil {
		return nil, st.Return, haError(err)
	}
	u, err := a.haAccount(ctx, hu)
	if err != nil {
		return nil, st.Return, err
	}
	session, err := a.startSession(ctx, u)
	if err == nil {
		_ = a.DB.AddEvent(ctx, "user:"+u.ID, "auth.login", u.ID, map[string]string{"method": "homeassistant"})
	}
	return session, st.Return, err
}

type haTokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

func (a *Auth) haToken(ctx context.Context, base, code string) (haTokens, error) {
	var t haTokens
	form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "client_id": {base + "/"}}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, a.HA.internal()+"/auth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := a.HA.client().Do(req)
	if err != nil {
		return t, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return t, fmt.Errorf("token request: HTTP %d", res.StatusCode)
	}
	if err := json.NewDecoder(res.Body).Decode(&t); err != nil || t.AccessToken == "" {
		return t, errors.New("token request: no access token")
	}
	return t, nil
}

// haCurrentUser asks over Home Assistant's WebSocket API who the token belongs to.
func (a *Auth) haCurrentUser(ctx context.Context, token string) (haUser, error) {
	var u haUser
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	wsURL := "ws" + strings.TrimPrefix(a.HA.internal(), "http") + "/api/websocket"
	c, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPClient: a.HA.client()})
	if err != nil {
		return u, err
	}
	defer c.CloseNow()
	read := func() (map[string]json.RawMessage, error) {
		_, b, err := c.Read(ctx)
		if err != nil {
			return nil, err
		}
		var m map[string]json.RawMessage
		return m, json.Unmarshal(b, &m)
	}
	typ := func(m map[string]json.RawMessage) string {
		var s string
		_ = json.Unmarshal(m["type"], &s)
		return s
	}
	write := func(v any) error {
		b, _ := json.Marshal(v)
		return c.Write(ctx, websocket.MessageText, b)
	}
	if m, err := read(); err != nil || typ(m) != "auth_required" {
		return u, errors.New("unexpected websocket greeting")
	}
	if err := write(map[string]string{"type": "auth", "access_token": token}); err != nil {
		return u, err
	}
	if m, err := read(); err != nil || typ(m) != "auth_ok" {
		return u, errors.New("websocket authentication failed")
	}
	if err := write(map[string]any{"id": 1, "type": "auth/current_user"}); err != nil {
		return u, err
	}
	for {
		m, err := read()
		if err != nil {
			return u, err
		}
		if typ(m) != "result" {
			continue
		}
		var ok bool
		_ = json.Unmarshal(m["success"], &ok)
		if !ok || json.Unmarshal(m["result"], &u) != nil || u.ID == "" {
			return u, errors.New("current user unknown")
		}
		_ = c.Close(websocket.StatusNormalClosure, "")
		return u, nil
	}
}

// haRevoke ends the refresh token: Phoedox keeps no access to Home Assistant.
func (a *Auth) haRevoke(ctx context.Context, refresh string) {
	if refresh == "" {
		return
	}
	form := url.Values{"action": {"revoke"}, "token": {refresh}}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, a.HA.internal()+"/auth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if res, err := a.HA.client().Do(req); err == nil {
		res.Body.Close()
	}
}

// haAccount finds the account linked to a Home Assistant user or creates
// one (with a placeholder address the person can change in their profile).
func (a *Auth) haAccount(ctx context.Context, hu haUser) (store.User, error) {
	iss := a.HA.issuer()
	if u, err := a.DB.UserByIdentity(ctx, iss, hu.ID); err == nil {
		if u.Disabled {
			return u, core.Invalid("validation.user_disabled", nil, "the user is disabled")
		}
		return u, nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return u, err
	}
	if !a.HA.AutoCreate {
		return store.User{}, core.Invalid("validation.ha_no_account", nil, "no account is linked to this Home Assistant user")
	}
	email := "ha-" + strings.ToLower(hu.ID) + "@home-assistant.invalid"
	name := strings.TrimSpace(hu.Name)
	if name == "" {
		name = "Home Assistant"
	}
	u, err := a.DB.CreateUser(ctx, store.User{ID: string(graph.NewID()), Email: email, Name: name, IsAdmin: hu.IsOwner})
	if err != nil {
		return u, err
	}
	_ = a.DB.AddEvent(ctx, "homeassistant", "user.create", u.ID, map[string]any{"owner": hu.IsOwner})
	if err := a.DB.LinkIdentity(ctx, iss, hu.ID, u.ID, email); err != nil {
		return u, err
	}
	return u, nil
}
