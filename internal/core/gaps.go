package core

import (
	"context"
	"encoding/json"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/phoedox/phoedox/internal/graph"
	"github.com/phoedox/phoedox/internal/store"
)

// Documentation gaps: rule-based checks of what is missing or outdated in a
// project. No AI involved; an AI client can read them through MCP.

// Gap is one finding.
type Gap struct {
	Kind    string         `json:"kind"`    // see gapKinds
	Subject string         `json:"subject"` // document or entity slug
	Title   string         `json:"title"`
	Detail  map[string]any `json:"detail,omitempty"`
}

// GapReport lists the findings of a project, most important kinds first.
type GapReport struct {
	Gaps   []Gap          `json:"gaps"`
	Counts map[string]int `json:"counts"`
}

// Kinds of findings, in the order they are shown.
var gapKinds = []string{"failing_check", "review_due", "broken_mention", "unplaced_device", "undocumented_entry", "incomplete_entry", "empty_page", "stale_page"}

// placeless kinds need no location.
var placeless = map[string]bool{"room": true, "floor": true, "building": true, "site": true, "person": true, "service": true, "network": true, "vlan": true, "software": true, "contract": true}

// DocumentationGaps finds missing and outdated documentation (readers).
func (s *Service) DocumentationGaps(ctx context.Context, a Actor, ref string, staleDays int) (GapReport, error) {
	p, err := s.project(ctx, a, ref, store.RoleReader)
	if err != nil {
		return GapReport{}, err
	}
	if staleDays <= 0 {
		staleDays = 365
	}
	r := GapReport{Gaps: []Gap{}, Counts: map[string]int{}}
	add := func(g Gap) {
		r.Gaps = append(r.Gaps, g)
		r.Counts[g.Kind]++
	}
	ents, err := s.DB.ListEntities(ctx, p.ID, "")
	if err != nil {
		return r, err
	}
	edges, err := s.DB.EdgesOfProject(ctx, p.ID)
	if err != nil {
		return r, err
	}
	mentioned, err := s.DB.MentionedEntities(ctx, p.ID)
	if err != nil {
		return r, err
	}
	m, _ := s.model(ctx, p.ID)
	kinds := map[string]Kind{}
	for _, k := range m.Kinds {
		kinds[k.ID] = k
	}
	placed := map[string]bool{}
	connected := map[string]bool{}
	for _, e := range edges {
		connected[e.From], connected[e.To] = true, true
		if e.Rel == "located_in" || e.Rel == "mounted_in" || e.Rel == "part_of" || e.Rel == "runs_on" {
			placed[e.From] = true
		}
	}
	// Devices drawn on a floor plan have a location too.
	onPlan := map[string]bool{}
	if plans, err := s.DB.ListDocuments(ctx, p.ID, store.DocFilter{Type: store.DocFloorplan}); err == nil {
		for _, d := range plans {
			full, err := s.DB.GetDocument(ctx, p.ID, d.ID)
			if err != nil {
				continue
			}
			var fp Floorplan
			if json.Unmarshal(full.Content, &fp) == nil {
				for _, dev := range fp.Devices {
					onPlan[dev.Entity] = true
				}
			}
		}
	}
	slugs := map[string]bool{}
	for _, e := range ents {
		slugs[e.Slug] = true
		if !mentioned[e.ID] && !connected[e.ID] {
			add(Gap{Kind: "undocumented_entry", Subject: e.Slug, Title: e.Name, Detail: map[string]any{"kind": e.Kind}})
		}
		if !placeless[e.Kind] && !placed[e.ID] && !onPlan[e.Slug] {
			add(Gap{Kind: "unplaced_device", Subject: e.Slug, Title: e.Name, Detail: map[string]any{"kind": e.Kind}})
		}
		attrs := map[string]any{}
		_ = json.Unmarshal(e.Attrs, &attrs)
		var empty []string
		for _, at := range kinds[e.Kind].Attrs {
			if at.Type == "bool" {
				continue
			}
			if v, ok := attrs[at.Name]; !ok || v == nil || v == "" {
				empty = append(empty, at.Name)
			}
		}
		if len(kinds[e.Kind].Attrs) >= 2 && len(empty)*2 > len(kinds[e.Kind].Attrs) {
			add(Gap{Kind: "incomplete_entry", Subject: e.Slug, Title: e.Name, Detail: map[string]any{"kind": e.Kind, "missing": empty}})
		}
	}
	allowed, err := s.docFilter(ctx, a, p)
	if err != nil {
		return r, err
	}
	docs, err := s.DB.ListDocuments(ctx, p.ID, store.DocFilter{Type: store.DocMarkdown})
	if err != nil {
		return r, err
	}
	stale := time.Now().AddDate(0, 0, -staleDays)
	for i, d := range docs {
		if !allowed(d.ID, d.Restricted) || i >= 2000 {
			continue
		}
		full, err := s.DB.GetDocument(ctx, p.ID, d.ID)
		if err != nil {
			continue
		}
		md := markdownOf(full)
		var broken []string
		for _, mm := range graph.ParseMentions(md) {
			if (mm.Project == "" || mm.Project == p.Slug) && !slugs[mm.Slug] && !slices.Contains(broken, mm.Slug) {
				broken = append(broken, mm.Slug)
			}
		}
		if len(broken) > 0 {
			add(Gap{Kind: "broken_mention", Subject: d.Slug, Title: d.Title, Detail: map[string]any{"mentions": broken}})
		}
		if len(strings.TrimSpace(graph.StripCode(md))) < 40 {
			add(Gap{Kind: "empty_page", Subject: d.Slug, Title: d.Title})
		} else if d.UpdatedAt.Before(stale) {
			add(Gap{Kind: "stale_page", Subject: d.Slug, Title: d.Title, Detail: map[string]any{"updatedAt": d.UpdatedAt}})
		}
	}
	// Pages whose review date has passed (any type).
	if rules, err := s.DB.PageRules(ctx, p.ID); err == nil {
		now := time.Now()
		for _, rule := range rules {
			if rule.ReviewDays <= 0 || !allowed(rule.DocumentID, false) {
				continue
			}
			doc, err := s.DB.GetDocument(ctx, p.ID, rule.DocumentID)
			if err != nil || !allowed(doc.ID, doc.Restricted) {
				continue
			}
			if due := reviewDue(rule, doc); !due.After(now) {
				add(Gap{Kind: "review_due", Subject: doc.Slug, Title: doc.Title, Detail: map[string]any{"due": due.Format(time.DateOnly)}})
			}
		}
	}
	if checks, err := s.DB.ProjectChecks(ctx, p.ID); err == nil {
		for _, c := range checks {
			if c.Enabled && c.LastStatus == store.CheckFail {
				add(Gap{Kind: "failing_check", Subject: c.ID, Title: c.Name, Detail: map[string]any{"since": c.LastChange, "detail": c.LastDetail}})
			}
		}
	}
	order := map[string]int{}
	for i, k := range gapKinds {
		order[k] = i
	}
	sort.SliceStable(r.Gaps, func(i, j int) bool { return order[r.Gaps[i].Kind] < order[r.Gaps[j].Kind] })
	return r, nil
}
