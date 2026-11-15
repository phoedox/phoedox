package core

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/phoedox/phoedox/internal/store"
)

func TestDecideOptions(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, _ := setup(t)
	p, err := s.CreateProject(ctx, owner, ProjectInput{Name: "Entscheiden"})
	if err != nil {
		t.Fatal(err)
	}
	_ = s.SetMember(ctx, owner, p.Slug, reader.User.Email, store.RoleReader)
	adrOf := func(d *store.Document) ADR {
		var a ADR
		_ = json.Unmarshal(d.Content, &a)
		return a
	}

	// Without options an empty decision text is still an error.
	if _, err := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "adr", Title: "Leer", ADR: &ADR{Status: "proposed"}}); invalidKey(err) != "validation.adr_decision_required" {
		t.Fatalf("empty: %v", err)
	}
	if _, err := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "adr", Title: "Leer", ADR: &ADR{Status: "accepted", Options: []ADROption{{Title: "A"}}}}); invalidKey(err) != "validation.adr_decision_required" {
		t.Fatalf("accepted without text: %v", err)
	}
	if _, err := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "adr", Title: "X", ADR: &ADR{Status: "proposed", Options: []ADROption{{Description: "no title"}}}}); invalidKey(err) != "validation.adr_option_title" {
		t.Fatalf("option without title: %v", err)
	}
	res, err := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "adr", Title: "Datenbank", ADR: &ADR{Status: "proposed", Context: "Wir brauchen eine DB.",
		Options:   []ADROption{{Title: "PostgreSQL", Description: "+ RLS"}, {Title: "SQLite", Description: "+ einfach"}, {Title: "  "}},
		Responses: []ADRResponse{{Choice: "o1", By: "Fake"}}, Chosen: "o1"}})
	if err != nil {
		t.Fatal(err)
	}
	doc := res.Document
	a := adrOf(doc)
	if len(a.Options) != 2 || a.Options[0].ID != "o1" || a.Options[1].ID != "o2" || len(a.Responses) != 0 || a.Chosen != "" {
		t.Fatalf("created: %+v", a)
	}

	// Readers and AI clients cannot decide; unknown choices are refused.
	if _, err := s.Decide(ctx, reader, p.Slug, doc.Slug, DecideInput{Choice: "o1"}); err == nil {
		t.Fatal("reader decided")
	}
	if _, err := s.Decide(ctx, Actor{User: owner.User, Via: "mcp"}, p.Slug, doc.Slug, DecideInput{Choice: "o1"}); err != ErrForbidden {
		t.Fatalf("ai decided: %v", err)
	}
	if _, err := s.Decide(ctx, owner, p.Slug, doc.Slug, DecideInput{Choice: "o9"}); invalidKey(err) != "validation.decide_choice" {
		t.Fatalf("unknown choice: %v", err)
	}

	// Nothing fits: stays proposed, the answer is kept, new options are asked for.
	r, err := s.Decide(ctx, owner, p.Slug, doc.Slug, DecideInput{Choice: ChoiceNone, Comment: "Wir brauchen etwas Verteiltes."})
	if err != nil {
		t.Fatal(err)
	}
	a = adrOf(r.Document)
	if a.Status != "proposed" || len(a.Responses) != 1 || a.Responses[0].Comment != "Wir brauchen etwas Verteiltes." || a.Responses[0].By != owner.User.Name {
		t.Fatalf("none: %+v", a)
	}
	ds, _ := s.Decisions(ctx, owner, p.Slug, "", "")
	if len(ds) != 1 || !ds[0].NeedsOptions || ds[0].Options != 2 {
		t.Fatalf("summary: %+v", ds)
	}

	// New options replace the old ones; the answers stay.
	next := a
	next.Options = []ADROption{{Title: "CockroachDB", Description: "verteilt"}, {Title: "PostgreSQL mit Patroni"}}
	next.Responses = nil
	u, err := s.UpdateDocument(ctx, owner, p.Slug, doc.Slug, DocumentPatch{ADR: &next})
	if err != nil {
		t.Fatal(err)
	}
	if a = adrOf(u.Document); len(a.Responses) != 1 || a.Options[0].Title != "CockroachDB" {
		t.Fatalf("new options: %+v", a)
	}

	// Choosing accepts the decision.
	r, err = s.Decide(ctx, owner, p.Slug, doc.Slug, DecideInput{Choice: a.Options[1].ID, Comment: "Passt."})
	if err != nil {
		t.Fatal(err)
	}
	a = adrOf(r.Document)
	if a.Status != "accepted" || a.Chosen != a.Options[1].ID || !strings.Contains(a.Decision, "PostgreSQL mit Patroni") ||
		!strings.Contains(a.Alternatives, "CockroachDB") || len(a.Responses) != 2 || a.Responses[1].Option != "PostgreSQL mit Patroni" ||
		len(a.Deciders) != 1 || a.Date == "" {
		t.Fatalf("chosen: %+v", a)
	}
	if _, err := s.Decide(ctx, owner, p.Slug, doc.Slug, DecideInput{Choice: ChoiceNone}); invalidKey(err) != "validation.decide_not_open" {
		t.Fatalf("decided twice: %v", err)
	}
	if OpenOptions(r.Document.Content) != "" || ResponsesMarkdown(r.Document.Content) == "" {
		t.Fatal("markdown helpers")
	}
}
