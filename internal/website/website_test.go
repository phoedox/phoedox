package website

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func websiteFor(t *testing.T) (*Website, http.Handler) {
	t.Helper()
	dir := t.TempDir()
	s := &Website{Dir: dir, Web: fstest.MapFS{
		"index.html":    {Data: []byte("english")},
		"de/index.html": {Data: []byte("deutsch")},
	}}
	return s, s
}

func get(h http.Handler, url string, hdr map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, url, nil)
	r.Host = "phoedox.com"
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestWebsiteLanguages(t *testing.T) {
	_, h := websiteFor(t)
	if w := get(h, "/", nil); w.Body.String() != "english" {
		t.Fatalf("default should be English, got %d %q", w.Code, w.Body.String())
	}
	if w := get(h, "/", map[string]string{"Accept-Language": "de-DE,de;q=0.9,en;q=0.8"}); w.Code != http.StatusFound || w.Header().Get("Location") != "/de/" {
		t.Fatalf("German browsers go to /de/, got %d %q", w.Code, w.Header().Get("Location"))
	}
	w := get(h, "/?lang=en", map[string]string{"Accept-Language": "de"})
	if w.Body.String() != "english" || len(w.Result().Cookies()) != 1 || w.Result().Cookies()[0].Value != "en" {
		t.Fatalf("?lang=en should serve English and remember it, got %q %v", w.Body.String(), w.Result().Cookies())
	}
	if w := get(h, "/", map[string]string{"Accept-Language": "de", "Cookie": "phx_lang=en"}); w.Body.String() != "english" {
		t.Fatalf("the cookie wins over the browser, got %d", w.Code)
	}
	if w := get(h, "/de/", nil); w.Body.String() != "deutsch" {
		t.Fatalf("/de/ serves German, got %q", w.Body.String())
	}
	if w := get(h, "/healthz", nil); w.Body.String() != "ok" {
		t.Fatal("healthz")
	}
}

func TestWebsiteOperatorPages(t *testing.T) {
	s, h := websiteFor(t)
	for _, p := range []string{"/impressum", "/datenschutz", "/kontakt"} {
		if w := get(h, p, nil); w.Code != http.StatusNotFound {
			t.Fatalf("%s without a text must be 404, got %d", p, w.Code)
		}
	}
	_ = os.WriteFile(filepath.Join(s.Dir, "contact.txt"), []byte("hallo@example.com\n"), 0o600)
	if w := get(h, "/kontakt", nil); w.Code != http.StatusFound || w.Header().Get("Location") != "mailto:hallo@example.com" {
		t.Fatalf("contact redirect, got %d %q", w.Code, w.Header().Get("Location"))
	}
	_ = os.WriteFile(filepath.Join(s.Dir, "contact.txt"), []byte("a@b.c?subject=x"), 0o600)
	if w := get(h, "/kontakt", nil); w.Code != http.StatusNotFound {
		t.Fatalf("odd addresses are refused, got %d", w.Code)
	}
	_ = os.WriteFile(filepath.Join(s.Dir, "impressum.md"), []byte("# Impressum\n\nMax"), 0o600)
	if w := get(h, "/impressum", nil); w.Code != http.StatusOK {
		t.Fatalf("impressum, got %d", w.Code)
	}
}
