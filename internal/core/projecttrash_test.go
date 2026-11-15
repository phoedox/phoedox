package core

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/phoedox/phoedox/internal/store"
)

func TestDeleteAndRestoreProject(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, stranger := setup(t)
	s.TrashDays = 30
	p, err := s.CreateProject(ctx, owner, ProjectInput{Name: "Homelab"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetMember(ctx, owner, p.Slug, "reader@x.de", store.RoleReader); err != nil {
		t.Fatal(err)
	}
	link, err := s.CreateShareLink(ctx, owner, p.Slug, ShareLinkInput{Label: "caretaker"})
	if err != nil {
		t.Fatal(err)
	}

	// Only owners, never AI clients, and only with the address typed.
	if err := s.DeleteProject(ctx, reader, p.Slug, p.Slug); !errors.Is(err, ErrForbidden) {
		t.Fatalf("reader: %v", err)
	}
	if err := s.DeleteProject(ctx, Actor{User: owner.User, Via: "mcp"}, p.Slug, p.Slug); !errors.Is(err, ErrForbidden) {
		t.Fatalf("mcp: %v", err)
	}
	if err := s.DeleteProject(ctx, owner, p.Slug, "wrong"); err == nil {
		t.Fatal("wrong confirmation accepted")
	}
	if err := s.DeleteProject(ctx, owner, p.Slug, p.Slug); err != nil {
		t.Fatal(err)
	}

	// Gone for everyone; share links stop working.
	if _, err := s.Project(ctx, reader, p.Slug); !errors.Is(err, ErrNotFound) {
		t.Fatalf("reader still sees it: %v", err)
	}
	if ps, _ := s.Projects(ctx, owner); len(ps) != 0 {
		t.Fatalf("listed: %+v", ps)
	}
	if _, err := s.Shared(ctx, link.Token); err == nil {
		t.Fatal("share link still opens")
	}

	// The owner sees it in the restore list; readers and strangers do not.
	list, err := s.TrashedProjects(ctx, owner)
	if err != nil || len(list) != 1 || list[0].Slug != p.Slug || list[0].PurgeAt == nil || list[0].DeletedBy != owner.User.ID {
		t.Fatalf("trash: %+v %v", list, err)
	}
	if l, _ := s.TrashedProjects(ctx, reader); len(l) != 0 {
		t.Fatalf("reader trash: %+v", l)
	}
	if _, err := s.RestoreProject(ctx, stranger, p.Slug); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stranger restore: %v", err)
	}
	// The address stays reserved.
	if _, err := s.CreateProject(ctx, stranger, ProjectInput{Name: "Homelab", Slug: p.Slug}); err == nil {
		t.Fatal("slug of a deleted project reused")
	}

	r, err := s.RestoreProject(ctx, owner, p.Slug)
	if err != nil || r.ID != p.ID {
		t.Fatalf("restore: %+v %v", r, err)
	}
	if _, err := s.Project(ctx, reader, p.Slug); err != nil {
		t.Fatalf("reader after restore: %v", err)
	}
	if _, err := s.Shared(ctx, link.Token); err != nil {
		t.Fatalf("share link after restore: %v", err)
	}
	for _, kind := range []string{"project.delete", "project.restore"} {
		if ev, _ := s.DB.Events(ctx, store.EventFilter{Kind: kind, Limit: 5}); len(ev) != 1 || ev[0].Actor == "" {
			t.Fatalf("%s events: %+v", kind, ev)
		}
	}

	// After the trash period the project is purged.
	if err := s.DeleteProject(ctx, owner, p.Slug, p.Slug); err != nil {
		t.Fatal(err)
	}
	s.PurgeDeletedProjects(store.System(ctx), time.Now().Add(29*24*time.Hour))
	if l, _ := s.TrashedProjects(ctx, owner); len(l) != 1 {
		t.Fatalf("purged too early: %+v", l)
	}
	s.PurgeDeletedProjects(store.System(ctx), time.Now().Add(31*24*time.Hour))
	if l, _ := s.TrashedProjects(ctx, owner); len(l) != 0 {
		t.Fatalf("not purged: %+v", l)
	}
	if _, err := s.CreateProject(ctx, owner, ProjectInput{Name: "Homelab"}); err != nil {
		t.Fatalf("address free again: %v", err)
	}
}
