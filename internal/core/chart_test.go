package core

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/phoedox/phoedox/internal/store"
)

const vormaerzChart = `{
  "title": "Pressure of the movement on the old powers",
  "x": {"segments": [{"from": "1815", "to": "1847-12", "label": "compressed"}, {"from": "1848-01", "to": "1849-10", "weight": 1.1, "label": "stretched"}]},
  "y": {"ticks": [{"value": 0, "label": "low"}, {"value": 5, "label": "medium"}, {"value": 10, "label": "high"}]},
  "bands": [{"from": "1815", "to": "1848-02", "label": "Restoration", "color": "orange"}, {"from": "1848-02", "to": "1848-05", "label": "March revolution", "color": "red"}],
  "series": [{"points": [
    {"x": "1815", "y": 1, "label": "1", "title": "Congress of Vienna", "text": "Restoration."},
    {"x": "1848-03-18", "y": 9.5, "label": "15", "title": "Barricades in Berlin"}
  ]}, {"dashed": true, "points": [{"x": "1849-08", "y": 0.5}, {"x": "1849-10", "y": 2}]}]
}`

func TestParseChart(t *testing.T) {
	c, ps := ParseChart([]byte(vormaerzChart))
	if len(ps) > 0 || c.xKind() != "time" || !strings.Contains(chartText(c), "Barricades in Berlin") {
		t.Fatalf("valid chart: %v", ps)
	}
	for src, want := range map[string]string{
		"{\n  \"points\": [\n    {\"x\": 1, \"y\": }\n  ]\n}": "line 3",
		`{"points": []}`: "no points",
		`{"points": [{"x": "2026", "y": "high"}]}`:                                                 "must be float64",
		`{"type": "pie", "points": [{"x": 1, "y": 1}]}`:                                            "unknown type",
		`{"points": [{"x": 1, "y": 1, "color": "teal"}]}`:                                          "unknown colour",
		`{"x": {"type": "time"}, "points": [{"x": "May", "y": 1}]}`:                                "does not fit",
		`{"x": {"segments": [{"from": "1849", "to": "1848"}]}, "points": [{"x": "1848", "y": 1}]}`: "end must come after",
		`{"points": [{"y": 1}]}`:                                                                   "x is missing",
	} {
		_, ps := ParseChart([]byte(src))
		got := ""
		if len(ps) > 0 {
			got = ps[0].Message
			if strings.Contains(want, "line") {
				got = "line " + string(rune('0'+ps[0].Line))
			}
		}
		if !strings.Contains(got, want) {
			t.Errorf("%s: %q, want %q", src, got, want)
		}
	}
	// Bars with names, numbers on a number axis, nulls as gaps.
	if _, ps := ParseChart([]byte(`{"type":"bar","points":[{"x":"NAS","y":4},{"x":"PC","y":null}]}`)); len(ps) > 0 {
		t.Fatal(ps)
	}
}

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
}
