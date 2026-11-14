package core

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/phoedox/phoedox/internal/store"
)

func TestHome(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, _ := setup(t)
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Haus", AIWriteMode: AISuggest})
	other, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Labor"})
	_ = s.SetMember(ctx, owner, p.Slug, reader.User.Email, store.RoleReader)
	_, _ = s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "markdown", Title: "Netz", Markdown: "x"})
	secret, _ := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "markdown", Title: "Geheim", Markdown: "x"})
	_, _ = s.SetRestricted(ctx, owner, p.Slug, secret.Document.Slug, true)
	board, _ := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "board", Title: "Aufgaben"})
	today := time.Now().Format(time.DateOnly)
	_, _ = s.CreateTask(ctx, owner, p.Slug, board.Document.Slug, TaskInput{Title: "Router tauschen", Assignee: owner.User.ID, Due: today})
	_, _ = s.CreateTask(ctx, owner, p.Slug, board.Document.Slug, TaskInput{Title: "Kabel"})
	// An AI suggestion waits for an editor.
	if r, err := s.CreateDocument(ctx, Actor{User: owner.User, Via: "mcp"}, p.Slug, DocumentInput{Type: "markdown", Title: "Vorschlag"}); err != nil || r.Suggestion == nil {
		t.Fatalf("suggestion: %+v %v", r, err)
	}

	h, err := s.Home(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	st := h.Stats[p.ID]
	if st == nil || st.Documents != 3 || st.OpenTasks != 2 || st.Suggestions != 1 || st.LastActivity.IsZero() || h.DueTasks != 1 {
		t.Fatalf("owner home: %+v %+v", st, h)
	}
	if len(h.Suggestions) != 1 || h.Suggestions[0].Project != p.Slug || h.Stats[other.ID] == nil {
		t.Fatalf("suggestions: %+v", h.Suggestions)
	}
	// Readers do not count restricted pages and do not see suggestions.
	h, _ = s.Home(ctx, reader)
	if st := h.Stats[p.ID]; st == nil || st.Documents != 2 || st.Suggestions != 0 || len(h.Suggestions) != 0 || h.Stats[other.ID] != nil {
		t.Fatalf("reader home: %+v %+v", st, h)
	}

	// Pinning keeps only projects the person can open.
	pinned, err := s.SetPinnedProjects(ctx, reader, []string{other.ID, p.ID, p.ID, "nope"})
	if err != nil || len(pinned) != 1 || pinned[0] != p.ID {
		t.Fatalf("pinned: %v %v", pinned, err)
	}
	if h, _ = s.Home(ctx, reader); len(h.Pinned) != 1 {
		t.Fatalf("home pinned: %v", h.Pinned)
	}
	if _, err := s.SetPinnedProjects(ctx, Actor{User: reader.User, Via: "mcp"}, nil); !errors.Is(err, ErrForbidden) {
		t.Fatalf("AI pinned: %v", err)
	}
}
