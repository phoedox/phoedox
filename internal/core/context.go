package core

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/phoedox/phoedox/internal/graph"
	"github.com/phoedox/phoedox/internal/store"
)

// Context packs (concept "Wissensnetz für KI", level 3): a short Markdown
// summary of an entry with its values, status, neighbours, documents and
// sources, so an AI client needs one or two calls instead of ten. Built on
// request; every part is a cheap indexed query.

const (
	ctxMaxNeighbours = 40
	ctxMaxDocs       = 8
	ctxSnippet       = 280
)

// networkKinds get the network path in their pack.
var networkKinds = []string{"device", "router", "repeater", "access_point", "switch", "server", "vm", "lxc", "phone", "smart_plug", "light", "lan_port"}

// EntityContext returns the context pack of an entry as Markdown.
func (s *Service) EntityContext(ctx context.Context, a Actor, ref, slug string) (string, error) {
	p, err := s.project(ctx, a, ref, store.RoleReader)
	if err != nil {
		return "", err
	}
	detail, err := s.Entity(ctx, a, p.Slug, slug)
	if err != nil {
		return "", err
	}
	e := detail.Entity
	var b strings.Builder
	fmt.Fprintf(&b, "# %s (%s, @%s)\n", e.Name, e.Kind, e.Slug)

	attrs := map[string]any{}
	_ = json.Unmarshal(e.Attrs, &attrs)
	if len(attrs) > 0 {
		keys := make([]string, 0, len(attrs))
		for k := range attrs {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			if v := fmt.Sprint(attrs[k]); v != "" && v != "<nil>" {
				parts = append(parts, k+": "+v)
			}
		}
		fmt.Fprintf(&b, "Values: %s\n", strings.Join(parts, "; "))
	}
	if h := stationStatus(e, time.Now()); h.Status != "unknown" {
		fmt.Fprintf(&b, "Status: %s (%s)\n", h.Status, strings.TrimPrefix(h.Reason, "diag."))
	}
	if slices.Contains(networkKinds, e.Kind) {
		if d, err := s.Diagnose(ctx, a, p.Slug, e.Slug); err == nil && len(d.Paths) == 1 && len(d.Paths[0].Hops) > 1 {
			fmt.Fprintf(&b, "Network path: %s\n", d.Text)
		}
	}

	if len(detail.Edges) > 0 {
		b.WriteString("\nRelations:\n")
		for i, x := range detail.Edges {
			if i == ctxMaxNeighbours {
				fmt.Fprintf(&b, "- … and %d more\n", len(detail.Edges)-i)
				break
			}
			src := ""
			if x.Source != "" && x.Source != string(graph.SourceManual) {
				src = " [" + x.Source + "]"
			}
			if x.From == e.ID {
				fmt.Fprintf(&b, "- %s → %s (%s, @%s)%s\n", x.Rel, x.ToName, x.ToKind, x.ToSlug, src)
			} else {
				fmt.Fprintf(&b, "- ← %s from %s (%s, @%s)%s\n", x.Rel, x.FromName, x.FromKind, x.FromSlug, src)
			}
		}
	}

	if len(detail.Documents) > 0 {
		b.WriteString("\nDocuments:\n")
		for i, d := range detail.Documents {
			if i == ctxMaxDocs {
				fmt.Fprintf(&b, "- … and %d more\n", len(detail.Documents)-i)
				break
			}
			full, err := s.DB.GetDocument(ctx, p.ID, d.ID)
			snippet := ""
			if err == nil {
				snippet = docSnippet(full)
			}
			fmt.Fprintf(&b, "- %s (%s, slug %s)", d.Title, d.Type, d.Slug)
			if snippet != "" {
				b.WriteString(": " + snippet)
			}
			b.WriteString("\n")
		}
	}

	if slices.Contains([]string{"server", "vm", "lxc"}, e.Kind) {
		if tree, err := s.SoftwareTree(ctx, a, p.Slug, e.Slug); err == nil && len(tree.Children) > 0 {
			count := map[string]int{}
			var walk func(n TreeNode)
			walk = func(n TreeNode) {
				for _, c := range n.Children {
					count[c.Kind]++
					walk(c)
				}
			}
			walk(tree)
			parts := []string{}
			for _, k := range []string{"software", "vm", "lxc", "stack", "container", "service", "database"} {
				if count[k] > 0 {
					parts = append(parts, fmt.Sprintf("%d %s", count[k], k))
				}
			}
			fmt.Fprintf(&b, "\nRuns here: %s (details: software_tree)\n", strings.Join(parts, ", "))
		}
	}
	return b.String(), nil
}

// docSnippet is the start of a document's text without Markdown noise.
func docSnippet(d store.Document) string {
	c := map[string]any{}
	_ = json.Unmarshal(d.Content, &c)
	text := ""
	for _, k := range []string{"markdown", "decision", "description", "context"} {
		if v, ok := c[k].(string); ok && strings.TrimSpace(v) != "" {
			text = v
			break
		}
	}
	var out []string
	for _, line := range strings.Split(graph.StripCode(text), "\n") {
		line = strings.TrimSpace(strings.TrimLeft(line, "#>-*| "))
		if line != "" {
			out = append(out, line)
		}
	}
	s := strings.Join(out, " ")
	if r := []rune(s); len(r) > ctxSnippet {
		s = string(r[:ctxSnippet]) + "…"
	}
	return s
}
