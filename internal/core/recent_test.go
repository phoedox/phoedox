package core

import (
	"context"
	"errors"
	"testing"

	"github.com/phoedox/phoedox/internal/store"
)

func TestRecentAndShortcuts(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, _ := setup(t)
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Haus"})
	_ = s.SetMember(ctx, owner, p.Slug, "reader@x.de", store.RoleReader)
	a, _ := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "markdown", Title: "Netz", Markdown: "x"})
	b, _ := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "markdown", Title: "Strom", Markdown: "y"})
	e, _ := s.UpsertEntity(ctx, owner, p.Slug, EntityInput{Kind: "router", Name: "Fritzbox"})

	for _, ref := range []string{a.Document.Slug, b.Document.ID, a.Document.ID} {
		if err := s.RecordView(ctx, reader, p.Slug, "document", ref); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.RecordView(ctx, reader, p.Slug, "entity", e.Entity.Slug); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordView(ctx, reader, p.Slug, "folder", "x"); invalidKey(err) != "validation.recent_kind" {
		t.Fatalf("kind: %v", err)
	}
	// AI clients leave no history.
	if err := s.RecordView(ctx, Actor{User: reader.User, Via: "mcp"}, p.Slug, "document", a.Document.Slug); err != nil {
		t.Fatal(err)
	}
	list, err := s.Recent(ctx, reader, 10)
	if err != nil || len(list) != 3 || list[0].Title != "Fritzbox" || list[1].Title != "Netz" || list[2].Title != "Strom" || list[1].ProjectSlug != p.Slug {
		t.Fatalf("recent: %v %+v", err, list)
	}
	// Losing access hides entries; deleted pages disappear.
	if _, err := s.DeleteDocument(ctx, owner, p.Slug, b.Document.Slug); err != nil {
		t.Fatal(err)
	}
	if list, _ = s.Recent(ctx, reader, 10); len(list) != 2 {
		t.Fatalf("after delete: %+v", list)
	}
	_ = s.RemoveMember(ctx, owner, p.Slug, mustUser(t, s, "reader@x.de"))
	if list, _ = s.Recent(ctx, reader, 10); len(list) != 0 {
		t.Fatalf("after removal: %+v", list)
	}
	if err := s.RecordView(ctx, reader, p.Slug, "document", a.Document.Slug); err == nil {
		t.Fatal("recorded without access")
	}
	if err := s.ClearRecent(ctx, reader, ""); err != nil {
		t.Fatal(err)
	}

	// Shortcuts: valid combinations only, no doubles, web only.
	if _, err := s.SaveShortcuts(ctx, owner, map[string]string{"palette": "Mod+K+X"}); invalidKey(err) != "validation.shortcut" {
		t.Fatalf("combo: %v", err)
	}
	if _, err := s.SaveShortcuts(ctx, owner, map[string]string{"palette": "Alt+P", "focus": "Alt+p"}); invalidKey(err) != "validation.shortcut_taken" {
		t.Fatalf("double: %v", err)
	}
	if _, err := s.SaveShortcuts(ctx, Actor{User: owner.User, Via: "mcp"}, map[string]string{}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("mcp: %v", err)
	}
	if _, err := s.SaveShortcuts(ctx, owner, map[string]string{"palette": "Alt+P", "focus": "Mod+Shift+F", "history": ""}); err != nil {
		t.Fatal(err)
	}
	m, _ := s.Shortcuts(ctx, owner)
	if m["palette"] != "Alt+P" || m["focus"] != "Mod+Shift+F" || m["history"] != "" || len(m) != 3 {
		t.Fatalf("shortcuts: %+v", m)
	}
}
