package mcp

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"

	"github.com/phoedox/phoedox/internal/core"
)

// Tool results carry "url": the address in Phoedox of every project, page,
// decision, board card and entry they contain, so an AI client can hand
// the user a link to what it just created or found.

var linkSvc *core.Service

var docTypes = map[string]bool{"markdown": true, "adr": true, "link": true, "board": true, "floorplan": true}

// withURLs returns v as generic JSON with "url" fields added.
func withURLs(ctx context.Context, v any) any {
	if linkSvc == nil || v == nil {
		return v
	}
	b, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var tree any
	if json.Unmarshal(b, &tree) != nil {
		return v
	}
	l := linker{ctx: ctx, base: strings.TrimRight(linkSvc.PublicURL, "/"), slugs: map[string]string{}}
	l.walk(tree, "")
	return tree
}

type linker struct {
	ctx   context.Context
	base  string
	slugs map[string]string // project id → slug
}

func (l *linker) project(id string) string {
	if id == "" {
		return ""
	}
	if s, ok := l.slugs[id]; ok {
		return s
	}
	p, err := linkSvc.DB.ProjectFor(l.ctx, id, "")
	if err != nil {
		l.slugs[id] = ""
		return ""
	}
	l.slugs[id] = p.Slug
	return p.Slug
}

func str(m map[string]any, k string) string {
	s, _ := m[k].(string)
	return s
}

// walk adds addresses; project is the slug of the enclosing object's project.
func (l *linker) walk(n any, project string) {
	switch x := n.(type) {
	case []any:
		for _, e := range x {
			l.walk(e, project)
		}
	case map[string]any:
		if p := l.project(str(x, "projectId")); p != "" {
			project = p
		}
		_, has := x["url"]
		slug := str(x, "slug")
		switch {
		case project == "":
		case str(x, "boardId") != "" && str(x, "boardSlug") != "" && str(x, "id") != "":
			// A card: the board opens it.
			if !has {
				x["url"] = l.base + "/p/" + project + "/d/" + str(x, "boardSlug") + "?task=" + url.QueryEscape(str(x, "id"))
			}
		case docTypes[str(x, "type")] && slug != "" && str(x, "title") != "":
			if !has {
				x["url"] = l.base + "/p/" + project + "/d/" + slug
			}
		case str(x, "kind") != "" && slug != "" && str(x, "name") != "" && str(x, "projectId") != "":
			if !has {
				x["url"] = l.base + "/p/" + project + "/e/" + slug
			}
		}
		// A project itself.
		if str(x, "projectId") == "" && str(x, "ownerId") != "" && slug != "" && !has {
			x["url"] = l.base + "/p/" + slug
		}
		// A pending suggestion: people accept it here.
		if s, ok := x["suggestion"].(map[string]any); ok {
			sp := project
			if p := l.project(str(s, "projectId")); p != "" {
				sp = p
			}
			if _, ok := s["url"]; !ok && sp != "" {
				s["url"] = l.base + "/p/" + sp + "/suggestions"
			}
		}
		for k, v := range x {
			if k != "suggestion" {
				l.walk(v, project)
			}
		}
	}
}
