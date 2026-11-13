package core

import (
	"context"
	"errors"
	"testing"

	"github.com/phoedox/phoedox/internal/store"
)

func TestAIAccess(t *testing.T) {
	ctx := context.Background()
	s, owner, _, _ := setup(t)
	haus, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Haus", AIWriteMode: "direct"})
	firma, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Firma", AIWriteMode: "direct"})
	_, _ = s.CreateDocument(ctx, owner, haus.Slug, DocumentInput{Type: "markdown", Title: "Router", Markdown: "Router im Keller"})
	_, _ = s.CreateDocument(ctx, owner, firma.Slug, DocumentInput{Type: "markdown", Title: "Router", Markdown: "Router im Serverraum"})
	write := DocumentInput{Type: "markdown", Title: "Neu", Markdown: "x"}

	// read-only token: reading yes, writing and creating projects no
	ro := Actor{User: owner.User, Via: "token", Scope: NewScope(store.AccessRead, nil)}
	if _, err := s.Documents(ctx, ro, haus.Slug, store.DocFilter{}); err != nil {
		t.Fatalf("read-only read: %v", err)
	}
	if _, err := s.CreateDocument(ctx, ro, haus.Slug, write); !errors.Is(err, ErrForbidden) {
		t.Fatalf("read-only write: %v", err)
	}
	if _, err := s.CreateProject(ctx, ro, ProjectInput{Name: "X"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("read-only create project: %v", err)
	}
	if !ro.Restricted() || (Actor{}).Restricted() {
		t.Fatal("Restricted")
	}

	// project-limited token: other projects do not exist for it, even for admins
	owner.User.IsAdmin = true
	only := Actor{User: owner.User, Via: "mcp", Scope: NewScope(store.AccessWrite, []string{haus.ID})}
	if _, err := s.Project(ctx, only, firma.Slug); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other project: %v", err)
	}
	if ps, _ := s.Projects(ctx, only); len(ps) != 1 || ps[0].ID != haus.ID {
		t.Fatalf("projects: %+v", ps)
	}
	if hits, _ := s.Search(ctx, only, "router", "", 10); len(hits) != 1 || hits[0].ProjectID != haus.ID {
		t.Fatalf("search: %+v", hits)
	}
	if _, err := s.CreateDocument(ctx, only, haus.Slug, write); err != nil {
		t.Fatalf("write in allowed project: %v", err)
	}
	if NewScope(store.AccessWrite, nil) != nil {
		t.Fatal("full access must be a nil scope")
	}

	// per-project AI level: off hides the project, read allows reading only
	ai := Actor{User: owner.User, Via: "mcp", TokenID: "t"}
	_, _ = s.UpdateProject(ctx, owner, firma.Slug, ProjectInput{Name: "Firma", AIWriteMode: AIOff})
	_, _ = s.UpdateProject(ctx, owner, haus.Slug, ProjectInput{Name: "Haus", AIWriteMode: AIRead})
	if _, err := s.Project(ctx, ai, firma.Slug); !errors.Is(err, ErrNotFound) {
		t.Fatalf("AI off: %v", err)
	}
	if ps, _ := s.Projects(ctx, ai); len(ps) != 1 {
		t.Fatalf("AI project list: %+v", ps)
	}
	if _, err := s.Documents(ctx, ai, haus.Slug, store.DocFilter{}); err != nil {
		t.Fatalf("AI read: %v", err)
	}
	if _, err := s.CreateDocument(ctx, ai, haus.Slug, write); !errors.Is(err, ErrForbidden) {
		t.Fatalf("AI write in read mode: %v", err)
	}
	if _, err := s.Project(ctx, owner, firma.Slug); err != nil {
		t.Fatalf("people still see the project: %v", err)
	}
	// the AI cannot raise its own level
	_, _ = s.UpdateProject(ctx, owner, haus.Slug, ProjectInput{Name: "Haus", AIWriteMode: AISuggest})
	if _, err := s.UpdateProject(ctx, ai, haus.Slug, ProjectInput{Name: "Haus", AIWriteMode: AIDirect}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("AI raises its level: %v", err)
	}

	// instance switch
	if _, err := s.UpdateSettings(ctx, ro, InstanceSettings{AIEnabled: false}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("restricted admin token changes settings: %v", err)
	}
	if _, err := s.UpdateSettings(ctx, owner, InstanceSettings{AIEnabled: false}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Project(ctx, ai, haus.Slug); invalidKey(err) != "validation.ai_disabled" {
		t.Fatalf("AI disabled: %v", err)
	}
	if _, err := s.Projects(ctx, ai); invalidKey(err) != "validation.ai_disabled" {
		t.Fatalf("AI disabled list: %v", err)
	}
	if _, err := s.Project(ctx, owner, haus.Slug); err != nil {
		t.Fatalf("web unaffected: %v", err)
	}
	if st, _ := s.Settings(ctx, owner); st.AIEnabled {
		t.Fatal("setting not stored")
	}

	// scope validation for new tokens and grants
	if _, _, err := s.TokenScope(ctx, owner, "admin", nil); invalidKey(err) != "validation.access" {
		t.Fatalf("access: %v", err)
	}
	if _, _, err := s.TokenScope(ctx, owner, "read", []string{"gibt-es-nicht"}); invalidKey(err) != "validation.project_unknown" {
		t.Fatalf("unknown project: %v", err)
	}
	if acc, ids, err := s.TokenScope(ctx, owner, "", []string{haus.Slug, haus.ID}); err != nil || acc != "write" || len(ids) != 1 || ids[0] != haus.ID {
		t.Fatalf("scope: %v %s %v", err, acc, ids)
	}
}
