package core

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/phoedox/phoedox/internal/graph"
	"github.com/phoedox/phoedox/internal/store"
)

// EntityInput creates or updates an entity (matched by slug).
type EntityInput struct {
	Kind  string         `json:"kind"`
	Name  string         `json:"name"`
	Slug  string         `json:"slug,omitempty"`
	Attrs map[string]any `json:"attrs,omitempty"`
}

// EdgeInput links two entities by id or slug.
type EdgeInput struct {
	From  string         `json:"from"`
	Rel   string         `json:"rel"`
	To    string         `json:"to"`
	Attrs map[string]any `json:"attrs,omitempty"`
}

// EntityResult is either an applied change or a pending suggestion.
type EntityResult struct {
	Applied    bool              `json:"applied"`
	Created    bool              `json:"created,omitempty"`
	Entity     *store.Entity     `json:"entity,omitempty"`
	Edge       *store.Edge       `json:"edge,omitempty"`
	Suggestion *store.Suggestion `json:"suggestion,omitempty"`
}

// EntityDetail is an entity with its edges and the documents mentioning it.
type EntityDetail struct {
	Entity    store.Entity     `json:"entity"`
	Edges     []store.Edge     `json:"edges"`
	Documents []store.Document `json:"documents"`
}

func validRel(rel string) bool {
	if rel == "" || len(rel) > 40 {
		return false
	}
	for _, r := range rel {
		if !(r >= 'a' && r <= 'z' || r == '_' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

func (in *EntityInput) normalize() error {
	in.Name = strings.TrimSpace(in.Name)
	in.Kind = strings.TrimSpace(in.Kind)
	if in.Name == "" || in.Kind == "" {
		return invalid("validation.kind_name_required", nil, "kind and name are required")
	}
	if !validRel(in.Kind) {
		return invalid("validation.kind", nil, "kind must be lowercase letters, digits and _")
	}
	if in.Slug == "" {
		in.Slug = graph.Slugify(in.Name)
	}
	if err := graph.ValidateSlug(in.Slug); err != nil {
		return invalid("validation.slug", nil, "slug: %v", err)
	}
	return nil
}

// Entities lists entities of a project.
func (s *Service) Entities(ctx context.Context, a Actor, ref, kind string) ([]store.Entity, error) {
	p, err := s.project(ctx, a, ref, store.RoleReader)
	if err != nil {
		return nil, err
	}
	return s.DB.ListEntities(ctx, p.ID, kind)
}

// Entity returns an entity with edges and mentioning documents.
func (s *Service) Entity(ctx context.Context, a Actor, ref, entRef string) (EntityDetail, error) {
	p, err := s.project(ctx, a, ref, store.RoleReader)
	if err != nil {
		return EntityDetail{}, err
	}
	e, err := s.DB.GetEntity(ctx, p.ID, entRef)
	if err != nil {
		return EntityDetail{}, err
	}
	edges, err := s.DB.EdgesOf(ctx, e.ID)
	if err != nil {
		return EntityDetail{}, err
	}
	docs, err := s.DB.DocumentsMentioning(ctx, e.ID)
	return EntityDetail{Entity: e, Edges: edges, Documents: docs}, err
}

// UpsertEntity creates or updates an entity.
func (s *Service) UpsertEntity(ctx context.Context, a Actor, ref string, in EntityInput) (EntityResult, error) {
	p, err := s.project(ctx, a, ref, store.RoleEditor)
	if err != nil {
		return EntityResult{}, err
	}
	if err := in.normalize(); err != nil {
		return EntityResult{}, err
	}
	if sug, ok, err := s.maybeSuggest(ctx, a, p, "entity.upsert", "Eintrag „"+in.Name+"“ ("+in.Kind+")", opPayload{Project: p.ID, Entity: &in}); ok || err != nil {
		return EntityResult{Suggestion: sug}, err
	}
	return s.upsertEntity(ctx, a, p, in)
}

func (s *Service) upsertEntity(ctx context.Context, a Actor, p store.Project, in EntityInput) (EntityResult, error) {
	attrs, _ := json.Marshal(in.Attrs)
	if in.Attrs == nil {
		attrs = []byte("{}")
	}
	source := string(graph.SourceManual)
	if a.Via == "mcp" {
		source = "mcp"
	}
	e, created, err := s.DB.UpsertEntity(ctx, store.Entity{ID: newID(), ProjectID: p.ID, Kind: in.Kind, Slug: in.Slug, Name: in.Name, Attrs: attrs, Source: source})
	if err != nil {
		return EntityResult{}, err
	}
	s.event(ctx, a, "entity.upsert", e.ID, in)
	return EntityResult{Applied: true, Created: created, Entity: &e}, nil
}

// DeleteEntity moves an entity and its edges to the trash.
func (s *Service) DeleteEntity(ctx context.Context, a Actor, ref, entRef string) (EntityResult, error) {
	p, err := s.project(ctx, a, ref, store.RoleEditor)
	if err != nil {
		return EntityResult{}, err
	}
	e, err := s.DB.GetEntity(ctx, p.ID, entRef)
	if err != nil {
		return EntityResult{}, err
	}
	if sug, ok, err := s.maybeSuggest(ctx, a, p, "entity.delete", "Eintrag „"+e.Name+"“ löschen", opPayload{Project: p.ID, EntityRef: e.ID}); ok || err != nil {
		return EntityResult{Suggestion: sug}, err
	}
	if err := s.DB.DeleteEntity(ctx, e.ID); err != nil {
		return EntityResult{}, err
	}
	s.event(ctx, a, "entity.delete", e.ID, nil)
	return EntityResult{Applied: true, Entity: &e}, nil
}

// Link creates an edge between two entities of the project.
func (s *Service) Link(ctx context.Context, a Actor, ref string, in EdgeInput) (EntityResult, error) {
	p, err := s.project(ctx, a, ref, store.RoleEditor)
	if err != nil {
		return EntityResult{}, err
	}
	if !validRel(in.Rel) {
		return EntityResult{}, invalid("validation.rel", nil, "rel must be lowercase letters, digits and _, e.g. located_in")
	}
	from, err := s.DB.GetEntity(ctx, p.ID, in.From)
	if err != nil {
		return EntityResult{}, invalid("validation.entity_not_found", map[string]any{"entity": in.From}, "from: entity %q not found", in.From)
	}
	to, err := s.DB.GetEntity(ctx, p.ID, in.To)
	if err != nil {
		return EntityResult{}, invalid("validation.entity_not_found", map[string]any{"entity": in.To}, "to: entity %q not found", in.To)
	}
	in.From, in.To = from.ID, to.ID
	if sug, ok, err := s.maybeSuggest(ctx, a, p, "edge.link", from.Name+" –"+in.Rel+"→ "+to.Name, opPayload{Project: p.ID, Edge: &in}); ok || err != nil {
		return EntityResult{Suggestion: sug}, err
	}
	return s.link(ctx, a, in)
}

func (s *Service) link(ctx context.Context, a Actor, in EdgeInput) (EntityResult, error) {
	attrs, _ := json.Marshal(in.Attrs)
	if in.Attrs == nil {
		attrs = []byte("{}")
	}
	e, err := s.DB.LinkEntities(ctx, store.Edge{ID: newID(), From: in.From, Rel: in.Rel, To: in.To, Attrs: attrs, Source: "manual"})
	if err != nil {
		return EntityResult{}, err
	}
	s.event(ctx, a, "edge.link", e.ID, in)
	return EntityResult{Applied: true, Edge: &e}, nil
}

// Unlink removes edges between two entities (rel empty = all).
func (s *Service) Unlink(ctx context.Context, a Actor, ref string, in EdgeInput) (EntityResult, error) {
	p, err := s.project(ctx, a, ref, store.RoleEditor)
	if err != nil {
		return EntityResult{}, err
	}
	from, err := s.DB.GetEntity(ctx, p.ID, in.From)
	if err != nil {
		return EntityResult{}, err
	}
	to, err := s.DB.GetEntity(ctx, p.ID, in.To)
	if err != nil {
		return EntityResult{}, err
	}
	in.From, in.To = from.ID, to.ID
	if sug, ok, err := s.maybeSuggest(ctx, a, p, "edge.unlink", from.Name+" –"+in.Rel+"→ "+to.Name+" entfernen", opPayload{Project: p.ID, Edge: &in}); ok || err != nil {
		return EntityResult{Suggestion: sug}, err
	}
	if err := s.DB.UnlinkEntities(ctx, in.From, in.Rel, in.To); err != nil {
		return EntityResult{}, err
	}
	s.event(ctx, a, "edge.unlink", from.ID, in)
	return EntityResult{Applied: true}, nil
}

// Traverse follows edges from an entity up to depth (capped by MaxDepth).
func (s *Service) Traverse(ctx context.Context, a Actor, ref, entRef, rel, dir string, depth int) ([]store.Hop, error) {
	p, err := s.project(ctx, a, ref, store.RoleReader)
	if err != nil {
		return nil, err
	}
	e, err := s.DB.GetEntity(ctx, p.ID, entRef)
	if err != nil {
		return nil, err
	}
	if depth <= 0 || depth > s.MaxDepth {
		depth = s.MaxDepth
	}
	if dir != "in" && dir != "both" {
		dir = "out"
	}
	return s.DB.Traverse(ctx, e.ID, rel, dir, depth)
}

// Search searches one project or all visible projects.
func (s *Service) Search(ctx context.Context, a Actor, query, ref string, limit int) ([]store.Hit, error) {
	var ids []string
	if ref != "" {
		p, err := s.project(ctx, a, ref, store.RoleReader)
		if err != nil {
			return nil, err
		}
		ids = []string{p.ID}
	} else {
		var err error
		if ids, err = s.DB.VisibleProjectIDs(ctx, a.User.ID, a.User.IsAdmin); err != nil {
			return nil, err
		}
	}
	return s.DB.Search(ctx, ids, query, limit)
}
