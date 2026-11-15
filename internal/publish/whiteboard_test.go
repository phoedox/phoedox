package publish

import (
	"strings"
	"testing"

	"github.com/phoedox/phoedox/internal/core"
)

func TestWhiteboardSVG(t *testing.T) {
	half := 0.5
	wb := &core.Whiteboard{Elements: []core.WBElement{
		{ID: "a", Type: "rect", X: 0, Y: 0, W: 100, H: 50, Text: "NAS <&>", Stroke: "blue", Fill: "green"},
		{ID: "b", Type: "ellipse", X: 300, Y: 0, W: 100, H: 50},
		{ID: "l", Type: "arrow", Points: [][2]float64{{0, 0}, {0, 0}}, Start: &core.WBBinding{Element: "a"}, End: &core.WBBinding{Element: "b", FX: &half, FY: &half}, Text: "nightly"},
		{ID: "e", Type: "embed", X: 0, Y: 100, W: 300, H: 100, URL: "https://example.org/x?a=1&b=2", Kind: "link", Preview: &core.WBPreview{Title: "Docs", Site: "example.org"}},
		{ID: "s", Type: "sticky", X: 500, Y: 0, W: 100, H: 100, Text: "Line 1\nLine 2"},
		{ID: "o", Type: "ellipse", X: 0, Y: 300, W: 120, H: 60, Text: "Storage box off-site (weekly, encrypted)"},
		{ID: "c", Type: "line", Round: true, Points: [][2]float64{{0, 0}, {50, 80}, {100, 0}}},
	}}
	out := WhiteboardSVG(wb, "Board", "/files/")
	for _, want := range []string{
		`NAS &lt;&amp;&gt;`, `var(--tc-blue)`, `var(--tb-green)`,
		`d="M108,25 L350,25"`, // from the box outline (with a gap) to the fixed point
		`href="https://example.org/x?a=1&amp;b=2"`, `>Docs<`, `>Line 2<`, `var(--tb-yellow)`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s in\n%s", want, out)
		}
	}
	// Text wraps inside shapes like in the editor; round lines are curves.
	if !strings.Contains(out, `>Storage</tspan>`) || !strings.Contains(out, `>encrypted)</tspan>`) {
		t.Errorf("ellipse text should wrap into several lines:\n%s", out)
	}
	if !strings.Contains(out, " C") {
		t.Error("round line should be a curve")
	}
	if WhiteboardSVG(&core.Whiteboard{}, "Empty", "") != "" {
		t.Error("empty board should draw nothing")
	}
}
