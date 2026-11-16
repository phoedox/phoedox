package core

import (
	"context"
	"strings"
	"testing"
)

func TestFindAndReplace(t *testing.T) {
	ctx := context.Background()
	s, owner, _, _ := setup(t)
	str := func(s string) *string { return &s }
	locales := []string{"de"}
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Docs", AIWriteMode: "direct"})
	if _, err := s.UpdateProject(ctx, owner, p.Slug, ProjectInput{Name: p.Name, Short: p.Short, PrimaryColor: p.PrimaryColor, AIWriteMode: p.AIWriteMode, MainLocale: str("en"), Locales: &locales}); err != nil {
		t.Fatal(err)
	}
	a, _ := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "markdown", Title: "Intro Foo", Markdown: "Foo is here.\nAnother foo, and food."})
	s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "markdown", Title: "Other", Markdown: "No match"})
	s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "adr", Title: "Use Foo", ADR: &ADR{Status: "accepted", Context: "foo context", Decision: "We use foo."}})
	if _, err := s.SaveTranslation(ctx, owner, p.Slug, a.Document.Slug, "de", TranslationInput{Title: "Einleitung", Markdown: str("Foo ist hier.")}); err != nil {
		t.Fatal(err)
	}

	// Search: whole words, any case, in every document.
	r, err := s.FindInDocuments(ctx, owner, p.Slug, FindInput{Find: "foo", WholeWord: true})
	if err != nil || r.Total != 4 || len(r.Documents) != 2 || r.Scanned != 3 {
		t.Fatalf("find: %v %+v", err, r)
	}
	m := r.Documents[0].Matches[1]
	if m.Line != 2 || m.Before != "Another " || m.Text != "foo" || m.After != ", and food." {
		t.Fatalf("context: %+v", m)
	}
	// Preview with a regex and a group.
	r, _ = s.FindInDocuments(ctx, owner, p.Slug, FindInput{Find: `(?m)^(\w+) is`, Regex: true, Replace: str("$1 was"), Documents: []string{"intro-foo"}})
	if r.Total != 1 || r.Documents[0].Matches[0].Replacement != "Foo was" {
		t.Fatalf("preview: %+v", r)
	}
	if _, err := s.FindInDocuments(ctx, owner, p.Slug, FindInput{Find: "(", Regex: true}); invalidKey(err) != "validation.find_regex" {
		t.Fatalf("bad regex: %v", err)
	}
	if _, err := s.FindInDocuments(ctx, owner, p.Slug, FindInput{Find: "x*", Regex: true}); invalidKey(err) != "validation.find_matches_empty" {
		t.Fatalf("empty match: %v", err)
	}

	// Apply: only the second match on the page, all in the decision, titles too.
	r, err = s.FindInDocuments(ctx, owner, p.Slug, FindInput{Find: "foo", WholeWord: true, Titles: true, Replace: str("Bar"), Apply: true,
		Only: map[string][]int{a.Document.ID: {3}}, Versions: map[string]int{a.Document.ID: 1}})
	if err != nil || r.Replaced != 4 {
		t.Fatalf("apply: %v %+v", err, r)
	}
	d, _ := s.Document(ctx, owner, p.Slug, a.Document.ID)
	if !strings.Contains(string(d.Content), `Foo is here.\nAnother Bar, and food.`) || d.Title != "Intro Foo" || d.Version != 2 {
		t.Fatalf("page: %s %s v%d", d.Title, d.Content, d.Version)
	}
	adr, _ := s.Document(ctx, owner, p.Slug, "use-foo")
	if adr.Title != "Use Bar" || !strings.Contains(string(adr.Content), `"context":"Bar context"`) || !strings.Contains(string(adr.Content), `"status":"accepted"`) {
		t.Fatalf("decision: %s %s", adr.Title, adr.Content)
	}
	// A stale preview skips the document.
	r, _ = s.FindInDocuments(ctx, owner, p.Slug, FindInput{Find: "Foo", Replace: str("Baz"), Apply: true, Documents: []string{a.Document.ID}, Versions: map[string]int{a.Document.ID: 1}})
	if r.Replaced != 0 || r.Documents[0].Result != "skipped" {
		t.Fatalf("stale: %+v", r)
	}
	// Translations: only that language, the translation keeps its source version.
	r, err = s.FindInDocuments(ctx, owner, p.Slug, FindInput{Find: "hier", Replace: str("da"), Apply: true, Lang: "de"})
	if err != nil || r.Replaced != 1 {
		t.Fatalf("translation: %v %+v", err, r)
	}
	tr, _ := s.Translation(ctx, owner, p.Slug, a.Document.ID, "de")
	if !strings.Contains(string(tr.Content), "Foo ist da.") || tr.SourceVersion != 1 || !tr.Outdated {
		t.Fatalf("translation after replace: %s v%d %v", tr.Content, tr.SourceVersion, tr.Outdated)
	}
}

func TestMoveKeepsVersion(t *testing.T) {
	ctx := context.Background()
	s, owner, _, _ := setup(t)
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Move", AIWriteMode: "direct"})
	d, _ := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "markdown", Title: "Page", Markdown: "text", Category: "A"})
	cat, pos := "B/C", 4
	r, err := s.UpdateDocument(ctx, owner, p.Slug, d.Document.ID, DocumentPatch{Category: &cat, Position: &pos})
	if err != nil || r.Document.Version != 1 {
		t.Fatalf("move: %v %+v", err, r.Document)
	}
	got, _ := s.Document(ctx, owner, p.Slug, d.Document.ID)
	if got.Category != "B/C" || got.Position != 4 || got.Version != 1 || !got.UpdatedAt.Equal(d.Document.UpdatedAt.Time) {
		t.Fatalf("after move: %+v", got)
	}
	if vs, _ := s.Versions(ctx, owner, p.Slug, d.Document.ID); len(vs) != 1 {
		t.Fatalf("versions: %d", len(vs))
	}
}
