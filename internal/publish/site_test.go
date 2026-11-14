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

func TestSiteLLMsAndMembers(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, store.SQLite, filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := core.New(db)
	u, _ := db.CreateUser(ctx, store.User{ID: "u1", Email: "o@x.de", Name: "O"})
	a := core.Actor{User: u, Via: "web"}
	p, _ := svc.CreateProject(ctx, a, core.ProjectInput{Name: "Handbuch"})
	if _, err := svc.CreateDocument(ctx, a, p.Slug, core.DocumentInput{Type: "markdown", Title: "Start", Markdown: "Hallo **Welt**"}); err != nil {
		t.Fatal(err)
	}
	pub, err := svc.CreatePublication(ctx, a, core.PublicationInput{Title: "Docs", Description: "Alles zu X", Projects: []string{p.Slug}})
	if err != nil {
		t.Fatal(err)
	}
	site := &Site{Core: svc, Web: fstest.MapFS{"x": &fstest.MapFile{}}, AppURL: "https://app.example.org"}
	get := func(path string) *httptest.ResponseRecorder {
		row, _ := db.GetPublication(ctx, pub.Slug)
		w := httptest.NewRecorder()
		site.Serve(w, httptest.NewRequest(http.MethodGet, "/pub/"+pub.Slug+path, nil), row, "/pub/"+pub.Slug)
		return w
	}
	w := get("/llms.txt")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "# Docs") || !strings.Contains(w.Body.String(), "> Alles zu X") ||
		!strings.Contains(w.Body.String(), "- [Start](") || !strings.Contains(w.Body.String(), "/pub/"+pub.Slug+"/handbuch/start.md") {
		t.Fatalf("llms.txt: %d %s", w.Code, w.Body)
	}
	w = get("/handbuch/start.md")
	if w.Code != 200 || w.Body.String() != "# Start\n\nHallo **Welt**" || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/markdown") {
		t.Fatalf("markdown: %d %q", w.Code, w.Body)
	}

	// Members only: strangers go to the app's login and come back.
	if _, err := svc.UpdatePublication(ctx, a, pub.ID, core.PublicationInput{Title: "Docs", Projects: []string{p.Slug}, Access: "members"}); err != nil {
		t.Fatal(err)
	}
	w = get("/handbuch/start")
	if w.Code != http.StatusFound || w.Header().Get("Location") != "https://app.example.org/login?next=%2Fpub%2F"+pub.Slug+"%2Fhandbuch%2Fstart" {
		t.Fatalf("members redirect: %d %s", w.Code, w.Header().Get("Location"))
	}
	site.Member = func(*http.Request) bool { return true }
	if w = get("/handbuch/start.md"); w.Code != 200 || w.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("member reads: %d %v", w.Code, w.Header())
	}
}
