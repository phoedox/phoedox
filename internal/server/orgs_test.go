package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/phoedox/phoedox/internal/auth"
	"github.com/phoedox/phoedox/internal/core"
	"github.com/phoedox/phoedox/internal/store"
)

func TestOrgsAndShareLinks(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, store.SQLite, filepath.Join(t.TempDir(), "o.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	au := &auth.Auth{DB: db}
	_, _ = au.CreateUser(ctx, "admin@coph.dev", "Admin", "sehr-geheimes-passwort", true)
	_, _ = au.CreateUser(ctx, "jana@coph.dev", "Jana", "sehr-geheimes-passwort", false)
	h := New(Deps{Core: core.New(db), Auth: au})
	c := &client{t: t, h: h}
	c.do("POST", "/api/v1/auth/login", map[string]string{"email": "admin@coph.dev", "password": "sehr-geheimes-passwort"}, false)

	if code, body := c.do("POST", "/api/v1/orgs", map[string]string{"name": "Firma"}, true); code != 201 || body["slug"] != "firma" {
		t.Fatalf("create org: %d %v", code, body)
	}
	if code, _ := c.do("PUT", "/api/v1/orgs/firma/members", map[string]string{"email": "jana@coph.dev", "role": "member"}, true); code != 200 {
		t.Fatalf("org member: %d", code)
	}
	if code, body := c.do("POST", "/api/v1/projects", map[string]any{"name": "Netz", "org": "firma"}, true); code != 200 && code != 201 || body["orgName"] != "Firma" {
		t.Fatalf("project in org: %d %v", code, body)
	}
	if code, _ := c.do("PUT", "/api/v1/projects/netz/members", map[string]string{"email": "jana@coph.dev", "role": "reader", "until": "2099-01-31"}, true); code != 204 {
		t.Fatalf("guest: %d", code)
	}
	c.do("POST", "/api/v1/projects/netz/documents", map[string]any{"type": "markdown", "title": "Anleitung", "markdown": "Hallo"}, true)
	code, body := c.do("POST", "/api/v1/projects/netz/share-links", map[string]any{"days": 7}, true)
	token, _ := body["token"].(string)
	if code != 201 || !strings.HasPrefix(token, "phxs_") {
		t.Fatalf("share link: %d %v", code, body)
	}
	anon := &client{t: t, h: h}
	if code, body := anon.do("GET", "/api/v1/shared/"+token, nil, false); code != 200 || !strings.Contains(strings.ToLower(toJSON(body)), "anleitung") {
		t.Fatalf("shared view: %d %v", code, body)
	}
	r := httptest.NewRequest("GET", "/api/v1/shared/"+token+"/documents/anleitung", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != 200 || rec.Header().Get("X-Robots-Tag") != "noindex" || !strings.Contains(rec.Body.String(), "Hallo") {
		t.Fatalf("shared doc: %d %v", rec.Code, rec.Header())
	}
	if code, _ := anon.do("GET", "/api/v1/shared/phxs_nope", nil, false); code != 404 {
		t.Fatalf("bad token: %d", code)
	}
}

func toJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
