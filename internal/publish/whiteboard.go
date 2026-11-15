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
		return "var(--tc-" + c + ")"
	}
	fill := func(c string) string {
		if c == "" {
			return "none"
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
	text := func(out *strings.Builder, s string, x, y, size float64, anchor, color string) {
		if s == "" {
			return
		}
		lines := strings.Split(s, "\n")
		y -= float64(len(lines)-1) * size * 0.6
		for i, l := range lines {
			fmt.Fprintf(out, `<text x="%s" y="%s" font-size="%s" text-anchor="%s" dominant-baseline="middle" style="fill:%s">%s</text>`,
				num(x), num(y+float64(i)*size*1.2), num(size), anchor, color, esc(l))
		}
	}
	// End points of lines and arrows follow their bindings.
	anchor := func(b *core.WBBinding, toward [2]float64) ([2]float64, bool) {
		if b == nil {
			return [2]float64{}, false
		}
		t, ok := byID[b.Element]
		if !ok {
			return [2]float64{}, false
		}
		if b.FX != nil && b.FY != nil {
			return [2]float64{t.X + *b.FX*t.W, t.Y + *b.FY*t.H}, true
		}
		cx, cy := t.X+t.W/2, t.Y+t.H/2
		dx, dy := toward[0]-cx, toward[1]-cy
		if dx == 0 && dy == 0 {
			return [2]float64{cx, cy}, true
		}
		// Where the ray from the centre leaves the box (plus a small gap).
		s := math.Min(math.Abs((t.W/2+8)/nz(dx)), math.Abs((t.H/2+8)/nz(dy)))
		return [2]float64{cx + dx*s, cy + dy*s}, true
	}
	var out strings.Builder
	fmt.Fprintf(&out, `<figure class="whiteboard"><svg viewBox="%s %s %s %s" role="img" aria-label="%s" font-family="inherit">`,
		num(minX), num(minY), num(maxX-minX), num(maxY-minY), esc(title))
	out.WriteString(`<defs><marker id="wb-head" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse"><path d="M0 0L10 5L0 10z" fill="context-stroke"/></marker></defs>`)
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
		case "rect", "entity":
			fmt.Fprintf(&out, `<rect x="%s" y="%s" width="%s" height="%s" rx="8" style="stroke:%s;fill:%s"%s%s%s/>`, num(e.X), num(e.Y), num(e.W), num(e.H), stroke(e.Stroke, "gray"), fill(e.Fill), sw, dash(e), op)
			label := e.Text
			if e.Type == "entity" && label == "" {
				label = "@" + e.Entity
			}
			text(&out, label, cx, cy, size, "middle", "var(--fg)")
		case "ellipse":
			fmt.Fprintf(&out, `<ellipse cx="%s" cy="%s" rx="%s" ry="%s" style="stroke:%s;fill:%s"%s%s%s/>`, num(cx), num(cy), num(e.W/2), num(e.H/2), stroke(e.Stroke, "gray"), fill(e.Fill), sw, dash(e), op)
			text(&out, e.Text, cx, cy, size, "middle", "var(--fg)")
		case "diamond":
			fmt.Fprintf(&out, `<polygon points="%s,%s %s,%s %s,%s %s,%s" style="stroke:%s;fill:%s"%s%s%s/>`, num(cx), num(e.Y), num(e.X+e.W), num(cy), num(cx), num(e.Y+e.H), num(e.X), num(cy),
				stroke(e.Stroke, "gray"), fill(e.Fill), sw, dash(e), op)
			text(&out, e.Text, cx, cy, size, "middle", "var(--fg)")
		case "sticky":
			f := e.Fill
			if f == "" {
				f = "yellow"
			}
			fmt.Fprintf(&out, `<rect x="%s" y="%s" width="%s" height="%s" rx="4" style="fill:%s;stroke:none"%s/>`, num(e.X), num(e.Y), num(e.W), num(e.H), fill(f), op)
			text(&out, e.Text, cx, cy, size, "middle", "var(--fg)")
		case "frame":
			fmt.Fprintf(&out, `<rect x="%s" y="%s" width="%s" height="%s" rx="10" style="fill:none;stroke:var(--line)" stroke-width="1.5" stroke-dasharray="6 6"/>`, num(e.X), num(e.Y), num(e.W), num(e.H))
			text(&out, e.Text, e.X+8, e.Y-14, 16, "start", "var(--muted)")
		case "text":
			anchorPos, x := "start", e.X
			switch e.Align {
			case "center":
				anchorPos, x = "middle", cx
			case "right":
				anchorPos, x = "end", e.X+e.W
			}
			text(&out, e.Text, x, e.Y+size*0.6, size, anchorPos, stroke(e.Stroke, "gray"))
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
			fmt.Fprintf(&out, `<a href="%s" rel="noopener external"><rect x="%s" y="%s" width="%s" height="%s" rx="10" style="fill:var(--surface);stroke:var(--line)" stroke-width="1.5"/>`,
				esc(e.URL), num(e.X), num(e.Y), num(e.W), num(e.H))
			text(&out, title, e.X+16, e.Y+28, 16, "start", "var(--fg)")
			text(&out, site, e.X+16, e.Y+52, 13, "start", "var(--muted)")
			out.WriteString(`</a>`)
		case "line", "arrow", "pen":
			pts := slices.Clone(e.Points)
			if e.Type != "pen" && len(pts) >= 2 {
				// Each end aims at its neighbour: the next waypoint, or the
				// other end's anchor (or element centre).
				aim := func(b *core.WBBinding, fallback [2]float64) [2]float64 {
					if b == nil {
						return fallback
					}
					if t, ok := byID[b.Element]; ok {
						if b.FX != nil && b.FY != nil {
							return [2]float64{t.X + *b.FX*t.W, t.Y + *b.FY*t.H}
						}
						return [2]float64{t.X + t.W/2, t.Y + t.H/2}
					}
					return fallback
				}
				last := len(pts) - 1
				towardStart, towardEnd := pts[1], pts[last-1]
				if len(pts) == 2 {
					towardStart, towardEnd = aim(e.End, pts[last]), aim(e.Start, pts[0])
				}
				if p, ok := anchor(e.Start, towardStart); ok {
					pts[0] = p
				}
				if p, ok := anchor(e.End, towardEnd); ok {
					pts[last] = p
				}
			}
			s := make([]string, len(pts))
			for i, p := range pts {
				s[i] = num(p[0]) + "," + num(p[1])
			}
			head := ""
			if e.Type == "arrow" {
				head = ` marker-end="url(#wb-head)"`
			}
			fmt.Fprintf(&out, `<polyline points="%s" style="fill:none;stroke:%s" stroke-linecap="round" stroke-linejoin="round"%s%s%s%s/>`, strings.Join(s, " "), stroke(e.Stroke, "gray"), sw, dash(e), head, op)
			if e.Text != "" && len(pts) >= 2 {
				m := len(pts) / 2
				mx, my := (pts[m-1][0]+pts[m][0])/2, (pts[m-1][1]+pts[m][1])/2
				text(&out, e.Text, mx, my-12, 15, "middle", "var(--muted)")
			}
		}
	}
	out.WriteString(`</svg></figure>`)
	return out.String()
}

func nz(v float64) float64 {
	if v == 0 {
		return 1e-9
	}
	return v
}
