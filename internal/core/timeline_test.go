package core

import (
	"context"
	"testing"
)

func TestEntityTimeline(t *testing.T) {
	ctx := context.Background()
	s, owner, _, stranger := setup(t)
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Homelab"})
	if _, err := s.UpsertEntity(ctx, owner, p.Slug, EntityInput{Kind: "server", Name: "NAS", Slug: "nas", Attrs: map[string]any{"ip": "10.0.0.5"}}); err != nil {
		t.Fatal(err)
	}
	agent := Actor{User: owner.User, Via: "agent", TokenID: "ag1"}
	// An agent report that only refreshes volatile values is no news.
	_, _ = s.UpsertEntity(ctx, agent, p.Slug, EntityInput{Kind: "server", Name: "NAS", Slug: "nas", Attrs: map[string]any{"ip": "10.0.0.5", "last_seen": "1"}})
	_, _ = s.UpsertEntity(ctx, agent, p.Slug, EntityInput{Kind: "server", Name: "NAS", Slug: "nas", Attrs: map[string]any{"ip": "10.0.0.6", "os": "Debian 13", "last_seen": "2"}})
	doc, _ := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "markdown", Title: "Backups", Markdown: "x"})
	if _, err := s.AddComment(ctx, owner, p.Slug, doc.Document.Slug, CommentInput{Body: "Is @nas still on the old disks?"}); err != nil {
		t.Fatal(err)
	}
	_, _ = s.AddComment(ctx, owner, p.Slug, doc.Document.Slug, CommentInput{Body: "Unrelated @nas-2 note"})
	board, _ := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "board", Title: "Tasks"})
	if _, err := s.CreateTask(ctx, owner, p.Slug, board.Document.Slug, TaskInput{Title: "Replace disks", Description: "in @nas"}); err != nil {
		t.Fatal(err)
	}

	items, err := s.EntityTimeline(ctx, owner, p.Slug, "nas", 0)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]int{}
	for _, it := range items {
		kinds[it.Kind]++
		if it.Kind == "changed" && (len(it.Changed) != 2 || it.Changed[0] != "ip" || it.Changed[1] != "os" || it.Via != "agent") {
			t.Fatalf("change: %+v", it)
		}
	}
	if kinds["created"] != 1 || kinds["changed"] != 1 || kinds["comment"] != 1 || kinds["task"] != 1 || len(items) != 4 {
		t.Fatalf("timeline: %+v", items)
	}
	if items[0].Kind != "task" { // newest first
		t.Fatalf("order: %+v", items)
	}
	if _, err := s.EntityTimeline(ctx, stranger, p.Slug, "nas", 0); err == nil {
		t.Fatal("stranger reads the timeline")
	}
}
