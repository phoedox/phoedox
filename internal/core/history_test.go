package core

import (
	"context"
	"errors"
	"testing"

	"github.com/phoedox/phoedox/internal/store"
)

func TestHistoryTrashFavorites(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, _ := setup(t)
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Haus", AIWriteMode: "direct"})
	_ = s.SetMember(ctx, owner, p.Slug, "reader@x.de", store.RoleReader)
	res, _ := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "markdown", Title: "Netz", Markdown: "eins"})
	slug := res.Document.Slug
	_, _ = s.UpdateDocument(ctx, owner, p.Slug, slug, DocumentPatch{Markdown: strp("zwei")})
	_, _ = s.UpdateDocument(ctx, owner, p.Slug, slug, DocumentPatch{Title: strp("Netzwerk"), Markdown: strp("drei")})

	// history with names, old version, restore as new version
	hist, err := s.VersionHistory(ctx, reader, p.Slug, slug)
	if err != nil || len(hist) != 3 || hist[0].ActorName != "owner@x.de" || hist[0].Version.Version != 3 {
		t.Fatalf("history: %v %+v", err, hist)
	}
	v1, err := s.DocumentVersion(ctx, reader, p.Slug, slug, 1)
	if err != nil || string(v1.Content) != `{"markdown":"eins"}` || v1.Title != "Netz" {
		t.Fatalf("version 1: %v %+v", err, v1)
	}
	if _, err := s.RestoreVersion(ctx, reader, p.Slug, slug, 1); !errors.Is(err, ErrForbidden) {
		t.Fatalf("reader restore: %v", err)
	}
	rr, err := s.RestoreVersion(ctx, owner, p.Slug, slug, 1)
	if err != nil || rr.Document.Version != 4 || rr.Document.Title != "Netz" || string(rr.Document.Content) != `{"markdown":"eins"}` {
		t.Fatalf("restore: %v %+v", err, rr.Document)
	}
	if len(mustSearch(t, s, owner, "eins")) != 1 || len(mustSearch(t, s, owner, "drei")) != 0 {
		t.Fatal("restore not indexed")
	}
	// AI restores become suggestions in suggest mode
	_, _ = s.UpdateProject(ctx, owner, p.Slug, ProjectInput{Name: "Haus", AIWriteMode: "suggest"})
	ai := Actor{User: owner.User, Via: "mcp", TokenID: "t"}
	if r, err := s.RestoreVersion(ctx, ai, p.Slug, slug, 2); err != nil || r.Suggestion == nil {
		t.Fatalf("AI restore: %v %+v", err, r)
	}

	// favorites: project ones need editor rights, personal ones not
	if err := s.SetFavorite(ctx, reader, p.Slug, slug, false, true); !errors.Is(err, ErrForbidden) {
		t.Fatalf("reader project favorite: %v", err)
	}
	if err := s.SetFavorite(ctx, reader, p.Slug, slug, true, true); err != nil {
		t.Fatal(err)
	}
	other, _ := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "markdown", Title: "Strom", Markdown: "x"})
	_ = s.SetFavorite(ctx, owner, p.Slug, slug, false, true)
	_ = s.SetFavorite(ctx, owner, p.Slug, other.Document.Slug, false, true)
	_ = s.SetFavorite(ctx, owner, p.Slug, other.Document.Slug, false, true) // twice is fine
	if err := s.ReorderFavorites(ctx, owner, p.Slug, false, []string{other.Document.ID, res.Document.ID}); err != nil {
		t.Fatal(err)
	}
	favs, _ := s.Favorites(ctx, reader, p.Slug)
	if len(favs) != 3 || favs[0].DocumentID != other.Document.ID || favs[0].Personal || !favs[2].Personal {
		t.Fatalf("favorites for reader: %+v", favs)
	}
	if f, _ := s.Favorites(ctx, owner, p.Slug); len(f) != 2 {
		t.Fatalf("owner must not see the reader's own favorites: %+v", f)
	}

	// trash: delete, list, slug reuse conflict, restore, purge
	_, _ = s.UpdateProject(ctx, owner, p.Slug, ProjectInput{Name: "Haus", AIWriteMode: "direct"})
	if _, err := s.DeleteDocument(ctx, owner, p.Slug, slug); err != nil {
		t.Fatal(err)
	}
	if f, _ := s.Favorites(ctx, owner, p.Slug); len(f) != 1 {
		t.Fatalf("trashed favorites must be hidden: %+v", f)
	}
	r1, _ := s.UpsertEntity(ctx, owner, p.Slug, EntityInput{Kind: "router", Name: "Router"})
	r2, _ := s.UpsertEntity(ctx, owner, p.Slug, EntityInput{Kind: "server", Name: "NAS"})
	_, _ = s.Link(ctx, owner, p.Slug, EdgeInput{From: "nas", Rel: "wired_to", To: "router"})
	_, _ = s.DeleteEntity(ctx, owner, p.Slug, "router")
	items, err := s.Trash(ctx, owner, p.Slug)
	if err != nil || len(items) != 2 {
		t.Fatalf("trash: %v %+v", err, items)
	}
	if _, err := s.Trash(ctx, reader, p.Slug); !errors.Is(err, ErrForbidden) {
		t.Fatalf("reader trash: %v", err)
	}
	// a new document took the slug: restoring needs a decision
	_, _ = s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "markdown", Title: "Netz", Slug: slug, Markdown: "neu"})
	if _, err := s.RestoreFromTrash(ctx, owner, p.Slug, res.Document.ID); invalidKey(err) != "validation.slug_taken" {
		t.Fatalf("slug conflict: %v", err)
	}
	if _, err := s.RestoreFromTrash(ctx, owner, p.Slug, r1.Entity.ID); err != nil {
		t.Fatal(err)
	}
	if d, _ := s.Entity(ctx, owner, p.Slug, "nas"); len(d.Edges) != 1 {
		t.Fatalf("edge must come back with the entity: %+v", d.Edges)
	}
	if len(mustSearch(t, s, owner, "router")) != 1 {
		t.Fatal("restored entity not indexed")
	}
	if err := s.PurgeFromTrash(ctx, Actor{User: reader.User, Via: "web"}, p.Slug, res.Document.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("reader purge: %v", err)
	}
	if err := s.PurgeFromTrash(ctx, owner, p.Slug, res.Document.ID); err != nil {
		t.Fatal(err)
	}
	if items, _ := s.Trash(ctx, owner, p.Slug); len(items) != 0 {
		t.Fatalf("trash after purge: %+v", items)
	}
	// expiry
	_, _ = s.DeleteEntity(ctx, owner, p.Slug, "nas")
	s.PurgeExpiredTrash(ctx, 0) // 0 keeps forever
	if items, _ := s.Trash(ctx, owner, p.Slug); len(items) != 1 {
		t.Fatal("days=0 must keep the trash")
	}
	if n, err := s.DB.PurgeBefore(ctx, store.Time{Time: store.Now().Time.Add(1e9)}); err != nil || n != 1 {
		t.Fatalf("purge before: %v %d", err, n)
	}
	_ = r2
}

func mustSearch(t *testing.T, s *Service, a Actor, q string) []store.Hit {
	t.Helper()
	hs, err := s.Search(context.Background(), a, q, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	return hs
}
