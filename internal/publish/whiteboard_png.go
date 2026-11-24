package publish

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io/fs"
	"math"
	"slices"
	"strings"
	"sync"

	"github.com/tdewolff/canvas"
	"github.com/tdewolff/canvas/renderers/rasterizer"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"

	"github.com/phoedox/phoedox/internal/core"
	"github.com/phoedox/phoedox/web"
)

// WhiteboardPNG draws a whiteboard as a picture for AI clients that want to
// see a board (whiteboard_screenshot): shapes, lines and arrows with the
// editor's hand-drawn strokes (Rough.js with each element's seed), text in
// the app's font, sticky notes, frames, prototype screens with their
// components, images; embeds, entries and blocks as cards.

// PNGOptions choose what to draw.
type PNGOptions struct {
	// Frame: id or title of a frame (or prototype screen); only its area.
	Frame string
	Dark  bool
	// Max is the longest side in pixels (default 1568, the most AI models
	// look at without scaling down).
	Max int
	// Image loads a project file for image elements (nil: placeholders).
	Image func(file string) (image.Image, error)
}

// PNGInfo describes the picture.
type PNGInfo struct {
	Width, Height int
	// Area is the part of the board shown: x, y, w, h in board units.
	Area [4]float64
	// Scale is pixels per board unit.
	Scale float64
	// Simplified counts elements drawn only as a card or box, by type.
	Simplified map[string]int
}

// ErrNoFrame: the frame asked for is not on the board.
var ErrNoFrame = errors.New("no such frame")

const pngDefaultMax = 1568

var (
	fontsOnce sync.Once
	fontSans  *canvas.FontFamily
	fontErr   error
)

// sans is the app's text font (IBM Plex Sans from the built web app), or the
// Go fonts when the web app is not built (tests).
func sans() (*canvas.FontFamily, error) {
	fontsOnce.Do(func() {
		ff := canvas.NewFontFamily("sans")
		regular, bold := goregular.TTF, gobold.TTF
		if d := web.Dist(); d != nil {
			if b, err := fs.ReadFile(d, "fonts/ibm-plex-sans-latin-400-normal.woff2"); err == nil {
				regular = b
			}
			if b, err := fs.ReadFile(d, "fonts/ibm-plex-sans-latin-600-normal.woff2"); err == nil {
				bold = b
			}
		}
		if fontErr = ff.LoadFont(regular, 0, canvas.FontRegular); fontErr != nil {
			return
		}
		if fontErr = ff.LoadFont(bold, 0, canvas.FontBold); fontErr != nil {
			return
		}
		fontSans = ff
	})
	return fontSans, fontErr
}

// pt converts a size in board units (CSS pixels) to the font's points; the
// canvas works in millimetres.
func pt(px float64) float64 { return px * 72 / 25.4 }

type wbPainter struct {
	ctx    *canvas.Context
	th     theme
	ff     *canvas.FontFamily
	byID   map[string]core.WBElement
	opt    PNGOptions
	info   *PNGInfo
	alpha  float64
	protos map[string]string
}

// WhiteboardPNG renders a board (or one frame of it) as PNG.
func WhiteboardPNG(wb *core.Whiteboard, o PNGOptions) ([]byte, PNGInfo, error) {
	info := PNGInfo{Simplified: map[string]int{}}
	if o.Max <= 0 {
		o.Max = pngDefaultMax
	}
	ff, err := sans()
	if err != nil {
		return nil, info, err
	}
	light, dark := themes()
	th := light
	if o.Dark {
		th = dark
	}
	els := slices.Clone(wb.Elements)
	slices.SortStableFunc(els, func(a, b core.WBElement) int { return strings.Compare(a.Order, b.Order) })
	// Screens first, as in the editor: their components lie on top.
	slices.SortStableFunc(els, func(a, b core.WBElement) int {
		return boolInt(b.Screen != nil) - boolInt(a.Screen != nil)
	})
	byID := map[string]core.WBElement{}
	for _, e := range els {
		byID[e.ID] = e
	}
	// The area: one frame, or everything.
	var ax, ay, aw, ah float64
	if o.Frame != "" {
		var f *core.WBElement
		for i := range els {
			e := &els[i]
			if e.Type == "frame" && (e.ID == o.Frame || strings.EqualFold(strings.TrimSpace(e.Text), strings.TrimSpace(o.Frame))) {
				f = e
				break
			}
		}
		if f == nil {
			return nil, info, ErrNoFrame
		}
		ax, ay, aw, ah = f.X-24, f.Y-36, f.W+48, f.H+60
	} else {
		minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
		grow := func(x, y float64) {
			minX, minY, maxX, maxY = math.Min(minX, x), math.Min(minY, y), math.Max(maxX, x), math.Max(maxY, y)
		}
		for _, e := range els {
			if e.Type == "line" || e.Type == "arrow" {
				for _, p := range linePoints(e, byID) {
					grow(p[0], p[1])
				}
				continue
			}
			if len(e.Points) > 0 {
				for _, p := range e.Points {
					grow(p[0], p[1])
				}
				continue
			}
			top := e.Y
			if e.Screen != nil || e.Type == "frame" {
				top -= 24 // the title above
			}
			grow(e.X, top)
			grow(e.X+e.W, e.Y+e.H)
		}
		if math.IsInf(minX, 0) {
			minX, minY, maxX, maxY = 0, 0, 400, 300
		}
		ax, ay, aw, ah = minX-40, minY-40, maxX-minX+80, maxY-minY+80
	}
	scale := math.Min(float64(o.Max)/aw, float64(o.Max)/ah)
	scale = math.Min(scale, 2)
	info.Area = [4]float64{ax, ay, aw, ah}
	info.Scale = scale

	c := canvas.New(aw, ah)
	ctx := canvas.NewContext(c)
	ctx.SetCoordSystem(canvas.CartesianIV)
	bg := th.get("canvas", "bg")
	ctx.SetFillColor(bg)
	ctx.SetStrokeColor(canvas.Transparent)
	ctx.DrawPath(0, 0, canvas.Rectangle(aw, ah))
	ctx.Translate(-ax, -ay)

	protos := map[string]string{}
	for _, p := range wb.Prototypes {
		protos[p.ID] = p.Name
	}
	pt := &wbPainter{ctx: ctx, th: th, ff: ff, byID: byID, opt: o, info: &info, protos: protos}
	for _, e := range els {
		if err := pt.draw(e); err != nil {
			return nil, info, fmt.Errorf("element %s: %w", e.ID, err)
		}
	}
	img := rasterizer.Draw(c, canvas.DPMM(scale), canvas.DefaultColorSpace)
	info.Width, info.Height = img.Bounds().Dx(), img.Bounds().Dy()
	var buf bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(&buf, img); err != nil {
		return nil, info, err
	}
	return buf.Bytes(), info, nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// col applies the element's opacity.
func (p *wbPainter) col(c color.RGBA) color.RGBA {
	if p.alpha <= 0 || p.alpha >= 1 {
		return c
	}
	a := p.alpha
	return color.RGBA{uint8(float64(c.R) * a), uint8(float64(c.G) * a), uint8(float64(c.B) * a), uint8(float64(c.A) * a)}
}

// roughOpts are the editor's Rough.js options (web/src/components/whiteboard.ts opts()).
func roughOpts(e core.WBElement, fill bool) map[string]any {
	seed := e.Seed
	if seed == 0 {
		seed = 1
	}
	o := map[string]any{"seed": seed, "roughness": 1.1, "bowing": 1, "strokeWidth": strokeW(e), "fillStyle": "solid", "disableMultiStroke": e.Smooth}
	if e.Smooth {
		o["roughness"], o["bowing"] = 0, 0
	}
	if fill && e.Fill != "" {
		o["fill"] = e.Fill
	}
	return o
}

func strokeW(e core.WBElement) float64 {
	if e.Width > 0 {
		return e.Width
	}
	return 2
}

func (p *wbPainter) dashes(e core.WBElement) []float64 {
	switch e.Dash {
	case "dashed":
		return []float64{10, 8}
	case "dotted":
		return []float64{2, 6}
	}
	return nil
}

// sets draws Rough.js sets: fills first, then strokes.
func (p *wbPainter) sets(sets []roughSet, stroke color.RGBA, width float64, fill color.RGBA, dash []float64) {
	for _, s := range sets {
		path := &canvas.Path{}
		for _, o := range s.Ops {
			switch {
			case o.Op == "move" && len(o.Data) >= 2:
				path.MoveTo(o.Data[0], o.Data[1])
			case o.Op == "lineTo" && len(o.Data) >= 2:
				path.LineTo(o.Data[0], o.Data[1])
			case o.Op == "bcurveTo" && len(o.Data) >= 6:
				path.CubeTo(o.Data[0], o.Data[1], o.Data[2], o.Data[3], o.Data[4], o.Data[5])
			}
		}
		switch s.Type {
		case "fillPath":
			path.Close()
			p.ctx.SetFillColor(p.col(fill))
			p.ctx.SetStrokeColor(canvas.Transparent)
		case "path":
			p.ctx.SetFillColor(canvas.Transparent)
			p.ctx.SetStrokeColor(p.col(stroke))
			p.ctx.SetStrokeWidth(width)
			p.ctx.SetStrokeCapper(canvas.RoundCap)
			p.ctx.SetStrokeJoiner(canvas.RoundJoin)
			p.ctx.SetDashes(0, dash...)
		default:
			continue
		}
		p.ctx.DrawPath(0, 0, path)
		p.ctx.SetDashes(0)
	}
}

func (p *wbPainter) rough(e core.WBElement, shape string, fill bool, args ...any) error {
	sets, err := roughShape(shape, roughOpts(e, fill), args...)
	if err != nil {
		return err
	}
	p.sets(sets, p.th.stroke(e.Stroke), strokeW(e), p.th.fill(e.Fill), p.dashes(e))
	return nil
}

// text draws wrapped text into a box; valign top or centre.
func (p *wbPainter) text(s string, x, y, w, h, size float64, c color.RGBA, halign canvas.TextAlign, top, bold bool) {
	s = strings.TrimSpace(s)
	if s == "" || w <= 0 {
		return
	}
	style := canvas.FontRegular
	if bold {
		style = canvas.FontBold
	}
	face := p.ff.Face(pt(size), p.col(c), style, canvas.FontNormal)
	valign := canvas.Center
	if top {
		valign = canvas.Top
	}
	if h <= 0 {
		// No height: as many lines as the text needs (height 0 = no limit).
		p.ctx.DrawText(x, y, canvas.NewTextBox(face, s, w, 0, halign, canvas.Top, nil))
		return
	}
	tb := canvas.NewTextBox(face, s, w, h, halign, valign, nil)
	if !top && h > 0 {
		// Centre on y + h/2 even when the text is taller than the box.
		tb = canvas.NewTextBox(face, s, w, 0, halign, canvas.Top, nil)
		th := tb.Bounds().H()
		p.ctx.DrawText(x, y+(h-th)/2, tb)
		return
	}
	p.ctx.DrawText(x, y, tb)
}

func (p *wbPainter) roundRect(x, y, w, h, r float64, fill, stroke color.RGBA, width float64, dash []float64) {
	p.ctx.SetFillColor(p.col(fill))
	p.ctx.SetStrokeColor(p.col(stroke))
	p.ctx.SetStrokeWidth(width)
	p.ctx.SetDashes(0, dash...)
	p.ctx.DrawPath(x, y, canvas.RoundedRectangle(w, h, r))
	p.ctx.SetDashes(0)
}

func (p *wbPainter) draw(e core.WBElement) error {
	p.alpha = e.Opacity
	defer func() { p.alpha = 0 }()
	size := e.FontSize
	if size == 0 {
		size = 20
	}
	x, y, w, h := e.X, e.Y, e.W, e.H
	fg := p.th.get("fg")
	switch e.Type {
	case "frame":
		if e.Screen != nil {
			p.screen(e)
			return nil
		}
		p.roundRect(x, y, w, h, 14, canvas.Transparent, p.th.get("line"), 1.5, []float64{6, 6})
		title := e.Text
		if title == "" {
			title = "Frame"
		}
		p.text(title, x+12, y+6, w-24, 0, 13, p.th.get("muted"), canvas.Left, true, true)
	case "ui":
		p.component(e)
	case "rect":
		if err := p.rough(e, "rectangle", true, x, y, w, h); err != nil {
			return err
		}
		p.text(e.Text, x+8, y, w-16, h, size, fg, canvas.Center, false, false)
	case "ellipse":
		if err := p.rough(e, "ellipse", true, x+w/2, y+h/2, w, h); err != nil {
			return err
		}
		p.text(e.Text, x+w*0.15, y, w*0.7, h, size, fg, canvas.Center, false, false)
	case "diamond":
		cx, cy := x+w/2, y+h/2
		if err := p.rough(e, "polygon", true, [][]float64{{cx, y}, {x + w, cy}, {cx, y + h}, {x, cy}}); err != nil {
			return err
		}
		p.text(e.Text, cx-w*0.275, y, w*0.55, h, size, fg, canvas.Center, false, false)
	case "sticky":
		f := e.Fill
		if f == "" {
			f = "yellow"
		}
		p.roundRect(x, y, w, h, 4, p.th.fill(f), canvas.Transparent, 0, nil)
		fs := e.FontSize
		if fs == 0 {
			fs = 18
		}
		p.text(e.Text, x+12, y+12, w-24, h-24, fs, fg, canvas.Left, true, false)
	case "text":
		fs := e.FontSize
		if fs == 0 {
			fs = 24
		}
		align := canvas.Left
		switch e.Align {
		case "center":
			align = canvas.Center
		case "right":
			align = canvas.Right
		}
		p.text(e.Text, x, y, math.Max(w, 60), 0, fs, p.th.stroke(e.Stroke), align, true, false)
	case "pen":
		pts := e.Points
		if len(pts) == 0 {
			return nil
		}
		path := &canvas.Path{}
		path.MoveTo(pts[0][0], pts[0][1])
		for i := 1; i < len(pts)-1; i++ {
			path.QuadTo(pts[i][0], pts[i][1], (pts[i][0]+pts[i+1][0])/2, (pts[i][1]+pts[i+1][1])/2)
		}
		if len(pts) > 1 {
			path.LineTo(pts[len(pts)-1][0], pts[len(pts)-1][1])
		} else {
			path.LineTo(pts[0][0]+0.01, pts[0][1])
		}
		p.ctx.SetFillColor(canvas.Transparent)
		p.ctx.SetStrokeColor(p.col(p.th.stroke(e.Stroke)))
		p.ctx.SetStrokeWidth(strokeW(e) + 1.5)
		p.ctx.SetStrokeCapper(canvas.RoundCap)
		p.ctx.SetStrokeJoiner(canvas.RoundJoin)
		p.ctx.DrawPath(0, 0, path)
	case "line", "arrow":
		return p.line(e)
	case "image":
		p.roundRect(x, y, w, h, 6, p.th.get("surface2"), canvas.Transparent, 0, nil)
		if p.opt.Image != nil && e.File != "" {
			if img, err := p.opt.Image(e.File); err == nil && img != nil {
				b := img.Bounds()
				s := math.Min(w/float64(b.Dx()), h/float64(b.Dy()))
				iw, ih := float64(b.Dx())*s, float64(b.Dy())*s
				// DrawImage takes the resolution in pixels per millimetre (board unit).
				p.ctx.DrawImage(x+(w-iw)/2, y+(h-ih)/2+ih, img, canvas.DPMM(1/s))
				return nil
			}
		}
		p.info.Simplified["image"]++
		p.text("Image", x, y, w, h, 13, p.th.get("muted"), canvas.Center, false, false)
	case "embed":
		title, site := e.URL, e.Kind
		if e.Preview != nil {
			if e.Preview.Title != "" {
				title = e.Preview.Title
			}
			if e.Preview.Site != "" {
				site = e.Preview.Site
			}
			if e.Preview.Status != "" {
				site += " · " + e.Preview.Status
			}
		}
		if e.Kind == "app" && title == "" {
			title = "App " + e.Doc
		}
		p.card(e, title, site)
	case "entity":
		name := e.Text
		if name == "" {
			name = e.Entity
		}
		p.roundRect(x, y, w, h, 3, p.th.get("surface"), p.th.stroke(e.Stroke), strokeW(e), p.dashes(e))
		cy := y + h/2
		p.roundRect(x+12, cy-19, 38, 38, 10, p.th.get("accent"), canvas.Transparent, 0, nil)
		if name != "" {
			p.text(strings.ToUpper(string([]rune(name)[:1])), x+12, cy-19, 38, 38, 16, p.th.get("surface"), canvas.Center, false, true)
		}
		p.text(name, x+60, cy-20, w-72, 0, 16, fg, canvas.Left, true, true)
		p.text("@"+e.Entity, x+60, cy+3, w-72, 0, 12, p.th.get("muted"), canvas.Left, true, false)
	case "block", "chart":
		kind := e.Kind
		if e.Type == "chart" || kind == "" {
			kind = "chart"
		}
		first := strings.TrimSpace(strings.SplitN(strings.TrimSpace(e.Source), "\n", 2)[0])
		p.card(e, strings.ToUpper(kind[:1])+kind[1:]+" block", first)
	default:
		p.info.Simplified[e.Type]++
	}
	return nil
}

// card: an embed, block or other element shown as a titled box.
func (p *wbPainter) card(e core.WBElement, title, sub string) {
	p.info.Simplified[e.Type]++
	p.roundRect(e.X, e.Y, e.W, e.H, 10, p.th.get("surface"), p.th.get("line"), 1, nil)
	p.text(title, e.X+12, e.Y+12, e.W-24, 0, 14, p.th.get("fg"), canvas.Left, true, true)
	if sub != "" {
		p.text(sub, e.X+12, e.Y+e.H-26, e.W-24, 0, 11, p.th.get("muted"), canvas.Left, true, false)
	}
}

func (p *wbPainter) line(e core.WBElement) error {
	pts := linePoints(e, p.byID)
	if len(pts) < 2 {
		return nil
	}
	n := len(pts)
	js := make([][]float64, n)
	for i, q := range pts {
		js[i] = []float64{q[0], q[1]}
	}
	var err error
	switch {
	case n == 2:
		err = p.rough(e, "line", false, pts[0][0], pts[0][1], pts[1][0], pts[1][1])
	case e.Round:
		err = p.rough(e, "curve", false, js)
	default:
		err = p.rough(e, "linearPath", false, js)
	}
	if err != nil {
		return err
	}
	if e.Type == "arrow" {
		a := pts[n-2]
		if e.Round && n > 2 {
			a = [2]float64{pts[n-1][0] - (pts[n-1][0]-pts[n-2][0])*0.25, pts[n-1][1] - (pts[n-1][1]-pts[n-2][1])*0.25}
		}
		p2 := pts[n-1]
		ang, l := math.Atan2(p2[1]-a[1], p2[0]-a[0]), 14.0
		head := [][]float64{{p2[0] - l*math.Cos(ang-0.45), p2[1] - l*math.Sin(ang-0.45)}, {p2[0], p2[1]}, {p2[0] - l*math.Cos(ang+0.45), p2[1] - l*math.Sin(ang+0.45)}}
		if err := p.rough(e, "linearPath", false, head); err != nil {
			return err
		}
	}
	if e.Text != "" {
		k := (n - 1) / 2
		mx, my := (pts[k][0]+pts[k+1][0])/2, (pts[k][1]+pts[k+1][1])/2
		lines := wrapText(e.Text, 240, 18)
		longest := 0
		for _, l := range lines {
			longest = max(longest, len([]rune(l)))
		}
		wd := math.Min(250, float64(longest)*9.4+14)
		hh := float64(len(lines)) * 20.7
		p.roundRect(mx-wd/2, my-hh/2-4, wd, hh+8, 6, p.th.get("canvas", "bg"), canvas.Transparent, 0, nil)
		p.text(e.Text, mx-125, my-hh/2-4, 250, hh+8, 18, p.th.get("muted"), canvas.Center, false, false)
	}
	return nil
}

// screen: a prototype screen as a device with its name above.
func (p *wbPainter) screen(e core.WBElement) {
	r := 14.0
	switch e.Screen.Device {
	case "phone":
		r = 40
	case "tablet":
		r = 26
	case "desktop":
		r = 10
	}
	fill := p.th.get("surface")
	if e.Fill != "" {
		fill = p.th.fill(e.Fill)
	}
	p.roundRect(e.X, e.Y, e.W, e.H, r, fill, p.th.get("line"), 1.5, nil)
	title := e.Text
	if name := p.protos[e.Screen.Proto]; name != "" {
		title += " · " + name
	}
	if e.Screen.Overlay != "" {
		title += " (" + e.Screen.Overlay + ")"
	}
	p.text(title, e.X+4, e.Y-22, e.W, 0, 12, p.th.get("muted"), canvas.Left, true, true)
}

// component: a prototype component in its box, readable rather than exact.
func (p *wbPainter) component(e core.WBElement) {
	u := e.UI
	if u == nil {
		return
	}
	x, y, w, h := e.X, e.Y, e.W, e.H
	fg, muted, line, surface, surface2, accent := p.th.get("fg"), p.th.get("muted"), p.th.get("line"), p.th.get("surface"), p.th.get("surface2"), p.th.get("accent")
	label := e.Text
	size := e.FontSize
	if size == 0 {
		size = 15
	}
	switch u.Comp {
	case "button":
		bg, tc, st := accent, p.th.get("on-color"), color.RGBA(canvas.Transparent)
		switch u.Variant {
		case "secondary":
			bg, tc, st = surface2, fg, line
		case "ghost":
			bg, tc = color.RGBA(canvas.Transparent), accent
		case "danger":
			bg = p.th.get("bad")
		}
		p.roundRect(x, y, w, h, 8, bg, st, 1, nil)
		p.text(label, x+6, y, w-12, h, size, tc, canvas.Center, false, true)
	case "link":
		p.text(label, x, y, w, h, size, accent, canvas.Left, false, false)
	case "input", "textarea", "select":
		p.roundRect(x, y, w, h, 8, surface, line, 1, nil)
		v, c := u.Value, fg
		if v == "" {
			v, c = u.Placeholder, muted
		}
		if v == "" && u.Comp == "select" && len(u.Options) > 0 {
			v, c = u.Options[0], fg
		}
		if label != "" && v == "" {
			v, c = label, muted
		}
		if u.Comp == "select" {
			v += "  ▾"
		}
		p.text(v, x+10, y, w-20, map[bool]float64{true: 0, false: h}[u.Comp == "textarea"], size, c, canvas.Left, u.Comp == "textarea", false)
	case "checkbox", "radio", "switch":
		bw := 18.0
		if u.Comp == "switch" {
			bw = 34
		}
		r := 4.0
		if u.Comp != "checkbox" {
			r = 9
		}
		p.roundRect(x, y+(h-18)/2, bw, 18, r, surface, line, 1.5, nil)
		p.text(label, x+bw+8, y, w-bw-8, h, size, fg, canvas.Left, false, false)
	case "slider", "progress":
		p.roundRect(x, y+h/2-3, w, 6, 3, surface2, canvas.Transparent, 0, nil)
		p.roundRect(x, y+h/2-3, w*0.45, 6, 3, accent, canvas.Transparent, 0, nil)
	case "tabs", "navbar":
		if u.Comp == "navbar" {
			p.roundRect(x, y, w, h, 0, surface, line, 1, nil)
		}
		n := len(u.Options)
		if n == 0 {
			p.text(label, x, y, w, h, size, fg, canvas.Center, false, false)
			break
		}
		cw := w / float64(n)
		for i, o := range u.Options {
			c := muted
			if i == 0 {
				c = accent
			}
			p.text(o, x+float64(i)*cw, y, cw, h, 13, c, canvas.Center, false, i == 0)
		}
	case "list", "table":
		p.roundRect(x, y, w, h, 8, surface, line, 1, nil)
		rows := u.Options
		if len(rows) == 0 && u.Data != "" {
			rows = []string{"(" + u.Data + ")"}
		}
		rh := math.Min(44, h/math.Max(1, float64(len(rows))))
		for i, o := range rows {
			ry := y + float64(i)*rh
			if ry+rh > y+h+0.5 {
				break
			}
			if i > 0 {
				p.roundRect(x+1, ry, w-2, 1, 0, line, canvas.Transparent, 0, nil)
			}
			p.text(o, x+12, ry, w-24, rh, 14, fg, canvas.Left, false, false)
		}
	case "card", "toast":
		p.roundRect(x, y, w, h, 12, surface, line, 1, nil)
		p.text(label, x+12, y+10, w-24, h-20, size, fg, canvas.Left, true, false)
	case "badge":
		p.roundRect(x, y, w, h, h/2, p.th.get("accent-soft", "surface2"), canvas.Transparent, 0, nil)
		p.text(label, x+4, y, w-8, h, 12, accent, canvas.Center, false, true)
	case "avatar", "icon":
		p.roundRect(x, y, w, h, math.Min(w, h)/2, surface2, line, 1, nil)
		t := label
		if t == "" {
			t = u.Icon
		}
		p.text(t, x, y, w, h, 11, muted, canvas.Center, false, false)
	case "chart":
		p.roundRect(x, y, w, h, 8, surface, line, 1, nil)
		path := &canvas.Path{}
		path.MoveTo(x+12, y+h-16)
		path.LineTo(x+w*0.35, y+h*0.55)
		path.LineTo(x+w*0.6, y+h*0.65)
		path.LineTo(x+w-12, y+16)
		p.ctx.SetFillColor(canvas.Transparent)
		p.ctx.SetStrokeColor(p.col(accent))
		p.ctx.SetStrokeWidth(2)
		p.ctx.DrawPath(0, 0, path)
	default:
		p.info.Simplified["ui:"+u.Comp]++
		p.roundRect(x, y, w, h, 6, canvas.Transparent, line, 1, []float64{4, 4})
		p.text(label, x, y, w, h, size, fg, canvas.Center, false, false)
	}
}
