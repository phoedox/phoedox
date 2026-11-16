package core

import (
	"context"
	"errors"
	"testing"

	"github.com/phoedox/phoedox/internal/store"
)

func TestGlossary(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, _ := setup(t)
	main, locales := "en", []string{"de"}
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Spiel"})
	if _, err := s.UpdateProject(ctx, owner, p.Slug, ProjectInput{Name: "Spiel", MainLocale: &main, Locales: &locales}); err != nil {
		t.Fatal(err)
	}
	_ = s.SetMember(ctx, owner, p.Slug, reader.User.Email, store.RoleReader)
	if _, err := s.SetGlossary(ctx, reader, p.Slug, nil); !errors.Is(err, ErrForbidden) {
		t.Fatalf("readers must not change the glossary: %v", err)
	}
	if _, err := s.SetGlossary(ctx, owner, p.Slug, []GlossaryEntry{{Term: "Board"}, {Term: "board"}}); invalidKey(err) != "validation.glossary_term" {
		t.Fatalf("duplicate: %v", err)
	}
	g, err := s.SetGlossary(ctx, owner, p.Slug, []GlossaryEntry{
		{Term: "board", Translations: map[string]string{"de": "Board"}},
		{Term: "Phoedox", Keep: true},
		{Term: "e-mail", Translations: map[string]string{"de": "E-Mail"}, Avoid: map[string][]string{"": {"email"}, "de": {"Email", "Mail"}}},
		{Term: "decision", Translations: map[string]string{"de": "Entscheidung"}},
	})
	if err != nil || len(g) != 4 || g[0].Term != "board" {
		t.Fatalf("set: %v %+v", err, g)
	}
	res, _ := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "markdown", Title: "Notifications", Markdown: "Phoedox sends an email when a card on the board moves."})
	doc := res.Document.Slug
	ps, err := s.GlossaryCheck(ctx, reader, p.Slug, doc, "")
	if err != nil || len(ps) != 1 || ps[0].Kind != "avoid" || ps[0].Found != "email" {
		t.Fatalf("style: %v %+v", err, ps)
	}
	rel, _ := s.GlossaryForDocument(ctx, reader, p.Slug, doc)
	if len(rel) != 2 { // board, Phoedox (e-mail does not occur, "email" is a spelling to avoid)
		t.Fatalf("relevant: %+v", rel)
	}
	md := "Phödox schickt eine Mail, wenn eine Karte auf der Tafel wandert."
	if _, err := s.SaveTranslation(ctx, owner, p.Slug, doc, "de", TranslationInput{Title: "Benachrichtigungen", Markdown: &md}); err != nil {
		t.Fatal(err)
	}
	ps, _ = s.GlossaryCheck(ctx, owner, p.Slug, doc, "de")
	kinds := map[string]string{}
	for _, x := range ps {
		kinds[x.Kind+":"+x.Term] = x.Expected
	}
	if kinds["missing_term:board"] != "Board" || kinds["missing_term:Phoedox"] != "Phoedox" || kinds["avoid:e-mail"] != "E-Mail" || len(ps) != 3 {
		t.Fatalf("translation check: %+v", ps)
	}
}
