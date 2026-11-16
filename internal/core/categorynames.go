package core

import (
	"context"
	"slices"
	"strings"

	"github.com/phoedox/phoedox/internal/store"
)

// Folder names per language (multilingual projects, ADR-0028): a category
// path keeps a translated name of its last part per further language. The
// sidebar, lists and the print view show the folders in the chosen language.

func cleanCategory(c string) string {
	var parts []string
	for _, p := range strings.Split(c, "/") {
		if p = strings.TrimSpace(p); p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, "/")
}

// SetCategoryName sets (or with an empty name removes) the name of a folder
// in one of the project's further languages.
func (s *Service) SetCategoryName(ctx context.Context, a Actor, ref, category, locale, name string) (store.Project, error) {
	p, err := s.project(ctx, a, ref, store.RoleEditor)
	if err != nil {
		return p, err
	}
	if a.Via == "mcp" && p.AIWriteMode != AIDirect {
		// Folder names have no suggestions, like cards.
		return p, invalid("validation.ai_category_direct", nil, "AI clients name folders only in projects where AI writes directly")
	}
	category, name = cleanCategory(category), strings.TrimSpace(name)
	if category == "" {
		return p, invalid("validation.category_required", nil, "category is required")
	}
	if !slices.Contains(p.Locales, locale) {
		return p, invalid("validation.locale_not_in_project", map[string]any{"locale": locale}, "%q is not a further language of this project", locale)
	}
	if strings.Contains(name, "/") || len([]rune(name)) > 80 {
		return p, invalid("validation.category_name", nil, "a folder name has up to 80 characters and no /")
	}
	names := p.CategoryNames
	if names == nil {
		names = map[string]map[string]string{}
	}
	if names[category] == nil {
		names[category] = map[string]string{}
	}
	if name == "" {
		delete(names[category], locale)
		if len(names[category]) == 0 {
			delete(names, category)
		}
	} else {
		names[category][locale] = name
	}
	if err := s.DB.SetCategoryNames(ctx, p.ID, names); err != nil {
		return p, err
	}
	p.CategoryNames = names
	s.event(ctx, a, "project.category_name", p.ID, map[string]string{"category": category, "locale": locale, "name": name})
	return p, nil
}

// RenameCategoryNames moves the folder names of a renamed folder (and its
// sub-folders) to the new path.
func (s *Service) RenameCategoryNames(ctx context.Context, a Actor, ref, from, to string) error {
	p, err := s.project(ctx, a, ref, store.RoleEditor)
	if err != nil {
		return err
	}
	from, to = cleanCategory(from), cleanCategory(to)
	if from == "" || to == "" || from == to || len(p.CategoryNames) == 0 {
		return nil
	}
	moved := map[string]map[string]string{}
	for k, v := range p.CategoryNames {
		switch {
		case k == from:
			moved[to] = v
		case strings.HasPrefix(k, from+"/"):
			moved[to+k[len(from):]] = v
		default:
			moved[k] = v
		}
	}
	return s.DB.SetCategoryNames(ctx, p.ID, moved)
}

// LocalizedCategory is a folder path in a language: every part that has a
// name in that language is replaced.
func LocalizedCategory(p store.Project, category, locale string) string {
	if locale == "" || locale == p.MainLocale || len(p.CategoryNames) == 0 || category == "" {
		return category
	}
	parts := strings.Split(category, "/")
	out := make([]string, len(parts))
	for i := range parts {
		path := strings.Join(parts[:i+1], "/")
		if n := p.CategoryNames[path][locale]; n != "" {
			out[i] = n
		} else {
			out[i] = parts[i]
		}
	}
	return strings.Join(out, "/")
}
