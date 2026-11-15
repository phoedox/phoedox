package store

import (
	"context"
	"database/sql"
	"errors"
)

// CodeRepo is a Git repository connected to a project (ADR-0063).
type CodeRepo struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`
	Provider  string `json:"provider"` // github | gitea
	WebURL    string `json:"webUrl"`
	APIURL    string `json:"apiUrl"`
	Owner     string `json:"owner"`
	Name      string `json:"name"`
	TokenVar  string `json:"tokenVar,omitempty"`
	CreatedBy string `json:"createdBy"`
	CreatedAt Time   `json:"createdAt"`
	LastCheck Time   `json:"lastCheck"`
	LastError string `json:"lastError,omitempty"`
}

// TaskCodeLink is a commit, release or pull request linked to a task.
type TaskCodeLink struct {
	ID        string `json:"id"`
	TaskID    string `json:"taskId"`
	RepoID    string `json:"repoId"`
	Repo      string `json:"repo"` // owner/name
	Kind      string `json:"kind"` // commit | release | pull
	Ref       string `json:"ref"`  // sha, tag or number
	Title     string `json:"title,omitempty"`
	URL       string `json:"url,omitempty"`
	At        Time   `json:"at"`
	Auto      bool   `json:"auto,omitempty"`
	CreatedBy string `json:"createdBy"`
	CreatedAt Time   `json:"createdAt"`
}

const repoCols = `id, project_id, provider, web_url, api_url, owner, name, token_var, created_by, created_at, last_check, last_error`

func scanRepo(r interface{ Scan(...any) error }) (CodeRepo, error) {
	var x CodeRepo
	err := r.Scan(&x.ID, &x.ProjectID, &x.Provider, &x.WebURL, &x.APIURL, &x.Owner, &x.Name, &x.TokenVar, &x.CreatedBy, &x.CreatedAt, &x.LastCheck, &x.LastError)
	if errors.Is(err, sql.ErrNoRows) {
		return x, ErrNotFound
	}
	return x, err
}

// CodeRepos lists the repositories of a project ("" = of all projects).
func (d *DB) CodeRepos(ctx context.Context, projectID string) ([]CodeRepo, error) {
	q, args := `SELECT `+repoCols+` FROM code_repos ORDER BY owner, name`, []any{}
	if projectID != "" {
		q, args = `SELECT `+repoCols+` FROM code_repos WHERE project_id = ? ORDER BY owner, name`, []any{projectID}
	}
	rows, err := d.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CodeRepo{}
	for rows.Next() {
		x, err := scanRepo(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// GetCodeRepo returns a repository of a project.
func (d *DB) GetCodeRepo(ctx context.Context, projectID, id string) (CodeRepo, error) {
	return scanRepo(d.QueryRow(ctx, `SELECT `+repoCols+` FROM code_repos WHERE id = ? AND project_id = ?`, id, projectID))
}

// AddCodeRepo connects a repository.
func (d *DB) AddCodeRepo(ctx context.Context, x CodeRepo) error {
	_, err := d.Exec(ctx, `INSERT INTO code_repos (id, tenant_id, project_id, provider, web_url, api_url, owner, name, token_var, created_by, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, x.ID, TenantOf(ctx), x.ProjectID, x.Provider, x.WebURL, x.APIURL, x.Owner, x.Name, x.TokenVar, x.CreatedBy, Now())
	if err != nil && isUniqueViolation(err) {
		return ErrConflict
	}
	return err
}

// SetCodeRepoToken changes the secret a repository uses.
func (d *DB) SetCodeRepoToken(ctx context.Context, projectID, id, tokenVar string) error {
	res, err := d.Exec(ctx, `UPDATE code_repos SET token_var = ? WHERE id = ? AND project_id = ?`, tokenVar, id, projectID)
	return affected(res, err)
}

// CodeRepoChecked stores the result of the last release check.
func (d *DB) CodeRepoChecked(ctx context.Context, id, message string) error {
	_, err := d.Exec(ctx, `UPDATE code_repos SET last_check = ?, last_error = ? WHERE id = ?`, Now(), message, id)
	return err
}

// DeleteCodeRepo disconnects a repository and drops its links.
func (d *DB) DeleteCodeRepo(ctx context.Context, projectID, id string) error {
	res, err := d.Exec(ctx, `DELETE FROM code_repos WHERE id = ? AND project_id = ?`, id, projectID)
	return affected(res, err)
}

const linkCols = `l.id, l.task_id, l.repo_id, r.owner || '/' || r.name, l.kind, l.ref, l.title, l.url, l.at, l.auto, l.created_by, l.created_at`

// TaskCodeLinks lists the code links of a project, or of one task.
func (d *DB) TaskCodeLinks(ctx context.Context, projectID, taskID string) ([]TaskCodeLink, error) {
	q := `SELECT ` + linkCols + ` FROM task_code_links l JOIN code_repos r ON r.id = l.repo_id WHERE l.project_id = ?`
	args := []any{projectID}
	if taskID != "" {
		q += ` AND l.task_id = ?`
		args = append(args, taskID)
	}
	rows, err := d.Query(ctx, q+` ORDER BY l.kind, l.at, l.created_at`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TaskCodeLink{}
	for rows.Next() {
		var x TaskCodeLink
		if err := rows.Scan(&x.ID, &x.TaskID, &x.RepoID, &x.Repo, &x.Kind, &x.Ref, &x.Title, &x.URL, &x.At, &x.Auto, &x.CreatedBy, &x.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// AddTaskCodeLink links a commit, release or pull request; false if it
// was linked already.
func (d *DB) AddTaskCodeLink(ctx context.Context, projectID string, x TaskCodeLink) (bool, error) {
	var at any
	if !x.At.IsZero() {
		at = x.At
	}
	res, err := d.Exec(ctx, `INSERT INTO task_code_links (id, tenant_id, project_id, task_id, repo_id, kind, ref, title, url, at, auto, created_by, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`,
		x.ID, TenantOf(ctx), projectID, x.TaskID, x.RepoID, x.Kind, x.Ref, x.Title, x.URL, at, x.Auto, x.CreatedBy, Now())
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// DeleteTaskCodeLink removes a link of a task.
func (d *DB) DeleteTaskCodeLink(ctx context.Context, projectID, taskID, id string) error {
	res, err := d.Exec(ctx, `DELETE FROM task_code_links WHERE id = ? AND task_id = ? AND project_id = ?`, id, taskID, projectID)
	return affected(res, err)
}
