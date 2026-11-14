package core

import (
	"context"
	"slices"
	"time"

	"github.com/phoedox/phoedox/internal/store"
)

// HomeInfo is what the projects page shows beside the project list: the
// numbers per project, pinned projects and what is waiting for the person.
type HomeInfo struct {
	Stats       map[string]*store.ProjectStats `json:"stats"` // by project id
	Pinned      []string                       `json:"pinned"`
	DueTasks    int                            `json:"dueTasks"`    // due today or earlier
	Suggestions []HomeSuggestions              `json:"suggestions"` // projects with pending AI suggestions the person can decide
}

// HomeSuggestions counts pending suggestions of one project.
type HomeSuggestions struct {
	Project string `json:"project"`
	Name    string `json:"name"`
	Count   int    `json:"count"`
}

// Home collects the projects page's numbers for the actor.
func (s *Service) Home(ctx context.Context, a Actor) (HomeInfo, error) {
	out := HomeInfo{Stats: map[string]*store.ProjectStats{}, Pinned: []string{}, Suggestions: []HomeSuggestions{}}
	ps, err := s.Projects(ctx, a)
	if err != nil {
		return out, err
	}
	ids := []string{}
	public := map[string]bool{}
	byID := map[string]store.Project{}
	for _, p := range ps {
		if p.Role == store.RoleDoc {
			continue // only single pages are shared: no numbers about the rest
		}
		ids = append(ids, p.ID)
		byID[p.ID] = p
		public[p.ID] = p.Role != store.RoleOwner
	}
	if out.Stats, err = s.DB.ProjectStatsFor(ctx, ids, public); err != nil {
		return out, err
	}
	for id, st := range out.Stats {
		p := byID[id]
		if rank[p.Role] < rank[store.RoleEditor] {
			st.Suggestions = 0 // only editors decide suggestions
		} else if st.Suggestions > 0 {
			out.Suggestions = append(out.Suggestions, HomeSuggestions{Project: p.Slug, Name: p.Name, Count: st.Suggestions})
		}
	}
	slices.SortFunc(out.Suggestions, func(x, y HomeSuggestions) int { return y.Count - x.Count })
	if pinned, err := s.DB.PinnedProjects(ctx, a.User.ID); err == nil {
		for _, id := range pinned {
			if _, ok := byID[id]; ok {
				out.Pinned = append(out.Pinned, id)
			}
		}
	}
	if tasks, err := s.MyTasks(ctx, a); err == nil {
		today := time.Now().Format(time.DateOnly)
		for _, t := range tasks {
			if t.Due != "" && t.Due <= today {
				out.DueTasks++
			}
		}
	}
	return out, nil
}

// SetPinnedProjects stores the projects the person pinned (at most 50 of
// the projects they can open).
func (s *Service) SetPinnedProjects(ctx context.Context, a Actor, ids []string) ([]string, error) {
	if a.Via == "mcp" || a.Plugin != "" {
		return nil, ErrForbidden
	}
	ps, err := s.Projects(ctx, a)
	if err != nil {
		return nil, err
	}
	ok := map[string]bool{}
	for _, p := range ps {
		ok[p.ID] = true
	}
	keep := []string{}
	for _, id := range ids {
		if ok[id] && !slices.Contains(keep, id) && len(keep) < 50 {
			keep = append(keep, id)
		}
	}
	return keep, s.DB.SavePinnedProjects(ctx, a.User.ID, keep)
}
