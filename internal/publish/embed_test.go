package publish

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/phoedox/phoedox/internal/core"
	"github.com/phoedox/phoedox/internal/store"
)

func TestEmbedAndPublicMCP(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, store.SQLite, filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := core.New(db)
	svc.HashPassword = func(p string) (string, error) { return "hash:" + p, nil }
	u, _ := db.CreateUser(ctx, store.User{ID: "u1", Email: "o@x.de", Name: "O"})
	a := core.Actor{User: u, Via: "web"}
	p, _ := svc.CreateProject(ctx, a, core.ProjectInput{Name: "Handbook"})
	_, _ = svc.CreateDocument(ctx, a, p.Slug, core.DocumentInput{Type: "markdown", Title: "Backup", Markdown: "Backups run nightly to the NAS."})
	pub, err := svc.CreatePublication(ctx, a, core.PublicationInput{Title: "Docs", Projects: []string{p.Slug}})
	if err != nil {
		t.Fatal(err)
	}
	site := &Site{Core: svc, Web: fstest.MapFS{"x": &fstest.MapFile{}}, AppURL: "https://app.example.org"}
	do := func(method, path, body string) *httptest.ResponseRecorder {
		row, _ := db.GetPublication(ctx, pub.Slug)
		req := httptest.NewRequest(method, "/pub/"+pub.Slug+path, strings.NewReader(body))
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json, text/event-stream")
		}
		w := httptest.NewRecorder()
		site.Serve(w, req, row, "/pub/"+pub.Slug)
		return w
	}

	w := do(http.MethodGet, "/_embed/handbook/backup", "")
	csp := w.Header().Get("Content-Security-Policy")
	if w.Code != 200 || !strings.Contains(csp, "frame-ancestors *") || !strings.Contains(w.Body.String(), "Backups run nightly") ||
		strings.Contains(w.Body.String(), `<header class="top">`) || !strings.Contains(w.Body.String(), `<base target="_top">`) {
		t.Fatalf("embed: %d %s %s", w.Code, csp, w.Body)
	}
	if w = do(http.MethodGet, "/handbook/backup", ""); !strings.Contains(w.Header().Get("Content-Security-Policy"), "frame-ancestors 'self'") {
		t.Fatalf("normal pages may not be framed elsewhere: %s", w.Header().Get("Content-Security-Policy"))
	}
	if w = do(http.MethodGet, "/_embed/status", ""); w.Code != 200 {
		t.Fatalf("status embed: %d", w.Code)
	}

	// MCP: list, search and read without an account.
	call := func(id int, method, params string) string {
		w := do(http.MethodPost, "/mcp", `{"jsonrpc":"2.0","id":`+string(rune('0'+id))+`,"method":"`+method+`","params":`+params+`}`)
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", method, w.Code, w.Body)
		}
		return w.Body.String()
	}
	if out := call(1, "initialize", `{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"1"}}`); !strings.Contains(out, "phoedox-publication") {
		t.Fatalf("initialize: %s", out)
	}
	if out := call(2, "tools/call", `{"name":"search","arguments":{"query":"nightly"}}`); !strings.Contains(out, "backup") {
		t.Fatalf("search: %s", out)
	}
	if out := call(3, "tools/call", `{"name":"get_page","arguments":{"project":"handbook","page":"backup"}}`); !strings.Contains(out, "Backups run nightly") {
		t.Fatalf("get_page: %s", out)
	}
	if out := call(4, "tools/call", `{"name":"get_page","arguments":{"project":"handbook","page":"secret"}}`); !strings.Contains(out, "isError") {
		t.Fatalf("unpublished page: %s", out)
	}

	// Password publications have neither.
	pw := "geheim-genug-1234"
	if _, err := svc.UpdatePublication(ctx, a, pub.ID, core.PublicationInput{Title: "Docs", Projects: []string{p.Slug}, Access: "password", Password: &pw}); err != nil {
		t.Fatal(err)
	}
	if w = do(http.MethodGet, "/_embed/handbook/backup", ""); w.Code != 404 {
		t.Fatalf("embed of a password publication: %d", w.Code)
	}
	if w = do(http.MethodPost, "/mcp", `{}`); w.Code != 404 {
		t.Fatalf("mcp of a password publication: %d", w.Code)
	}
}
