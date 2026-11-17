package core

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/phoedox/phoedox/internal/store"
)

const vormaerzChart = `{"title": "Pressure of the movement on the old powers",
  "series": [{"points": [{"x": "1815", "y": 1, "label": "1", "title": "Congress of Vienna", "text": "Restoration."},
    {"x": "1848-03-18", "y": 9.5, "label": "15", "title": "Barricades in Berlin"}]}]}`

func TestChartBlocksAndWhiteboardCharts(t *testing.T) {
	ps := CheckBlocks("# Load\n\n```chart\n{\"points\": [{\"x\": 1, \"y\": \"x\"}]}\n```\n\n```chart\n"+vormaerzChart+"\n```\n", nil)
	if len(ps) != 1 || ps[0].Kind != "chart" || ps[0].Block != 1 || ps[0].Line < 3 {
		t.Fatalf("blocks: %+v", ps)
	}
	ctx := context.Background()
	s, owner, _, _ := setup(t)
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Geschichte"})
	el := func(chart string) WBElement {
		return WBElement{ID: "c1", Type: "chart", X: 0, Y: 0, W: 560, H: 360, Chart: json.RawMessage(chart)}
	}
	res, err := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: store.DocWhiteboard, Title: "1848", Whiteboard: &Whiteboard{Elements: []WBElement{el(vormaerzChart)}}})
	if err != nil {
		t.Fatal(err)
	}
	v, err := s.WhiteboardView(ctx, owner, p.Slug, res.Document.Slug)
	if err != nil || len(v.Elements) != 1 || !strings.Contains(string(v.Elements[0].Chart), "Congress of Vienna") {
		t.Fatalf("view: %+v %v", v, err)
	}
	if _, err := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: store.DocWhiteboard, Title: "x", Whiteboard: &Whiteboard{Elements: []WBElement{el(`{"points":[]}`)}}}); invalidKey(err) != "validation.whiteboard_chart" {
		t.Fatalf("bad chart: %v", err)
	}
	// The generic block element: kind and source as in a page.
	res2, err := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: store.DocWhiteboard, Title: "Blocks", Whiteboard: &Whiteboard{Elements: []WBElement{
		{ID: "b1", Type: "block", Kind: "chart", Source: vormaerzChart, W: 560, H: 360}}}})
	if err != nil {
		t.Fatal(err)
	}
	v2, _ := s.WhiteboardView(ctx, owner, p.Slug, res2.Document.Slug)
	if v2.Elements[0].Kind != "chart" || !strings.Contains(v2.Elements[0].Source, "Barricades") {
		t.Fatalf("block view: %+v", v2.Elements[0])
	}
	for _, e := range []WBElement{
		{ID: "b2", Type: "block", Kind: "No Pe", Source: "x", W: 1, H: 1},
		{ID: "b6", Type: "block", Kind: "mermaid", Source: "graph TD", W: 1, H: 1},
		{ID: "b3", Type: "block", Kind: "chart", Source: `{"points":[]}`, W: 1, H: 1},
		{ID: "b4", Type: "rect", Source: "x", W: 1, H: 1},
	} {
		if _, err := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: store.DocWhiteboard, Title: "x" + e.ID, Whiteboard: &Whiteboard{Elements: []WBElement{e}}}); err == nil {
			t.Errorf("%s accepted", e.ID)
		}
	}
}
