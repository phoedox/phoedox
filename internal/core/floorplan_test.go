package core

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/phoedox/phoedox/internal/blob"
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

// TestFloorplanStage2 covers walls, openings, photos and live drafts.
func TestFloorplanStage2(t *testing.T) {
	ctx := context.Background()
	s, owner, _, _ := setup(t)
	dir := t.TempDir()
	s.Blobs, s.TempDir = blob.Local{Dir: filepath.Join(dir, "files")}, filepath.Join(dir, "tmp")
	_ = os.MkdirAll(s.TempDir, 0o700)
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Haus"})
	wall := FloorWall{ID: "w1", A: [2]float64{0, 0}, B: [2]float64{4000, 0}}
	bad := []Floorplan{
		{Walls: []FloorWall{{ID: "w1", A: [2]float64{0, 0}, B: [2]float64{0, 0}}}},
		{Walls: []FloorWall{{ID: "w1", A: [2]float64{0, 0}, B: [2]float64{10, 0}, Thickness: 5}}},
		{Walls: []FloorWall{wall}, Openings: []FloorOpening{{ID: "o1", Wall: "w2", Kind: "door", Offset: 500, Width: 900}}},
		{Walls: []FloorWall{wall}, Openings: []FloorOpening{{ID: "o1", Wall: "w1", Kind: "hatch", Offset: 500, Width: 900}}},
		{Walls: []FloorWall{wall}, Openings: []FloorOpening{{ID: "o1", Wall: "w1", Kind: "door", Offset: 3800, Width: 900}}},
		{Photos: []FloorPhoto{{ID: "p1", File: "nope", X: 1, Y: 1}}},
	}
	for i, fp := range bad {
		if _, err := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "floorplan", Title: "X", Floorplan: &fp}); invalidKey(err) != "validation.floorplan" {
			t.Errorf("bad %d: %v", i, err)
		}
	}
	txt, err := s.UploadFile(ctx, owner, p.Slug, FileInput{Name: "note.txt"}, strings.NewReader("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.PlanImage(ctx, owner, p.Slug, txt.ID); invalidKey(err) != "validation.plan_image" {
		t.Fatalf("plan image of text: %v", err)
	}
	fp := Floorplan{Walls: []FloorWall{wall}, Openings: []FloorOpening{{ID: "o1", Wall: "w1", Kind: "window", Offset: 2000, Width: 1200}},
		Photos: []FloorPhoto{{ID: "p1", File: txt.ID, X: 100, Y: 100, Caption: " Kitchen "}}}
	res, err := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "floorplan", Title: "EG", Floorplan: &fp})
	if err != nil {
		t.Fatal(err)
	}
	var saved Floorplan
	_ = json.Unmarshal(res.Document.Content, &saved)
	if len(saved.Walls) != 1 || saved.Walls[0].Thickness != 120 || len(saved.Openings) != 1 || saved.Photos[0].Caption != "Kitchen" {
		t.Fatalf("saved: %+v", saved)
	}

	// Live editing works on a draft; publishing makes it the plan.
	doc := res.Document
	if _, err := s.LiveDocument(ctx, owner, p.Slug, doc.Slug); err == nil {
		t.Fatal("live without draft")
	}
	d, err := s.StartDraft(ctx, owner, p.Slug, doc.Slug)
	if err != nil || !sameJSON([]byte(d.Markdown), doc.Content) {
		t.Fatalf("start: %v %s", err, d.Markdown)
	}
	if _, err := s.LiveDocument(ctx, owner, p.Slug, doc.Slug); err != nil {
		t.Fatalf("live: %v", err)
	}
	next := saved
	next.Walls = append(next.Walls, FloorWall{ID: "w2", A: [2]float64{4000, 0}, B: [2]float64{4000, 3000}, Thickness: 240})
	if _, err := s.SaveDraft(ctx, owner, p.Slug, doc.Slug, DraftPatch{Floorplan: &Floorplan{Walls: []FloorWall{{ID: "w", A: [2]float64{0, 0}, B: [2]float64{0, 0}}}}}); invalidKey(err) != "validation.floorplan" {
		t.Fatalf("bad draft: %v", err)
	}
	if _, err := s.SaveDraft(ctx, owner, p.Slug, doc.Slug, DraftPatch{Floorplan: &next}); err != nil {
		t.Fatal(err)
	}
	// Readers still see the published plan.
	cur, _ := s.Document(ctx, owner, p.Slug, doc.Slug)
	if cur.Version != doc.Version {
		t.Fatal("draft changed the published plan")
	}
	pub, err := s.PublishDraft(ctx, owner, p.Slug, doc.Slug)
	if err != nil {
		t.Fatal(err)
	}
	var after Floorplan
	_ = json.Unmarshal(pub.Document.Content, &after)
	if len(after.Walls) != 2 || after.Walls[1].Thickness != 240 || pub.Document.Version != doc.Version+1 {
		t.Fatalf("published: %+v v%d", after, pub.Document.Version)
	}
}

// TestFloorplanCables: cable routes are validated and link their ends.
func TestFloorplanCables(t *testing.T) {
	ctx := context.Background()
	s, owner, _, _ := setup(t)
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Haus"})
	for _, e := range []EntityInput{{Kind: "switch", Name: "Switch", Slug: "switch"}, {Kind: "device", Name: "Drucker", Slug: "drucker"}} {
		if _, err := s.UpsertEntity(ctx, owner, p.Slug, e); err != nil {
			t.Fatal(err)
		}
	}
	bad := []Floorplan{
		{Cables: []FloorCable{{ID: "c1", Points: [][3]float64{{0, 0, 0}}}}},
		{Cables: []FloorCable{{ID: "c1", Kind: "fiber", Points: [][3]float64{{0, 0, 0}, {1, 1, 0}}}}},
		{Cables: []FloorCable{{ID: "c1", Points: [][3]float64{{0, 0, 0}, {1, 1, 200000}}}}},
	}
	for i, fp := range bad {
		if _, err := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "floorplan", Title: "X", Floorplan: &fp}); invalidKey(err) != "validation.floorplan" {
			t.Errorf("bad %d: %v", i, err)
		}
	}
	fp := Floorplan{Cables: []FloorCable{{ID: "c1", Label: "Patch 4", From: "drucker", To: "switch", Points: [][3]float64{{100, 100, 300}, {2000, 100, 2400}, {2000, 100, 3200}}}}}
	res, err := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "floorplan", Title: "EG", Floorplan: &fp})
	if err != nil {
		t.Fatal(err)
	}
	var saved Floorplan
	_ = json.Unmarshal(res.Document.Content, &saved)
	if saved.Cables[0].Kind != "network" || saved.Cables[0].Points[2][2] != 3200 {
		t.Fatalf("saved: %+v", saved.Cables)
	}
	d, _ := s.Entity(ctx, owner, p.Slug, "drucker")
	found := false
	for _, e := range d.Edges {
		if e.Rel == "wired_to" && e.ToSlug == "switch" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no wired_to edge: %+v", d.Edges)
	}
}
