package blocks

import (
	"strings"
	"testing"
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
	if len(ps) > 0 || c.xKind() != "time" || !strings.Contains(ChartText(c), "Barricades in Berlin") {
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

func TestRegistry(t *testing.T) {
	if d, ok := Get("chart"); !ok || !d.Board {
		t.Fatal("chart not registered for boards")
	}
	if ps := Check("nope", []byte("x")); len(ps) != 1 || !strings.Contains(ps[0].Message, "unknown block kind") {
		t.Fatalf("unknown: %v", ps)
	}
	if ps := Check("chart", []byte(`{"points":[]}`)); len(ps) != 1 || ps[0].Kind != "chart" {
		t.Fatalf("kind: %v", ps)
	}
}
