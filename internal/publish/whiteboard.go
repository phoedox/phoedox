package publish

import (
	"fmt"
	"html/template"
	"math"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/phoedox/phoedox/internal/core"
	"github.com/phoedox/phoedox/internal/store"
)

// whiteboardSVG draws a whiteboard as static SVG for publications and the
// emergency copy (ADR-0068): shapes, lines and arrows with their bindings,
// text, sticky notes, frames, images, and embeds as cards with a link
// (never a live frame).
func whiteboardSVG(d store.Document, files string) string {
	wb, err := core.WhiteboardOf(d)
	if err != nil || len(wb.Elements) == 0 {
		return ""
	}
	return WhiteboardSVG(wb, d.Title, files)
}

// WhiteboardSVG renders a board; files is the prefix for project files.
func WhiteboardSVG(wb *core.Whiteboard, title, files string) string {
	esc := template.HTMLEscapeString
	num := func(v float64) string { return strconv.FormatFloat(math.Round(v*10)/10, 'f', -1, 64) }
	els := slices.Clone(wb.Elements)
	slices.SortStableFunc(els, func(a, b core.WBElement) int { return strings.Compare(a.Order, b.Order) })
	byID := map[string]core.WBElement{}
	for _, e := range els {
		byID[e.ID] = e
	}
	// Extent of the board.
	minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	grow := func(x, y float64) {
		minX, minY, maxX, maxY = math.Min(minX, x), math.Min(minY, y), math.Max(maxX, x), math.Max(maxY, y)
	}
	for _, e := range els {
		if len(e.Points) > 0 {
			for _, p := range e.Points {
				grow(p[0], p[1])
			}
			continue
		}
		grow(e.X, e.Y)
		grow(e.X+e.W, e.Y+e.H)
	}
	if math.IsInf(minX, 0) {
		return ""
	}
	pad := 40.0
	minX, minY, maxX, maxY = minX-pad, minY-pad, maxX+pad, maxY+pad
	stroke := func(c, def string) string {
		if c == "" {
			c = def
		}
		if core.WBCustomColor(c) {
			return c
		}
		return "var(--tc-" + c + ")"
	}
	fill := func(c string) string {
		if c == "" {
			return "none"
		}
		if core.WBCustomColor(c) {
			return c
		}
		return "var(--tb-" + c + ")"
	}
	dash := func(e core.WBElement) string {
		switch e.Dash {
		case "dashed":
			return ` stroke-dasharray="10 8"`
		case "dotted":
			return ` stroke-dasharray="2 6"`
		}
		return ""
	}
	width := func(e core.WBElement) float64 {
		if e.Width > 0 {
			return e.Width
		}
		return 2
	}
	// Text as in the editor: wrapped by an estimated character width, the
	// handwriting font, line height 1.15.
	text := func(out *strings.Builder, s string, x, y, maxW, size float64, anchor, color string, top bool, font string) {
		if s == "" {
			return
		}
		lines := wrapText(s, maxW, size)
		lh := size * 1.15
		base := "central"
		if top {
			base = "hanging"
		} else {
			y -= float64(len(lines)-1) * lh / 2
		}
		if font == "" {
			font = hand
		}
		fmt.Fprintf(out, `<text x="%s" y="%s" text-anchor="%s" dominant-baseline="%s" style="font:%spx/1.15 %s;fill:%s">`, num(x), num(y), anchor, base, num(size), font, color)
		for i, l := range lines {
			dy := "0"
			if i > 0 {
				dy = num(lh)
			}
			fmt.Fprintf(out, `<tspan x="%s" dy="%s">%s</tspan>`, num(x), dy, esc(l))
		}
		out.WriteString(`</text>`)
	}
	var out strings.Builder
	fmt.Fprintf(&out, `<figure class="whiteboard"><svg viewBox="%s %s %s %s" role="img" aria-label="%s" font-family="inherit">`,
		num(minX), num(minY), num(maxX-minX), num(maxY-minY), esc(title))
	for _, e := range els {
		op := ""
		if e.Opacity > 0 && e.Opacity < 1 {
			op = fmt.Sprintf(` opacity="%s"`, num(e.Opacity))
		}
		size := e.FontSize
		if size == 0 {
			size = 20
		}
		cx, cy := e.X+e.W/2, e.Y+e.H/2
		sw := fmt.Sprintf(` stroke-width="%s"`, num(width(e)))
		switch e.Type {
		case "rect":
			fmt.Fprintf(&out, `<rect x="%s" y="%s" width="%s" height="%s" rx="3" style="stroke:%s;fill:%s"%s%s%s/>`, num(e.X), num(e.Y), num(e.W), num(e.H), stroke(e.Stroke, "gray"), fill(e.Fill), sw, dash(e), op)
			text(&out, e.Text, cx, cy, e.W-16, size, "middle", "var(--fg)", false, "")
		case "entity":
			fmt.Fprintf(&out, `<g%s><rect x="%s" y="%s" width="%s" height="%s" rx="3" style="stroke:%s;fill:var(--surface)"%s%s/>`, op, num(e.X), num(e.Y), num(e.W), num(e.H), stroke(e.Stroke, "gray"), sw, dash(e))
			fmt.Fprintf(&out, `<rect x="%s" y="%s" width="38" height="38" rx="10" style="fill:var(--accent)"/>`, num(e.X+12), num(cy-19))
			name := e.Text
			if name == "" {
				name = e.Entity
			}
			if name != "" {
				fmt.Fprintf(&out, `<text x="%s" y="%s" text-anchor="middle" dominant-baseline="central" style="font:600 16px var(--font-body);fill:var(--surface)">%s</text>`, num(e.X+31), num(cy), esc(strings.ToUpper(string([]rune(name)[:1]))))
			}
			tx := e.X + 60
			fmt.Fprintf(&out, `<text x="%s" y="%s" dominant-baseline="central" style="font:600 16px var(--font-body);fill:var(--fg)">%s</text>`, num(tx), num(cy-9), esc(clipText(name, e.W-72, 16)))
			fmt.Fprintf(&out, `<text x="%s" y="%s" dominant-baseline="central" style="font:12px var(--font-mono);fill:var(--muted)">%s</text></g>`, num(tx), num(cy+11), esc(clipText("@"+e.Entity, e.W-72, 12)))
		case "ellipse":
			fmt.Fprintf(&out, `<ellipse cx="%s" cy="%s" rx="%s" ry="%s" style="stroke:%s;fill:%s"%s%s%s/>`, num(cx), num(cy), num(e.W/2), num(e.H/2), stroke(e.Stroke, "gray"), fill(e.Fill), sw, dash(e), op)
			text(&out, e.Text, cx, cy, e.W*0.7, size, "middle", "var(--fg)", false, "")
		case "diamond":
			fmt.Fprintf(&out, `<polygon points="%s,%s %s,%s %s,%s %s,%s" style="stroke:%s;fill:%s" stroke-linejoin="round"%s%s%s/>`, num(cx), num(e.Y), num(e.X+e.W), num(cy), num(cx), num(e.Y+e.H), num(e.X), num(cy),
				stroke(e.Stroke, "gray"), fill(e.Fill), sw, dash(e), op)
			text(&out, e.Text, cx, cy, e.W*0.55, size, "middle", "var(--fg)", false, "")
		case "sticky":
			f := e.Fill
			if f == "" {
				f = "yellow"
			}
			ss := e.FontSize
			if ss == 0 {
				ss = 18
			}
			fmt.Fprintf(&out, `<rect x="%s" y="%s" width="%s" height="%s" rx="4" style="fill:%s;stroke:none"%s/>`, num(e.X), num(e.Y), num(e.W), num(e.H), fill(f), op)
			text(&out, e.Text, e.X+12, e.Y+12, e.W-24, ss, "start", "var(--fg)", true, "")
		case "frame":
			fmt.Fprintf(&out, `<rect x="%s" y="%s" width="%s" height="%s" rx="14" style="fill:none;stroke:var(--line)" stroke-width="1.5" stroke-dasharray="6 6"/>`, num(e.X), num(e.Y), num(e.W), num(e.H))
			if e.Text != "" {
				fmt.Fprintf(&out, `<text x="%s" y="%s" style="font:600 13px var(--font-body);fill:var(--muted)">%s</text>`, num(e.X+12), num(e.Y+18), esc(e.Text))
			}
		case "text":
			ts := e.FontSize
			if ts == 0 {
				ts = 24
			}
			anchorPos, x := "start", e.X
			switch e.Align {
			case "center":
				anchorPos, x = "middle", cx
			case "right":
				anchorPos, x = "end", e.X+e.W
			}
			text(&out, e.Text, x, e.Y, math.Max(e.W, 60), ts, anchorPos, stroke(e.Stroke, "gray"), true, "")
		case "image":
			fmt.Fprintf(&out, `<image href="%s%s" x="%s" y="%s" width="%s" height="%s" preserveAspectRatio="xMidYMid meet"%s/>`, esc(files), esc(url.PathEscape(e.File)), num(e.X), num(e.Y), num(e.W), num(e.H), op)
		case "embed":
			title, site := e.URL, ""
			if e.Preview != nil {
				if e.Preview.Title != "" {
					title = e.Preview.Title
				}
				site = e.Preview.Site
				if e.Preview.Status != "" {
					site += " · " + e.Preview.Status
				}
			}
			fmt.Fprintf(&out, `<a href="%s" rel="noopener external"><rect x="%s" y="%s" width="%s" height="%s" rx="10" style="fill:var(--surface);stroke:var(--line)" stroke-width="1"/>`,
				esc(e.URL), num(e.X), num(e.Y), num(e.W), num(e.H))
			text(&out, title, e.X+12, e.Y+12, e.W-24, 14, "start", "var(--fg)", true, "var(--font-body)")
			if site != "" {
				fmt.Fprintf(&out, `<text x="%s" y="%s" style="font:11px var(--font-body);fill:var(--muted)">%s</text>`, num(e.X+10), num(e.Y+e.H-10), esc(clipText(site, e.W-20, 11)))
			}
			out.WriteString(`</a>`)
		case "pen":
			pts := e.Points
			if len(pts) == 0 {
				continue
			}
			var d strings.Builder
			fmt.Fprintf(&d, "M%s,%s", num(pts[0][0]), num(pts[0][1]))
			for i := 1; i < len(pts)-1; i++ {
				fmt.Fprintf(&d, " Q%s,%s %s,%s", num(pts[i][0]), num(pts[i][1]), num((pts[i][0]+pts[i+1][0])/2), num((pts[i][1]+pts[i+1][1])/2))
			}
			if len(pts) > 1 {
				fmt.Fprintf(&d, " L%s,%s", num(pts[len(pts)-1][0]), num(pts[len(pts)-1][1]))
			}
			fmt.Fprintf(&out, `<path d="%s" style="fill:none;stroke:%s" stroke-width="%s" stroke-linecap="round" stroke-linejoin="round"%s/>`, d.String(), stroke(e.Stroke, "gray"), num(width(e)+1.5), op)
		case "line", "arrow":
			pts := linePoints(e, byID)
			if len(pts) < 2 {
				continue
			}
			n := len(pts)
			var d string
			if e.Round && n > 2 {
				d = curvePath(pts, num)
			} else {
				s := make([]string, n)
				for i, p := range pts {
					s[i] = num(p[0]) + "," + num(p[1])
				}
				d = "M" + strings.Join(s, " L")
			}
			fmt.Fprintf(&out, `<g%s><path d="%s" style="fill:none;stroke:%s" stroke-linecap="round" stroke-linejoin="round"%s%s/>`, op, d, stroke(e.Stroke, "gray"), sw, dash(e))
			if e.Type == "arrow" {
				a := pts[n-2]
				if e.Round && n > 2 {
					a = [2]float64{pts[n-1][0] - (pts[n-1][0]-pts[n-2][0])*0.25, pts[n-1][1] - (pts[n-1][1]-pts[n-2][1])*0.25}
				}
				p2 := pts[n-1]
				ang, l := math.Atan2(p2[1]-a[1], p2[0]-a[0]), 14.0
				fmt.Fprintf(&out, `<path d="M%s,%s L%s,%s L%s,%s" style="fill:none;stroke:%s" stroke-linecap="round" stroke-linejoin="round"%s/>`,
					num(p2[0]-l*math.Cos(ang-0.45)), num(p2[1]-l*math.Sin(ang-0.45)), num(p2[0]), num(p2[1]), num(p2[0]-l*math.Cos(ang+0.45)), num(p2[1]-l*math.Sin(ang+0.45)), stroke(e.Stroke, "gray"), sw)
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
				fmt.Fprintf(&out, `<rect x="%s" y="%s" width="%s" height="%s" rx="6" style="fill:var(--canvas, var(--bg))"/>`, num(mx-wd/2), num(my-hh/2-4), num(wd), num(hh+8))
				text(&out, e.Text, mx, my, 240, 18, "middle", "var(--muted)", false, "")
			}
			out.WriteString(`</g>`)
		}
	}
	out.WriteString(`</svg></figure>`)
	return out.String()
}

const hand = "var(--font-hand, var(--font-body))"

// wrapText breaks text into lines like the editor (web/src/whiteboard.ts
// wrapText): an estimated character width of 0.52 × the font size.
func wrapText(text string, maxW, size float64) []string {
	var out []string
	cw := size * 0.52
	for _, para := range strings.Split(text, "\n") {
		cur := ""
		for _, w := range strings.Fields(para) {
			next := w
			if cur != "" {
				next = cur + " " + w
			}
			if float64(len([]rune(next)))*cw > maxW && cur != "" {
				out = append(out, cur)
				cur = w
			} else {
				cur = next
			}
		}
		out = append(out, cur)
	}
	return out
}

// clipText shortens a single line to the width with an ellipsis.
func clipText(s string, maxW, size float64) string {
	r := []rune(s)
	n := int(maxW / (size * 0.56))
	if n < 1 || len(r) <= n {
		return s
	}
	return string(r[:max(1, n-1)]) + "…"
}

func center(e core.WBElement) [2]float64 { return [2]float64{e.X + e.W/2, e.Y + e.H/2} }

// edgePoint is where the ray from the element's centre towards (tx, ty)
// leaves its outline plus a gap (same as the editor).
func edgePoint(e core.WBElement, tx, ty float64) [2]float64 {
	c := center(e)
	dx, dy := tx-c[0], ty-c[1]
	if dx == 0 && dy == 0 {
		return c
	}
	hw, hh := e.W/2+8, e.H/2+8
	var s float64
	switch e.Type {
	case "ellipse":
		s = 1 / math.Sqrt(dx*dx/(hw*hw)+dy*dy/(hh*hh))
	case "diamond":
		s = 1 / (math.Abs(dx)/hw + math.Abs(dy)/hh)
	default:
		s = math.Min(hw/math.Abs(nz(dx)), hh/math.Abs(nz(dy)))
	}
	return [2]float64{c[0] + dx*s, c[1] + dy*s}
}

// anchorPoint is a fixed point on the box, pushed out a little.
func anchorPoint(e core.WBElement, b *core.WBBinding) [2]float64 {
	px, py := e.X+*b.FX*e.W, e.Y+*b.FY*e.H
	c := center(e)
	d := math.Hypot(px-c[0], py-c[1])
	if d == 0 {
		d = 1
	}
	return [2]float64{px + (px-c[0])/d*6, py + (py-c[1])/d*6}
}

// linePoints applies the bindings of a line or arrow (as linePts in the editor).
func linePoints(e core.WBElement, byID map[string]core.WBElement) [][2]float64 {
	pts := slices.Clone(e.Points)
	n := len(pts)
	if n < 2 {
		return pts
	}
	resolve := func(b *core.WBBinding, fallback [2]float64) (core.WBElement, bool, [2]float64) {
		if b == nil {
			return core.WBElement{}, false, fallback
		}
		t, ok := byID[b.Element]
		if !ok {
			return t, false, fallback
		}
		if b.FX != nil && b.FY != nil {
			return t, true, anchorPoint(t, b)
		}
		return t, true, center(t)
	}
	s0, ok0, p0 := resolve(e.Start, pts[0])
	s1, ok1, p1 := resolve(e.End, pts[n-1])
	pts[0], pts[n-1] = p0, p1
	if ok0 && (e.Start.FX == nil || e.Start.FY == nil) {
		pts[0] = edgePoint(s0, pts[1][0], pts[1][1])
	}
	if ok1 && (e.End.FX == nil || e.End.FY == nil) {
		pts[n-1] = edgePoint(s1, pts[n-2][0], pts[n-2][1])
	}
	return pts
}

// curvePath draws a smooth curve through the points (Catmull-Rom as Bézier).
func curvePath(p [][2]float64, num func(float64) string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "M%s,%s", num(p[0][0]), num(p[0][1]))
	for i := 0; i < len(p)-1; i++ {
		p0, p1, p2, p3 := p[max(0, i-1)], p[i], p[i+1], p[min(len(p)-1, i+2)]
		c1 := [2]float64{p1[0] + (p2[0]-p0[0])/6, p1[1] + (p2[1]-p0[1])/6}
		c2 := [2]float64{p2[0] - (p3[0]-p1[0])/6, p2[1] - (p3[1]-p1[1])/6}
		fmt.Fprintf(&b, " C%s,%s %s,%s %s,%s", num(c1[0]), num(c1[1]), num(c2[0]), num(c2[1]), num(p2[0]), num(p2[1]))
	}
	return b.String()
}

func nz(v float64) float64 {
	if v == 0 {
		return 1e-9
	}
	return v
}
