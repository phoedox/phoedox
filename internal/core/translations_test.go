package core

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/phoedox/phoedox/internal/store"
)

// TestTranslations covers ADR-0028: languages per project, one translation
// per language, outdated when the main language changes, AI marks.
func TestTranslations(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, _ := setup(t)
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Doku", AIWriteMode: "direct"})
	_ = s.SetMember(ctx, owner, p.Slug, "reader@x.de", store.RoleReader)
	res, _ := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "markdown", Title: "Overview", Markdown: "Hello @router"})
	slug := res.Document.Slug
	md := func(s string) *string { return &s }

	// Not multilingual yet: no translations.
	if _, err := s.SaveTranslation(ctx, owner, p.Slug, slug, "de", TranslationInput{Title: "Überblick", Markdown: md("Hallo")}); invalidKey(err) != "validation.locale_not_in_project" {
		t.Fatalf("no languages: %v", err)
	}
	full := ProjectInput{Name: p.Name, Short: p.Short, PrimaryColor: p.PrimaryColor, AIWriteMode: p.AIWriteMode}
	bad := full
	bad.Locales = &[]string{"de"}
	if _, err := s.UpdateProject(ctx, owner, p.Slug, bad); invalidKey(err) != "validation.main_locale_required" {
		t.Fatalf("further language without main: %v", err)
	}
	bad.MainLocale, bad.Locales = strp("english"), &[]string{"de"}
	if _, err := s.UpdateProject(ctx, owner, p.Slug, bad); invalidKey(err) != "validation.locale" {
		t.Fatalf("bad code: %v", err)
	}
	full.MainLocale, full.Locales = strp("en"), &[]string{"de", "en", "de"}
	if p, err := s.UpdateProject(ctx, owner, p.Slug, full); err != nil || p.MainLocale != "en" || len(p.Locales) != 1 {
		t.Fatalf("languages: %v %+v", err, p)
	}

	// Readers cannot translate; editors can.
	if _, err := s.SaveTranslation(ctx, reader, p.Slug, slug, "de", TranslationInput{Title: "Ü", Markdown: md("x")}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("reader: %v", err)
	}
	tr, err := s.SaveTranslation(ctx, owner, p.Slug, slug, "de", TranslationInput{Title: "Überblick", Markdown: md("Hallo @router")})
	if err != nil || tr.Machine || tr.SourceVersion != 1 {
		t.Fatalf("save: %v %+v", err, tr)
	}

	// Reading in German shows the translation; English and unknown languages the main text.
	d, _ := s.LocalizedDocument(ctx, reader, p.Slug, slug, "de")
	if d.Title != "Überblick" || markdownOf(d) != "Hallo @router" || d.Locale != "de" || d.Translation == nil || len(d.Translations) != 1 {
		t.Fatalf("de: %+v", d)
	}
	if d, _ := s.LocalizedDocument(ctx, reader, p.Slug, slug, "fr"); d.Title != "Overview" || d.Locale != "en" || d.Translation != nil {
		t.Fatalf("fr falls back: %+v", d)
	}

	// Changing the main language makes the translation outdated.
	if _, err := s.UpdateDocument(ctx, owner, p.Slug, slug, DocumentPatch{Markdown: strp("Hello again")}); err != nil {
		t.Fatal(err)
	}
	if ti, _ := s.Translation(ctx, reader, p.Slug, slug, "de"); !ti.Outdated || ti.MainVersion != 2 {
		t.Fatalf("outdated: %+v", ti)
	}

	// AI translations are marked; decisions keep number and status.
	ai := Actor{User: owner.User, Via: "mcp", TokenID: "t"}
	adr, _ := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "adr", Title: "Use Go", ADR: &ADR{Status: "accepted", Decision: "Go", Context: "Speed"}})
	ti, err := s.SaveTranslation(ctx, ai, p.Slug, adr.Document.Slug, "de", TranslationInput{Title: "Go nutzen", Decision: md("Go"), Context: md("Tempo")})
	if err != nil || !ti.Machine {
		t.Fatalf("ai: %v %+v", err, ti)
	}
	dd, _ := s.LocalizedDocument(ctx, reader, p.Slug, "ADR-0001", "de")
	var c ADR
	_ = json.Unmarshal(dd.Content, &c)
	if dd.Title != "Go nutzen" || c.Context != "Tempo" || c.Status != "accepted" || dd.Number != 1 {
		t.Fatalf("adr de: %+v %+v", dd, c)
	}
	if err := s.DeleteTranslation(ctx, owner, p.Slug, slug, "de"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Translation(ctx, owner, p.Slug, slug, "de"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("deleted: %v", err)
	}
}
