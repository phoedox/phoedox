package publish

import (
	"bytes"
	"errors"
	"image/png"
	"os"
	"testing"

	"github.com/phoedox/phoedox/internal/core"
)

func testBoard() *core.Whiteboard {
	half := 0.5
	return &core.Whiteboard{
		Prototypes: []core.WBPrototype{{ID: "p1", Name: "Shop"}},
		Elements: []core.WBElement{
			{ID: "f1", Type: "frame", X: -20, Y: -40, W: 900, H: 330, Text: "1. Sign-in", Order: "a0"},
			{ID: "r1", Type: "rect", X: 0, Y: 0, W: 220, H: 110, Text: "Web app", Fill: "blue", Stroke: "blue", Seed: 1234, Order: "a1"},
			{ID: "e1", Type: "ellipse", X: 320, Y: 0, W: 200, H: 110, Text: "API", Stroke: "#0f8a7e", Fill: "#c9f2ec", Seed: 99, Order: "a2"},
			{ID: "d1", Type: "diamond", X: 620, Y: -10, W: 200, H: 130, Text: "Valid?", Fill: "yellow", Seed: 7, Order: "a3"},
			{ID: "a1", Type: "arrow", Points: [][2]float64{{0, 0}, {1, 1}}, Start: &core.WBBinding{Element: "r1"}, End: &core.WBBinding{Element: "e1"}, Seed: 5, Order: "a4"},
			{ID: "a2", Type: "arrow", Points: [][2]float64{{0, 0}, {1, 1}}, Start: &core.WBBinding{Element: "e1"}, End: &core.WBBinding{Element: "d1", FX: &half, FY: &half}, Text: "check", Seed: 6, Dash: "dashed", Order: "a5"},
			{ID: "s1", Type: "sticky", X: 0, Y: 160, W: 200, H: 110, Text: "Ask the parents about the font size", Order: "a6"},
			{ID: "t1", Type: "text", X: 260, Y: 170, W: 300, Text: "Plain text, red", Stroke: "red", Order: "a7"},
			{ID: "p1", Type: "pen", Points: [][2]float64{{600, 200}, {640, 170}, {700, 230}, {760, 180}}, Stroke: "purple", Order: "a8"},
			{ID: "sc", Type: "frame", X: 1000, Y: 0, W: 300, H: 560, Text: "Start", Screen: &core.WBScreen{Proto: "p1", Device: "phone"}, Order: "b0"},
			{ID: "u1", Type: "ui", X: 1020, Y: 40, W: 260, H: 44, Text: "Sign in", UI: &core.WBUI{Comp: "button"}, Order: "b1"},
			{ID: "u2", Type: "ui", X: 1020, Y: 100, W: 260, H: 40, UI: &core.WBUI{Comp: "input", Placeholder: "E-mail"}, Order: "b2"},
			{ID: "u3", Type: "ui", X: 1020, Y: 160, W: 260, H: 132, UI: &core.WBUI{Comp: "list", Options: []string{"Milk", "Butter", "Bananas"}}, Order: "b3"},
			{ID: "u4", Type: "ui", X: 1000, Y: 500, W: 300, H: 60, UI: &core.WBUI{Comp: "navbar", Options: []string{"Lists", "Items", "More"}}, Order: "b4"},
			{ID: "em", Type: "embed", X: 0, Y: 320, W: 300, H: 90, URL: "https://example.org", Kind: "link", Order: "c0"},
		},
	}
}

func TestWhiteboardPNG(t *testing.T) {
	for _, dark := range []bool{false, true} {
		b, info, err := WhiteboardPNG(testBoard(), PNGOptions{Dark: dark, Max: 1200})
		if err != nil {
			t.Fatal(err)
		}
		img, err := png.Decode(bytes.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		if w, h := img.Bounds().Dx(), img.Bounds().Dy(); max(w, h) > 1200 || w != info.Width || h != info.Height || w < 600 {
			t.Fatalf("size %dx%d, info %+v", w, h, info)
		}
		if info.Simplified["embed"] != 1 {
			t.Errorf("simplified: %v", info.Simplified)
		}
		if out := os.Getenv("WB_PNG_OUT"); out != "" {
			name := out + "/board-light.png"
			if dark {
				name = out + "/board-dark.png"
			}
			_ = os.WriteFile(name, b, 0o644)
		}
	}
	// One frame only, by its title.
	b, info, err := WhiteboardPNG(testBoard(), PNGOptions{Frame: "start"})
	if err != nil || len(b) == 0 {
		t.Fatal(err)
	}
	if info.Area[0] != 1000-24 || info.Area[2] != 300+48 {
		t.Errorf("frame area %v", info.Area)
	}
	if out := os.Getenv("WB_PNG_OUT"); out != "" {
		_ = os.WriteFile(out+"/board-frame.png", b, 0o644)
	}
	if _, _, err := WhiteboardPNG(testBoard(), PNGOptions{Frame: "nope"}); !errors.Is(err, ErrNoFrame) {
		t.Errorf("missing frame: %v", err)
	}
}

// The strokes are the editor's: Rough.js with the element's seed gives the
// same first point as in the browser (web: rough.generator().rectangle(0, 0,
// 220, 110, {seed: 1234, roughness: 1.1, bowing: 1, strokeWidth: 2})).
func TestRoughSeeded(t *testing.T) {
	a, err := roughShape("rectangle", roughOpts(core.WBElement{Seed: 1234}, false), 0, 0, 220, 110)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := roughShape("rectangle", roughOpts(core.WBElement{Seed: 1234}, false), 0, 0, 220, 110)
	if len(a) == 0 || len(a[0].Ops) == 0 || a[0].Ops[0].Data[0] != b[0].Ops[0].Data[0] {
		t.Fatalf("not deterministic: %v", a)
	}
	c, _ := roughShape("rectangle", roughOpts(core.WBElement{Seed: 1235}, false), 0, 0, 220, 110)
	if c[0].Ops[0].Data[0] == a[0].Ops[0].Data[0] {
		t.Error("another seed gives the same strokes")
	}
}

func TestThemeTokens(t *testing.T) {
	light, dark := themes()
	for _, k := range []string{"bg", "fg", "muted", "line", "surface", "accent", "tc-red", "tb-yellow"} {
		if _, ok := light[k]; !ok {
			t.Errorf("light theme lacks %s", k)
		}
	}
	if light["bg"] == dark["bg"] || light["tc-blue"] == dark["tc-blue"] {
		t.Error("dark theme not read")
	}
	if c := light.stroke("#102030"); c.R != 0x10 || c.G != 0x20 || c.B != 0x30 {
		t.Errorf("custom colour %v", c)
	}
}
