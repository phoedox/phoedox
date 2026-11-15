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
	}}
	out := WhiteboardSVG(wb, "Board", "/files/")
	for _, want := range []string{
		`NAS &lt;&amp;&gt;`, `var(--tc-blue)`, `var(--tb-green)`, `marker-end="url(#wb-head)"`,
		`points="108,25 350,25"`, // from the box outline (with a gap) to the fixed point
		`href="https://example.org/x?a=1&amp;b=2"`, `>Docs<`, `>Line 2<`, `var(--tb-yellow)`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s in\n%s", want, out)
		}
	}
	if WhiteboardSVG(&core.Whiteboard{}, "Empty", "") != "" {
		t.Error("empty board should draw nothing")
	}
}
