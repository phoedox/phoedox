package core

import (
	"context"
	"testing"
)

func TestDocumentRefs(t *testing.T) {
	ctx := context.Background()
	s, owner, _, _ := setup(t)
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Somaro"})
	imported, err := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "adr", Title: "D-321 · iOS project and Swift bridge", Slug: "swift-bridge", ADR: &ADR{Status: "accepted", Decision: "Swift"}})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "markdown", Title: "D-3210 · Something else", Markdown: "x"})
	_, _ = s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "markdown", Title: "Netz", Markdown: "x"})
	for ref, want := range map[string]string{
		"swift-bridge": imported.Document.ID, "SWIFT-BRIDGE": imported.Document.ID,
		"ADR-0001": imported.Document.ID, "D-321": imported.Document.ID, "d-321": imported.Document.ID,
	} {
		d, err := s.Document(ctx, owner, p.Slug, ref)
		if err != nil || d.ID != want {
			t.Errorf("%s: %v %s", ref, err, d.Title)
		}
	}
	if d, err := s.Document(ctx, owner, p.Slug, "D-3210"); err != nil || d.Title != "D-3210 · Something else" {
		t.Errorf("D-3210: %v %s", err, d.Title)
	}
	if _, err := s.Document(ctx, owner, p.Slug, "D-32"); err == nil {
		t.Error("a prefix of an identifier must not match")
	}
	if _, err := s.Document(ctx, owner, p.Slug, "50%_x"); err == nil {
		t.Error("LIKE wildcards must not match")
	}
}
