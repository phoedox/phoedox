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
