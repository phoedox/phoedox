package blocks

import (
	"regexp"
	"strings"
)

// Blocks that show the knowledge graph (ADR-0085): trees of entries,
// racks and floor plan excerpts, read like web/src/blocks/{tree,rackblock,
// floorplanblock}.ts. They only name entries and documents; whether those
// exist is checked by the core with the project at hand.

var (
	slugRe   = regexp.MustCompile(`^@?([a-z0-9]+(?:-[a-z0-9]+)*)$`)
	docSlug  = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	dirRe    = regexp.MustCompile(`(?i)^(in|ein|eingehend|up|out|aus|ausgehend|down)$`)
	dirOutRe = regexp.MustCompile(`(?i)^(out|aus|ausgehend|down)$`)
)

// TreeSpec is a tree block: the root entry and how to walk from it.
type TreeSpec struct {
	Root, Relation, Direction string
	Depth                     int
}

// ParseTree reads a tree block.
func ParseTree(src []byte) (TreeSpec, []Problem) {
	t := TreeSpec{Direction: "in", Depth: 4}
	var ps []Problem
	for _, l := range lines(src) {
		k, v, ok := key(l, "root", "wurzel", "relation", "beziehung", "direction", "richtung", "depth", "tiefe", "title", "titel")
		if !ok {
			if m := slugRe.FindStringSubmatch(l); m != nil && t.Root == "" {
				t.Root = m[1]
				continue
			}
			ps = append(ps, problem("unknown line %q", l))
			continue
		}
		switch k {
		case "root", "wurzel":
			if m := slugRe.FindStringSubmatch(v); m != nil {
				t.Root = m[1]
			} else {
				ps = append(ps, problem("the root %q is not an entry (root: @slug)", v))
			}
		case "relation", "beziehung":
			t.Relation = v
		case "direction", "richtung":
			if !dirRe.MatchString(v) {
				ps = append(ps, problem("%q is not a direction (in or out)", v))
			} else if dirOutRe.MatchString(v) {
				t.Direction = "out"
			}
		}
	}
	if t.Root == "" && len(ps) == 0 {
		ps = append(ps, problem("the root is missing (root: @slug)"))
	}
	return t, ps
}

// ParseRack reads a rack block: the rack's entry.
func ParseRack(src []byte) (string, []Problem) {
	rack := ""
	var ps []Problem
	for _, l := range lines(src) {
		k, v, ok := key(l, "rack", "title", "titel")
		if ok && k != "rack" {
			continue
		}
		if !ok {
			v = l
		}
		if m := slugRe.FindStringSubmatch(strings.TrimSpace(v)); m != nil && rack == "" {
			rack = m[1]
		} else {
			ps = append(ps, problem("unknown line %q (the rack's entry, e.g. @rack-basement)", l))
		}
	}
	if rack == "" && len(ps) == 0 {
		ps = append(ps, problem("which rack? write its entry, e.g. @rack-basement"))
	}
	return rack, ps
}

// ParsePlan reads a floor plan block: the plan document and optionally a room.
func ParsePlan(src []byte) (plan, room string, ps []Problem) {
	for _, l := range lines(src) {
		k, v, ok := key(l, "plan", "grundriss", "room", "raum", "title", "titel", "devices", "geraete")
		if !ok {
			if docSlug.MatchString(l) && plan == "" {
				plan = l
				continue
			}
			ps = append(ps, problem("unknown line %q", l))
			continue
		}
		switch k {
		case "plan", "grundriss":
			if docSlug.MatchString(v) {
				plan = v
			} else {
				ps = append(ps, problem("%q is not a page slug (plan: <floor plan>)", v))
			}
		case "room", "raum":
			room = v
		}
	}
	if plan == "" && len(ps) == 0 {
		ps = append(ps, problem("which floor plan? plan: <page>"))
	}
	return plan, room, ps
}

func init() {
	Register(Def{Kind: "tree", Board: true, Data: true,
		Check: func(src []byte) []Problem { _, ps := ParseTree(src); return ps },
		Text:  func(src []byte) string { t, _ := ParseTree(src); return "@" + t.Root + " " + t.Relation }})
	Register(Def{Kind: "rack", Board: true, Data: true,
		Check: func(src []byte) []Problem { _, ps := ParseRack(src); return ps },
		Text:  func(src []byte) string { r, _ := ParseRack(src); return "@" + r }})
	Register(Def{Kind: "floorplan", Board: true, Data: true,
		Check: func(src []byte) []Problem { _, _, ps := ParsePlan(src); return ps },
		Text:  func(src []byte) string { p, r, _ := ParsePlan(src); return strings.TrimSpace(p + " " + r) }})
}
