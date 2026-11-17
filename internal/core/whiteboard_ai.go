package core

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/rand/v2"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/phoedox/phoedox/internal/store"
)

// Whiteboards for AI clients (ADR-0068): a compact reading of the board,
// operations on elements, diagrams laid out by Phoedox (models are good at
// structure, bad at coordinates) and answers to comments. Writes follow
// the project's AI write mode like other document changes, and everyone on
// the board sees them at once.

// WBView is a whiteboard as AI clients read it.
type WBView struct {
	Document   string        `json:"document"`
	Title      string        `json:"title"`
	Background string        `json:"background,omitempty"`
	Elements   []WBViewItem  `json:"elements"`
	Comments   []WBViewNote  `json:"comments,omitempty"`
	Extent     [4]int        `json:"extent"` // minX, minY, maxX, maxY
	Hint       string        `json:"hint,omitempty"`
	Frames     []WBViewFrame `json:"frames,omitempty"`
	// Prototypes: screens are frames with "screen", components have "ui".
	Prototypes []WBPrototype `json:"prototypes,omitempty"`
}

// WBViewItem is one element, rounded and without styling noise.
type WBViewItem struct {
	ID     string   `json:"id"`
	Type   string   `json:"type"`
	Text   string   `json:"text,omitempty"`
	Box    []int    `json:"box,omitempty"`    // x, y, w, h
	Points [][2]int `json:"points,omitempty"` // lines, arrows, freehand
	From   string   `json:"from,omitempty"`   // bound start element
	To     string   `json:"to,omitempty"`     // bound end element
	Frame  string   `json:"frame,omitempty"`
	Color  string   `json:"color,omitempty"`
	Fill   string   `json:"fill,omitempty"`
	Entity string   `json:"entity,omitempty"`
	Doc    string   `json:"doc,omitempty"`
	URL    string   `json:"url,omitempty"`
	Kind   string   `json:"kind,omitempty"`
	Title  string   `json:"title,omitempty"`  // embeds
	Status string   `json:"status,omitempty"` // code embeds
	// Prototypes.
	Screen  *WBScreen  `json:"screen,omitempty"`
	UI      *WBUI      `json:"ui,omitempty"`
	Actions []WBAction `json:"actions,omitempty"`
	// Charts: the chart JSON (as in a ```chart block).
	Chart json.RawMessage `json:"chart,omitempty"`
}

// WBViewFrame names a frame and lists what is inside.
type WBViewFrame struct {
	ID       string   `json:"id"`
	Title    string   `json:"title,omitempty"`
	Elements []string `json:"elements"`
}

// WBViewNote is an open comment with its replies.
type WBViewNote struct {
	ID          string    `json:"id"`
	At          [2]int    `json:"at"`
	Element     string    `json:"element,omitempty"`
	ElementText string    `json:"elementText,omitempty"`
	Author      string    `json:"author,omitempty"`
	Text        string    `json:"text,omitempty"`
	Handwritten bool      `json:"handwritten,omitempty"`
	Replies     []WBReply `json:"replies,omitempty"`
}

func (s *Service) whiteboardDoc(ctx context.Context, a Actor, ref, docRef, need string) (store.Project, store.Document, *Whiteboard, error) {
	p, doc, err := s.docAccess(ctx, a, ref, docRef, need)
	if err != nil {
		return p, doc, nil, err
	}
	wb, err := WhiteboardOf(doc)
	return p, doc, wb, err
}

// WhiteboardView reads a whiteboard for an AI client: elements in a compact
// form (rounded positions, bindings by id, frames with their content) and
// the open comments with the element they point to.
func (s *Service) WhiteboardView(ctx context.Context, a Actor, ref, docRef string) (WBView, error) {
	_, doc, wb, err := s.whiteboardDoc(ctx, a, ref, docRef, store.RoleReader)
	if err != nil {
		return WBView{}, err
	}
	v := WBView{Document: doc.Slug, Title: doc.Title, Background: wb.Background, Elements: []WBViewItem{}, Prototypes: wb.Prototypes}
	r := func(f float64) int { return int(math.Round(f)) }
	texts := map[string]string{}
	frames := map[string]*WBViewFrame{}
	minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	grow := func(x, y float64) {
		minX, minY, maxX, maxY = math.Min(minX, x), math.Min(minY, y), math.Max(maxX, x), math.Max(maxY, y)
	}
	els := slices.Clone(wb.Elements)
	slices.SortStableFunc(els, func(x, y WBElement) int { return strings.Compare(x.Order, y.Order) })
	for _, e := range els {
		it := WBViewItem{ID: e.ID, Type: e.Type, Text: e.Text, Frame: e.Frame, Color: e.Stroke, Fill: e.Fill, Entity: e.Entity, Doc: e.Doc, URL: e.URL, Kind: e.Kind,
			Screen: e.Screen, UI: e.UI, Actions: e.Actions, Chart: e.Chart}
		if len(e.Points) > 0 {
			step := 1
			if e.Type == "pen" && len(e.Points) > 12 {
				step = len(e.Points) / 12 // the shape of a stroke, not every point
			}
			for i := 0; i < len(e.Points); i += step {
				it.Points = append(it.Points, [2]int{r(e.Points[i][0]), r(e.Points[i][1])})
				grow(e.Points[i][0], e.Points[i][1])
			}
		} else {
			it.Box = []int{r(e.X), r(e.Y), r(e.W), r(e.H)}
			grow(e.X, e.Y)
			grow(e.X+e.W, e.Y+e.H)
		}
		if e.Start != nil {
			it.From = e.Start.Element
		}
		if e.End != nil {
			it.To = e.End.Element
		}
		if e.Preview != nil {
			it.Title, it.Status = e.Preview.Title, e.Preview.Status
		}
		texts[e.ID] = e.Text
		if e.Type == "frame" {
			frames[e.ID] = &WBViewFrame{ID: e.ID, Title: e.Text, Elements: []string{}}
		}
		v.Elements = append(v.Elements, it)
	}
	for _, e := range els {
		if f := frames[e.Frame]; f != nil {
			f.Elements = append(f.Elements, e.ID)
		}
	}
	for _, e := range els {
		if f := frames[e.ID]; f != nil {
			v.Frames = append(v.Frames, *f)
		}
	}
	if !math.IsInf(minX, 0) {
		v.Extent = [4]int{r(minX), r(minY), r(maxX), r(maxY)}
	}
	for _, c := range wb.Comments {
		if c.Resolved {
			continue
		}
		v.Comments = append(v.Comments, WBViewNote{ID: c.ID, At: [2]int{r(c.X), r(c.Y)}, Element: c.Element, ElementText: texts[c.Element],
			Author: c.Name, Text: c.Text, Handwritten: len(c.Ink) > 0, Replies: c.Replies})
	}
	if len(v.Elements) == 0 {
		v.Hint = "The board is empty. draw_diagram places nodes and arrows for you."
	}
	return v, nil
}

// WBOp is one operation of edit_whiteboard.
type WBOp struct {
	Op string `json:"op"` // add | update | delete
	ID string `json:"id,omitempty"`
	// Element: a whole element for add, the changed fields for update
	// (null removes a field).
	Element json.RawMessage `json:"element,omitempty"`
}

// EditWhiteboard applies operations to a board. Added elements get an id,
// a seed and the front-most order; embeds get their kind and a preview;
// deleting an element unbinds arrows pointing to it.
func (s *Service) EditWhiteboard(ctx context.Context, a Actor, ref, docRef string, ops []WBOp) (DocResult, error) {
	p, doc, wb, err := s.whiteboardDoc(ctx, a, ref, docRef, store.RoleEditor)
	if err != nil {
		return DocResult{}, err
	}
	if len(ops) == 0 || len(ops) > 500 {
		return DocResult{}, invalid("validation.whiteboard_ops", nil, "send between 1 and 500 operations")
	}
	index := func(id string) int {
		return slices.IndexFunc(wb.Elements, func(e WBElement) bool { return e.ID == id })
	}
	order := nextOrder(wb)
	for i, op := range ops {
		bad := func(msg string) error {
			return invalid("validation.whiteboard_op", map[string]any{"n": i + 1, "msg": msg}, "operation %d: %s", i+1, msg)
		}
		switch op.Op {
		case "add":
			var e WBElement
			if err := json.Unmarshal(op.Element, &e); err != nil {
				return DocResult{}, bad("element: " + err.Error())
			}
			if e.ID == "" {
				e.ID = op.ID
			}
			if e.ID == "" || index(e.ID) >= 0 {
				e.ID = freeID(wb, "ai")
			}
			s.prepareElement(ctx, a, p, &e)
			e.Order = order()
			wb.Elements = append(wb.Elements, e)
		case "update":
			at := index(op.ID)
			if at < 0 {
				return DocResult{}, bad("no element " + op.ID)
			}
			var patch map[string]json.RawMessage
			if err := json.Unmarshal(op.Element, &patch); err != nil {
				return DocResult{}, bad("element: " + err.Error())
			}
			cur, _ := json.Marshal(wb.Elements[at])
			var m map[string]json.RawMessage
			_ = json.Unmarshal(cur, &m)
			for k, val := range patch {
				if k == "id" {
					continue
				}
				if string(val) == "null" {
					delete(m, k)
				} else {
					m[k] = val
				}
			}
			merged, _ := json.Marshal(m)
			var e WBElement
			if err := json.Unmarshal(merged, &e); err != nil {
				return DocResult{}, bad("element: " + err.Error())
			}
			if _, ok := patch["url"]; ok {
				e.Preview = nil
				e.Kind = ""
			}
			s.prepareElement(ctx, a, p, &e)
			wb.Elements[at] = e
		case "delete":
			at := index(op.ID)
			if at < 0 {
				return DocResult{}, bad("no element " + op.ID)
			}
			wb.Elements = slices.Delete(wb.Elements, at, at+1)
			for j := range wb.Elements {
				e := &wb.Elements[j]
				if e.Start != nil && e.Start.Element == op.ID {
					e.Start = nil
				}
				if e.End != nil && e.End.Element == op.ID {
					e.End = nil
				}
				if e.Frame == op.ID {
					e.Frame = ""
				}
			}
		default:
			return DocResult{}, bad("op must be add, update or delete")
		}
	}
	return s.UpdateDocument(ctx, a, p.Slug, doc.ID, DocumentPatch{Whiteboard: wb})
}

// prepareElement fills what an AI client usually leaves out.
func (s *Service) prepareElement(ctx context.Context, a Actor, p store.Project, e *WBElement) {
	if e.Seed == 0 {
		e.Seed = 1 + rand.IntN(1<<30)
	}
	if (e.Type == "line" || e.Type == "arrow") && len(e.Points) < 2 {
		// Bound ends are computed by the editor; the points are a fallback.
		e.Points = [][2]float64{{e.X, e.Y}, {e.X + e.W, e.Y + e.H}}
		e.X, e.Y, e.W, e.H = 0, 0, 0, 0
	}
	if e.Type == "embed" && e.URL != "" {
		if e.Kind == "" {
			e.Kind = EmbedKind(e.URL)
		}
		if e.Preview == nil {
			pv, _, err := s.EmbedPreview(ctx, a, p.Slug, e.URL)
			if err == nil {
				e.Preview = &pv
			}
		}
		if e.W == 0 || e.H == 0 {
			e.W, e.H = 320, 120
			if e.Kind == "video" {
				e.H = 210
			}
		}
	}
	if e.Type == "sticky" {
		if e.W == 0 || e.H == 0 {
			e.W, e.H = 160, 120
		}
		if e.Fill == "" {
			e.Fill = "yellow"
		}
	}
	if e.Type == "text" && e.W == 0 {
		e.W, e.H = float64(min(600, 12*len([]rune(e.Text))+20)), 32
	}
}

// nextOrder returns a function giving order keys in front of all elements.
func nextOrder(wb *Whiteboard) func() string {
	top := ""
	for _, e := range wb.Elements {
		if e.Order > top {
			top = e.Order
		}
	}
	n := 0
	return func() string {
		n++
		// Fixed width keys sort as strings; "z…" stays in front of older keys.
		return top + "z" + fmt.Sprintf("%04d", n)
	}
}

var idClean = regexp.MustCompile(`[^A-Za-z0-9_-]+`)

func freeID(wb *Whiteboard, base string) string {
	base = idClean.ReplaceAllString(base, "-")
	if len(base) > 30 {
		base = base[:30]
	}
	if base == "" {
		base = "e"
	}
	taken := map[string]bool{}
	for _, e := range wb.Elements {
		taken[e.ID] = true
	}
	if !taken[base] {
		return base
	}
	for i := 2; ; i++ {
		if id := base + "-" + strconv.Itoa(i); !taken[id] {
			return id
		}
	}
}

// DiagramInput is a diagram described by structure; Phoedox lays it out.
type DiagramInput struct {
	Nodes     []DiagramNode  `json:"nodes"`
	Edges     []DiagramEdge  `json:"edges"`
	Groups    []DiagramGroup `json:"groups,omitempty"`
	Direction string         `json:"direction,omitempty"` // right (default) | down
	Title     string         `json:"title,omitempty"`     // a frame around the whole diagram
}

// DiagramNode is a box of a diagram.
type DiagramNode struct {
	ID     string `json:"id"`
	Text   string `json:"text"`
	Shape  string `json:"shape,omitempty"` // rect | ellipse | diamond | sticky
	Color  string `json:"color,omitempty"`
	Entity string `json:"entity,omitempty"`
	Group  string `json:"group,omitempty"`
}

// DiagramEdge is an arrow between two nodes.
type DiagramEdge struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Label  string `json:"label,omitempty"`
	Dashed bool   `json:"dashed,omitempty"`
	Line   bool   `json:"line,omitempty"` // no arrow head
}

// DiagramGroup draws a frame around its nodes.
type DiagramGroup struct {
	ID    string `json:"id"`
	Label string `json:"label,omitempty"`
}

// DrawDiagram lays out nodes and edges (layered, the same idea as the
// topology view), places them right of what is on the board and binds the
// arrows to the boxes. Without docRef it creates a whiteboard titled title.
func (s *Service) DrawDiagram(ctx context.Context, a Actor, ref, docRef, title, category string, in DiagramInput) (DocResult, error) {
	if len(in.Nodes) == 0 || len(in.Nodes) > 300 || len(in.Edges) > 600 || len(in.Groups) > 50 {
		return DocResult{}, invalid("validation.diagram", map[string]any{"msg": "1–300 nodes, up to 600 edges and 50 groups"}, "diagram: 1–300 nodes, up to 600 edges and 50 groups")
	}
	known := map[string]bool{}
	for _, n := range in.Nodes {
		if n.ID == "" || known[n.ID] {
			return DocResult{}, invalid("validation.diagram", map[string]any{"msg": "every node needs its own id"}, "diagram: every node needs its own id")
		}
		known[n.ID] = true
	}
	for _, e := range in.Edges {
		if !known[e.From] || !known[e.To] {
			return DocResult{}, invalid("validation.diagram", map[string]any{"msg": "edge " + e.From + " → " + e.To + " names an unknown node"}, "diagram: edge %s → %s names an unknown node", e.From, e.To)
		}
	}
	wb := &Whiteboard{Elements: []WBElement{}}
	var p store.Project
	var doc store.Document
	var err error
	if docRef != "" {
		p, doc, wb, err = s.whiteboardDoc(ctx, a, ref, docRef, store.RoleEditor)
		if err != nil {
			return DocResult{}, err
		}
	}
	els := layoutDiagram(wb, in)
	wb.Elements = append(wb.Elements, els...)
	if docRef == "" {
		if strings.TrimSpace(title) == "" {
			title = in.Title
		}
		if strings.TrimSpace(title) == "" {
			title = "Diagram"
		}
		return s.CreateDocument(ctx, a, ref, DocumentInput{Type: store.DocWhiteboard, Title: title, Category: category, Whiteboard: wb})
	}
	return s.UpdateDocument(ctx, a, p.Slug, doc.ID, DocumentPatch{Whiteboard: wb})
}

// layoutDiagram turns a diagram into elements: nodes by layer (longest
// path from the sources), ordered within a layer by the mean position of
// their neighbours, right of the existing content.
func layoutDiagram(wb *Whiteboard, in DiagramInput) []WBElement {
	down := in.Direction == "down"
	// Layers: longest path from the sources; edges that close a cycle
	// (found by a depth-first search) are ignored for layering.
	out := map[string][]string{}
	indeg := map[string]int{}
	for _, e := range in.Edges {
		if e.From != e.To {
			out[e.From] = append(out[e.From], e.To)
			indeg[e.To]++
		}
	}
	state := map[string]int{} // 1 on the stack, 2 done
	var topo []string         // reverse post-order
	back := map[[2]string]bool{}
	var dfs func(id string)
	dfs = func(id string) {
		state[id] = 1
		for _, to := range out[id] {
			switch state[to] {
			case 0:
				dfs(to)
			case 1:
				back[[2]string{id, to}] = true
			}
		}
		state[id] = 2
		topo = append(topo, id)
	}
	for _, n := range in.Nodes {
		if indeg[n.ID] == 0 && state[n.ID] == 0 {
			dfs(n.ID)
		}
	}
	for _, n := range in.Nodes {
		if state[n.ID] == 0 {
			dfs(n.ID)
		}
	}
	slices.Reverse(topo)
	layer := map[string]int{}
	for _, id := range topo {
		for _, to := range out[id] {
			if !back[[2]string{id, to}] {
				layer[to] = max(layer[to], layer[id]+1)
			}
		}
	}
	nLayers := 0
	for _, n := range in.Nodes {
		nLayers = max(nLayers, layer[n.ID]+1)
	}
	layers := make([][]string, nLayers)
	for _, n := range in.Nodes {
		layers[layer[n.ID]] = append(layers[layer[n.ID]], n.ID)
	}
	// Order within layers: barycenter of the previous layer, two sweeps;
	// nodes of the same group stay together.
	nodeBy := map[string]DiagramNode{}
	for _, n := range in.Nodes {
		nodeBy[n.ID] = n
	}
	pos := map[string]float64{}
	for _, l := range layers {
		for i, id := range l {
			pos[id] = float64(i)
		}
	}
	in2 := map[string][]string{}
	for from, tos := range out {
		for _, to := range tos {
			in2[to] = append(in2[to], from)
		}
	}
	for sweep := 0; sweep < 2; sweep++ {
		for li := 1; li < len(layers); li++ {
			bc := map[string]float64{}
			for _, id := range layers[li] {
				sum, n := 0.0, 0
				for _, from := range in2[id] {
					sum += pos[from]
					n++
				}
				if n > 0 {
					bc[id] = sum / float64(n)
				} else {
					bc[id] = pos[id]
				}
			}
			slices.SortStableFunc(layers[li], func(x, y string) int {
				if g := strings.Compare(nodeBy[x].Group, nodeBy[y].Group); g != 0 {
					return g
				}
				switch {
				case bc[x] < bc[y]:
					return -1
				case bc[x] > bc[y]:
					return 1
				}
				return 0
			})
			for i, id := range layers[li] {
				pos[id] = float64(i)
			}
		}
	}
	// Sizes and positions.
	size := func(n DiagramNode) (float64, float64) {
		w := float64(min(320, max(160, 10*len([]rune(n.Text))+48)))
		h := 72.0
		if len([]rune(n.Text)) > 28 {
			h = 96
		}
		if n.Shape == "diamond" {
			w, h = w*1.3, h*1.5
		}
		return w, h
	}
	const gapMain, gapCross = 120.0, 48.0
	ox, oy := 0.0, 0.0
	if len(wb.Elements) > 0 {
		maxX, minY := math.Inf(-1), math.Inf(1)
		for _, e := range wb.Elements {
			if len(e.Points) > 0 {
				for _, p := range e.Points {
					maxX, minY = math.Max(maxX, p[0]), math.Min(minY, p[1])
				}
				continue
			}
			maxX, minY = math.Max(maxX, e.X+e.W), math.Min(minY, e.Y)
		}
		ox, oy = maxX+160, minY
	}
	if in.Title != "" {
		oy += 60
		ox += 40
	}
	type rect struct{ x, y, w, h float64 }
	boxes := map[string]rect{}
	main := ox
	if down {
		main = oy
	}
	// Each group gets its own band across the layers, so the frames of
	// different groups never overlap; within a band the layers are centred
	// on each other. Without groups there is one band.
	const bandGap = gapCross + 2*32.0 + 24
	var bands []string
	seenBand := map[string]bool{}
	for _, n := range in.Nodes {
		if !seenBand[n.Group] {
			seenBand[n.Group] = true
			bands = append(bands, n.Group)
		}
	}
	extent := func(li int, g string) float64 {
		total, k := 0.0, 0
		for _, id := range layers[li] {
			if nodeBy[id].Group != g {
				continue
			}
			w, h := size(nodeBy[id])
			if down {
				total += w
			} else {
				total += h
			}
			if k > 0 {
				total += gapCross
			}
			k++
		}
		return total
	}
	bandStart := map[string]float64{}
	bandSize := map[string]float64{}
	at0 := 0.0
	for _, g := range bands {
		for li := range layers {
			bandSize[g] = math.Max(bandSize[g], extent(li, g))
		}
		bandStart[g] = at0
		at0 += bandSize[g] + bandGap
	}
	for li, l := range layers {
		depth := 0.0
		next := map[string]float64{}
		for _, g := range bands {
			next[g] = bandStart[g] + (bandSize[g]-extent(li, g))/2
		}
		for _, id := range l {
			g := nodeBy[id].Group
			w, h := size(nodeBy[id])
			if down {
				boxes[id] = rect{ox + next[g], main, w, h}
				next[g] += w + gapCross
				depth = math.Max(depth, h)
			} else {
				boxes[id] = rect{main, oy + next[g], w, h}
				next[g] += h + gapCross
				depth = math.Max(depth, w)
			}
		}
		main += depth + gapMain
	}
	order := nextOrder(wb)
	ids := map[string]string{}
	var els []WBElement
	add := func(e WBElement) {
		wb2 := &Whiteboard{Elements: append(slices.Clone(wb.Elements), els...)}
		e.ID = freeID(wb2, e.ID)
		e.Order = order()
		e.Seed = 1 + rand.IntN(1<<30)
		els = append(els, e)
	}
	// Frames first, so they lie behind their nodes.
	frameOf := map[string]string{}
	const pad = 32.0
	bound := func(members []string) (float64, float64, float64, float64) {
		x0, y0, x1, y1 := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
		for _, id := range members {
			b := boxes[id]
			x0, y0, x1, y1 = math.Min(x0, b.x), math.Min(y0, b.y), math.Max(x1, b.x+b.w), math.Max(y1, b.y+b.h)
		}
		return x0 - pad, y0 - pad, x1 - x0 + 2*pad, y1 - y0 + 2*pad
	}
	if in.Title != "" {
		var all []string
		for _, n := range in.Nodes {
			all = append(all, n.ID)
		}
		x, y, w, h := bound(all)
		x, y, w, h = x-pad, y-pad, w+2*pad, h+2*pad
		add(WBElement{ID: "frame", Type: "frame", X: x, Y: y, W: w, H: h, Text: in.Title})
	}
	for _, g := range in.Groups {
		var members []string
		for _, n := range in.Nodes {
			if n.Group == g.ID {
				members = append(members, n.ID)
			}
		}
		if len(members) == 0 {
			continue
		}
		x, y, w, h := bound(members)
		add(WBElement{ID: "group-" + g.ID, Type: "frame", X: x, Y: y, W: w, H: h, Text: g.Label})
		frameOf[g.ID] = els[len(els)-1].ID
	}
	for _, n := range in.Nodes {
		b := boxes[n.ID]
		shape := n.Shape
		switch shape {
		case "rect", "ellipse", "diamond", "sticky":
		default:
			shape = "rect"
		}
		color := n.Color
		if !wbColors[color] {
			color = "blue"
		}
		e := WBElement{ID: "n-" + n.ID, Type: shape, X: b.x, Y: b.y, W: b.w, H: b.h, Text: n.Text, Stroke: color, Entity: n.Entity, Frame: frameOf[n.Group]}
		if n.Entity != "" && shape == "rect" {
			e.Type = "entity"
		}
		if shape == "sticky" {
			e.Fill, e.Stroke = color, ""
		}
		add(e)
		ids[n.ID] = els[len(els)-1].ID
	}
	for _, ed := range in.Edges {
		a, b := boxes[ed.From], boxes[ed.To]
		typ := "arrow"
		if ed.Line {
			typ = "line"
		}
		e := WBElement{ID: "edge-" + ed.From + "-" + ed.To, Type: typ, Text: ed.Label, Stroke: "gray",
			Points: [][2]float64{{a.x + a.w/2, a.y + a.h/2}, {b.x + b.w/2, b.y + b.h/2}},
			Start:  &WBBinding{Element: ids[ed.From]}, End: &WBBinding{Element: ids[ed.To]}}
		if ed.Dashed {
			e.Dash = "dashed"
		}
		add(e)
	}
	return els
}

// ReplyWhiteboardComment answers a comment thread on a board.
func (s *Service) ReplyWhiteboardComment(ctx context.Context, a Actor, ref, docRef, commentID, text string) (DocResult, error) {
	p, doc, wb, err := s.whiteboardDoc(ctx, a, ref, docRef, store.RoleEditor)
	if err != nil {
		return DocResult{}, err
	}
	text = strings.TrimSpace(text)
	if text == "" || len(text) > maxWBText {
		return DocResult{}, invalid("validation.comment_text", nil, "the reply needs a text")
	}
	at := slices.IndexFunc(wb.Comments, func(c WBComment) bool { return c.ID == commentID })
	if at < 0 {
		return DocResult{}, ErrNotFound
	}
	name := a.User.Name
	if a.Via == "mcp" {
		name = "AI"
		if a.User.Name != "" {
			name = "AI (" + a.User.Name + ")"
		}
	}
	c := &wb.Comments[at]
	c.Replies = append(c.Replies, WBReply{ID: fmt.Sprintf("r%d", len(c.Replies)+1+rand.IntN(1000)*100), Author: a.String(), Name: name, Text: text, At: time.Now().UTC().Format(time.RFC3339)})
	return s.UpdateDocument(ctx, a, p.Slug, doc.ID, DocumentPatch{Whiteboard: wb})
}
