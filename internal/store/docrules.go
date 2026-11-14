package store

import (
	"context"
	"database/sql"
	"errors"
)

// EntityMeta holds the responsible person and the review interval of an entry.
type EntityMeta struct {
	EntityID   string `json:"entityId"`
	TenantID   string `json:"-"`
	ProjectID  string `json:"-"`
	OwnerID    string `json:"ownerId,omitempty"`
	ReviewDays int    `json:"reviewDays"`
	ReviewedAt Time   `json:"reviewedAt"`
	ReviewedBy string `json:"-"`
	NotifiedAt Time   `json:"-"`
	UpdatedBy  string `json:"-"`
	UpdatedAt  Time   `json:"updatedAt"`
}

const entityMetaCols = `entity_id, tenant_id, project_id, COALESCE(owner_id, ''), review_days, reviewed_at, reviewed_by, notified_at, updated_by, updated_at`

func scanEntityMeta(r interface{ Scan(...any) error }) (EntityMeta, error) {
	var m EntityMeta
	err := r.Scan(&m.EntityID, &m.TenantID, &m.ProjectID, &m.OwnerID, &m.ReviewDays, &m.ReviewedAt, &m.ReviewedBy, &m.NotifiedAt, &m.UpdatedBy, &m.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return m, ErrNotFound
	}
	return m, err
}

// GetEntityMeta returns the meta data of an entry (ErrNotFound if none).
func (d *DB) GetEntityMeta(ctx context.Context, entityID string) (EntityMeta, error) {
	return scanEntityMeta(d.QueryRow(ctx, `SELECT `+entityMetaCols+` FROM entity_meta WHERE entity_id = ?`, entityID))
}

// SaveEntityMeta creates or replaces the meta data of an entry.
func (d *DB) SaveEntityMeta(ctx context.Context, m EntityMeta) error {
	var owner any
	if m.OwnerID != "" {
		owner = m.OwnerID
	}
	_, err := d.Exec(ctx, `INSERT INTO entity_meta (entity_id, tenant_id, project_id, owner_id, review_days, reviewed_at, reviewed_by, notified_at, updated_by, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (entity_id) DO UPDATE SET owner_id = excluded.owner_id, review_days = excluded.review_days, reviewed_at = excluded.reviewed_at,
		  reviewed_by = excluded.reviewed_by, notified_at = excluded.notified_at, updated_by = excluded.updated_by, updated_at = excluded.updated_at`,
		m.EntityID, TenantOf(ctx), m.ProjectID, owner, m.ReviewDays, m.ReviewedAt, m.ReviewedBy, m.NotifiedAt, m.UpdatedBy, m.UpdatedAt)
	return err
}

// EntityMetas lists the meta data of a project's live entries by entity id,
// or of all projects (projectID "", System context).
func (d *DB) EntityMetas(ctx context.Context, projectID string) (map[string]EntityMeta, error) {
	q := `SELECT m.entity_id, m.tenant_id, m.project_id, COALESCE(m.owner_id, ''), m.review_days, m.reviewed_at, m.reviewed_by, m.notified_at, m.updated_by, m.updated_at
		FROM entity_meta m JOIN entities e ON e.id = m.entity_id WHERE e.deleted_at IS NULL`
	args := []any{}
	if projectID != "" {
		q += ` AND m.project_id = ?`
		args = append(args, projectID)
	}
	rows, err := d.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]EntityMeta{}
	for rows.Next() {
		m, err := scanEntityMeta(rows)
		if err != nil {
			return nil, err
		}
		out[m.EntityID] = m
	}
	return out, rows.Err()
}

// KindRules returns the documentation rules of a project's kinds (raw JSON by kind).
func (d *DB) KindRules(ctx context.Context, projectID string) (map[string]JSON, error) {
	rows, err := d.Query(ctx, `SELECT kind, rules FROM kind_rules WHERE project_id = ?`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]JSON{}
	for rows.Next() {
		var kind, rules string
		if err := rows.Scan(&kind, &rules); err != nil {
			return nil, err
		}
		out[kind] = JSON(rules)
	}
	return out, rows.Err()
}

// SetKindRules stores the rules of a kind; empty rules remove them.
func (d *DB) SetKindRules(ctx context.Context, projectID, kind string, rules JSON, by string) error {
	if len(rules) == 0 || string(rules) == "{}" {
		_, err := d.Exec(ctx, `DELETE FROM kind_rules WHERE project_id = ? AND kind = ?`, projectID, kind)
		return err
	}
	_, err := d.Exec(ctx, `INSERT INTO kind_rules (tenant_id, project_id, kind, rules, updated_by, updated_at) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (project_id, kind) DO UPDATE SET rules = excluded.rules, updated_by = excluded.updated_by, updated_at = excluded.updated_at`,
		TenantOf(ctx), projectID, kind, string(rules), by, Now())
	return err
}
