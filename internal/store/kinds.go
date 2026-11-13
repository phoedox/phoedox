package store

import "context"

// ProjectKind is a project's own kind definition or an extension of a core kind.
type ProjectKind struct {
	ProjectID string `json:"projectId"`
	Kind      string `json:"kind"`
	Def       JSON   `json:"def"`
	UpdatedBy string `json:"updatedBy"`
	UpdatedAt Time   `json:"updatedAt"`
}

// ProjectKinds lists the kind definitions of a project.
func (d *DB) ProjectKinds(ctx context.Context, projectID string) ([]ProjectKind, error) {
	rows, err := d.Query(ctx, `SELECT project_id, kind, def, updated_by, updated_at FROM project_kinds WHERE project_id = ? ORDER BY kind`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProjectKind
	for rows.Next() {
		var k ProjectKind
		if err := rows.Scan(&k.ProjectID, &k.Kind, &k.Def, &k.UpdatedBy, &k.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// SetProjectKind stores (replaces) a kind definition of a project.
func (d *DB) SetProjectKind(ctx context.Context, k ProjectKind) error {
	return d.InTx(ctx, func(tx *Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM project_kinds WHERE project_id = ? AND kind = ?`, k.ProjectID, k.Kind); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO project_kinds (tenant_id, project_id, kind, def, updated_by, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
			TenantOf(ctx), k.ProjectID, k.Kind, k.Def, k.UpdatedBy, Now())
		return err
	})
}

// DeleteProjectKind removes a kind definition of a project.
func (d *DB) DeleteProjectKind(ctx context.Context, projectID, kind string) error {
	res, err := d.Exec(ctx, `DELETE FROM project_kinds WHERE project_id = ? AND kind = ?`, projectID, kind)
	return affected(res, err)
}

// CountEntitiesByKind counts live entities per kind in a project.
func (d *DB) CountEntitiesByKind(ctx context.Context, projectID string) (map[string]int, error) {
	rows, err := d.Query(ctx, `SELECT kind, COUNT(*) FROM entities WHERE project_id = ? AND deleted_at IS NULL GROUP BY kind`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var k string
		var n int
		if err := rows.Scan(&k, &n); err != nil {
			return nil, err
		}
		out[k] = n
	}
	return out, rows.Err()
}

// EdgesOfProject lists all live edges between live entities of a project.
func (d *DB) EdgesOfProject(ctx context.Context, projectID string) ([]Edge, error) {
	return d.queryEdges(ctx, `f.project_id = ?`, `e.created_at`, projectID)
}
