package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// Entity is a node of the knowledge graph.
type Entity struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`
	Kind      string `json:"kind"`
	Slug      string `json:"slug"`
	Name      string `json:"name"`
	Attrs     JSON   `json:"attrs"`
	Source    string `json:"source"`
	CreatedAt Time   `json:"createdAt"`
	UpdatedAt Time   `json:"updatedAt"`
}

const entityCols = `id, project_id, kind, slug, name, attrs, source, created_at, updated_at`

func scanEntity(r interface{ Scan(...any) error }) (Entity, error) {
	var e Entity
	err := r.Scan(&e.ID, &e.ProjectID, &e.Kind, &e.Slug, &e.Name, &e.Attrs, &e.Source, &e.CreatedAt, &e.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return e, ErrNotFound
	}
	return e, err
}

// UpsertEntity creates the entity or updates it if the slug exists in the project.
// It returns the stored entity and whether it was created.
func (d *DB) UpsertEntity(ctx context.Context, e Entity) (Entity, bool, error) {
	now := Now()
	existing, err := d.GetEntity(ctx, e.ProjectID, e.Slug)
	switch {
	case err == nil:
		e.ID, e.CreatedAt, e.UpdatedAt = existing.ID, existing.CreatedAt, now
		_, err = d.Exec(ctx, `UPDATE entities SET kind = ?, name = ?, attrs = ?, source = ?, updated_at = ? WHERE id = ?`,
			e.Kind, e.Name, e.Attrs, e.Source, now, e.ID)
		return e, false, err
	case errors.Is(err, ErrNotFound):
		e.CreatedAt, e.UpdatedAt = now, now
		_, err = d.Exec(ctx, `INSERT INTO entities (id, tenant_id, project_id, kind, slug, name, attrs, source, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, e.ID, TenantOf(ctx), e.ProjectID, e.Kind, e.Slug, e.Name, e.Attrs, e.Source, now, now)
		return e, true, err
	default:
		return e, false, err
	}
}

// GetEntity returns an entity of a project by id or slug.
func (d *DB) GetEntity(ctx context.Context, projectID, ref string) (Entity, error) {
	return scanEntity(d.QueryRow(ctx, `SELECT `+entityCols+` FROM entities
		WHERE project_id = ? AND (id = ? OR slug = ?) AND deleted_at IS NULL`, projectID, ref, ref))
}

// EntitiesBySlugs resolves slugs in one project (used for mentions).
func (d *DB) EntitiesBySlugs(ctx context.Context, projectID string, slugs []string) ([]Entity, error) {
	if len(slugs) == 0 {
		return nil, nil
	}
	ph := strings.TrimSuffix(strings.Repeat("?, ", len(slugs)), ", ")
	args := []any{projectID}
	for _, s := range slugs {
		args = append(args, s)
	}
	rows, err := d.Query(ctx, `SELECT `+entityCols+` FROM entities WHERE project_id = ? AND deleted_at IS NULL AND slug IN (`+ph+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectEntities(rows)
}

func collectEntities(rows *sql.Rows) ([]Entity, error) {
	var out []Entity
	for rows.Next() {
		e, err := scanEntity(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ListEntities lists entities, optionally of one kind.
func (d *DB) ListEntities(ctx context.Context, projectID, kind string) ([]Entity, error) {
	query := `SELECT ` + entityCols + ` FROM entities WHERE project_id = ? AND deleted_at IS NULL`
	args := []any{projectID}
	if kind != "" {
		query += ` AND kind = ?`
		args = append(args, kind)
	}
	rows, err := d.Query(ctx, query+` ORDER BY kind, name`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectEntities(rows)
}

// DeleteEntity moves an entity and its edges to the trash.
func (d *DB) DeleteEntity(ctx context.Context, id string) error {
	now := Now()
	return d.InTx(ctx, func(tx *Tx) error {
		res, err := tx.Exec(ctx, `UPDATE entities SET deleted_at = ? WHERE id = ? AND deleted_at IS NULL`, now, id)
		if err := affected(res, err); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE edges SET deleted_at = ? WHERE (from_id = ? OR to_id = ?) AND deleted_at IS NULL`, now, id, id)
		return err
	})
}

// Edge is a typed relation between two entities.
type Edge struct {
	ID       string `json:"id"`
	From     string `json:"from"`
	Rel      string `json:"rel"`
	To       string `json:"to"`
	Attrs    JSON   `json:"attrs"`
	Source   string `json:"source"`
	FromName string `json:"fromName,omitempty"`
	ToName   string `json:"toName,omitempty"`
	FromSlug string `json:"fromSlug,omitempty"`
	ToSlug   string `json:"toSlug,omitempty"`
	FromKind string `json:"fromKind,omitempty"`
	ToKind   string `json:"toKind,omitempty"`
}

// LinkEntities creates an edge unless the same (from, rel, to) exists; then it updates attrs.
func (d *DB) LinkEntities(ctx context.Context, e Edge) (Edge, error) {
	var existing string
	err := d.QueryRow(ctx, `SELECT id FROM edges WHERE from_id = ? AND rel = ? AND to_id = ? AND deleted_at IS NULL`, e.From, e.Rel, e.To).Scan(&existing)
	if err == nil {
		e.ID = existing
		_, err = d.Exec(ctx, `UPDATE edges SET attrs = ?, source = ? WHERE id = ?`, e.Attrs, e.Source, e.ID)
		return e, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return e, err
	}
	_, err = d.Exec(ctx, `INSERT INTO edges (id, tenant_id, from_id, rel, to_id, attrs, source, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID, TenantOf(ctx), e.From, e.Rel, e.To, e.Attrs, e.Source, Now())
	return e, err
}

// UnlinkEntities removes edges between two entities (all relations if rel is empty).
func (d *DB) UnlinkEntities(ctx context.Context, from, rel, to string) error {
	query := `UPDATE edges SET deleted_at = ? WHERE from_id = ? AND to_id = ? AND deleted_at IS NULL`
	args := []any{Now(), from, to}
	if rel != "" {
		query += ` AND rel = ?`
		args = append(args, rel)
	}
	res, err := d.Exec(ctx, query, args...)
	return affected(res, err)
}

// EdgesOf returns all edges touching an entity, with the names of both ends.
func (d *DB) EdgesOf(ctx context.Context, entityID string) ([]Edge, error) {
	return d.queryEdges(ctx, `(e.from_id = ? OR e.to_id = ?)`, `e.rel`, entityID, entityID)
}

func (d *DB) queryEdges(ctx context.Context, where, order string, args ...any) ([]Edge, error) {
	rows, err := d.Query(ctx, `SELECT e.id, e.from_id, e.rel, e.to_id, e.attrs, e.source, f.name, t.name, f.slug, t.slug, f.kind, t.kind
		FROM edges e JOIN entities f ON f.id = e.from_id JOIN entities t ON t.id = e.to_id
		WHERE `+where+` AND e.deleted_at IS NULL AND f.deleted_at IS NULL AND t.deleted_at IS NULL
		ORDER BY `+order, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Edge
	for rows.Next() {
		var e Edge
		if err := rows.Scan(&e.ID, &e.From, &e.Rel, &e.To, &e.Attrs, &e.Source, &e.FromName, &e.ToName, &e.FromSlug, &e.ToSlug, &e.FromKind, &e.ToKind); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Hop is one step of a traversal.
type Hop struct {
	Depth  int    `json:"depth"`
	Entity Entity `json:"entity"`
	Via    string `json:"via"` // relation used to reach this entity
}

// Traverse follows edges from an entity. dir is "out", "in" or "both";
// rel limits the relation (empty = all). Cycles are not revisited and every
// entity is returned once, at its shortest distance.
func (d *DB) Traverse(ctx context.Context, startID, rel, dir string, maxDepth int) ([]Hop, error) {
	var join, next string
	switch dir {
	case "in":
		join, next = `e.to_id = w.id`, `e.from_id`
	case "both":
		join, next = `(e.from_id = w.id OR e.to_id = w.id)`, `CASE WHEN e.from_id = w.id THEN e.to_id ELSE e.from_id END`
	default:
		join, next = `e.from_id = w.id`, `e.to_id`
	}
	args := []any{startID, startID}
	relSQL := ""
	if rel != "" {
		relSQL = ` AND e.rel = ?`
		args = append(args, rel)
	}
	args = append(args, maxDepth)
	query := `WITH RECURSIVE walk(id, depth, via, path) AS (
		SELECT CAST(? AS TEXT), 0, CAST('' AS TEXT), CAST(',' || ? || ',' AS TEXT)
		UNION ALL
		SELECT ` + next + `, w.depth + 1, e.rel, w.path || ` + next + ` || ','
		FROM walk w JOIN edges e ON ` + join + ` AND e.deleted_at IS NULL` + relSQL + `
		WHERE w.depth < ? AND w.path NOT LIKE '%,' || ` + next + ` || ',%'
	)
	SELECT w.depth, w.via, ` + prefixCols("en.", entityCols) + `
	FROM walk w JOIN entities en ON en.id = w.id
	WHERE w.depth > 0 AND en.deleted_at IS NULL
	ORDER BY w.depth, en.name`
	rows, err := d.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Hop
	seen := map[string]bool{startID: true}
	for rows.Next() {
		var h Hop
		e := &h.Entity
		if err := rows.Scan(&h.Depth, &h.Via, &e.ID, &e.ProjectID, &e.Kind, &e.Slug, &e.Name, &e.Attrs, &e.Source, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, err
		}
		if seen[e.ID] {
			continue
		}
		seen[e.ID] = true
		out = append(out, h)
	}
	return out, rows.Err()
}

// MergeEntities moves relations, page links and attachments of remove to
// keep and puts remove in the trash. Relations that would exist twice or
// point to keep itself are dropped.
func (d *DB) MergeEntities(ctx context.Context, keep, remove string) error {
	now := Now()
	return d.InTx(ctx, func(tx *Tx) error {
		steps := []struct {
			q    string
			args []any
		}{
			{`UPDATE edges SET from_id = ? WHERE from_id = ? AND deleted_at IS NULL`, []any{keep, remove}},
			{`UPDATE edges SET to_id = ? WHERE to_id = ? AND deleted_at IS NULL`, []any{keep, remove}},
			{`UPDATE edges SET deleted_at = ? WHERE from_id = to_id AND from_id = ? AND deleted_at IS NULL`, []any{now, keep}},
			// Keep the oldest of edges that now exist twice.
			{`UPDATE edges SET deleted_at = ? WHERE deleted_at IS NULL AND (from_id = ? OR to_id = ?) AND EXISTS (
				SELECT 1 FROM edges o WHERE o.deleted_at IS NULL AND o.from_id = edges.from_id AND o.rel = edges.rel AND o.to_id = edges.to_id
				AND (o.created_at < edges.created_at OR (o.created_at = edges.created_at AND o.id < edges.id)))`, []any{now, keep, keep}},
			{`DELETE FROM document_links WHERE entity_id = ? AND document_id IN (SELECT document_id FROM document_links WHERE entity_id = ?)`, []any{remove, keep}},
			{`UPDATE document_links SET entity_id = ? WHERE entity_id = ?`, []any{keep, remove}},
			{`UPDATE files SET entity_id = ? WHERE entity_id = ?`, []any{keep, remove}},
			{`UPDATE entities SET deleted_at = ? WHERE id = ? AND deleted_at IS NULL`, []any{now, remove}},
		}
		for _, s := range steps {
			if _, err := tx.Exec(ctx, s.q, s.args...); err != nil {
				return err
			}
		}
		return nil
	})
}
