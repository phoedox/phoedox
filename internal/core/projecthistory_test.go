package core

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestProjectRollback(t *testing.T) {
	ctx := context.Background()
	s, owner, _, _ := setup(t)
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Docs", AIWriteMode: "direct"})
	md := func(s string) *string { return &s }
	a, _ := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "markdown", Title: "Intro", Markdown: "first"})
	b, _ := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "markdown", Title: "Setup", Markdown: "steps"})
	time.Sleep(15 * time.Millisecond)
	point := time.Now()
	time.Sleep(15 * time.Millisecond)

	// After the point: Intro changes, Setup is deleted, News is new.
	if _, err := s.UpdateDocument(ctx, owner, p.Slug, a.Document.Slug, DocumentPatch{Markdown: md("second")}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DeleteDocument(ctx, owner, p.Slug, b.Document.Slug); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "markdown", Title: "News", Markdown: "new"}); err != nil {
		t.Fatal(err)
	}

	pts, err := s.ProjectPoints(ctx, owner, p.Slug, time.Time{}, 10)
	if err != nil || len(pts) < 4 || pts[0].Title != "News" {
		t.Fatalf("points: %v %+v", err, pts)
	}
	st, err := s.ProjectStateAt(ctx, owner, p.Slug, point)
	if err != nil || len(st.Changes) != 3 {
		t.Fatalf("state at: %v %+v", err, st)
	}
	kinds := map[string]string{}
	for _, c := range st.Changes {
		kinds[c.Title] = c.Change
	}
	if kinds["Intro"] != "changed" || kinds["Setup"] != "deleted" || kinds["News"] != "created" {
		t.Fatalf("changes: %+v", kinds)
	}

	// AI clients cannot roll back; owners can.
	if _, err := s.RollbackProject(ctx, Actor{User: owner.User, Via: "mcp", TokenID: "t"}, p.Slug, point); !errors.Is(err, ErrForbidden) {
		t.Fatalf("AI rollback: %v", err)
	}
	res, err := s.RollbackProject(ctx, owner, p.Slug, point)
	if err != nil || res.Restored != 1 || res.Revived != 1 || res.Trashed != 1 || len(res.Failed) != 0 {
		t.Fatalf("rollback: %v %+v", err, res)
	}
	d, _ := s.Document(ctx, owner, p.Slug, "intro")
	if string(d.Content) != `{"markdown":"first"}` || d.Version != 3 {
		t.Fatalf("intro after rollback: v%d %s", d.Version, d.Content)
	}
	if _, err := s.Document(ctx, owner, p.Slug, "setup"); err != nil {
		t.Fatalf("setup back: %v", err)
	}
	if _, err := s.Document(ctx, owner, p.Slug, "news"); err == nil {
		t.Fatal("news must be in the trash")
	}
	// Now nothing differs from the point.
	// Intro has a new version number but the old content: nothing differs.
	if st, _ := s.ProjectStateAt(ctx, owner, p.Slug, point); len(st.Changes) != 0 {
		t.Fatalf("after rollback: %+v", st.Changes)
	}
}
