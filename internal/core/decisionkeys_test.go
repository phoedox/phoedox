package core

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/phoedox/phoedox/internal/store"
)

// Feedback from a large import: own numbers, translations that stay current
// on metadata changes, a translatable supersede scope and original texts.
func TestDecisionKeysAndTranslations(t *testing.T) {
	ctx := context.Background()
	s, owner, _, _ := setup(t)
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Log", AIWriteMode: "direct"})
	full := ProjectInput{Name: p.Name, Short: p.Short, PrimaryColor: p.PrimaryColor, AIWriteMode: p.AIWriteMode, MainLocale: strp("en"), Locales: &[]string{"de"}}
	if _, err := s.UpdateProject(ctx, owner, p.Slug, full); err != nil {
		t.Fatal(err)
	}
	str := func(s string) *string { return &s }
	ai := Actor{User: owner.User, Via: "mcp", TokenID: "t"}

	// Own numbers: stored, unique, not like Phoedox's own, found by links.
	old, err := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "adr", Title: "Postgres", ExtRef: "D-639",
		ADR: &ADR{Status: "accepted", Context: "c", Decision: "Postgres"}})
	if err != nil || old.Document.ExtRef != "D-639" {
		t.Fatalf("ext ref: %v %+v", err, old.Document)
	}
	for ref, key := range map[string]string{"D-639": "validation.ext_ref_taken", "d-639": "validation.ext_ref_taken", "ADR-0002": "validation.ext_ref", "two words": "validation.ext_ref"} {
		if _, err := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "adr", Title: "X", ExtRef: ref, ADR: &ADR{Status: "accepted", Decision: "x"}}); invalidKey(err) != key {
			t.Fatalf("%q: %v", ref, err)
		}
	}
	if d, err := s.Document(ctx, owner, p.Slug, "d-639"); err != nil || d.ID != old.Document.ID {
		t.Fatalf("find by own number: %v", err)
	}
	if ds, _ := s.Decisions(ctx, owner, p.Slug, "", "D-639"); len(ds) != 1 || ds[0].ExtRef != "D-639" {
		t.Fatalf("decision log: %+v", ds)
	}
	if hits, _ := s.Search(ctx, owner, "D-639", p.Slug, 10); len(hits) == 0 || hits[0].ID != old.Document.ID {
		t.Fatalf("search by own number: %+v", hits)
	}

	// The original German text is not a machine translation, even from an AI client.
	tr, err := s.SaveTranslation(ctx, ai, p.Slug, old.Document.Slug, "de", TranslationInput{Title: "Postgres", Decision: str("Postgres"), Original: true})
	if err != nil || tr.Machine || !tr.Original {
		t.Fatalf("original: %v %+v", err, tr)
	}
	if tr, _ := s.SaveTranslation(ctx, ai, p.Slug, old.Document.Slug, "de", TranslationInput{Title: "Postgres", Decision: str("Postgres")}); !tr.Machine || tr.Original {
		t.Fatalf("AI translation: %+v", tr)
	}

	// A newer decision partly supersedes it: the older one changes status
	// only, so its translation stays current.
	nw, err := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "adr", Title: "Postgres 17", ExtRef: "D-640",
		ADR: &ADR{Status: "accepted", Decision: "17", Supersedes: "D-639", SupersedesPartly: true, SupersedesScope: "only the version"}})
	if err != nil {
		t.Fatal(err)
	}
	o, _ := s.LocalizedDocument(ctx, owner, p.Slug, old.Document.Slug, "de")
	if o.Version < 2 || o.Translation == nil || o.Translation.SourceVersion != o.Version {
		t.Fatalf("translation outdated by a status change: v%d %+v", o.Version, o.Translation)
	}
	// The scope of the partial supersede is translated.
	if _, err := s.SaveTranslation(ctx, owner, p.Slug, nw.Document.Slug, "de", TranslationInput{Title: "Postgres 17", Decision: str("17"), SupersedesScope: str("nur die Version")}); err != nil {
		t.Fatal(err)
	}
	n, _ := s.LocalizedDocument(ctx, owner, p.Slug, "D-640", "de")
	var c ADR
	_ = json.Unmarshal(n.Content, &c)
	if c.SupersedesScope != "nur die Version" || c.Supersedes == "" {
		t.Fatalf("scope: %+v", c)
	}
	// A text change still makes the translation outdated.
	if _, err := s.UpdateDocument(ctx, owner, p.Slug, "D-640", DocumentPatch{ADR: &ADR{Status: "accepted", Decision: "17.1", Supersedes: "D-639", SupersedesPartly: true, SupersedesScope: "only the version"}}); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.LocalizedDocument(ctx, owner, p.Slug, "D-640", "de"); n.Translation == nil || n.Translation.SourceVersion >= n.Version {
		t.Fatalf("text change must outdate: %+v", n.Translation)
	}
	// The own number can change and be removed.
	if res, err := s.UpdateDocument(ctx, owner, p.Slug, "D-640", DocumentPatch{ExtRef: str("")}); err != nil || res.Document.ExtRef != "" {
		t.Fatalf("remove ext ref: %v", err)
	}
	if _, err := s.Document(ctx, owner, p.Slug, "D-640"); err == nil {
		t.Fatal("removed number still found")
	}
	_ = store.DocADR
}
