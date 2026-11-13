package core

import (
	"context"
	"errors"
	"testing"

	"github.com/phoedox/phoedox/internal/store"
)

func TestLiveSaveAndReset(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, _ := setup(t)
	var resets []string
	s.OnContentChange = func(_ context.Context, id, by string) { resets = append(resets, id+":"+by) }
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Haus", AIWriteMode: "direct"})
	_ = s.SetMember(ctx, owner, p.Slug, "reader@x.de", store.RoleReader)
	_ = s.SetMember(ctx, owner, p.Slug, "stranger@x.de", store.RoleEditor)
	res, _ := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "markdown", Title: "Netz", Markdown: "eins"})
	slug := res.Document.Slug

	// only editors in the browser, only Markdown pages
	if _, err := s.LiveSave(ctx, reader, p.Slug, slug, "x"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("reader: %v", err)
	}
	if _, err := s.LiveSave(ctx, Actor{User: owner.User, Via: "token"}, p.Slug, slug, "x"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("token: %v", err)
	}
	adr, _ := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "adr", Title: "A", ADR: &ADR{Decision: "d"}})
	if _, err := s.LiveDocument(ctx, owner, p.Slug, adr.Document.Slug); !errors.Is(err, ErrForbidden) {
		t.Fatalf("adr live: %v", err)
	}

	// saves of one person merge into one version; another person starts a new one
	d1, err := s.LiveSave(ctx, owner, p.Slug, slug, "eins zwei")
	if err != nil || d1.Version != 2 {
		t.Fatalf("first live save: %v v%d", err, d1.Version)
	}
	d2, _ := s.LiveSave(ctx, owner, p.Slug, slug, "eins zwei drei")
	if d2.Version != 2 {
		t.Fatalf("merged save must keep the version: v%d", d2.Version)
	}
	if v, _ := s.DocumentVersion(ctx, owner, p.Slug, slug, 2); string(v.Content) != `{"markdown":"eins zwei drei\n"}` {
		t.Fatalf("merged version content: %s", v.Content)
	}
	other := Actor{User: store.User{ID: mustUser(t, s, "stranger@x.de"), Name: "Stranger"}, Via: "web"}
	d3, err := s.LiveSave(ctx, other, p.Slug, slug, "eins zwei drei vier")
	if err != nil || d3.Version != 3 {
		t.Fatalf("other editor: %v v%d", err, d3.Version)
	}
	if d4, _ := s.LiveSave(ctx, other, p.Slug, slug, "eins zwei drei vier\n\n"); d4.Version != 3 {
		t.Fatal("unchanged content must not save")
	}
	// the owner saves the same shared state: credited as co-author
	_, _ = s.LiveSave(ctx, owner, p.Slug, slug, "eins zwei drei vier")
	if h, _ := s.VersionHistory(ctx, owner, p.Slug, slug); len(h[0].Coauthors) != 1 || h[0].Coauthors[0] != "owner@x.de" || h[0].ActorName != "Stranger" && h[0].ActorName != "stranger@x.de" {
		t.Fatalf("co-author: %+v", h[0])
	}
	if len(mustSearch(t, s, owner, "vier")) != 1 {
		t.Fatal("live save not indexed")
	}
	if len(resets) != 0 {
		t.Fatalf("live saves must not reset the session: %v", resets)
	}

	// changes outside the live editor reset it; title-only changes do not
	_, _ = s.UpdateDocument(ctx, owner, p.Slug, slug, DocumentPatch{Title: strp("Netzwerk")})
	if len(resets) != 0 {
		t.Fatalf("title change reset: %v", resets)
	}
	_, _ = s.UpdateDocument(ctx, Actor{User: owner.User, Via: "mcp", TokenID: "t"}, p.Slug, slug, DocumentPatch{Markdown: strp("von der KI")})
	if len(resets) != 1 || resets[0] != res.Document.ID+":owner@x.de" {
		t.Fatalf("MCP change must reset: %v", resets)
	}
	_, _ = s.DeleteDocument(ctx, owner, p.Slug, slug)
	if len(resets) != 2 {
		t.Fatalf("delete must reset: %v", resets)
	}
}

func mustUser(t *testing.T, s *Service, email string) string {
	t.Helper()
	u, err := s.DB.UserByEmail(context.Background(), email)
	if err != nil {
		t.Fatal(err)
	}
	return u.ID
}
