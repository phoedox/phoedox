package core

import (
	"fmt"
	"math"
	"strings"
	"unicode/utf8"
)

// Layout of draw_screens: containers (column, row, grid) are laid out by
// the server into plain, absolutely placed elements, so the result stays
// editable by hand on the whiteboard.

// screenNode is an element with its items (the nesting of ScreenElement …
// ScreenLeaf flattened into one type).
type screenNode struct {
	ScreenBase
	kids []screenNode
}

func screenNodeOf(e ScreenElement) screenNode {
	n := screenNode{ScreenBase: e.ScreenBase}
	for _, c := range e.Items {
		k := screenNode{ScreenBase: c.ScreenBase}
		for _, g := range c.Items {
			gg := screenNode{ScreenBase: g.ScreenBase}
			for _, l := range g.Items {
				gg.kids = append(gg.kids, screenNode{ScreenBase: l.ScreenBase})
			}
			k.kids = append(k.kids, gg)
		}
		n.kids = append(n.kids, k)
	}
	return n
}

// laidOut is an element with its place relative to the screen. bg marks
// the surface drawn behind a container.
type laidOut struct {
	ScreenBase
	x, y, w, h float64
	bg         bool
	stacked    bool // placed by the layout (not by x/y)
}

// textStyles: font size per text style.
var textStyles = map[string]float64{"title": 28, "heading": 22, "subheading": 18, "body": 15, "caption": 13, "label": 12}

func (n screenNode) container() bool { return n.Layout != "" }

func (n screenNode) surface() bool {
	return n.container() && (n.Style == "card" || n.Fill != "" || n.Stroke != "")
}

// textHeight estimates the height of a text wrapped at width w.
func textHeight(text string, size, w float64) float64 {
	if size <= 0 {
		size = 20
	}
	perLine := math.Max(1, math.Floor(w/(size*0.55)))
	lines := 0.0
	for _, p := range strings.Split(text, "\n") {
		lines += math.Max(1, math.Ceil(float64(utf8.RuneCountInString(p))/perLine))
	}
	return math.Ceil(size*1.3*lines) + 4
}

// naturalWidth is the width of an item that does not take the full width.
func naturalWidth(n screenNode) float64 {
	if n.W > 0 {
		return n.W
	}
	switch n.Comp {
	case "icon":
		return 32
	case "avatar":
		return 56
	case "badge":
		return math.Max(64, float64(utf8.RuneCountInString(n.Text))*8+28)
	}
	return 0
}

// itemHeight is the height of a leaf at width w.
func itemHeight(n screenNode, w float64) float64 {
	if n.H > 0 {
		return n.H
	}
	if n.Comp == "" && (n.Type == "" || n.Type == "text") {
		return textHeight(n.Text, fontOf(n.ScreenBase), w)
	}
	if n.Comp == "card" {
		// Title plus wrapped body.
		title, body, _ := strings.Cut(n.Text, "\n")
		return math.Max(80, 28+textHeight(title, 16, w-28)+textHeight(body, 14, w-28))
	}
	return compHeight(ScreenElement{ScreenBase: n.ScreenBase})
}

// fontOf is the font size of a text: its own, its style's or 20.
func fontOf(b ScreenBase) float64 {
	if b.FontSize > 0 {
		return b.FontSize
	}
	if s, ok := textStyles[b.Style]; ok {
		return s
	}
	return 20
}

// layout places n's items in the box (x, y, w) and returns them with the
// height used. A container with a surface adds it behind its items.
func layout(n screenNode, x, y, w float64) ([]laidOut, float64) {
	if !n.container() {
		ww := w
		if nw := naturalWidth(n); nw > 0 {
			ww = math.Min(nw, w)
		}
		h := itemHeight(n, ww)
		return []laidOut{{ScreenBase: n.ScreenBase, x: x, y: y, w: ww, h: h, stacked: true}}, h
	}
	pad := n.Pad
	if pad == 0 && n.surface() {
		pad = 16
	}
	gap := n.Gap
	if gap == 0 {
		gap = 12
	}
	ix, iy, iw := x+pad, y+pad, math.Max(1, w-2*pad)
	var out []laidOut
	used := 0.0
	switch n.Layout {
	case "row":
		widths := rowWidths(n.kids, iw, gap)
		cx := ix
		var cells [][]laidOut
		var hs []float64
		for i, k := range n.kids {
			items, h := layout(k, cx, iy, widths[i])
			cells, hs = append(cells, items), append(hs, h)
			used = math.Max(used, h)
			cx += widths[i] + gap
		}
		// Containers stretch to the row's height; leaves are centred.
		for i, items := range cells {
			if n.kids[i].container() {
				stretch(items, hs[i], used)
			} else {
				for j := range items {
					items[j].y += (used - hs[i]) / 2
				}
			}
			out = append(out, items...)
		}
	case "grid":
		cols := n.Cols
		if cols <= 0 {
			cols = 2
		}
		cw := (iw - gap*float64(cols-1)) / float64(cols)
		cy := iy
		for r := 0; r < len(n.kids); r += cols {
			rowH := 0.0
			var cells [][]laidOut
			var hs []float64
			for c := 0; c < cols && r+c < len(n.kids); c++ {
				items, h := layout(n.kids[r+c], ix+float64(c)*(cw+gap), cy, cw)
				cells, hs = append(cells, items), append(hs, h)
				rowH = math.Max(rowH, h)
			}
			for i, items := range cells {
				if n.kids[r+i].container() {
					stretch(items, hs[i], rowH)
				}
				out = append(out, items...)
			}
			cy += rowH + gap
			used = cy - gap - iy
		}
	default: // column
		cy := iy
		for _, k := range n.kids {
			items, h := layout(k, ix, cy, iw)
			out = append(out, items...)
			cy += h + gap
		}
		if len(n.kids) > 0 {
			used = cy - gap - iy
		}
	}
	total := used + 2*pad
	if n.H > total {
		total = n.H
	}
	if n.surface() {
		// A plain card surface, or a rounded rectangle in palette colours.
		bg := ScreenBase{Fill: n.Fill, Stroke: n.Stroke, Actions: n.Actions, Type: "rect"}
		if bg.Fill == "" && bg.Stroke == "" {
			bg.Type, bg.Comp = "", "card"
		} else if bg.Stroke == "" {
			bg.Stroke = bg.Fill
		}
		out = append([]laidOut{{ScreenBase: bg, x: x, y: y, w: w, h: total, bg: true, stacked: true}}, out...)
	}
	return out, total
}

// stretch makes the surface of a container (the first item, when it is
// one) as tall as its row.
func stretch(items []laidOut, h, to float64) {
	if len(items) > 0 && items[0].bg && to > h {
		items[0].h = to
	}
}

// rowWidths shares the width of a row: fixed widths first, the rest by flex.
func rowWidths(kids []screenNode, w, gap float64) []float64 {
	out := make([]float64, len(kids))
	free := w - gap*float64(max(0, len(kids)-1))
	flex := 0.0
	for i, k := range kids {
		if nw := naturalWidth(k); nw > 0 && k.Flex == 0 {
			out[i] = nw
			free -= nw
		} else {
			flex += flexOf(k)
		}
	}
	for i, k := range kids {
		if out[i] == 0 {
			out[i] = math.Max(24, free*flexOf(k)/math.Max(flex, 1e-9))
		}
	}
	return out
}

func flexOf(k screenNode) float64 {
	if k.Flex > 0 {
		return k.Flex
	}
	return 1
}

// screenLayout lays out the elements of a screen of width w: stacked from
// the top in a column (16 px margins, 12 px gaps), with x/y elements where
// they say and navigation bars at the bottom (placed by the caller, as the
// screen may still grow). It returns the elements and the bottom of the
// stacked content.
func screenLayout(items []ScreenElement, w float64) ([]laidOut, float64) {
	var out []laidOut
	cy := 24.0
	for _, it := range items {
		n := screenNodeOf(it)
		if it.Comp == "navbar" && it.Y == nil {
			h := n.H
			if h == 0 {
				h = compHeight(it)
			}
			out = append(out, laidOut{ScreenBase: n.ScreenBase, x: 0, w: w, h: h})
			continue
		}
		if it.X != nil || it.Y != nil {
			ex, ey := 16.0, cy
			if it.X != nil {
				ex = *it.X
			}
			if it.Y != nil {
				ey = *it.Y
			}
			bw := w - 32
			if it.X != nil {
				bw = w - ex - 16
			}
			if nw := naturalWidth(n); nw > 0 {
				bw = nw
			}
			items, h := layout(n, ex, ey, math.Max(1, bw))
			for i := range items {
				items[i].stacked = false
			}
			out = append(out, items...)
			if it.Y == nil {
				cy += h + 12
			}
			continue
		}
		items, h := layout(n, 16, cy, w-32)
		out = append(out, items...)
		cy += h + 12
	}
	return out, cy - 12
}

// layoutWarnings lists problems a person would see: elements outside the
// screen and placed elements that cover each other.
func layoutWarnings(screen string, items []laidOut, w, h float64) []string {
	var out []string
	label := func(e laidOut) string {
		what := e.Comp
		if what == "" {
			what = e.Type
		}
		if what == "" {
			what = "text"
		}
		t := e.Text
		if utf8.RuneCountInString(t) > 30 {
			t = string([]rune(t)[:30]) + "…"
		}
		return fmt.Sprintf("%s %q", what, t)
	}
	for _, e := range items {
		if e.x < -1 || e.y < -1 || e.x+e.w > w+1 || e.y+e.h > h+1 {
			out = append(out, fmt.Sprintf("screen %q: %s lies outside the screen (%.0f×%.0f)", screen, label(e), w, h))
		}
	}
	shape := func(e laidOut) bool {
		return e.bg || e.Type == "rect" || e.Type == "ellipse" || e.Type == "sticky" || e.Type == "line"
	}
	for i := 0; i < len(items); i++ {
		for j := i + 1; j < len(items); j++ {
			a, b := items[i], items[j]
			if shape(a) || shape(b) || (a.stacked && b.stacked) {
				continue
			}
			ow := math.Min(a.x+a.w, b.x+b.w) - math.Max(a.x, b.x)
			oh := math.Min(a.y+a.h, b.y+b.h) - math.Max(a.y, b.y)
			if ow <= 2 || oh <= 2 {
				continue
			}
			if ow*oh > 0.15*math.Min(a.w*a.h, b.w*b.h) {
				out = append(out, fmt.Sprintf("screen %q: %s overlaps %s", screen, label(a), label(b)))
			}
		}
	}
	if len(out) > 20 {
		out = append(out[:20], fmt.Sprintf("… and %d more", len(out)-20))
	}
	return out
}
