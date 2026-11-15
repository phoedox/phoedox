package core

import (
	"context"
	"strings"
	"testing"
)

func TestAnswer(t *testing.T) {
	ctx := context.Background()
	s, owner, _, reader := setup(t)
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Haus"})
	_ = s.SetMember(ctx, owner, p.Slug, reader.User.Email, "reader")
	keller, _ := s.UpsertEntity(ctx, owner, p.Slug, EntityInput{Kind: "room", Name: "Keller"})
	nas, err := s.UpsertEntity(ctx, owner, p.Slug, EntityInput{Kind: "server", Name: "NAS", Attrs: map[string]any{"ip": "192.168.1.20", "hostname": "nas"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Link(ctx, owner, p.Slug, EdgeInput{From: nas.Entity.Slug, Rel: "located_in", To: keller.Entity.Slug}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "markdown", Title: "Notfall", Markdown: "Der Hauptschalter für den Strom ist im Flur hinter der Tür."}); err != nil {
		t.Fatal(err)
	}
	for q, want := range map[string]string{
		"Wo steht das NAS?":              "NAS ist hier: Keller.",
		"Welche IP hat das NAS?":         "IP-Adresse von NAS: 192.168.1.20.",
		"Where is the NAS?":              "NAS is here: Keller.",
		"Hauptschalter":                  "Laut der Seite „Notfall“",
		"Wie spät ist es auf dem Mond?": "nichts gefunden",
	} {
		lang := "de"
		if strings.HasPrefix(q, "Where") {
			lang = "en"
		}
		r, err := s.Answer(ctx, reader, p.Slug, q, lang)
		if err != nil || !strings.Contains(r.Answer, want) {
			t.Errorf("%q: %+v %v (want %q)", q, r, err, want)
		}
	}
}
