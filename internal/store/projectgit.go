package store

import (
	"context"
	"database/sql"
	"errors"
)

// ProjectGit is the Git sync setting of a project.
type ProjectGit struct {
	ProjectID  string `json:"-"`
	TenantID   string `json:"-"`
	URL        string `json:"url"`
	Branch     string `json:"branch"`
	Folder     string `json:"folder"`
	TokenVar   string `json:"tokenVar"`
	Enabled    bool   `json:"enabled"`
	LastSync   Time   `json:"lastSync"`
	LastCommit string `json:"lastCommit"`
	LastError  string `json:"lastError"`
	UpdatedBy  string `json:"-"`
	UpdatedAt  Time   `json:"updatedAt"`
}

const gitCols = `project_id, tenant_id, url, branch, folder, token_var, enabled, last_sync, last_commit, last_error, updated_by, updated_at`

func scanGit(r interface{ Scan(...any) error }) (ProjectGit, error) {
	var g ProjectGit
	err := r.Scan(&g.ProjectID, &g.TenantID, &g.URL, &g.Branch, &g.Folder, &g.TokenVar, &g.Enabled, &g.LastSync, &g.LastCommit, &g.LastError, &g.UpdatedBy, &g.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return g, ErrNotFound
	}
	return g, err
}

// GetProjectGit returns the Git sync setting of a project.
func (d *DB) GetProjectGit(ctx context.Context, projectID string) (ProjectGit, error) {
	return scanGit(d.QueryRow(ctx, `SELECT `+gitCols+` FROM project_git WHERE project_id = ?`, projectID))
}

// SaveProjectGit stores the setting (keeps the sync state).
func (d *DB) SaveProjectGit(ctx context.Context, g ProjectGit) error {
	_, err := d.Exec(ctx, `INSERT INTO project_git (project_id, tenant_id, url, branch, folder, token_var, enabled, updated_by, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (project_id) DO UPDATE SET url = excluded.url, branch = excluded.branch, folder = excluded.folder, token_var = excluded.token_var,
		enabled = excluded.enabled, updated_by = excluded.updated_by, updated_at = excluded.updated_at`,
		g.ProjectID, TenantOf(ctx), g.URL, g.Branch, g.Folder, g.TokenVar, g.Enabled, g.UpdatedBy, Now())
	return err
}

// SaveGitResult stores the outcome of a sync.
func (d *DB) SaveGitResult(ctx context.Context, projectID string, at Time, commit, errText string) error {
	if commit == "" {
		_, err := d.Exec(ctx, `UPDATE project_git SET last_sync = ?, last_error = ? WHERE project_id = ?`, at, errText, projectID)
		return err
	}
	_, err := d.Exec(ctx, `UPDATE project_git SET last_sync = ?, last_commit = ?, last_error = ? WHERE project_id = ?`, at, commit, errText, projectID)
	return err
}

// DeleteProjectGit switches Git sync off for good.
func (d *DB) DeleteProjectGit(ctx context.Context, projectID string) error {
	_, err := d.Exec(ctx, `DELETE FROM project_git WHERE project_id = ?`, projectID)
	return err
}

// EnabledProjectGits lists the enabled Git syncs of all tenants (system context).
func (d *DB) EnabledProjectGits(ctx context.Context) ([]ProjectGit, error) {
	rows, err := d.Query(ctx, `SELECT `+gitCols+` FROM project_git WHERE enabled = ?`, true)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProjectGit
	for rows.Next() {
		g, err := scanGit(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// ProjectChangedSince tells whether pages, entries or cards changed after t.
func (d *DB) ProjectChangedSince(ctx context.Context, projectID string, t Time) (bool, error) {
	var n int
	err := d.QueryRow(ctx, `SELECT
		(SELECT COUNT(*) FROM documents WHERE project_id = ? AND (updated_at > ? OR (deleted_at IS NOT NULL AND deleted_at > ?))) +
		(SELECT COUNT(*) FROM entities WHERE project_id = ? AND updated_at > ?) +
		(SELECT COUNT(*) FROM tasks WHERE project_id = ? AND updated_at > ?)`,
		projectID, t, t, projectID, t, projectID, t).Scan(&n)
	return n > 0, err
}
