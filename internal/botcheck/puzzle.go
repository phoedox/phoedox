package botcheck

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
)

// Visible challenges. Images are drawn on the server from random shapes
// (no stock photos), so they cannot be looked up.

const (
	sliderW, sliderH = 300, 150
	pieceSize        = 44
	rotateSize       = 160
)

func dataURL(img image.Image) (string, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return "", err
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

func hsl(h, s, l float64) color.RGBA {
	c := (1 - math.Abs(2*l-1)) * s
	x := c * (1 - math.Abs(math.Mod(h/60, 2)-1))
	m := l - c/2
	var r, g, b float64
	switch {
	case h < 60:
		r, g, b = c, x, 0
	case h < 120:
		r, g, b = x, c, 0
	case h < 180:
		r, g, b = 0, c, x
	case h < 240:
		r, g, b = 0, x, c
	case h < 300:
		r, g, b = x, 0, c
	default:
		r, g, b = c, 0, x
	}
	return color.RGBA{uint8((r + m) * 255), uint8((g + m) * 255), uint8((b + m) * 255), 255}
}

func blend(dst *image.RGBA, x, y int, c color.RGBA, a float64) {
	if !(image.Point{x, y}.In(dst.Bounds())) {
		return
	}
	o := dst.RGBAAt(x, y)
	mix := func(p, q uint8) uint8 { return uint8(float64(p)*(1-a) + float64(q)*a) }
	dst.SetRGBA(x, y, color.RGBA{mix(o.R, c.R), mix(o.G, c.G), mix(o.B, c.B), 255})
}

func circle(dst *image.RGBA, cx, cy, r int, c color.RGBA, a float64) {
	for y := cy - r; y <= cy+r; y++ {
		for x := cx - r; x <= cx+r; x++ {
			if (x-cx)*(x-cx)+(y-cy)*(y-cy) <= r*r {
				blend(dst, x, y, c, a)
			}
		}
	}
}

func rect(dst *image.RGBA, x0, y0, x1, y1 int, c color.RGBA, a float64) {
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			blend(dst, x, y, c, a)
		}
	}
}

// noiseScene draws a random background of gradients and shapes.
func noiseScene(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	h0 := float64(randInt(360))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			t := float64(x+y) / float64(w+h)
			img.SetRGBA(x, y, hsl(math.Mod(h0+t*90, 360), 0.55, 0.45+0.15*t))
		}
	}
	for i := 0; i < 18; i++ {
		c := hsl(float64(randInt(360)), 0.6, 0.35+float64(randInt(40))/100)
		if randInt(2) == 0 {
			circle(img, randInt(w), randInt(h), 6+randInt(26), c, 0.55)
		} else {
			x, y := randInt(w), randInt(h)
			rect(img, x, y, x+8+randInt(40), y+8+randInt(30), c, 0.5)
		}
	}
	return img
}

// sliderPuzzle returns a background with a gap and the missing piece; the
// answer is the gap's x position.
func sliderPuzzle() (Visible, int, error) {
	img := noiseScene(sliderW, sliderH)
	x := 70 + randInt(sliderW-pieceSize-80)
	y := 10 + randInt(sliderH-pieceSize-20)
	piece := image.NewRGBA(image.Rect(0, 0, pieceSize, pieceSize))
	draw.Draw(piece, piece.Bounds(), img, image.Point{x, y}, draw.Src)
	white := color.RGBA{255, 255, 255, 255}
	for i := 0; i < pieceSize; i++ {
		for _, p := range []image.Point{{i, 0}, {i, 1}, {i, pieceSize - 1}, {i, pieceSize - 2}, {0, i}, {1, i}, {pieceSize - 1, i}, {pieceSize - 2, i}} {
			piece.SetRGBA(p.X, p.Y, white)
		}
	}
	// The gap: darkened with a light outline.
	rect(img, x, y, x+pieceSize, y+pieceSize, color.RGBA{0, 0, 0, 255}, 0.55)
	for i := 0; i < pieceSize; i++ {
		blend(img, x+i, y, white, 0.7)
		blend(img, x+i, y+pieceSize-1, white, 0.7)
		blend(img, x, y+i, white, 0.7)
		blend(img, x+pieceSize-1, y+i, white, 0.7)
	}
	// A decoy gap, fainter, so edge detection alone is not enough.
	dx := 70 + randInt(sliderW-pieceSize-80)
	if math.Abs(float64(dx-x)) > pieceSize {
		rect(img, dx, y, dx+pieceSize, y+pieceSize, color.RGBA{0, 0, 0, 255}, 0.18)
	}
	bg, err := dataURL(img)
	if err != nil {
		return Visible{}, 0, err
	}
	pc, err := dataURL(piece)
	return Visible{Kind: "slider", Image: bg, Piece: pc, PieceY: y, Width: sliderW, Height: sliderH, Size: pieceSize}, x, err
}

// uprightScene draws a picture with an obvious "up": sky, sun, ground and a
// tree.
func uprightScene(n int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, n, n))
	hue := float64(190 + randInt(30))
	ground := int(float64(n) * (0.62 + float64(randInt(10))/100))
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			if y < ground {
				img.SetRGBA(x, y, hsl(hue, 0.6, 0.55+0.25*float64(y)/float64(ground)))
			} else {
				img.SetRGBA(x, y, hsl(float64(95+randInt(3)), 0.45, 0.35))
			}
		}
	}
	circle(img, n/5+randInt(n/2), n/6+randInt(n/10), n/12, color.RGBA{255, 214, 80, 255}, 1)
	tx := n/3 + randInt(n/3)
	rect(img, tx-4, ground-n/4, tx+4, ground+2, color.RGBA{110, 70, 40, 255}, 1)
	circle(img, tx, ground-n/4-n/10, n/7, hsl(float64(100+randInt(30)), 0.5, 0.3), 1)
	// A house roof shape on the other side.
	hx := tx + n/4
	if hx > n-n/6 {
		hx = tx - n/4
	}
	rect(img, hx-n/12, ground-n/7, hx+n/12, ground, color.RGBA{200, 90, 60, 255}, 1)
	for i := 0; i < n/9; i++ {
		rect(img, hx-n/10+i, ground-n/7-i, hx+n/10-i, ground-n/7-i+1, color.RGBA{120, 50, 40, 255}, 1)
	}
	return img
}

// rotatePuzzle returns a round picture turned by a random angle; the answer
// is the rotation (degrees, clockwise) that turns it upright.
func rotatePuzzle() (Visible, int, error) {
	src := uprightScene(rotateSize)
	angle := 40 + randInt(280)
	out := image.NewRGBA(src.Bounds())
	c := float64(rotateSize-1) / 2
	rad := float64(angle) * math.Pi / 180
	sin, cos := math.Sin(rad), math.Cos(rad)
	for y := 0; y < rotateSize; y++ {
		for x := 0; x < rotateSize; x++ {
			dx, dy := float64(x)-c, float64(y)-c
			if dx*dx+dy*dy > c*c {
				continue // transparent outside the circle
			}
			// Inverse of a clockwise rotation by angle.
			sx := cos*dx + sin*dy + c
			sy := -sin*dx + cos*dy + c
			if sx >= 0 && sy >= 0 && sx < float64(rotateSize) && sy < float64(rotateSize) {
				out.SetRGBA(x, y, src.RGBAAt(int(sx), int(sy)))
			}
		}
	}
	data, err := dataURL(out)
	return Visible{Kind: "rotate", Image: data, Width: rotateSize, Height: rotateSize}, (360 - angle) % 360, err
}
