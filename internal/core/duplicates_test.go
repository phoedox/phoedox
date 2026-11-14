package core

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/phoedox/phoedox/internal/store"
)

func TestDuplicatesAndMerge(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, _ := setup(t)
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Netz"})
	_ = s.SetMember(ctx, owner, p.Slug, "reader@x.de", store.RoleReader)
	for _, e := range []EntityInput{
		{Kind: "server", Name: "NAS", Slug: "nas", Attrs: map[string]any{"hostname": "nas.local", "location": "Keller"}},
		{Kind: "device", Name: "Synology", Slug: "ha-synology", Attrs: map[string]any{"mac": "AA:BB:CC:DD:EE:01", "hostname": "NAS", "model": "DS920+"}},
		{Kind: "device", Name: "Synology LAN2", Slug: "syn2", Attrs: map[string]any{"mac": "aabb.ccdd.ee01"}},
		{Kind: "room", Name: "Keller", Slug: "keller"},
		{Kind: "switch", Name: "Switch", Slug: "sw"},
	} {
		if _, err := s.UpsertEntity(ctx, owner, p.Slug, e); err != nil {
			t.Fatal(err)
		}
	}
	dups, err := s.Duplicates(ctx, reader, p.Slug)
	if err != nil || len(dups) != 2 {
		t.Fatalf("duplicates: %v %+v", err, dups)
	}
	reasons := map[string]bool{}
	for _, d := range dups {
		reasons[d.Reason] = true
	}
	if !reasons["mac"] || !reasons["hostname"] {
		t.Fatalf("reasons: %+v", dups)
	}

	_, _ = s.Link(ctx, owner, p.Slug, EdgeInput{From: "ha-synology", Rel: "located_in", To: "keller"})
	_, _ = s.Link(ctx, owner, p.Slug, EdgeInput{From: "nas", Rel: "located_in", To: "keller"})
	_, _ = s.Link(ctx, owner, p.Slug, EdgeInput{From: "ha-synology", Rel: "wired_to", To: "sw"})
	_, _ = s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "markdown", Title: "Backup", Markdown: "Das @ha-synology sichert."})

	if _, err := s.MergeEntities(ctx, reader, p.Slug, "nas", "ha-synology"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("readers cannot merge: %v", err)
	}
	if _, err := s.MergeEntities(ctx, owner, p.Slug, "nas", "nas"); invalidKey(err) != "validation.merge_self" {
		t.Fatalf("self: %v", err)
	}
	keep, err := s.MergeEntities(ctx, owner, p.Slug, "nas", "ha-synology")
	if err != nil {
		t.Fatal(err)
	}
	attrs := map[string]any{}
	_ = json.Unmarshal(keep.Attrs, &attrs)
	if attrs["hostname"] != "nas.local" || attrs["model"] != "DS920+" || attrs["aliases"] != "ha-synology" {
		t.Fatalf("attrs: %v", attrs)
	}
	d, _ := s.Entity(ctx, owner, p.Slug, "nas")
	rels := map[string]int{}
	for _, e := range d.Edges {
		rels[e.Rel+">"+e.ToSlug]++
	}
	if rels["located_in>keller"] != 1 || rels["wired_to>sw"] != 1 || len(d.Documents) != 1 {
		t.Fatalf("moved: %v docs %d", rels, len(d.Documents))
	}
	if _, err := s.Entity(ctx, owner, p.Slug, "ha-synology"); err == nil {
		t.Fatal("merged entry must be gone")
	}
}
