package core

import (
	"context"
	"testing"

	"github.com/phoedox/phoedox/internal/store"
)

func TestTaskDocuments(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, _ := setup(t)
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Karten"})
	_ = s.SetMember(ctx, owner, p.Slug, reader.User.Email, store.RoleReader)
	page, _ := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "markdown", Title: "Konzept", Markdown: "x"})
	adr, _ := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "adr", Title: "DB", ADR: &ADR{Status: "accepted", Decision: "Postgres"}})
	adr2, _ := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "adr", Title: "Cache", ADR: &ADR{Status: "proposed", Decision: "Redis?"}})
	secret, _ := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "markdown", Title: "Geheim", Markdown: "x"})
	if _, err := s.SetRestricted(ctx, owner, p.Slug, secret.Document.Slug, true); err != nil {
		t.Fatal(err)
	}
	board, _ := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: store.DocBoard, Title: "Plan", Board: &Board{}})

	if _, err := s.CreateTask(ctx, owner, p.Slug, board.Document.Slug, TaskInput{Title: "X", Documents: []string{"gibt-es-nicht"}}); invalidKey(err) != "validation.task_doc_unknown" {
		t.Fatalf("unknown doc: %v", err)
	}
	task, err := s.CreateTask(ctx, owner, p.Slug, board.Document.Slug, TaskInput{Title: "Umsetzen",
		Documents: []string{page.Document.Slug, "ADR-0001", adr.Document.ID, "adr-2", secret.Document.Slug}})
	if err != nil {
		t.Fatal(err)
	}
	if len(task.Documents) != 4 || task.Documents[0].Slug != "konzept" || task.Documents[1].Number != 1 || task.Documents[2].Number != 2 {
		t.Fatalf("documents: %+v", task.Documents)
	}
	_ = adr2
	// The reader does not see the restricted page; their change keeps it linked.
	ts, _ := s.Tasks(ctx, reader, p.Slug, board.Document.Slug, "", "", false)
	if len(ts) != 1 || len(ts[0].Documents) != 3 {
		t.Fatalf("reader view: %+v", ts)
	}
	empty := []string{}
	editor := owner
	if _, err := s.UpdateTask(ctx, reader, p.Slug, task.ID, TaskPatch{Documents: &empty}); err == nil {
		t.Fatal("reader changed documents")
	}
	one := []string{"konzept"}
	u, err := s.UpdateTask(ctx, editor, p.Slug, task.ID, TaskPatch{Documents: &one})
	if err != nil || len(u.Documents) != 1 {
		t.Fatalf("replace: %+v %v", u.Documents, err)
	}
	// Pages show the cards that link them.
	cards, err := s.DocumentTasks(ctx, reader, p.Slug, "konzept")
	if err != nil || len(cards) != 1 || cards[0].Title != "Umsetzen" {
		t.Fatalf("backlinks: %+v %v", cards, err)
	}
	// A page in the trash disappears from the card.
	if _, err := s.DeleteDocument(ctx, owner, p.Slug, "konzept"); err != nil {
		t.Fatal(err)
	}
	ts, _ = s.Tasks(ctx, owner, p.Slug, board.Document.Slug, "", "", false)
	if len(ts[0].Documents) != 0 {
		t.Fatalf("trashed doc still linked: %+v", ts[0].Documents)
	}
}
