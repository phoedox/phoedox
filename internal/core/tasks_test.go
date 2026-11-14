package core

import (
	"context"
	"errors"
	"testing"

	"github.com/phoedox/phoedox/internal/store"
)

// TestTasks covers ADR-0030: boards with columns, cards that move between
// columns, done tracking, permissions and AI rules.
func TestTasks(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, _ := setup(t)
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Haus", AIWriteMode: "suggest"})
	_ = s.SetMember(ctx, owner, p.Slug, "reader@x.de", store.RoleReader)
	res, err := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "board", Title: "Umbau"})
	if err != nil {
		t.Fatal(err)
	}
	board := res.Document.Slug
	if b := boardOf(*res.Document); len(b.Columns) != 3 || !b.Columns[2].Done {
		t.Fatalf("default columns: %+v", b)
	}
	if _, err := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "board", Title: "X", Board: &Board{Columns: []BoardColumn{{Name: "A"}, {Name: "a"}}}}); invalidKey(err) != "validation.board_column" {
		t.Fatalf("duplicate columns: %v", err)
	}

	a, err := s.CreateTask(ctx, owner, p.Slug, board, TaskInput{Title: "Kabel ziehen", Assignee: "reader@x.de", Labels: []string{"netz", "netz", " "}, Due: "2026-12-01"})
	if err != nil || a.Column != "todo" || a.AssigneeName == "" || len(a.Labels) != 1 || a.Position != 1 {
		t.Fatalf("create: %v %+v", err, a)
	}
	b, _ := s.CreateTask(ctx, owner, p.Slug, board, TaskInput{Title: "Dosen setzen"})
	if b.Position != 2 {
		t.Fatalf("second goes to the end: %+v", b)
	}
	if _, err := s.CreateTask(ctx, owner, p.Slug, board, TaskInput{Title: "x", Assignee: "nobody@x.de"}); invalidKey(err) != "validation.assignee" {
		t.Fatalf("assignee must be member: %v", err)
	}
	byText, err := s.CreateTask(ctx, owner, p.Slug, board, TaskInput{Title: "Doku schreiben", Assignee: " Claude "})
	if err != nil || byText.AssigneeID != "" || byText.AssigneeText != "Claude" {
		t.Fatalf("free-text assignee: %v %+v", err, byText)
	}
	if list, _ := s.Tasks(ctx, owner, p.Slug, board, "", "claude", false); len(list) != 1 || list[0].ID != byText.ID {
		t.Fatalf("filter by free text: %+v", list)
	}
	member := "reader@x.de"
	if moved, err := s.UpdateTask(ctx, owner, p.Slug, byText.ID, TaskPatch{Assignee: &member}); err != nil || moved.AssigneeText != "" || moved.AssigneeID == "" {
		t.Fatalf("back to a member: %v %+v", err, moved)
	}
	_ = s.DeleteTask(ctx, owner, p.Slug, byText.ID)
	if _, err := s.CreateTask(ctx, owner, p.Slug, board, TaskInput{Title: "x", Due: "morgen"}); invalidKey(err) != "validation.due" {
		t.Fatalf("due: %v", err)
	}
	// Readers see but do not change.
	if l, _ := s.Tasks(ctx, reader, p.Slug, board, "", "", false); len(l) != 2 {
		t.Fatalf("reader list: %d", len(l))
	}
	if _, err := s.CreateTask(ctx, reader, p.Slug, board, TaskInput{Title: "x"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("reader create: %v", err)
	}

	// Moving into the done column marks done; out of it clears.
	done := "Done"
	a, err = s.UpdateTask(ctx, owner, p.Slug, a.ID, TaskPatch{Column: &done})
	if err != nil || a.Column != "done" || a.DoneAt.IsZero() {
		t.Fatalf("move done: %v %+v", err, a)
	}
	if l, _ := s.Tasks(ctx, owner, p.Slug, "", "", "", true); len(l) != 1 || l[0].ID != b.ID {
		t.Fatalf("open tasks: %+v", l)
	}
	back := "doing"
	if a, _ = s.UpdateTask(ctx, owner, p.Slug, a.ID, TaskPatch{Column: &back}); !a.DoneAt.IsZero() {
		t.Fatal("done must clear")
	}
	if l, _ := s.Tasks(ctx, reader, p.Slug, "", "", "me", false); len(l) != 1 {
		t.Fatalf("my tasks: %d", len(l))
	}

	// Removing a column moves its cards to the first one.
	if _, err := s.UpdateDocument(ctx, owner, p.Slug, board, DocumentPatch{Board: &Board{Columns: []BoardColumn{{ID: "todo", Name: "Offen"}, {ID: "done", Name: "Fertig", Done: true}}}}); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.DB.GetTask(ctx, p.ID, a.ID); got.Column != "todo" {
		t.Fatalf("orphan moved: %+v", got)
	}

	// AI clients change tasks only in projects where AI writes directly.
	ai := Actor{User: owner.User, Via: "mcp", TokenID: "t"}
	if _, err := s.CreateTask(ctx, ai, p.Slug, board, TaskInput{Title: "KI"}); invalidKey(err) != "validation.ai_tasks_direct" {
		t.Fatalf("ai in suggest mode: %v", err)
	}
	if l, err := s.Tasks(ctx, ai, p.Slug, board, "", "", false); err != nil || len(l) != 2 {
		t.Fatalf("ai may read: %v", err)
	}
	if err := s.DeleteTask(ctx, owner, p.Slug, b.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTask(ctx, owner, p.Slug, "gibt-es-nicht", TaskInput{Title: "x"}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown board: %v", err)
	}
}

// TestFeatures covers ADR-0032: optional tabs per project.
func TestFeatures(t *testing.T) {
	ctx := context.Background()
	s, owner, _, _ := setup(t)
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Neu"})
	if len(p.Features) != 0 {
		t.Fatalf("new projects start without tabs: %v", p.Features)
	}
	in := ProjectInput{Name: p.Name, Short: p.Short, PrimaryColor: p.PrimaryColor, AIWriteMode: p.AIWriteMode, Features: &[]string{"files", "decisions"}}
	if p, err := s.UpdateProject(ctx, owner, p.Slug, in); err != nil || len(p.Features) != 2 || p.Features[0] != "decisions" {
		t.Fatalf("features: %v %v", err, p.Features)
	}
	in.Features = &[]string{"chat"}
	if _, err := s.UpdateProject(ctx, owner, p.Slug, in); invalidKey(err) != "validation.feature" {
		t.Fatalf("unknown feature: %v", err)
	}
	if got, _ := s.Project(ctx, owner, p.Slug); len(got.Features) != 2 {
		t.Fatalf("stored: %v", got.Features)
	}
}
