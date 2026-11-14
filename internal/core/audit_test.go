package core

import (
	"context"
	"errors"
	"testing"

	"github.com/phoedox/phoedox/internal/store"
)

func TestAuditLog(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, _ := setup(t)
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Audit", AIWriteMode: "direct"})
	other, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Anderes"})
	_ = s.SetMember(ctx, owner, p.Slug, "reader@x.de", store.RoleReader)
	ai := Actor{User: owner.User, Via: "mcp", TokenID: "tok1"}
	if _, err := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "markdown", Title: "Von Hand"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateDocument(ctx, ai, p.Slug, DocumentInput{Type: "markdown", Title: "Von KI"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpsertEntity(ctx, owner, p.Slug, EntityInput{Kind: "router", Name: "Router"}); err != nil {
		t.Fatal(err)
	}
	_, _ = s.CreateDocument(ctx, owner, other.Slug, DocumentInput{Type: "markdown", Title: "Woanders"})

	all, err := s.AuditLog(ctx, owner, p.Slug, AuditQuery{})
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]int{}
	for _, e := range all {
		if e.ProjectID != p.ID {
			t.Fatalf("event of another project: %+v", e)
		}
		kinds[e.Kind]++
	}
	if kinds["document.create"] != 2 || kinds["entity.upsert"] == 0 && kinds["entity.create"] == 0 {
		t.Fatalf("kinds: %v", kinds)
	}
	mcp, _ := s.AuditLog(ctx, owner, p.Slug, AuditQuery{Via: "mcp"})
	if len(mcp) != 1 || mcp[0].Via != "mcp" || mcp[0].Who != "owner@x.de" {
		t.Fatalf("mcp filter: %+v", mcp)
	}
	docs, _ := s.AuditLog(ctx, owner, p.Slug, AuditQuery{Kind: "document."})
	if len(docs) != 2 {
		t.Fatalf("kind filter: %+v", docs)
	}
	if page, _ := s.AuditLog(ctx, owner, p.Slug, AuditQuery{Limit: 1, Before: all[0].ID}); len(page) != 1 || page[0].ID >= all[0].ID {
		t.Fatalf("paging: %+v", page)
	}
	if _, err := s.AuditLog(ctx, reader, p.Slug, AuditQuery{}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("readers: %v", err)
	}
	if _, err := s.AuditLog(ctx, owner, "", AuditQuery{}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("instance log needs admin: %v", err)
	}
	admin := owner
	admin.User.IsAdmin = true
	if inst, err := s.AuditLog(ctx, admin, "", AuditQuery{Kind: "document.create"}); err != nil || len(inst) != 3 {
		t.Fatalf("instance log: %v %d", err, len(inst))
	}
	if _, err := s.AuditLog(ctx, ai, p.Slug, AuditQuery{}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("AI clients do not read the audit log: %v", err)
	}
}
