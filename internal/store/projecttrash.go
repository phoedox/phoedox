package store

import "context"

// liveProject limits a query on a table with project_id to projects that
// are not in the trash (background jobs, tokens and links stop working
// while a project is deleted).
const liveProject = `project_id IN (SELECT id FROM projects WHERE deleted_at IS NULL)`

// TrashedProject is a deleted project that can still be restored.
type TrashedProject struct {
	Project
	DeletedAt Time   `json:"deletedAt"`
	DeletedBy string `json:"deletedBy"`
}

// withExtra scans the project columns followed by further columns.
type withExtra struct {
	r     interface{ Scan(...any) error }
	extra []any
}

func (w withExtra) Scan(dest ...any) error { return w.r.Scan(append(dest, w.extra...)...) }

// TrashProject moves a project to the trash.
func (d *DB) TrashProject(ctx context.Context, id, by string) error {
	res, err := d.Exec(ctx, `UPDATE projects SET deleted_at = ?, deleted_by = ? WHERE id = ? AND deleted_at IS NULL`, Now(), by, id)
	return affected(res, err)
}

// RestoreProject takes a project out of the trash.
func (d *DB) RestoreProject(ctx context.Context, id string) error {
	res, err := d.Exec(ctx, `UPDATE projects SET deleted_at = NULL, deleted_by = NULL WHERE id = ? AND deleted_at IS NOT NULL`, id)
	return affected(res, err)
}

// TrashedProjects lists deleted projects of the tenant that userID owns
// (all of them with all).
func (d *DB) TrashedProjects(ctx context.Context, userID string, all bool) ([]TrashedProject, error) {
	args := append(rankArgs(userID), TenantOf(ctx))
	q := `SELECT ` + projectCols + `, ` + d.rankExpr() + `, p.deleted_at, COALESCE(p.deleted_by, '')` + projectFrom + ` WHERE p.tenant_id = ? AND p.deleted_at IS NOT NULL`
	if !all {
		q += ` AND ` + d.rankExpr() + ` >= 4`
		args = append(args, rankArgs(userID)...)
	}
	rows, err := d.Query(ctx, q+` ORDER BY p.deleted_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TrashedProject{}
	for rows.Next() {
		var t TrashedProject
		p, err := scanProject(withExtra{rows, []any{&t.DeletedAt, &t.DeletedBy}}, true)
		if err != nil {
			return nil, err
		}
		t.Project = p
		out = append(out, t)
	}
	return out, rows.Err()
}

// TrashedProjectFor returns a deleted project by id or slug with the user's role.
func (d *DB) TrashedProjectFor(ctx context.Context, ref, userID string) (TrashedProject, error) {
	var t TrashedProject
	args := append(rankArgs(userID), TenantOf(ctx), ref, ref)
	p, err := scanProject(withExtra{d.QueryRow(ctx, `SELECT `+projectCols+`, `+d.rankExpr()+`, p.deleted_at, COALESCE(p.deleted_by, '')`+projectFrom+`
		WHERE p.tenant_id = ? AND (p.id = ? OR p.slug = ?) AND p.deleted_at IS NOT NULL ORDER BY p.deleted_at DESC LIMIT 1`, args...), []any{&t.DeletedAt, &t.DeletedBy}}, true)
	t.Project = p
	return t, err
}

// ExpiredProject names a project whose trash period is over.
type ExpiredProject struct{ ID, TenantID, Slug string }

// ExpiredTrashedProjects lists projects deleted before the cutoff (all tenants with a System context).
func (d *DB) ExpiredTrashedProjects(ctx context.Context, before Time) ([]ExpiredProject, error) {
	rows, err := d.Query(ctx, `SELECT id, tenant_id, slug FROM projects WHERE deleted_at IS NOT NULL AND deleted_at < ?`, before)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ExpiredProject
	for rows.Next() {
		var e ExpiredProject
		if err := rows.Scan(&e.ID, &e.TenantID, &e.Slug); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
