package store

import (
	"context"
	"database/sql"
	"errors"
)

// PublicationRelease is a fixed state of a publication.
type PublicationRelease struct {
	ID            string `json:"id"`
	PublicationID string `json:"publicationId"`
	Number        int    `json:"number"`
	Note          string `json:"note"`
	CreatedBy     string `json:"createdBy"`
	CreatedAt     Time   `json:"createdAt"`
}

// AddRelease stores the next release of a publication (numbers count up from 1).
func (d *DB) AddRelease(ctx context.Context, r PublicationRelease) (PublicationRelease, error) {
	err := d.InTx(ctx, func(tx *Tx) error {
		var n sql.NullInt64
		if err := tx.QueryRow(ctx, `SELECT MAX(number) FROM publication_releases WHERE publication_id = ?`, r.PublicationID).Scan(&n); err != nil {
			return err
		}
		r.Number = int(n.Int64) + 1
		_, err := tx.Exec(ctx, `INSERT INTO publication_releases (id, tenant_id, publication_id, number, note, created_by, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			r.ID, TenantOf(ctx), r.PublicationID, r.Number, r.Note, r.CreatedBy, r.CreatedAt)
		return err
	})
	return r, err
}

// Releases lists the releases of a publication, newest first.
func (d *DB) Releases(ctx context.Context, publicationID string) ([]PublicationRelease, error) {
	rows, err := d.Query(ctx, `SELECT id, publication_id, number, note, created_by, created_at FROM publication_releases WHERE publication_id = ? ORDER BY number DESC`, publicationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PublicationRelease{}
	for rows.Next() {
		var r PublicationRelease
		if err := rows.Scan(&r.ID, &r.PublicationID, &r.Number, &r.Note, &r.CreatedBy, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Release returns one release by number.
func (d *DB) Release(ctx context.Context, publicationID string, number int) (PublicationRelease, error) {
	var r PublicationRelease
	err := d.QueryRow(ctx, `SELECT id, publication_id, number, note, created_by, created_at FROM publication_releases WHERE publication_id = ? AND number = ?`,
		publicationID, number).Scan(&r.ID, &r.PublicationID, &r.Number, &r.Note, &r.CreatedBy, &r.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	return r, err
}

// DocumentsAt lists the documents of a project as they were at a point in
// time: existing then, with title and content of their last version before it.
func (d *DB) DocumentsAt(ctx context.Context, projectID string, at Time) ([]Document, error) {
	rows, err := d.Query(ctx, `SELECT `+docCols+`, content FROM documents
		WHERE project_id = ? AND created_at <= ? AND (deleted_at IS NULL OR deleted_at > ?)
		ORDER BY category, position, COALESCE(number, 0), title`, projectID, at, at)
	if err != nil {
		return nil, err
	}
	var docs []Document
	for rows.Next() {
		doc, err := scanDoc(rows, true)
		if err != nil {
			rows.Close()
			return nil, err
		}
		docs = append(docs, doc)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := docs[:0]
	for _, doc := range docs {
		var v Version
		err := d.QueryRow(ctx, `SELECT version, title, content, created_at FROM document_versions WHERE document_id = ? AND created_at <= ? ORDER BY version DESC LIMIT 1`,
			doc.ID, at).Scan(&v.Version, &v.Title, &v.Content, &v.CreatedAt)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		doc.Version, doc.Title, doc.Content, doc.UpdatedAt = v.Version, v.Title, v.Content, v.CreatedAt
		out = append(out, doc)
	}
	return out, nil
}

// ProjectVersion is one saved version of a document of a project, for the
// project history (points to look at or roll back to).
type ProjectVersion struct {
	DocumentID string `json:"documentId"`
	Slug       string `json:"slug"`
	Type       string `json:"type"`
	Version    int    `json:"version"`
	Title      string `json:"title"`
	Actor      string `json:"actor"`
	CreatedAt  Time   `json:"createdAt"`
}

// ProjectVersions lists the newest document versions of a project, newest
// first, before a time (zero = now).
func (d *DB) ProjectVersions(ctx context.Context, projectID, documentID string, before Time, limit int) ([]ProjectVersion, error) {
	q := `SELECT v.document_id, doc.slug, doc.doc_type, v.version, v.title, v.actor, v.created_at FROM document_versions v
		JOIN documents doc ON doc.id = v.document_id WHERE doc.project_id = ?`
	args := []any{projectID}
	if documentID != "" {
		q += ` AND v.document_id = ?`
		args = append(args, documentID)
	}
	if !before.IsZero() {
		q += ` AND v.created_at < ?`
		args = append(args, before)
	}
	q += ` ORDER BY v.created_at DESC, v.version DESC LIMIT ?`
	args = append(args, limit)
	rows, err := d.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ProjectVersion{}
	for rows.Next() {
		var v ProjectVersion
		if err := rows.Scan(&v.DocumentID, &v.Slug, &v.Type, &v.Version, &v.Title, &v.Actor, &v.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
