package core

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/phoedox/phoedox/internal/graph"
	"github.com/phoedox/phoedox/internal/importer"
	"github.com/phoedox/phoedox/internal/store"
)

// Imports (concept "Einstieg"): Markdown folders (also Obsidian vaults) and
// OpenAPI descriptions become pages of a project.

// ImportResult says what an import did.
type ImportResult struct {
	Created []string `json:"created"` // slugs
	Skipped []string `json:"skipped"` // files that are not Markdown
	Entity  string   `json:"entity,omitempty"`
}

func (s *Service) importAccess(ctx context.Context, a Actor, ref string) (store.Project, error) {
	if a.Via == "mcp" || a.Plugin != "" {
		return store.Project{}, ErrForbidden
	}
	return s.project(ctx, a, ref, store.RoleEditor)
}

// freeSlug returns a page slug not used in the project yet.
func (s *Service) freeSlug(ctx context.Context, p store.Project, title string, taken map[string]bool) string {
	base := graph.Slugify(title)
	if base == "" {
		base = "seite"
	}
	slug := base
	for i := 2; ; i++ {
		if _, err := s.DB.GetDocument(ctx, p.ID, slug); errors.Is(err, store.ErrNotFound) && !taken[slug] {
			taken[slug] = true
			return slug
		}
		slug = fmt.Sprintf("%s-%d", base, i)
	}
}

// ImportMarkdown creates a page per Markdown file of a ZIP archive; folders
// become categories and [[wiki links]] links between the new pages.
func (s *Service) ImportMarkdown(ctx context.Context, a Actor, ref string, zipData []byte) (ImportResult, error) {
	res := ImportResult{Created: []string{}, Skipped: []string{}}
	p, err := s.importAccess(ctx, a, ref)
	if err != nil {
		return res, err
	}
	pages, skipped, err := importer.MarkdownZip(zipData)
	if errors.Is(err, importer.ErrNotZip) {
		return res, invalid("validation.import_zip", nil, "upload a ZIP archive with Markdown files")
	}
	if err != nil {
		return res, invalid("validation.import_failed", map[string]any{"error": err.Error()}, "%v", err)
	}
	res.Skipped = append(res.Skipped, skipped...)
	taken := map[string]bool{}
	slugs := make([]string, len(pages))
	byKey := map[string]string{}
	for i, pg := range pages {
		slugs[i] = s.freeSlug(ctx, p, pg.Title, taken)
		for _, k := range []string{pg.Key, pg.Title, strings.Trim(pg.Category+"/"+pg.Key, "/")} {
			if _, dup := byKey[strings.ToLower(k)]; !dup {
				byKey[strings.ToLower(k)] = slugs[i]
			}
		}
	}
	url := func(key string) string {
		if slug, ok := byKey[strings.ToLower(key)]; ok {
			return "/p/" + p.Slug + "/d/" + slug
		}
		return ""
	}
	for i, pg := range pages {
		md := importer.ResolveWikiLinks(pg.Markdown, url)
		r, err := s.createDocument(ctx, a, p, DocumentInput{Type: store.DocMarkdown, Title: pg.Title, Slug: slugs[i], Category: pg.Category, Markdown: md})
		if err != nil {
			res.Skipped = append(res.Skipped, pg.Category+"/"+pg.Key+".md")
			continue
		}
		res.Created = append(res.Created, r.Document.Slug)
	}
	s.event(ctx, a, "project.import", p.ID, map[string]any{"kind": "markdown", "created": len(res.Created), "skipped": len(res.Skipped)})
	return res, nil
}

// ImportOpenAPI creates a service entry for an API and a page per tag with
// runnable request blocks ({{base}} and {{token}} from the environment).
func (s *Service) ImportOpenAPI(ctx context.Context, a Actor, ref string, data []byte) (ImportResult, error) {
	res := ImportResult{Created: []string{}, Skipped: []string{}}
	p, err := s.importAccess(ctx, a, ref)
	if err != nil {
		return res, err
	}
	api, err := importer.OpenAPI(data)
	if err != nil {
		return res, invalid("validation.import_openapi", nil, "upload an OpenAPI 3 or Swagger 2 file (JSON or YAML)")
	}
	attrs := map[string]any{"version": api.Version}
	if api.Server != "" {
		attrs["url"] = api.Server
	}
	ent, err := s.upsertEntity(ctx, a, p, EntityInput{Kind: "service", Name: api.Title, Slug: graph.Slugify(api.Title), Attrs: attrs})
	if err == nil && ent.Entity != nil {
		res.Entity = ent.Entity.Slug
	}
	taken := map[string]bool{}
	for _, pg := range api.Pages {
		md := pg.Markdown
		head, rest, _ := strings.Cut(md, "\n\n")
		intro := "`{{base}}` = " + api.Server
		if res.Entity != "" {
			intro += " · @" + res.Entity
		}
		md = head + "\n\n" + intro + "\n\n" + rest
		r, err := s.createDocument(ctx, a, p, DocumentInput{Type: store.DocMarkdown, Title: api.Title + ": " + pg.Title,
			Slug: s.freeSlug(ctx, p, api.Title+" "+pg.Title, taken), Category: "API/" + api.Title, Markdown: md})
		if err != nil {
			res.Skipped = append(res.Skipped, pg.Title)
			continue
		}
		res.Created = append(res.Created, r.Document.Slug)
	}
	s.event(ctx, a, "project.import", p.ID, map[string]any{"kind": "openapi", "title": api.Title, "created": len(res.Created)})
	return res, nil
}
