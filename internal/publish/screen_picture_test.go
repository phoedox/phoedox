package publish

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/phoedox/phoedox/internal/core"
)

// An HTML screen with a picture from the browser shows the picture in its box.
func TestScreenPictureDrawn(t *testing.T) {
	red := image.NewRGBA(image.Rect(0, 0, 200, 100))
	for x := 0; x < 200; x++ {
		for y := 0; y < 100; y++ {
			red.Set(x, y, color.RGBA{220, 30, 30, 255})
		}
	}
	wb := &core.Whiteboard{Prototypes: []core.WBPrototype{{ID: "p", Name: "P"}}, Elements: []core.WBElement{
		{ID: "s", Type: "frame", Text: "S", X: 100, Y: 50, W: 400, H: 200, Order: "a0", Screen: &core.WBScreen{Proto: "p", Device: "custom", HTML: "<p>x</p>"}},
	}}
	pic, info, err := WhiteboardPNG(wb, PNGOptions{Frame: "s", Screen: func(core.WBElement, bool) (image.Image, bool) { return red, true }})
	if err != nil {
		t.Fatal(err)
	}
	img, _ := png.Decode(bytes.NewReader(pic))
	// The middle of the screen in pixels.
	cx := int((300 - info.Area[0]) * info.Scale)
	cy := int((150 - info.Area[1]) * info.Scale)
	r, g, b, _ := img.At(cx, cy).RGBA()
	if r>>8 < 180 || g>>8 > 80 || b>>8 > 80 {
		t.Fatalf("middle of the screen is %d,%d,%d, not the picture", r>>8, g>>8, b>>8)
	}
}

// An image element shows its picture in its box.
func TestImageElementDrawn(t *testing.T) {
	red := image.NewRGBA(image.Rect(0, 0, 200, 100))
	for x := 0; x < 200; x++ {
		for y := 0; y < 100; y++ {
			red.Set(x, y, color.RGBA{220, 30, 30, 255})
		}
	}
	wb := &core.Whiteboard{Elements: []core.WBElement{
		{ID: "f", Type: "frame", Text: "F", X: 0, Y: 0, W: 600, H: 400, Order: "a0"},
		{ID: "i", Type: "image", File: "x", X: 100, Y: 50, W: 400, H: 200, Order: "a1", Frame: "f"},
	}}
	pic, info, err := WhiteboardPNG(wb, PNGOptions{Frame: "f", Image: func(string) (image.Image, error) { return red, nil }})
	if err != nil {
		t.Fatal(err)
	}
	img, _ := png.Decode(bytes.NewReader(pic))
	cx := int((300 - info.Area[0]) * info.Scale)
	cy := int((150 - info.Area[1]) * info.Scale)
	r, g, b, _ := img.At(cx, cy).RGBA()
	if r>>8 < 180 || g>>8 > 80 || b>>8 > 80 {
		t.Fatalf("middle of the image box is %d,%d,%d, not the picture", r>>8, g>>8, b>>8)
	}
}
