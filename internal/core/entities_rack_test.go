package core

import (
	"context"
	"encoding/json"
	"testing"
)

func TestRackPlacement(t *testing.T) {
	ctx := context.Background()
	s, owner, _, _ := setup(t)
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "RZ"})
	_, _ = s.UpsertEntity(ctx, owner, p.Slug, EntityInput{Kind: "rack", Name: "Rack 1", Slug: "rack", Attrs: map[string]any{"height_units": 24}})
	_, _ = s.UpsertEntity(ctx, owner, p.Slug, EntityInput{Kind: "server", Name: "NAS", Slug: "nas"})
	if _, err := s.Link(ctx, owner, p.Slug, EdgeInput{From: "nas", Rel: "mounted_in", To: "rack", Attrs: map[string]any{"u": 0.5}}); invalidKey(err) != "validation.rack_unit" {
		t.Fatalf("bad unit: %v", err)
	}
	if _, err := s.Link(ctx, owner, p.Slug, EdgeInput{From: "nas", Rel: "mounted_in", To: "rack", Attrs: map[string]any{"u": float64(3), "height": float64(2)}}); err != nil {
		t.Fatal(err)
	}
	d, _ := s.Entity(ctx, owner, p.Slug, "rack")
	var attrs map[string]any
	_ = json.Unmarshal(d.Edges[0].Attrs, &attrs)
	if attrs["u"] != 3.0 || attrs["height"] != 2.0 {
		t.Fatalf("attrs: %v", attrs)
	}
}
