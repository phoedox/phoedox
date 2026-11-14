package core

import (
	"context"
	"encoding/json"

	"github.com/phoedox/phoedox/internal/graph"
	"github.com/phoedox/phoedox/internal/store"
	"github.com/phoedox/phoedox/resources"
)

// Project templates (resources/templates/<id>.<locale>.json): starting
// pages, boards and switched-on tabs for common kinds of projects.

// Template describes a project template.
type Template struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Icon        string   `json:"icon"`
	Features    []string `json:"features"`
	Pages       []struct {
		Title    string `json:"title"`
		Category string `json:"category"`
		Markdown string `json:"markdown"`
	} `json:"pages,omitempty"`
	Boards []struct {
		Title       string        `json:"title"`
		Category    string        `json:"category"`
		Description string        `json:"description"`
		Columns     []BoardColumn `json:"columns"`
	} `json:"boards,omitempty"`
}

func loadTemplate(id, locale string) (Template, bool) {
	var t Template
	b := resources.Template(id, locale)
	if b == nil || json.Unmarshal(b, &t) != nil {
		return t, false
	}
	return t, true
}

// Templates lists the project templates in a language (without contents).
func Templates(locale string) []Template {
	out := []Template{}
	for _, id := range resources.TemplateIDs() {
		if t, ok := loadTemplate(id, locale); ok {
			t.Pages, t.Boards = nil, nil
			out = append(out, t)
		}
	}
	return out
}

// applyTemplate fills a new project; failures leave a usable project behind.
func (s *Service) applyTemplate(ctx context.Context, a Actor, p store.Project, t Template) {
	for _, pg := range t.Pages {
		_, _ = s.createDocument(ctx, a, p, DocumentInput{Type: store.DocMarkdown, Title: pg.Title, Category: pg.Category, Markdown: pg.Markdown, Slug: graph.Slugify(pg.Title)})
	}
	for _, b := range t.Boards {
		_, _ = s.createDocument(ctx, a, p, DocumentInput{Type: store.DocBoard, Title: b.Title, Slug: graph.Slugify(b.Title), Category: b.Category,
			Board: &Board{Description: b.Description, Columns: b.Columns}})
	}
	s.event(ctx, a, "project.template", p.ID, map[string]string{"template": t.ID})
}
