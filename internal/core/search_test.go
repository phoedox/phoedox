package core

import (
	"context"
	"strings"
	"testing"

	"github.com/phoedox/phoedox/internal/store"
)

func TestSearchFollowsWrites(t *testing.T) {
	ctx := context.Background()
	s, owner, _, stranger := setup(t)
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Haus"})
	find := func(a Actor, q string) []store.Hit {
		t.Helper()
		hs, err := s.Search(ctx, a, q, "", 10)
		if err != nil {
			t.Fatal(err)
		}
		return hs
	}
	res, err := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "markdown", Title: "Netzwerk", Markdown: "## Aufbau\n\nDer **Hauptrouter** steht im [Keller](https://example.org)."})
	if err != nil {
		t.Fatal(err)
	}
	hs := find(owner, "hauptrouter keller")
	if len(hs) != 1 || strings.Contains(hs[0].Snippet, "**") || strings.Contains(hs[0].Snippet, "example.org") {
		t.Fatalf("markdown indexed as plain text: %+v", hs)
	}
	if len(find(stranger, "hauptrouter")) != 0 {
		t.Fatal("stranger must not find other projects")
	}
	// update replaces the text
	if _, err := s.UpdateDocument(ctx, owner, p.Slug, res.Document.Slug, DocumentPatch{Markdown: strp("Jetzt im Flur.")}); err != nil {
		t.Fatal(err)
	}
	if len(find(owner, "keller")) != 0 || len(find(owner, "flur")) != 1 {
		t.Fatal("update not indexed")
	}
	// decisions are found by number, entities by attributes
	if _, err := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "adr", Title: "Datenbank", ADR: &ADR{Decision: "Postgres"}}); err != nil {
		t.Fatal(err)
	}
	if hs := find(owner, "ADR-0001"); len(hs) != 1 || hs[0].Kind != "adr" {
		t.Fatalf("decision number: %+v", hs)
	}
	if _, err := s.UpsertEntity(ctx, owner, p.Slug, EntityInput{Kind: "router", Name: "Fritz!Box", Attrs: map[string]any{"ip": "192.168.178.1"}}); err != nil {
		t.Fatal(err)
	}
	if hs := find(owner, "192.168.178.1"); len(hs) != 1 || hs[0].Type != "entity" {
		t.Fatalf("entity attrs: %+v", hs)
	}
	// delete removes it
	if _, err := s.DeleteDocument(ctx, owner, p.Slug, res.Document.Slug); err != nil {
		t.Fatal(err)
	}
	if len(find(owner, "flur")) != 0 {
		t.Fatal("deleted document still found")
	}
	// reindex rebuilds everything
	if n, err := s.Reindex(ctx); err != nil || n != 2 {
		t.Fatalf("reindex: %v %d", err, n)
	}
	// accepted AI suggestions are indexed too
	_, _ = s.UpdateProject(ctx, owner, p.Slug, ProjectInput{Name: "Haus", AIWriteMode: "suggest"})
	ai := Actor{User: owner.User, Via: "mcp", TokenID: "t"}
	sug, _ := s.CreateDocument(ctx, ai, p.Slug, DocumentInput{Type: "markdown", Title: "Wasser", Markdown: "Haupthahn im Keller"})
	if len(find(owner, "haupthahn")) != 0 {
		t.Fatal("pending suggestion must not be found")
	}
	if _, err := s.DecideSuggestion(ctx, owner, sug.Suggestion.ID, true); err != nil {
		t.Fatal(err)
	}
	if len(find(owner, "haupthahn")) != 1 {
		t.Fatal("accepted suggestion not indexed")
	}
}
