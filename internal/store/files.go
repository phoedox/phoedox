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
	Version    int    `json:"version"`
	// ThumbSHA256 is the content of the preview image ("" = none yet).
	ThumbSHA256 string `json:"-"`
	HasPreview  bool   `json:"hasPreview"`
	// TextStatus tells whether text was extracted for search: "" (pending), done, none, failed.
	TextStatus string `json:"textStatus"`
}

const fileCols = `id, project_id, sha256, name, mime, size, COALESCE(document_id, ''), COALESCE(entity_id, ''), created_by, created_at, version, thumb_sha256, text_status`

func scanFile(r interface{ Scan(...any) error }) (File, error) {
	var f File
	err := r.Scan(&f.ID, &f.ProjectID, &f.SHA256, &f.Name, &f.Mime, &f.Size, &f.DocumentID, &f.EntityID, &f.CreatedBy, &f.CreatedAt, &f.Version, &f.ThumbSHA256, &f.TextStatus)
	f.HasPreview = f.ThumbSHA256 != ""
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

// CreateFile stores the metadata of an uploaded file as version 1.
func (d *DB) CreateFile(ctx context.Context, f File) (File, error) {
	f.CreatedAt, f.Version = Now(), 1
	err := d.InTx(ctx, func(tx *Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO files (id, tenant_id, project_id, sha256, name, mime, size, document_id, entity_id, created_by, created_at, version)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1)`, f.ID, DefaultTenant, f.ProjectID, f.SHA256, f.Name, f.Mime, f.Size,
			nullable(f.DocumentID), nullable(f.EntityID), f.CreatedBy, f.CreatedAt); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO file_versions (file_id, version, tenant_id, sha256, mime, size, created_by, created_at) VALUES (?, 1, ?, ?, ?, ?, ?, ?)`,
			f.ID, DefaultTenant, f.SHA256, f.Mime, f.Size, f.CreatedBy, f.CreatedAt)
		return err
	})
	return f, err
}

// FileVersion is one stored content of a file.
type FileVersion struct {
	Version   int    `json:"version"`
	SHA256    string `json:"sha256"`
	Mime      string `json:"mime"`
	Size      int64  `json:"size"`
	CreatedBy string `json:"createdBy"`
	CreatedAt Time   `json:"createdAt"`
}

// AddFileVersion makes new content the current version of a file. The
// preview and extracted text are reset (they are made again in the background).
func (d *DB) AddFileVersion(ctx context.Context, fileID string, v FileVersion) (File, error) {
	v.CreatedAt = Now()
	err := d.InTx(ctx, func(tx *Tx) error {
		if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(version), 0) + 1 FROM file_versions WHERE file_id = ?`, fileID).Scan(&v.Version); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO file_versions (file_id, version, tenant_id, sha256, mime, size, created_by, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			fileID, v.Version, DefaultTenant, v.SHA256, v.Mime, v.Size, v.CreatedBy, v.CreatedAt); err != nil {
			return err
		}
		res, err := tx.Exec(ctx, `UPDATE files SET sha256 = ?, mime = ?, size = ?, version = ?, thumb_sha256 = '', text_status = '' WHERE id = ? AND deleted_at IS NULL`,
			v.SHA256, v.Mime, v.Size, v.Version, fileID)
		return affected(res, err)
	})
	if err != nil {
		return File{}, err
	}
	return d.GetFile(ctx, fileID)
}

// FileVersions lists the versions of a file, newest first.
func (d *DB) FileVersions(ctx context.Context, fileID string) ([]FileVersion, error) {
	rows, err := d.Query(ctx, `SELECT version, sha256, mime, size, created_by, created_at FROM file_versions WHERE file_id = ? ORDER BY version DESC`, fileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FileVersion{}
	for rows.Next() {
		var v FileVersion
		if err := rows.Scan(&v.Version, &v.SHA256, &v.Mime, &v.Size, &v.CreatedBy, &v.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// GetFileVersion returns one version of a file.
func (d *DB) GetFileVersion(ctx context.Context, fileID string, version int) (FileVersion, error) {
	var v FileVersion
	err := d.QueryRow(ctx, `SELECT version, sha256, mime, size, created_by, created_at FROM file_versions WHERE file_id = ? AND version = ?`, fileID, version).
		Scan(&v.Version, &v.SHA256, &v.Mime, &v.Size, &v.CreatedBy, &v.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrNotFound
	}
	return v, err
}

// SetFileProcessed stores the preview and text status of a file version
// (ignored if the file moved on to a newer version meanwhile).
func (d *DB) SetFileProcessed(ctx context.Context, fileID string, version int, thumb, textStatus string) error {
	_, err := d.Exec(ctx, `UPDATE files SET thumb_sha256 = ?, text_status = ? WHERE id = ? AND version = ?`, thumb, textStatus, fileID, version)
	return err
}

// GetFileAny returns a file also if it is in the trash (for background work).
func (d *DB) GetFileAny(ctx context.Context, id string) (File, error) {
	return scanFile(d.QueryRow(ctx, `SELECT `+fileCols+` FROM files WHERE id = ?`, id))
}

// AllFiles returns all live files (for rebuilding the search index).
func (d *DB) AllFiles(ctx context.Context) ([]File, error) {
	rows, err := d.Query(ctx, `SELECT `+fileCols+` FROM files WHERE deleted_at IS NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []File
	for rows.Next() {
		f, err := scanFile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
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

// HashInUse reports whether any file, older version or preview (live or
// trashed, any project) uses the content.
func (d *DB) HashInUse(ctx context.Context, hash string) (bool, error) {
	var one int
	err := d.QueryRow(ctx, `SELECT 1 FROM file_versions WHERE sha256 = ?
		UNION ALL SELECT 1 FROM files WHERE sha256 = ? OR thumb_sha256 = ? LIMIT 1`, hash, hash, hash).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// FileUsage is the stored size of all versions of a project's live and trashed files.
func (d *DB) FileUsage(ctx context.Context, projectID string) (int64, error) {
	var n sql.NullInt64
	err := d.QueryRow(ctx, `SELECT SUM(v.size) FROM file_versions v JOIN files f ON f.id = v.file_id WHERE f.project_id = ?`, projectID).Scan(&n)
	return n.Int64, err
}

// FileHashes returns the content hashes of files matching a condition
// (used to free contents after purging).
func (d *DB) fileHashes(ctx context.Context, where string, args ...any) ([]string, error) {
	rows, err := d.Query(ctx, `SELECT v.sha256 FROM file_versions v JOIN files ON files.id = v.file_id WHERE `+where+`
		UNION SELECT thumb_sha256 FROM files WHERE thumb_sha256 <> '' AND `+where, append(args, args...)...)
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
	return d.fileHashes(ctx, `files.id = ? AND files.project_id = ? AND files.deleted_at IS NOT NULL`, id, projectID)
}

// ExpiredFileHashes returns hashes of files trashed before a time.
func (d *DB) ExpiredFileHashes(ctx context.Context, before Time) ([]string, error) {
	return d.fileHashes(ctx, `files.deleted_at IS NOT NULL AND files.deleted_at < ?`, before)
}

// ProjectFileHashes returns the hashes of all files of a project.
func (d *DB) ProjectFileHashes(ctx context.Context, projectID string) ([]string, error) {
	return d.fileHashes(ctx, `files.project_id = ?`, projectID)
}

// Upload is a resumable (tus) upload in progress.
type Upload struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`
	UserID    string `json:"userId"`
	Length    int64  `json:"length"`
	Received  int64  `json:"received"`
	Meta      JSON   `json:"meta"`
	ExpiresAt Time   `json:"expiresAt"`
}

// CreateUpload starts a resumable upload.
func (d *DB) CreateUpload(ctx context.Context, u Upload) error {
	_, err := d.Exec(ctx, `INSERT INTO uploads (id, tenant_id, project_id, user_id, length, received, meta, created_at, expires_at) VALUES (?, ?, ?, ?, ?, 0, ?, ?, ?)`,
		u.ID, DefaultTenant, u.ProjectID, u.UserID, u.Length, u.Meta, Now(), u.ExpiresAt)
	return err
}

// GetUpload returns an upload that has not expired.
func (d *DB) GetUpload(ctx context.Context, id string) (Upload, error) {
	var u Upload
	err := d.QueryRow(ctx, `SELECT id, project_id, user_id, length, received, meta, expires_at FROM uploads WHERE id = ? AND expires_at > ?`, id, Now()).
		Scan(&u.ID, &u.ProjectID, &u.UserID, &u.Length, &u.Received, &u.Meta, &u.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return u, ErrNotFound
	}
	return u, err
}

// SetUploadReceived records how many bytes arrived; only if the offset matches (no parallel writers).
func (d *DB) SetUploadReceived(ctx context.Context, id string, from, to int64) error {
	res, err := d.Exec(ctx, `UPDATE uploads SET received = ? WHERE id = ? AND received = ?`, to, id, from)
	if err := affected(res, err); errors.Is(err, ErrNotFound) {
		return ErrConflict
	} else {
		return err
	}
}

// DeleteUpload removes an upload record.
func (d *DB) DeleteUpload(ctx context.Context, id string) error {
	_, err := d.Exec(ctx, `DELETE FROM uploads WHERE id = ?`, id)
	return err
}

// ExpiredUploads returns ids of uploads past their expiry.
func (d *DB) ExpiredUploads(ctx context.Context) ([]string, error) {
	rows, err := d.Query(ctx, `SELECT id FROM uploads WHERE expires_at <= ?`, Now())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// SetProjectQuota sets the storage quota of a project in MB (0 = only the instance limit).
func (d *DB) SetProjectQuota(ctx context.Context, projectID string, mb int) error {
	res, err := d.Exec(ctx, `UPDATE projects SET quota_mb = ? WHERE id = ?`, mb, projectID)
	return affected(res, err)
}

// ProjectQuota returns the quota of a project in MB.
func (d *DB) ProjectQuota(ctx context.Context, projectID string) (int, error) {
	var mb int
	err := d.QueryRow(ctx, `SELECT quota_mb FROM projects WHERE id = ?`, projectID).Scan(&mb)
	return mb, err
}
