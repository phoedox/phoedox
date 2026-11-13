package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/phoedox/phoedox/internal/auth"
	"github.com/phoedox/phoedox/internal/config"
	"github.com/phoedox/phoedox/internal/core"
	"github.com/phoedox/phoedox/internal/mail"
	"github.com/phoedox/phoedox/internal/store"
)

func TestHealthz(t *testing.T) {
	h := New(Deps{})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"ok"`) {
		t.Fatalf("status %d body %q", rec.Code, rec.Body.String())
	}
}

func TestSPAFallback(t *testing.T) {
	web := fstest.MapFS{"index.html": {Data: []byte("<!doctype html>app")}, "assets/a.js": {Data: []byte("js")}}
	h := New(Deps{Web: web})
	for path, want := range map[string]string{"/p/lindenstrasse/doc/netz": "app", "/assets/a.js": "js"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), want) {
			t.Errorf("%s: status %d body %q", path, rec.Code, rec.Body.String())
		}
	}
}

type client struct {
	t      *testing.T
	h      http.Handler
	cookie *http.Cookie
	bearer string
}

func (c *client) do(method, path string, body any, csrf bool) (int, map[string]any) {
	c.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	r := httptest.NewRequest(method, path, &buf)
	if c.cookie != nil {
		r.AddCookie(c.cookie)
	}
	if c.bearer != "" {
		r.Header.Set("Authorization", "Bearer "+c.bearer)
	}
	if csrf {
		r.Header.Set("X-Phoedox", "1")
	}
	rec := httptest.NewRecorder()
	c.h.ServeHTTP(rec, r)
	for _, ck := range rec.Result().Cookies() {
		if ck.Name == auth.CookieName {
			c.cookie = ck
		}
	}
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func TestAPIFlow(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, store.SQLite, filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	au := &auth.Auth{DB: db}
	if _, err := au.CreateUser(ctx, "admin@coph.dev", "Admin", "sehr-geheimes-passwort", true); err != nil {
		t.Fatal(err)
	}
	h := New(Deps{Core: core.New(db), Auth: au})
	c := &client{t: t, h: h}

	if code, _ := c.do("GET", "/api/v1/projects", nil, false); code != 401 {
		t.Fatalf("unauthenticated: %d", code)
	}
	if code, _ := c.do("POST", "/api/v1/auth/login", map[string]string{"email": "admin@coph.dev", "password": "falsch-falsch-falsch"}, false); code != 401 {
		t.Fatalf("bad login: %d", code)
	}
	if code, _ := c.do("POST", "/api/v1/auth/login", map[string]string{"email": "admin@coph.dev", "password": "sehr-geheimes-passwort"}, false); code != 200 || c.cookie == nil {
		t.Fatalf("login: %d", code)
	}
	if code, body := c.do("POST", "/api/v1/projects", map[string]string{"name": "Phoedox"}, false); code != 403 || body["code"] != "csrf" {
		t.Fatalf("cookie write without X-Phoedox must fail: %d %v", code, body)
	}
	if code, body := c.do("POST", "/api/v1/projects", map[string]string{"name": "Phoedox", "aiWriteMode": "direct"}, true); code != 201 || body["slug"] != "phoedox" {
		t.Fatalf("create project: %d %v", code, body)
	}
	code, body := c.do("POST", "/api/v1/projects/phoedox/documents", map[string]any{"type": "adr", "title": "Lizenz", "category": "Entscheidungen",
		"adr": map[string]string{"status": "accepted", "context": "Open Source", "decision": "AGPL-3.0"}}, true)
	if code != 201 || body["applied"] != true {
		t.Fatalf("create adr: %d %v", code, body)
	}
	if code, body := c.do("GET", "/api/v1/projects/phoedox/documents/lizenz", nil, false); code != 200 || body["type"] != "adr" {
		t.Fatalf("get adr: %d %v", code, body)
	}

	// token for scripts / MCP
	code, body = c.do("POST", "/api/v1/tokens", map[string]string{"name": "Claude"}, true)
	if code != 201 {
		t.Fatalf("token: %d %v", code, body)
	}
	tc := &client{t: t, h: h, bearer: body["secret"].(string)}
	if code, body := tc.do("GET", "/api/v1/search?q=agpl", nil, false); code != 200 || body != nil {
		// search returns a JSON array; body map is nil when decoding an array
		t.Fatalf("search with token: %d %v", code, body)
	}
	if code, _ := tc.do("POST", "/api/v1/projects/phoedox/entities", map[string]any{"kind": "service", "name": "Go-Server"}, false); code != 200 {
		t.Fatalf("token write without csrf header must work: %d", code)
	}
	if code, _ := c.do("GET", "/api/v1/projects/unbekannt", nil, false); code != 404 {
		t.Fatalf("unknown project: %d", code)
	}
}

func TestAccountAPI(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, store.SQLite, filepath.Join(t.TempDir(), "acc.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	box := &mail.Capture{}
	au := &auth.Auth{DB: db, Mail: box}
	if _, err := au.CreateUser(ctx, "admin@coph.dev", "Admin", "sehr-geheimes-passwort", true); err != nil {
		t.Fatal(err)
	}
	h := New(Deps{Config: config.Config{PublicURL: "https://docs.example.org"}, Core: core.New(db), Auth: au})
	anon := &client{t: t, h: h}
	if code, body := anon.do("GET", "/api/v1/instance", nil, false); code != 200 || body["passwordReset"] != true {
		t.Fatalf("instance: %d %v", code, body)
	}
	if code, _ := anon.do("POST", "/api/v1/auth/reset-request", map[string]string{"email": "x@y.de"}, false); code != 403 {
		t.Fatalf("public POST without header: %d", code)
	}
	if code, _ := anon.do("POST", "/api/v1/auth/reset-request", map[string]string{"email": "nobody@y.de"}, true); code != 204 {
		t.Fatalf("unknown reset: %d", code)
	}
	if code, body := anon.do("GET", "/api/v1/auth/links/nope", nil, false); code != 400 || body["key"] != "validation.link_invalid" {
		t.Fatalf("bad link: %d %v", code, body)
	}

	admin := &client{t: t, h: h}
	admin.do("POST", "/api/v1/auth/login", map[string]string{"email": "admin@coph.dev", "password": "sehr-geheimes-passwort"}, true)
	code, body := admin.do("POST", "/api/v1/admin/users", map[string]any{"email": "jan@example.org", "name": "Jan"}, true)
	link, _ := body["link"].(string)
	if code != 201 || body["mailed"] != true || !strings.HasPrefix(link, "https://docs.example.org/invite/") {
		t.Fatalf("invite: %d %v", code, body)
	}
	tok := link[strings.LastIndex(link, "/")+1:]
	jan := &client{t: t, h: h}
	if code, body := jan.do("GET", "/api/v1/auth/links/"+tok, nil, false); code != 200 || body["email"] != "jan@example.org" {
		t.Fatalf("link info: %d %v", code, body)
	}
	if code, body := jan.do("POST", "/api/v1/auth/links/"+tok, map[string]string{"password": "jans-langes-passwort"}, true); code != 200 || jan.cookie == nil || body["hasPassword"] != true {
		t.Fatalf("complete: %d %v", code, body)
	}
	if code, _ := jan.do("GET", "/api/v1/admin/users", nil, false); code != 403 {
		t.Fatalf("non-admin list: %d", code)
	}
	if code, body := jan.do("PATCH", "/api/v1/me", map[string]string{"locale": "en"}, true); code != 200 || body["locale"] != "en" {
		t.Fatalf("profile: %d %v", code, body)
	}
	if code, body := jan.do("POST", "/api/v1/me/password", map[string]string{"current": "x", "new": "y-y-y-y-y-y-y"}, true); code != 400 || body["key"] != "validation.password_wrong" {
		t.Fatalf("password: %d %v", code, body)
	}
	if code, body := admin.do("GET", "/api/v1/admin/users", nil, false); code != 200 || body != nil {
		t.Fatalf("list: %d", code) // array body decodes to nil map
	}
}
