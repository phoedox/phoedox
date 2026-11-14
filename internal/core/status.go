package core

import (
	"context"
	"strings"
	"time"

	"github.com/phoedox/phoedox/internal/store"
)

// Status tiles for dashboards (```status block): the state of entries (as
// in path diagnosis: agent, state attributes) and of monitoring checks.

// StatusBoard is what a status block shows.
type StatusBoard struct {
	Entries []DiagHop   `json:"entries"`
	Checks  []CheckView `json:"checks"`
}

const maxStatusEntries = 60

// Statuses returns the state of the given entries (slugs, with or without
// "@") and, if checks is set, of the project's checks ("*" = all, else names).
func (s *Service) Statuses(ctx context.Context, a Actor, ref string, slugs []string, checks []string) (StatusBoard, error) {
	out := StatusBoard{Entries: []DiagHop{}, Checks: []CheckView{}}
	p, err := s.project(ctx, a, ref, store.RoleReader)
	if err != nil {
		return out, err
	}
	now := time.Now()
	seen := map[string]bool{}
	for _, slug := range slugs {
		slug = strings.TrimPrefix(strings.TrimSpace(slug), "@")
		if slug == "" || seen[slug] || len(out.Entries) >= maxStatusEntries {
			continue
		}
		seen[slug] = true
		e, err := s.DB.GetEntity(ctx, p.ID, slug)
		if err != nil {
			out.Entries = append(out.Entries, DiagHop{DiagRef: DiagRef{Slug: slug, Name: slug}, Status: "unknown", Reason: "status.missing"})
			continue
		}
		out.Entries = append(out.Entries, stationStatus(e, now))
	}
	if len(checks) > 0 {
		all, err := s.Checks(ctx, a, ref, "")
		if err != nil {
			return out, err
		}
		want := map[string]bool{}
		for _, c := range checks {
			want[strings.ToLower(strings.TrimSpace(c))] = true
		}
		for _, c := range all {
			if want["*"] || want[strings.ToLower(c.Name)] {
				out.Checks = append(out.Checks, c)
			}
		}
	}
	return out, nil
}
