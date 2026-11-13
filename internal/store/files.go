package store

import (
	"context"
	"database/sql"
	"errors"
)

// File is an uploaded file of a project, optionally attached to a document or entry.
type File struct {
	ID         string `json:"id"`
	ProjectID  string `json:"projectId"`
	SHA256     string `json:"sha256"`
	Name       string `json:"name"`
	Mime       string `json:"mime"`
	Size       int64  `json:"size"`
	DocumentID string `json:"documentId,omitempty"`
	EntityID   string `json:"entityId,omitempty"`
	CreatedBy  string `json:"createdBy"`
	CreatedAt  Time   `json:"createdAt"`
}

const fileCols = `id, project_id, sha256, name, mime, size, COALESCE(document_id, ''), COALESCE(entity_id, ''), created_by, created_at`

func scanFile(r interface{ Scan(...any) error }) (File, error) {
	var f File
	err := r.Scan(&f.ID, &f.ProjectID, &f.SHA256, &f.Name, &f.Mime, &f.Size, &f.DocumentID, &f.EntityID, &f.CreatedBy, &f.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return f, ErrNotFound
	}
	return f, err
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// CreateFile stores the metadata of an uploaded file.
func (d *DB) CreateFile(ctx context.Context, f File) (File, error) {
	f.CreatedAt = Now()
	_, err := d.Exec(ctx, `INSERT INTO files (id, tenant_id, project_id, sha256, name, mime, size, document_id, entity_id, created_by, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, f.ID, DefaultTenant, f.ProjectID, f.SHA256, f.Name, f.Mime, f.Size,
		nullable(f.DocumentID), nullable(f.EntityID), f.CreatedBy, f.CreatedAt)
	return f, err
}

// GetFile returns a live file by id.
func (d *DB) GetFile(ctx context.Context, id string) (File, error) {
	return scanFile(d.QueryRow(ctx, `SELECT `+fileCols+` FROM files WHERE id = ? AND deleted_at IS NULL`, id))
}

// FileFilter selects files of a project.
type FileFilter struct {
	DocumentID string
	EntityID   string
}

// ListFiles lists live files of a project, newest first.
func (d *DB) ListFiles(ctx context.Context, projectID string, f FileFilter) ([]File, error) {
	q := `SELECT ` + fileCols + ` FROM files WHERE project_id = ? AND deleted_at IS NULL`
	args := []any{projectID}
	if f.DocumentID != "" {
		q += ` AND document_id = ?`
		args = append(args, f.DocumentID)
	}
	if f.EntityID != "" {
		q += ` AND entity_id = ?`
		args = append(args, f.EntityID)
	}
	rows, err := d.Query(ctx, q+` ORDER BY created_at DESC, name`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []File{}
	for rows.Next() {
		f, err := scanFile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// RenameFile changes the display name of a file.
func (d *DB) RenameFile(ctx context.Context, id, name string) error {
	res, err := d.Exec(ctx, `UPDATE files SET name = ? WHERE id = ? AND deleted_at IS NULL`, name, id)
	return affected(res, err)
}

// DeleteFile moves a file to the trash.
func (d *DB) DeleteFile(ctx context.Context, id string) error {
	res, err := d.Exec(ctx, `UPDATE files SET deleted_at = ? WHERE id = ? AND deleted_at IS NULL`, Now(), id)
	return affected(res, err)
}

// RestoreFile takes a file out of the trash.
func (d *DB) RestoreFile(ctx context.Context, projectID, id string) (File, error) {
	res, err := d.Exec(ctx, `UPDATE files SET deleted_at = NULL WHERE id = ? AND project_id = ? AND deleted_at IS NOT NULL`, id, projectID)
	if err := affected(res, err); err != nil {
		return File{}, err
	}
	return d.GetFile(ctx, id)
}

// HashInUse reports whether any file row (live or trashed, any project) uses the content.
func (d *DB) HashInUse(ctx context.Context, hash string) (bool, error) {
	var one int
	err := d.QueryRow(ctx, `SELECT 1 FROM files WHERE sha256 = ? LIMIT 1`, hash).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// FileUsage is the stored size of a project's live and trashed files.
func (d *DB) FileUsage(ctx context.Context, projectID string) (int64, error) {
	var n sql.NullInt64
	err := d.QueryRow(ctx, `SELECT SUM(size) FROM files WHERE project_id = ?`, projectID).Scan(&n)
	return n.Int64, err
}

// FileHashes returns the content hashes of files matching a condition
// (used to free contents after purging).
func (d *DB) fileHashes(ctx context.Context, where string, args ...any) ([]string, error) {
	rows, err := d.Query(ctx, `SELECT DISTINCT sha256 FROM files WHERE `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var h string
		if err := rows.Scan(&h); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// TrashedFileHashes returns the hash of a trashed file (empty if id is no trashed file).
func (d *DB) TrashedFileHashes(ctx context.Context, projectID, id string) ([]string, error) {
	return d.fileHashes(ctx, `id = ? AND project_id = ? AND deleted_at IS NOT NULL`, id, projectID)
}

// ExpiredFileHashes returns hashes of files trashed before a time.
func (d *DB) ExpiredFileHashes(ctx context.Context, before Time) ([]string, error) {
	return d.fileHashes(ctx, `deleted_at IS NOT NULL AND deleted_at < ?`, before)
}

// ProjectFileHashes returns the hashes of all files of a project.
func (d *DB) ProjectFileHashes(ctx context.Context, projectID string) ([]string, error) {
	return d.fileHashes(ctx, `project_id = ?`, projectID)
}
