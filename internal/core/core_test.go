package core

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/phoedox/phoedox/internal/store"
)

func setup(t *testing.T) (*Service, Actor, Actor, Actor) {
	t.Helper()
	ctx := context.Background()
	db, err := store.Open(ctx, store.SQLite, filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	mk := func(email string) store.User {
		u, err := db.CreateUser(ctx, store.User{ID: newID(), Email: email, Name: email})
		if err != nil {
			t.Fatal(err)
		}
		return u
	}
	owner, reader, stranger := mk("owner@x.de"), mk("reader@x.de"), mk("stranger@x.de")
	return New(db), Actor{User: owner, Via: "web"}, Actor{User: reader, Via: "web"}, Actor{User: stranger, Via: "web"}
}

func TestPermissionsAndDocuments(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, stranger := setup(t)
	p, err := s.CreateProject(ctx, owner, ProjectInput{Name: "Phoedox Entwicklung"})
	if err != nil || p.Slug != "phoedox-entwicklung" || p.Short != "PE" {
		t.Fatalf("create project: %v %+v", err, p)
	}
	if err := s.SetMember(ctx, owner, p.Slug, "reader@x.de", store.RoleReader); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Project(ctx, stranger, p.Slug); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stranger must not see the project: %v", err)
	}
	if _, err := s.CreateDocument(ctx, reader, p.Slug, DocumentInput{Type: "markdown", Title: "X"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("reader must not write: %v", err)
	}

	// entity + markdown mention -> document link
	if _, err := s.UpsertEntity(ctx, owner, p.Slug, EntityInput{Kind: "service", Name: "Go-Server"}); err != nil {
		t.Fatal(err)
	}
	res, err := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "markdown", Title: "Architektur", Category: "Technik", Markdown: "Der @go-server liefert die App aus."})
	if err != nil || !res.Applied {
		t.Fatalf("create doc: %v %+v", err, res)
	}
	detail, err := s.Entity(ctx, reader, p.Slug, "go-server")
	if err != nil || len(detail.Documents) != 1 {
		t.Fatalf("mention link: %v %+v", err, detail)
	}

	// ADR validation
	if _, err := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "adr", Title: "DB", ADR: &ADR{Status: "maybe", Decision: "x"}}); err == nil {
		t.Fatal("invalid ADR status accepted")
	}
	adr, err := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "adr", Title: "Datenbank", ADR: &ADR{Status: "accepted", Context: "Self-Hosting", Decision: "Postgres und SQLite"}})
	if err != nil {
		t.Fatal(err)
	}
	newDecision := "Postgres, SQLite und später mehr"
	up, err := s.UpdateDocument(ctx, owner, p.Slug, adr.Document.Slug, DocumentPatch{ADR: &ADR{Status: "accepted", Decision: newDecision}, ExpectVersion: 1})
	if err != nil || up.Document.Version != 2 {
		t.Fatalf("update adr: %v", err)
	}
	var got ADR
	_ = json.Unmarshal(up.Document.Content, &got)
	if got.Decision != newDecision {
		t.Fatalf("adr content: %+v", got)
	}
	if _, err := s.UpdateDocument(ctx, owner, p.Slug, adr.Document.Slug, DocumentPatch{Title: strp("x"), ExpectVersion: 1}); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale update must conflict: %v", err)
	}

	// link documents need http(s)
	if _, err := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "link", Title: "Riverpod", Link: &Link{URL: "javascript:alert(1)"}}); err == nil {
		t.Fatal("javascript: link accepted")
	}
	if _, err := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "link", Title: "Riverpod", Link: &Link{URL: "https://riverpod.dev/docs/introduction/why_riverpod"}}); err != nil {
		t.Fatal(err)
	}

	hits, err := s.Search(ctx, reader, "postgres", "", 10)
	if err != nil || len(hits) != 1 {
		t.Fatalf("search: %v %+v", err, hits)
	}
	if hits, _ := s.Search(ctx, stranger, "postgres", "", 10); len(hits) != 0 {
		t.Fatalf("stranger search must be empty: %+v", hits)
	}
}

func TestSuggestions(t *testing.T) {
	ctx := context.Background()
	s, owner, _, _ := setup(t)
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Wohnung"}) // default mode: suggest
	ai := Actor{User: owner.User, Via: "mcp", TokenID: "tok1"}

	res, err := s.CreateDocument(ctx, ai, p.Slug, DocumentInput{Type: "markdown", Title: "Netz", Markdown: "Router"})
	if err != nil || res.Applied || res.Suggestion == nil {
		t.Fatalf("AI write must become a suggestion: %v %+v", err, res)
	}
	if docs, _ := s.Documents(ctx, owner, p.Slug, store.DocFilter{}); len(docs) != 0 {
		t.Fatal("suggestion must not create the document yet")
	}
	if _, err := s.DecideSuggestion(ctx, ai, res.Suggestion.ID, true); !errors.Is(err, ErrForbidden) {
		t.Fatalf("AI must not approve itself: %v", err)
	}
	if _, err := s.DecideSuggestion(ctx, owner, res.Suggestion.ID, true); err != nil {
		t.Fatal(err)
	}
	if docs, _ := s.Documents(ctx, owner, p.Slug, store.DocFilter{}); len(docs) != 1 {
		t.Fatal("accepted suggestion must create the document")
	}

	// direct mode applies immediately
	if _, err := s.UpdateProject(ctx, owner, p.Slug, ProjectInput{Name: "Wohnung", AIWriteMode: "direct"}); err != nil {
		t.Fatal(err)
	}
	res, err = s.UpdateDocument(ctx, ai, p.Slug, "netz", DocumentPatch{Markdown: strp("Router im Flur")})
	if err != nil || !res.Applied {
		t.Fatalf("direct mode: %v %+v", err, res)
	}
}

func strp(s string) *string { return &s }
