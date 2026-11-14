package core

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/phoedox/phoedox/internal/store"
)

func TestWatchProject(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, stranger := setup(t)
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Haus"})
	_ = s.SetMember(ctx, owner, p.Slug, reader.User.Email, store.RoleReader)
	if _, _, err := s.WatchProject(ctx, stranger, p.Slug); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stranger watches: %v", err)
	}
	ch, stop, err := s.WatchProject(ctx, reader, p.Slug)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	res, err := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "markdown", Title: "Postgres", Markdown: "x"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case c := <-ch:
		if c.Kind != "document.create" || c.Subject != res.Document.ID || c.Actor != owner.User.ID {
			t.Fatalf("change: %+v", c)
		}
	case <-time.After(time.Second):
		t.Fatal("no change")
	}
	// Other projects stay quiet.
	other, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Labor"})
	_, _ = s.CreateDocument(ctx, owner, other.Slug, DocumentInput{Type: "markdown", Title: "X"})
	select {
	case c := <-ch:
		t.Fatalf("change of another project: %+v", c)
	case <-time.After(50 * time.Millisecond):
	}
}
