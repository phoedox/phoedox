package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/phoedox/phoedox/internal/config"
)

func TestHealthz(t *testing.T) {
	h := New(config.Config{}, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body["status"] != "ok" {
		t.Fatalf("unexpected body %q", rec.Body.String())
	}
}

func TestSPAFallback(t *testing.T) {
	web := fstest.MapFS{"index.html": {Data: []byte("<!doctype html>app")}, "assets/a.js": {Data: []byte("js")}}
	h := New(config.Config{}, web)
	for path, want := range map[string]string{"/p/lindenstrasse/doc/netz": "app", "/assets/a.js": "js"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), want) {
			t.Errorf("%s: status %d body %q", path, rec.Code, rec.Body.String())
		}
	}
}
