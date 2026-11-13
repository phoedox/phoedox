package core

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/phoedox/phoedox/internal/store"
)

func adrOf(t *testing.T, d store.Document) ADR {
	t.Helper()
	var a ADR
	if err := json.Unmarshal(d.Content, &a); err != nil {
		t.Fatal(err)
	}
	return a
}

func invalidKey(err error) string {
	var ie *InvalidError
	if errors.As(err, &ie) {
		return ie.Key
	}
	return ""
}

func TestDecisions(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, _ := setup(t)
	p, err := s.CreateProject(ctx, owner, ProjectInput{Name: "Phoedox"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetMember(ctx, owner, p.Slug, "reader@x.de", store.RoleReader); err != nil {
		t.Fatal(err)
	}
	decide := func(title string, adr ADR) store.Document {
		t.Helper()
		res, err := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "adr", Title: title, ADR: &adr})
		if err != nil {
			t.Fatalf("create %s: %v", title, err)
		}
		return *res.Document
	}
	get := func(ref string) store.Document {
		t.Helper()
		d, err := s.Document(ctx, reader, p.Slug, ref)
		if err != nil {
			t.Fatalf("get %s: %v", ref, err)
		}
		return d
	}

	// numbering, lookup by number
	db := decide("Datenbank", ADR{Status: "accepted", Decision: "Postgres und SQLite", SupersededBy: "ignored"})
	router := decide("Router", ADR{Status: "accepted", Decision: "chi"})
	if db.Number != 1 || router.Number != 2 {
		t.Fatalf("numbers: %d %d", db.Number, router.Number)
	}
	if a := adrOf(t, db); a.SupersededBy != "" {
		t.Fatalf("supersededBy must be server-maintained: %+v", a)
	}
	for _, ref := range []string{"ADR-0002", "adr-2", "ADR2", "router", router.ID} {
		if get(ref).ID != router.ID {
			t.Fatalf("ref %s not resolved", ref)
		}
	}
	if _, err := s.Document(ctx, reader, p.Slug, "ADR-0099"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown number: %v", err)
	}

	// numbers are never reused
	if _, err := s.DeleteDocument(ctx, owner, p.Slug, "ADR-0002"); err != nil {
		t.Fatal(err)
	}
	ui := decide("Oberfläche", ADR{Status: "proposed", Decision: "Lit"})
	if ui.Number != 3 {
		t.Fatalf("number after delete: %d", ui.Number)
	}

	// invalid supersedes references
	if _, err := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "markdown", Title: "Notizen", Markdown: "x"}); err != nil {
		t.Fatal(err)
	}
	for ref, key := range map[string]string{"ADR-0042": "validation.adr_supersedes_not_found", "notizen": "validation.adr_supersedes_type"} {
		_, err := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "adr", Title: "X " + ref, ADR: &ADR{Decision: "x", Supersedes: ref}})
		if invalidKey(err) != key {
			t.Fatalf("supersedes %s: want %s, got %v", ref, key, err)
		}
	}
	if _, err := s.UpdateDocument(ctx, owner, p.Slug, "ADR-0003", DocumentPatch{ADR: &ADR{Decision: "Lit", Supersedes: "ADR-0003"}}); invalidKey(err) != "validation.adr_supersedes_self" {
		t.Fatalf("self supersede: %v", err)
	}

	// superseding marks the older decision in the same step
	db2 := decide("Datenbank v2", ADR{Status: "accepted", Decision: "Postgres, SQLite, später mehr", Supersedes: "ADR-0001"})
	if db2.Number != 4 || adrOf(t, db2).Supersedes != "datenbank" {
		t.Fatalf("new decision: %d %+v", db2.Number, adrOf(t, db2))
	}
	old := get("ADR-0001")
	if a := adrOf(t, old); a.Status != "superseded" || a.SupersededBy != "datenbank-v2" || old.Version != 2 {
		t.Fatalf("old decision not marked: v%d %+v", old.Version, a)
	}

	// editing the old decision keeps the server-maintained link
	if _, err := s.UpdateDocument(ctx, owner, p.Slug, "ADR-0001", DocumentPatch{ADR: &ADR{Status: "superseded", Decision: "Postgres und SQLite (alt)"}}); err != nil {
		t.Fatal(err)
	}
	if a := adrOf(t, get("ADR-0001")); a.SupersededBy != "datenbank-v2" {
		t.Fatalf("supersededBy lost on edit: %+v", a)
	}

	// renaming either side keeps both links intact
	if _, err := s.UpdateDocument(ctx, owner, p.Slug, "ADR-0001", DocumentPatch{Slug: strp("datenbank-alt")}); err != nil {
		t.Fatal(err)
	}
	if a := adrOf(t, get("ADR-0004")); a.Supersedes != "datenbank-alt" {
		t.Fatalf("newer decision not repointed: %+v", a)
	}
	if _, err := s.UpdateDocument(ctx, owner, p.Slug, "ADR-0004", DocumentPatch{Slug: strp("datenbank-neu")}); err != nil {
		t.Fatal(err)
	}
	if a := adrOf(t, get("ADR-0001")); a.SupersededBy != "datenbank-neu" {
		t.Fatalf("older decision not repointed: %+v", a)
	}

	// decision log with status filter
	all, err := s.Decisions(ctx, reader, p.Slug, "", "")
	if err != nil || len(all) != 3 || all[0].Ref != "ADR-0001" || all[1].Number != 3 || all[2].Number != 4 {
		t.Fatalf("decisions: %v %+v", err, all)
	}
	sup, err := s.Decisions(ctx, reader, p.Slug, "superseded", "")
	if err != nil || len(sup) != 1 || sup[0].SupersededBy != "datenbank-neu" {
		t.Fatalf("superseded filter: %v %+v", err, sup)
	}
	if _, err := s.Decisions(ctx, reader, p.Slug, "maybe", ""); invalidKey(err) != "validation.adr_status" {
		t.Fatalf("invalid filter: %v", err)
	}
	// text search over number, title and text; all words must match
	if q, _ := s.Decisions(ctx, reader, p.Slug, "", "adr-0003"); len(q) != 1 || q[0].Number != 3 {
		t.Fatalf("search by number: %+v", q)
	}
	if q, _ := s.Decisions(ctx, reader, p.Slug, "", "gibt es nicht"); len(q) != 0 {
		t.Fatalf("search without match: %+v", q)
	}

	// AI suggestions supersede only when accepted
	ai := Actor{User: owner.User, Via: "mcp", TokenID: "tok"}
	res, err := s.CreateDocument(ctx, ai, p.Slug, DocumentInput{Type: "adr", Title: "Web Components", ADR: &ADR{Decision: "Lit bleibt", Supersedes: "adr-3"}})
	if err != nil || res.Suggestion == nil {
		t.Fatalf("suggestion: %v %+v", err, res)
	}
	if a := adrOf(t, get("ADR-0003")); a.Status != "proposed" {
		t.Fatalf("suggestion must not change the old decision yet: %+v", a)
	}
	acc, err := s.DecideSuggestion(ctx, owner, res.Suggestion.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if n := acc.(DocResult).Document.Number; n != 5 {
		t.Fatalf("accepted number: %d", n)
	}
	if a := adrOf(t, get("ADR-0003")); a.Status != "superseded" || a.SupersededBy != "web-components" {
		t.Fatalf("accepted suggestion did not supersede: %+v", a)
	}
}
