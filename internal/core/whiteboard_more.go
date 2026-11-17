package core

import (
	"context"
	"regexp"
	"slices"
	"strings"

	"github.com/phoedox/phoedox/internal/store"
)

// Whiteboards, phase 3 (whiteboard-concept): an entry's neighbourhood drawn
// from the knowledge graph, and Mermaid flowcharts turned into editable
// shapes. Both go through the diagram layout of draw_diagram.

const maxNeighbours = 60

// DrawNeighbourhood places an entry and the entries it is related to as
// shapes with bound arrows (labelled with the relation) on a whiteboard.
func (s *Service) DrawNeighbourhood(ctx context.Context, a Actor, ref, docRef, entRef string) (DocResult, error) {
	p, _, _, err := s.whiteboardDoc(ctx, a, ref, docRef, store.RoleEditor)
	if err != nil {
		return DocResult{}, err
	}
	e, err := s.DB.GetEntity(ctx, p.ID, entRef)
	if err != nil {
		return DocResult{}, err
	}
	edges, err := s.DB.EdgesOf(ctx, e.ID)
	if err != nil {
		return DocResult{}, err
	}
	if len(edges) > maxNeighbours {
		edges = edges[:maxNeighbours]
	}
	in := DiagramInput{Title: e.Name, Nodes: []DiagramNode{{ID: e.Slug, Text: e.Name, Entity: e.Slug, Color: "orange"}}}
	seen := map[string]bool{e.Slug: true}
	add := func(slug, name string) {
		if !seen[slug] {
			seen[slug] = true
			in.Nodes = append(in.Nodes, DiagramNode{ID: slug, Text: name, Entity: slug})
		}
	}
	for _, ed := range edges {
		add(ed.FromSlug, ed.FromName)
		add(ed.ToSlug, ed.ToName)
		in.Edges = append(in.Edges, DiagramEdge{From: ed.FromSlug, To: ed.ToSlug, Label: strings.ReplaceAll(ed.Rel, "_", " ")})
	}
	return s.DrawDiagram(ctx, a, p.Slug, docRef, "", "", in)
}

// DrawMermaid turns a Mermaid flowchart into shapes and arrows on a
// whiteboard (a new one without docRef).
func (s *Service) DrawMermaid(ctx context.Context, a Actor, ref, docRef, title, category, src string) (DocResult, error) {
	in, err := ParseMermaid(src)
	if err != nil {
		return DocResult{}, err
	}
	if title != "" {
		in.Title = title
	}
	return s.DrawDiagram(ctx, a, ref, docRef, title, category, in)
}

var (
	mmHeadRe  = regexp.MustCompile(`(?i)^(flowchart|graph)\b\s*(TD|TB|LR|RL|BT)?`)
	mmIDRe    = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_\-.]*`)
	mmOpRe    = regexp.MustCompile(`^(<-->|<-.->|<==>|-\.->|-\.-|-->|---|==>|===|--[ox]|~~~)(?:\s*\|([^|]*)\|)?`)
	mmLabelRe = regexp.MustCompile(`^(--|==|-\.)\s*([^-=.|][^|]*?)\s*(-->|---|\.->|\.-|==>|===)`)
	mmSubRe   = regexp.MustCompile(`(?i)^subgraph\s+([^\s\[]+)\s*(?:\[\s*"?(.*?)"?\s*\])?\s*$`)
	// Shapes, longest delimiters first.
	mmShapes = []struct{ open, close, shape string }{
		{"(((", ")))", "ellipse"}, {"((", "))", "ellipse"}, {"([", "])", "ellipse"}, {"[[", "]]", "rect"}, {"[(", ")]", "rect"},
		{"{{", "}}", "diamond"}, {"[/", "/]", "rect"}, {"[\\", "\\]", "rect"}, {"[/", "\\]", "rect"}, {"[\\", "/]", "rect"},
		{"[", "]", "rect"}, {"(", ")", "rect"}, {"{", "}", "diamond"}, {">", "]", "rect"},
	}
)

// ParseMermaid reads a Mermaid flowchart (graph/flowchart with nodes,
// shapes, edges with labels, dotted and thick links, chains, "&" and
// subgraphs). Styling lines (classDef, class, style, linkStyle, click) are
// ignored.
func ParseMermaid(src string) (DiagramInput, error) {
	bad := func(msg string) (DiagramInput, error) {
		return DiagramInput{}, invalid("validation.mermaid", map[string]any{"msg": msg}, "mermaid: %s", msg)
	}
	in := DiagramInput{}
	nodes := map[string]int{}
	var groups []string // open subgraphs
	node := func(id, text, shape string) {
		if i, ok := nodes[id]; ok {
			if text != "" {
				in.Nodes[i].Text, in.Nodes[i].Shape = text, shape
			}
			return
		}
		n := DiagramNode{ID: id, Text: text, Shape: shape}
		if n.Text == "" {
			n.Text = id
		}
		if len(groups) > 0 {
			n.Group = groups[len(groups)-1]
		}
		nodes[id] = len(in.Nodes)
		in.Nodes = append(in.Nodes, n)
	}
	headed := false
	var lines []string
	for _, l := range strings.Split(strings.ReplaceAll(src, "\r", ""), "\n") {
		if i := strings.Index(l, "%%"); i >= 0 {
			l = l[:i]
		}
		lines = append(lines, strings.Split(l, ";")...)
	}
	for _, raw := range lines {
		l := strings.TrimSpace(raw)
		if l == "" {
			continue
		}
		if !headed {
			m := mmHeadRe.FindStringSubmatch(l)
			if m == nil {
				return bad("only flowcharts (graph or flowchart) can become shapes")
			}
			headed = true
			if d := strings.ToUpper(m[2]); d == "TD" || d == "TB" || d == "BT" {
				in.Direction = "down"
			}
			continue
		}
		low := strings.ToLower(l)
		switch {
		case strings.HasPrefix(low, "subgraph"):
			m := mmSubRe.FindStringSubmatch(l)
			if m == nil {
				id := strings.TrimSpace(l[len("subgraph"):])
				m = []string{l, id, id}
			}
			label := strings.Trim(m[2], `"`)
			if label == "" {
				label = m[1]
			}
			in.Groups = append(in.Groups, DiagramGroup{ID: m[1], Label: label})
			groups = append(groups, m[1])
			continue
		case low == "end":
			if len(groups) > 0 {
				groups = groups[:len(groups)-1]
			}
			continue
		case strings.HasPrefix(low, "direction "):
			continue
		}
		if f := strings.Fields(low); len(f) > 0 && slices.Contains([]string{"classdef", "class", "style", "linkstyle", "click"}, f[0]) {
			continue
		}
		if err := mmStatement(l, node, &in); err != nil {
			return bad(err.Error())
		}
	}
	if !headed || len(in.Nodes) == 0 {
		return bad("no nodes found")
	}
	// Groups without nodes would be empty frames.
	used := map[string]bool{}
	for _, n := range in.Nodes {
		used[n.Group] = true
	}
	in.Groups = slices.DeleteFunc(in.Groups, func(g DiagramGroup) bool { return !used[g.ID] })
	return in, nil
}

type mmErr string

func (e mmErr) Error() string { return string(e) }

// mmStatement reads one line: node groups (a & b) joined by links.
func mmStatement(l string, node func(id, text, shape string), in *DiagramInput) error {
	rest := strings.TrimSpace(l)
	readGroup := func() ([]string, error) {
		var ids []string
		for {
			rest = strings.TrimSpace(rest)
			id := mmIDRe.FindString(rest)
			if id == "" {
				return nil, mmErr("cannot read " + clip(rest, 40))
			}
			rest = rest[len(id):]
			text, shape := "", "rect"
			for _, sh := range mmShapes {
				if strings.HasPrefix(rest, sh.open) {
					if j := strings.Index(rest[len(sh.open):], sh.close); j >= 0 {
						text = strings.TrimSpace(rest[len(sh.open) : len(sh.open)+j])
						text = strings.Trim(text, `"`)
						shape = sh.shape
						rest = rest[len(sh.open)+j+len(sh.close):]
						break
					}
				}
			}
			// :::class suffix
			if strings.HasPrefix(rest, ":::") {
				c := mmIDRe.FindString(rest[3:])
				rest = rest[3+len(c):]
			}
			text = strings.NewReplacer("<br>", "\n", "<br/>", "\n", "<br />", "\n").Replace(text)
			node(id, text, shape)
			ids = append(ids, id)
			rest = strings.TrimSpace(rest)
			if !strings.HasPrefix(rest, "&") {
				return ids, nil
			}
			rest = rest[1:]
		}
	}
	from, err := readGroup()
	if err != nil {
		return err
	}
	for rest != "" {
		var op, label string
		if m := mmOpRe.FindStringSubmatch(rest); m != nil {
			op, label = m[1], m[2]
			rest = rest[len(m[0]):]
		} else if m := mmLabelRe.FindStringSubmatch(rest); m != nil {
			op, label = m[1]+m[3], m[2]
			rest = rest[len(m[0]):]
		} else {
			return mmErr("unknown link at " + clip(rest, 40))
		}
		if strings.HasPrefix(strings.TrimSpace(rest), "|") {
			r := strings.TrimSpace(rest)[1:]
			if j := strings.Index(r, "|"); j >= 0 {
				label, rest = r[:j], r[j+1:]
			}
		}
		to, err := readGroup()
		if err != nil {
			return err
		}
		dashed := strings.Contains(op, ".")
		line := !strings.Contains(op, ">") && !strings.HasSuffix(op, "o") && !strings.HasSuffix(op, "x")
		for _, f := range from {
			for _, t := range to {
				in.Edges = append(in.Edges, DiagramEdge{From: f, To: t, Label: strings.Trim(strings.TrimSpace(label), `"`), Dashed: dashed, Line: line})
			}
		}
		from = to
	}
	return nil
}

// CodeStatus is the live state of a pull request or issue on a board.
type CodeStatus struct {
	Title  string `json:"title"`
	Status string `json:"status"` // open | draft | merged | closed
}

var codeURLRe = regexp.MustCompile(`^https?://[^/]+/([\w.-]+)/([\w.-]+)/(pull|pulls|issues)/(\d+)`)

// WhiteboardCodeStatus reads the state of pull requests and issues of the
// project's code connections (readers; at most 50 addresses). Addresses of
// repositories without a connection are left out.
func (s *Service) WhiteboardCodeStatus(ctx context.Context, a Actor, ref string, urls []string) (map[string]CodeStatus, error) {
	p, err := s.project(ctx, a, ref, store.RoleReader)
	if err != nil {
		return nil, err
	}
	out := map[string]CodeStatus{}
	if len(urls) > 50 {
		urls = urls[:50]
	}
	forges := map[string]*forge{}
	for _, u := range urls {
		m := codeURLRe.FindStringSubmatch(u)
		if m == nil {
			continue
		}
		key := strings.ToLower(m[1] + "/" + m[2])
		f, ok := forges[key]
		if !ok {
			if r, err := s.findRepo(ctx, p, m[1]+"/"+m[2]); err == nil {
				if fg, err := s.forge(ctx, r); err == nil {
					f = &fg
				}
			}
			forges[key] = f
		}
		if f == nil {
			continue
		}
		var it struct {
			Title    string `json:"title"`
			State    string `json:"state"`
			Draft    bool   `json:"draft"`
			Merged   bool   `json:"merged"`
			MergedAt string `json:"merged_at"`
		}
		path := "/issues/" + m[4]
		if m[3] != "issues" {
			path = "/pulls/" + m[4]
		}
		if f.get(ctx, f.repoPath(path), &it) != nil {
			continue
		}
		st := strings.ToLower(it.State)
		switch {
		case it.Merged || (it.MergedAt != "" && it.MergedAt != "null"):
			st = "merged"
		case st == "open" && it.Draft:
			st = "draft"
		}
		out[u] = CodeStatus{Title: clip(it.Title, 500), Status: st}
	}
	return out, nil
}
