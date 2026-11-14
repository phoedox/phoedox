package core

import (
	"context"
	"testing"
	"time"

	"github.com/phoedox/phoedox/internal/store"
)

func TestBoardSearchMentionsFeed(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, stranger := setup(t)
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Haus", AIWriteMode: "direct"})
	_ = s.SetMember(ctx, owner, p.Slug, "reader@x.de", store.RoleEditor)
	board, _ := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: store.DocBoard, Title: "Umbau", Board: &Board{}})
	nas, _ := s.UpsertEntity(ctx, owner, p.ID, EntityInput{Kind: "server", Name: "NAS"})

	feed, stop, err := s.WatchBoard(ctx, reader, p.Slug, board.Document.Slug)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	if _, _, err := s.WatchBoard(ctx, stranger, p.Slug, board.Document.Slug); err == nil {
		t.Fatal("stranger watches the board")
	}

	task, err := s.CreateTask(ctx, owner, p.Slug, board.Document.Slug, TaskInput{Title: "Festplatte tauschen", Description: "im @" + nas.Entity.Slug, Assignee: "reader@x.de", Due: "2026-12-01"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case c := <-feed:
		if c.Kind != "task.create" || c.Task != task.ID || c.Board != board.Document.ID {
			t.Fatalf("change: %+v", c)
		}
	case <-time.After(time.Second):
		t.Fatal("no change on the feed")
	}

	// the card's mention links the board to the entry
	docs, _ := s.DB.DocumentsMentioning(ctx, nas.Entity.ID)
	if len(docs) != 1 || docs[0].ID != board.Document.ID {
		t.Fatalf("mentions: %+v", docs)
	}
	// cards are found by search, with the board as slug
	hits, err := s.Search(ctx, reader, "festplatte", p.Slug, 10)
	if err != nil || len(hits) != 1 || hits[0].Type != "task" || hits[0].ID != task.ID || hits[0].Slug != board.Document.Slug || hits[0].Kind != "task" {
		t.Fatalf("search: %v %+v", err, hits)
	}
	// my tasks across projects
	mine, err := s.MyTasks(ctx, reader)
	if err != nil || len(mine) != 1 || mine[0].ProjectSlug != p.Slug || mine[0].Title != "Festplatte tauschen" {
		t.Fatalf("my tasks: %v %+v", err, mine)
	}
	if mine, _ := s.MyTasks(ctx, owner); len(mine) != 0 {
		t.Fatalf("owner has no tasks: %+v", mine)
	}

	// deleting the card removes it from search and the mention
	if err := s.DeleteTask(ctx, owner, p.Slug, task.ID); err != nil {
		t.Fatal(err)
	}
	if hits, _ := s.Search(ctx, reader, "festplatte", p.Slug, 10); len(hits) != 0 {
		t.Fatalf("deleted card found: %+v", hits)
	}
	if docs, _ := s.DB.DocumentsMentioning(ctx, nas.Entity.ID); len(docs) != 0 {
		t.Fatalf("mention kept: %+v", docs)
	}
	// a trashed board hides its cards from search
	t2, _ := s.CreateTask(ctx, owner, p.Slug, board.Document.Slug, TaskInput{Title: "Kabel verlegen"})
	_, _ = s.DeleteDocument(ctx, owner, p.Slug, board.Document.Slug)
	if hits, _ := s.Search(ctx, owner, "kabel", p.Slug, 10); len(hits) != 0 {
		t.Fatalf("card of trashed board found: %+v %s", hits, t2.ID)
	}
}
