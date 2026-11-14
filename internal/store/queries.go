package store

import (
	"context"
	"database/sql"
	"errors"
)

// SavedQuery is a named query over the knowledge graph.
type SavedQuery struct {
	ID        string `json:"id"`
	ProjectID string `json:"-"`
	Slug      string `json:"slug"`
	Name      string `json:"name"`
	Query     JSON   `json:"query"`
	CreatedBy string `json:"createdBy"`
	CreatedAt Time   `json:"createdAt"`
	UpdatedAt Time   `json:"updatedAt"`
}

const queryCols = `id, project_id, slug, name, query, created_by, created_at, updated_at`

func scanQuery(r interface{ Scan(...any) error }) (SavedQuery, error) {
	var q SavedQuery
	var raw string
	err := r.Scan(&q.ID, &q.ProjectID, &q.Slug, &q.Name, &raw, &q.CreatedBy, &q.CreatedAt, &q.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return q, ErrNotFound
	}
	q.Query = JSON(raw)
	return q, err
}

// SavedQueries lists the saved queries of a project by name.
func (d *DB) SavedQueries(ctx context.Context, projectID string) ([]SavedQuery, error) {
	rows, err := d.Query(ctx, `SELECT `+queryCols+` FROM saved_queries WHERE project_id = ? ORDER BY name`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SavedQuery{}
	for rows.Next() {
		q, err := scanQuery(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

// GetSavedQuery finds a saved query by id or slug.
func (d *DB) GetSavedQuery(ctx context.Context, projectID, ref string) (SavedQuery, error) {
	return scanQuery(d.QueryRow(ctx, `SELECT `+queryCols+` FROM saved_queries WHERE project_id = ? AND (id = ? OR slug = ?)`, projectID, ref, ref))
}

// SaveQuery creates or updates a saved query.
func (d *DB) SaveQuery(ctx context.Context, q SavedQuery) error {
	_, err := d.Exec(ctx, `INSERT INTO saved_queries (id, tenant_id, project_id, slug, name, query, created_by, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET slug = excluded.slug, name = excluded.name, query = excluded.query, updated_at = excluded.updated_at`,
		q.ID, TenantOf(ctx), q.ProjectID, q.Slug, q.Name, string(q.Query), q.CreatedBy, q.CreatedAt, q.UpdatedAt)
	if err != nil && isUniqueViolation(err) {
		return ErrConflict
	}
	return err
}

// DeleteSavedQuery removes a saved query.
func (d *DB) DeleteSavedQuery(ctx context.Context, projectID, id string) error {
	_, err := d.Exec(ctx, `DELETE FROM saved_queries WHERE project_id = ? AND id = ?`, projectID, id)
	return err
}
