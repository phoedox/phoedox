package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/coder/websocket"

	"github.com/phoedox/phoedox/internal/store"
)

// fakeHA answers the token endpoint and auth/current_user over WebSocket.
type fakeHA struct {
	mu      sync.Mutex
	users   map[string]haUser // access token → user
	revoked []string
	wrongWS bool
}

func (f *fakeHA) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/auth/token":
		_ = r.ParseForm()
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.Form.Get("action") == "revoke" {
			f.revoked = append(f.revoked, r.Form.Get("token"))
			return
		}
		if r.Form.Get("grant_type") != "authorization_code" || r.Form.Get("client_id") == "" {
			http.Error(w, "bad", 400)
			return
		}
		code := r.Form.Get("code")
		if _, ok := f.users["access-"+code]; !ok {
			http.Error(w, "bad code", 400)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "access-" + code, "refresh_token": "refresh-" + code, "token_type": "Bearer"})
	case "/api/websocket":
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		ctx := r.Context()
		send := func(v any) { b, _ := json.Marshal(v); _ = c.Write(ctx, websocket.MessageText, b) }
		send(map[string]string{"type": "auth_required"})
		_, b, err := c.Read(ctx)
		if err != nil {
			return
		}
		var auth map[string]string
		_ = json.Unmarshal(b, &auth)
		f.mu.Lock()
		u, ok := f.users[auth["access_token"]]
		f.mu.Unlock()
		if !ok {
			send(map[string]string{"type": "auth_invalid"})
			return
		}
		send(map[string]string{"type": "auth_ok"})
		if _, _, err := c.Read(ctx); err != nil {
			return
		}
		send(map[string]any{"id": 1, "type": "result", "success": true, "result": u})
		_, _, _ = c.Read(ctx)
	}
}

func TestHomeAssistantSignIn(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, store.SQLite, filepath.Join(t.TempDir(), "h.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	fake := &fakeHA{users: map[string]haUser{
		"access-c1": {ID: "abc123", Name: "Jan", IsOwner: true},
		"access-c2": {ID: "def456", Name: "Kind"},
	}}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	a := &Auth{DB: db, HA: &HAConfig{URL: "https://ha.example.org", InternalURL: srv.URL, AutoCreate: true}}
	base := "https://phoedox.example.org"

	login := func(code string, withCookie bool) (*http.Cookie, string, error) {
		u, cookie, err := a.HAStart(ctx, base, "/p/haus")
		if err != nil {
			t.Fatal(err)
		}
		pu, _ := url.Parse(u)
		q := pu.Query()
		if !strings.HasPrefix(u, "https://ha.example.org/auth/authorize?") || q.Get("client_id") != base+"/" || q.Get("redirect_uri") != base+"/api/v1/auth/ha/callback" {
			t.Fatalf("authorize url: %s", u)
		}
		r := httptest.NewRequest("GET", "/api/v1/auth/ha/callback?code="+code+"&state="+url.QueryEscape(q.Get("state")), nil)
		if withCookie {
			r.AddCookie(cookie)
		}
		return a.HACallback(ctx, base, r)
	}

	if _, _, err := login("c1", false); err == nil {
		t.Fatal("signed in without the state cookie")
	}
	session, ret, err := login("c1", true)
	if err != nil || session == nil || ret != "/p/haus" {
		t.Fatalf("owner: %v %v", ret, err)
	}
	owner, err := db.UserByIdentity(ctx, "homeassistant:https://ha.example.org", "abc123")
	if err != nil || !owner.IsAdmin || owner.Name != "Jan" || owner.Email != "ha-abc123@home-assistant.invalid" {
		t.Fatalf("owner account: %+v %v", owner, err)
	}
	// Second sign-in reuses the account; other HA users are not admins.
	if _, _, err := login("c1", true); err != nil {
		t.Fatal(err)
	}
	if _, _, err := login("c2", true); err != nil {
		t.Fatal(err)
	}
	kid, _ := db.UserByIdentity(ctx, "homeassistant:https://ha.example.org", "def456")
	if kid.IsAdmin || kid.ID == owner.ID {
		t.Fatalf("second user: %+v", kid)
	}
	if _, _, err := login("nope", true); err == nil {
		t.Fatal("bad code accepted")
	}
	fake.mu.Lock()
	if len(fake.revoked) != 3 || fake.revoked[0] != "refresh-c1" {
		t.Fatalf("tokens not revoked: %v", fake.revoked)
	}
	fake.mu.Unlock()

	// Without auto-create only linked users get in.
	a.HA.AutoCreate = false
	fake.users["access-c3"] = haUser{ID: "ghi789", Name: "Gast"}
	if _, _, err := login("c3", true); err == nil || !strings.Contains(err.Error(), "linked") {
		t.Fatalf("unlinked user: %v", err)
	}
}
