package server

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/phoedox/phoedox/internal/auth"
	"github.com/phoedox/phoedox/internal/blob"
	"github.com/phoedox/phoedox/internal/config"
	"github.com/phoedox/phoedox/internal/core"
	"github.com/phoedox/phoedox/internal/store"
)

func TestFileServing(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := store.Open(ctx, store.SQLite, filepath.Join(dir, "f.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	au := &auth.Auth{DB: db}
	_, _ = au.CreateUser(ctx, "admin@coph.dev", "Admin", "sehr-geheimes-passwort", true)
	svc := core.New(db)
	svc.Blobs, svc.TempDir = blob.Local{Dir: filepath.Join(dir, "files")}, filepath.Join(dir, "tmp")
	h := New(Deps{Core: svc, Auth: au})
	c := &client{t: t, h: h}
	c.do("POST", "/api/v1/auth/login", map[string]string{"email": "admin@coph.dev", "password": "sehr-geheimes-passwort"}, false)
	c.do("POST", "/api/v1/projects", map[string]string{"name": "Haus"}, true)

	upload := func(name, body string, csrf bool) (int, string) {
		r := httptest.NewRequest("POST", "/api/v1/projects/haus/files?name="+name, strings.NewReader(body))
		r.AddCookie(c.cookie)
		if csrf {
			r.Header.Set("X-Phoedox", "1")
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		return rec.Code, rec.Body.String()
	}
	if code, _ := upload("a.txt", "x", false); code != http.StatusForbidden {
		t.Fatalf("upload without CSRF header: %d", code)
	}
	code, body := upload("evil.svg", `<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`, true)
	if code != http.StatusCreated {
		t.Fatalf("upload: %d %s", code, body)
	}
	id := body[strings.Index(body, `"id":"`)+6:]
	id = id[:strings.Index(id, `"`)]

	get := func(path string, hdr ...string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", path, nil)
		if c.cookie != nil {
			r.AddCookie(c.cookie)
		}
		for i := 0; i+1 < len(hdr); i += 2 {
			r.Header.Set(hdr[i], hdr[i+1])
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		return rec
	}
	rec := get("/api/v1/files/" + id + "/evil.svg")
	hd := rec.Header()
	if rec.Code != 200 || hd.Get("X-Content-Type-Options") != "nosniff" || !strings.Contains(hd.Get("Content-Security-Policy"), "sandbox") ||
		!strings.HasPrefix(hd.Get("Content-Disposition"), "inline") || hd.Get("Content-Type") != "image/svg+xml" {
		t.Fatalf("svg headers: %d %v", rec.Code, hd)
	}
	if rec := get("/api/v1/files/"+id, "If-None-Match", hd.Get("ETag")); rec.Code != http.StatusNotModified {
		t.Fatalf("etag: %d", rec.Code)
	}
	if rec := get("/api/v1/files/" + id + "?download=1"); !strings.HasPrefix(rec.Header().Get("Content-Disposition"), "attachment; filename*=UTF-8''evil.svg") {
		t.Fatalf("download: %v", rec.Header())
	}
	_, html := upload("seite.html", "<!doctype html><p>hi", true)
	hid := html[strings.Index(html, `"id":"`)+6:]
	hid = hid[:strings.Index(hid, `"`)]
	if rec := get("/api/v1/files/" + hid); !strings.HasPrefix(rec.Header().Get("Content-Disposition"), "attachment") {
		t.Fatalf("html must be downloaded: %v", rec.Header())
	}
	if rec := get("/api/v1/files/"+id, "Range", "bytes=0-3"); rec.Code != http.StatusPartialContent || rec.Body.String() != "<svg" {
		t.Fatalf("range: %d %q", rec.Code, rec.Body.String())
	}
	c.cookie = nil
	if rec := get("/api/v1/files/" + id); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous: %d", rec.Code)
	}
}

func TestTus(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := store.Open(ctx, store.SQLite, filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	au := &auth.Auth{DB: db}
	_, _ = au.CreateUser(ctx, "admin@coph.dev", "Admin", "sehr-geheimes-passwort", true)
	svc := core.New(db)
	svc.Blobs, svc.TempDir = blob.Local{Dir: filepath.Join(dir, "files")}, filepath.Join(dir, "tmp")
	svc.MaxUpload = 10 << 20
	h := New(Deps{Core: svc, Auth: au, Config: config.Config{MaxUploadMB: 10}})
	c := &client{t: t, h: h}
	c.do("POST", "/api/v1/auth/login", map[string]string{"email": "admin@coph.dev", "password": "sehr-geheimes-passwort"}, false)
	c.do("POST", "/api/v1/projects", map[string]string{"name": "Haus"}, true)

	req := func(method, path, body string, hdr ...string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.AddCookie(c.cookie)
		r.Header.Set("X-Phoedox", "1")
		r.Header.Set("Tus-Resumable", "1.0.0")
		for i := 0; i+1 < len(hdr); i += 2 {
			r.Header.Set(hdr[i], hdr[i+1])
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		return rec
	}
	rec := req("POST", "/api/v1/projects/haus/uploads", "", "Upload-Length", "11", "Upload-Metadata", "name "+base64.StdEncoding.EncodeToString([]byte("Plan Keller.txt")))
	loc := rec.Header().Get("Location")
	if rec.Code != 201 || !strings.HasPrefix(loc, "/api/v1/uploads/") {
		t.Fatalf("create: %d %v %s", rec.Code, rec.Header(), rec.Body.String())
	}
	if rec := req("PATCH", loc, "hallo", "Content-Type", "application/offset+octet-stream", "Upload-Offset", "0"); rec.Code != 204 || rec.Header().Get("Upload-Offset") != "5" {
		t.Fatalf("patch 1: %d %v", rec.Code, rec.Header())
	}
	if rec := req("HEAD", loc, ""); rec.Code != 200 || rec.Header().Get("Upload-Offset") != "5" || rec.Header().Get("Upload-Length") != "11" {
		t.Fatalf("head: %d %v", rec.Code, rec.Header())
	}
	if rec := req("PATCH", loc, "xx", "Content-Type", "application/offset+octet-stream", "Upload-Offset", "2"); rec.Code != 409 {
		t.Fatalf("wrong offset: %d", rec.Code)
	}
	rec = req("PATCH", loc, " welt!", "Content-Type", "application/offset+octet-stream", "Upload-Offset", "5")
	id := rec.Header().Get("Phoedox-File-Id")
	if rec.Code != 204 || id == "" {
		t.Fatalf("finish: %d %v %s", rec.Code, rec.Header(), rec.Body.String())
	}
	if rec := req("GET", "/api/v1/files/"+id, ""); rec.Body.String() != "hallo welt!" || !strings.Contains(rec.Header().Get("Content-Disposition"), "Plan%20Keller.txt") {
		t.Fatalf("content: %q %v", rec.Body.String(), rec.Header())
	}
	if rec := req("POST", "/api/v1/projects/haus/uploads", "", "Upload-Length", "20000000"); rec.Code != 400 {
		t.Fatalf("too large: %d", rec.Code)
	}
}
