package core

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/phoedox/phoedox/internal/store"
)

// Recently opened pages and entries (personal history) and personal
// keyboard shortcuts. Only people in the browser leave a history.

// RecentItem is a history entry as the app shows it.
type RecentItem struct {
	Kind        string     `json:"kind"` // document | entity
	ID          string     `json:"id"`
	Slug        string     `json:"slug"`
	Title       string     `json:"title"`
	Type        string     `json:"type"` // document type or entity kind
	ProjectSlug string     `json:"projectSlug"`
	ProjectName string     `json:"projectName"`
	ViewedAt    store.Time `json:"viewedAt"`
}

// RecordView notes that the actor opened a page or entry (ref: id or slug).
func (s *Service) RecordView(ctx context.Context, a Actor, project, kind, ref string) error {
	if a.Via != "web" || a.User.ID == "" {
		return nil
	}
	var p store.Project
	id := ""
	switch kind {
	case "document":
		pp, d, err := s.docAccess(ctx, a, project, ref, store.RoleDoc)
		if err != nil {
			return err
		}
		p, id = pp, d.ID
	case "entity":
		pp, err := s.project(ctx, a, project, store.RoleReader)
		if err != nil {
			return err
		}
		e, err := s.DB.GetEntity(ctx, pp.ID, ref)
		if err != nil {
			return err
		}
		p, id = pp, e.ID
	default:
		return invalid("validation.recent_kind", nil, "kind must be document or entity")
	}
	return s.DB.RecordView(ctx, a.User.ID, store.RecentView{ProjectID: p.ID, Kind: kind, RefID: id, ViewedAt: store.Now()})
}

// Recent lists what the actor opened last that they can still see.
func (s *Service) Recent(ctx context.Context, a Actor, limit int) ([]RecentItem, error) {
	if limit <= 0 || limit > 200 {
		limit = 20
	}
	views, err := s.DB.RecentViews(ctx, a.User.ID, limit)
	if err != nil {
		return nil, err
	}
	out := []RecentItem{}
	projects := map[string]*store.Project{}
	for _, v := range views {
		p, ok := projects[v.ProjectID]
		if !ok {
			if pp, err := s.project(ctx, a, v.ProjectID, store.RoleDoc); err == nil {
				p = &pp
			}
			projects[v.ProjectID] = p
		}
		if p == nil {
			continue
		}
		it := RecentItem{Kind: v.Kind, ID: v.RefID, ProjectSlug: p.Slug, ProjectName: p.Name, ViewedAt: v.ViewedAt}
		switch v.Kind {
		case "document":
			_, d, err := s.docAccess(ctx, a, p.ID, v.RefID, store.RoleDoc)
			if err != nil {
				continue
			}
			it.Slug, it.Title, it.Type = d.Slug, d.Title, d.Type
		case "entity":
			if p.Role == store.RoleDoc {
				continue
			}
			e, err := s.DB.GetEntity(ctx, p.ID, v.RefID)
			if err != nil {
				continue
			}
			it.Slug, it.Title, it.Type = e.Slug, e.Name, e.Kind
		}
		out = append(out, it)
	}
	return out, nil
}

// ClearRecent forgets one entry (ref = id) or the whole history.
func (s *Service) ClearRecent(ctx context.Context, a Actor, ref string) error {
	return s.DB.ClearRecentViews(ctx, a.User.ID, ref)
}

var (
	shortcutAction = regexp.MustCompile(`^[a-z][a-zA-Z0-9.]{0,40}$`)
	shortcutCombo  = regexp.MustCompile(`^((Mod|Ctrl|Alt|Shift|Meta)\+){0,4}([A-Za-z0-9]|F[0-9]{1,2}|Enter|Escape|Space|Tab|Backspace|Delete|ArrowUp|ArrowDown|ArrowLeft|ArrowRight|Home|End|PageUp|PageDown|[.,;/\-=\[\]\\'` + "`" + `])$`)
)

// Shortcuts returns the actor's own keyboard shortcuts (action → combination;
// actions not listed keep their default, "" turns one off).
func (s *Service) Shortcuts(ctx context.Context, a Actor) (map[string]string, error) {
	raw, err := s.DB.UserShortcuts(ctx, a.User.ID)
	if err != nil {
		return nil, err
	}
	m := map[string]string{}
	_ = json.Unmarshal([]byte(raw), &m)
	return m, nil
}

// SaveShortcuts stores the actor's shortcuts.
func (s *Service) SaveShortcuts(ctx context.Context, a Actor, in map[string]string) (map[string]string, error) {
	if a.Via != "web" {
		return nil, ErrForbidden
	}
	if len(in) > 50 {
		return nil, invalid("validation.shortcut", map[string]any{"combo": ""}, "too many shortcuts")
	}
	clean := map[string]string{}
	seen := map[string]string{}
	for action, combo := range in {
		combo = strings.TrimSpace(combo)
		if !shortcutAction.MatchString(action) || (combo != "" && !shortcutCombo.MatchString(combo)) {
			return nil, invalid("validation.shortcut", map[string]any{"combo": combo}, "invalid shortcut %q", combo)
		}
		if other, dup := seen[strings.ToLower(combo)]; dup && combo != "" {
			return nil, invalid("validation.shortcut_taken", map[string]any{"combo": combo, "action": other}, "%s is used twice", combo)
		}
		if combo != "" {
			seen[strings.ToLower(combo)] = action
		}
		clean[action] = combo
	}
	b, _ := json.Marshal(clean)
	if err := s.DB.SaveUserShortcuts(ctx, a.User.ID, string(b)); err != nil {
		return nil, err
	}
	return clean, nil
}
