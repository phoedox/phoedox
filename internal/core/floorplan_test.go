package core

import (
	"context"
	"encoding/json"
	"testing"
)

// TestFloorplan covers ADR-0034: validation, mentions and the graph sync
// (located_in by position, part_of floor, room area, own edges only).
func TestFloorplan(t *testing.T) {
	ctx := context.Background()
	s, owner, _, _ := setup(t)
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Haus"})
	for _, e := range []EntityInput{
		{Kind: "floor", Name: "EG", Slug: "eg", Attrs: map[string]any{"level": 0}},
		{Kind: "room", Name: "Küche", Slug: "kueche"},
		{Kind: "room", Name: "Flur", Slug: "flur"},
		{Kind: "router", Name: "Fritzbox", Slug: "fritzbox"},
		{Kind: "device", Name: "Drucker", Slug: "drucker"},
	} {
		if _, err := s.UpsertEntity(ctx, owner, p.Slug, e); err != nil {
			t.Fatal(err)
		}
	}
	// A hand-drawn edge must survive the sync.
	if _, err := s.Link(ctx, owner, p.Slug, EdgeInput{From: "drucker", Rel: "located_in", To: "flur"}); err != nil {
		t.Fatal(err)
	}

	bad := []Floorplan{
		{Rooms: []FloorRoom{{ID: "r1", Points: [][2]float64{{0, 0}, {1, 1}}}}},
		{Rooms: []FloorRoom{{ID: "r1", Points: [][2]float64{{0, 0}, {1, 0}, {1, 1}}, Color: "#fff"}}},
		{Devices: []FloorDevice{{ID: "d1"}}},
		{Devices: []FloorDevice{{ID: "x", Entity: "a"}, {ID: "x", Entity: "b"}}},
		{Width: 2e6},
	}
	for i, fp := range bad {
		if _, err := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "floorplan", Title: "X", Floorplan: &fp}); invalidKey(err) != "validation.floorplan" {
			t.Errorf("bad %d: %v", i, err)
		}
	}

	kitchen := FloorRoom{ID: "r1", Name: "Küche", Entity: "kueche", Color: "green", Points: [][2]float64{{0, 0}, {4000, 0}, {4000, 3000}, {0, 3000}}}
	hall := FloorRoom{ID: "r2", Name: "Flur", Entity: "flur", Points: [][2]float64{{4000, 0}, {6000, 0}, {6000, 3000}, {4000, 3000}}}
	fp := Floorplan{Floor: "eg", Rooms: []FloorRoom{kitchen, hall}, Devices: []FloorDevice{{ID: "d1", Entity: "fritzbox", X: 1000, Y: 1000}, {ID: "d2", Entity: "drucker", X: 1500, Y: 2000}}}
	res, err := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "floorplan", Title: "Erdgeschoss", Floorplan: &fp})
	if err != nil || !res.Applied {
		t.Fatalf("create: %v", err)
	}
	var saved Floorplan
	_ = json.Unmarshal(res.Document.Content, &saved)
	if saved.Width != 20000 || len(saved.Rooms) != 2 {
		t.Fatalf("defaults: %+v", saved)
	}

	rels := func(slug string) map[string]string {
		d, err := s.Entity(ctx, owner, p.Slug, slug)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]string{}
		for _, e := range d.Edges {
			if e.FromSlug == slug {
				out[e.Rel+">"+e.ToSlug] = e.Source
			}
		}
		return out
	}
	if r := rels("fritzbox"); r["located_in>kueche"] != "floorplan" {
		t.Fatalf("router in kitchen: %v", r)
	}
	if r := rels("drucker"); r["located_in>kueche"] != "floorplan" || r["located_in>flur"] != "manual" {
		t.Fatalf("printer: %v", r)
	}
	if r := rels("kueche"); r["part_of>eg"] != "floorplan" {
		t.Fatalf("room on floor: %v", r)
	}
	k, _ := s.Entity(ctx, owner, p.Slug, "kueche")
	attrs := map[string]any{}
	_ = json.Unmarshal(k.Entity.Attrs, &attrs)
	if attrs["area"] != 12.0 {
		t.Fatalf("area: %v", attrs)
	}
	if len(k.Documents) != 1 {
		t.Fatalf("plan is linked to the room: %+v", k.Documents)
	}

	// Moving the router to the hall replaces only the plan's own edge.
	fp.Devices[0].X = 5000
	fp.Devices[1].X = 9000 // outside every room
	if _, err := s.UpdateDocument(ctx, owner, p.Slug, res.Document.Slug, DocumentPatch{Floorplan: &fp}); err != nil {
		t.Fatal(err)
	}
	if r := rels("fritzbox"); r["located_in>flur"] != "floorplan" || r["located_in>kueche"] != "" {
		t.Fatalf("moved router: %v", r)
	}
	if r := rels("drucker"); r["located_in>kueche"] != "" || r["located_in>flur"] != "manual" {
		t.Fatalf("printer outside: %v", r)
	}
}

func TestPolygon(t *testing.T) {
	l := [][2]float64{{0, 0}, {4, 0}, {4, 1}, {1, 1}, {1, 4}, {0, 4}} // L shape
	if a := polygonArea(l); a != 7 {
		t.Fatalf("area %v", a)
	}
	if !inPolygon(0.5, 3, l) || inPolygon(3, 3, l) {
		t.Fatal("point in L")
	}
}
