package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/phoedox/phoedox/internal/auth"
	"github.com/phoedox/phoedox/internal/core"
	"github.com/phoedox/phoedox/internal/store"
)

func TestFeedbackWidgetEmbed(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, store.SQLite, filepath.Join(t.TempDir(), "w.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	au := &auth.Auth{DB: db}
	if _, err := au.CreateUser(ctx, "admin@coph.dev", "Admin", "sehr-geheimes-passwort", true); err != nil {
		t.Fatal(err)
	}
	web := fstest.MapFS{"index.html": {Data: []byte("<!doctype html>app")}}
	h := New(Deps{Core: core.New(db), Auth: au, Web: web})
	c := &client{t: t, h: h}
	if code, _ := c.do("POST", "/api/v1/auth/login", map[string]string{"email": "admin@coph.dev", "password": "sehr-geheimes-passwort"}, false); code != 200 {
		t.Fatal("login")
	}
	c.do("POST", "/api/v1/projects", map[string]string{"name": "Starfall"}, true)
	settings := core.DefaultPortalSettings()
	settings.Anonymous = true
	code, body := c.do("POST", "/api/v1/projects/starfall/feedback/portals", map[string]any{"handle": "starfall", "name": "Starfall", "settings": settings}, true)
	if code != 200 && code != 201 {
		t.Fatalf("portal: %d %v", code, body)
	}
	id := body["id"].(string)

	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		return rec
	}
	// The normal app may never be framed; the widget page only when switched on.
	if rec := get("/feedback/starfall"); rec.Header().Get("X-Frame-Options") != "DENY" {
		t.Fatal("portal page must not be frameable")
	}
	if rec := get("/feedback/starfall/embed"); rec.Code != http.StatusNotFound {
		t.Fatalf("widget off: %d", rec.Code)
	}
	settings.Widget = true
	settings.WidgetOrigins = []string{"ftp://bad"}
	if code, body := c.do("PUT", "/api/v1/projects/starfall/feedback/portals/"+id, map[string]any{"handle": "starfall", "name": "Starfall", "settings": settings}, true); code != 400 || body["key"] != "validation.feedback_widget_origin" {
		t.Fatalf("bad origin: %d %v", code, body)
	}
	settings.WidgetOrigins = []string{"https://Game.Example/", "https://*.example.org", "http://localhost:5173", "https://game.example"}
	if code, body := c.do("PUT", "/api/v1/projects/starfall/feedback/portals/"+id, map[string]any{"handle": "starfall", "name": "Starfall", "settings": settings}, true); code != 200 {
		t.Fatalf("save: %d %v", code, body)
	}
	rec := get("/feedback/starfall/embed")
	if rec.Code != 200 || rec.Body.String() != "<!doctype html>app" || rec.Header().Get("X-Frame-Options") != "" {
		t.Fatalf("embed: %d %q %v", rec.Code, rec.Body.String(), rec.Header())
	}
	if csp := rec.Header().Get("Content-Security-Policy"); csp != "frame-ancestors https://game.example https://*.example.org http://localhost:5173" {
		t.Fatalf("csp: %q", csp)
	}
	if code, body := c.do("GET", "/api/v1/feedback/starfall", nil, false); code != 200 || body["widget"] != true {
		t.Fatalf("public portal: %d %v", code, body)
	}
	settings.WidgetOrigins = nil
	c.do("PUT", "/api/v1/projects/starfall/feedback/portals/"+id, map[string]any{"handle": "starfall", "name": "Starfall", "settings": settings}, true)
	if csp := get("/feedback/starfall/embed").Header().Get("Content-Security-Policy"); csp != "frame-ancestors *" {
		t.Fatalf("any site: %q", csp)
	}
}
