package store

import (
	"context"
	"database/sql"
	"errors"
)

// Translation is a document in one of the project's further languages
// (ADR-0028). Number, slug, category, status, permissions, files and
// mentions belong to the document; the translation holds title and content.
type Translation struct {
	DocumentID    string `json:"documentId"`
	Locale        string `json:"locale"`
	Title         string `json:"title"`
	Content       JSON   `json:"content,omitempty"`
	SourceVersion int    `json:"sourceVersion"`
	Machine       bool   `json:"machine"`
	UpdatedBy     string `json:"-"`
	UpdatedAt     Time   `json:"updatedAt"`
}

const translationCols = `document_id, locale, title, content, source_version, machine, updated_by, updated_at`

func scanTranslation(r interface{ Scan(...any) error }) (Translation, error) {
	var t Translation
	err := r.Scan(&t.DocumentID, &t.Locale, &t.Title, &t.Content, &t.SourceVersion, &t.Machine, &t.UpdatedBy, &t.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return t, ErrNotFound
	}
	return t, err
}

// GetTranslation returns one language version of a document.
func (d *DB) GetTranslation(ctx context.Context, documentID, locale string) (Translation, error) {
	return scanTranslation(d.QueryRow(ctx, `SELECT `+translationCols+` FROM document_translations WHERE document_id = ? AND locale = ?`, documentID, locale))
}

// Translations lists the language versions of a document (without content).
func (d *DB) Translations(ctx context.Context, documentID string) ([]Translation, error) {
	rows, err := d.Query(ctx, `SELECT `+translationCols+` FROM document_translations WHERE document_id = ? ORDER BY locale`, documentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Translation{}
	for rows.Next() {
		t, err := scanTranslation(rows)
		if err != nil {
			return nil, err
		}
		t.Content = nil
		out = append(out, t)
	}
	return out, rows.Err()
}

// ProjectTranslations maps document id to its locales with source versions,
// for marking outdated translations in lists.
func (d *DB) ProjectTranslations(ctx context.Context, projectID string) (map[string][]Translation, error) {
	rows, err := d.Query(ctx, `SELECT t.document_id, t.locale, t.source_version, t.machine FROM document_translations t
		JOIN documents doc ON doc.id = t.document_id WHERE doc.project_id = ? AND doc.deleted_at IS NULL`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]Translation{}
	for rows.Next() {
		var t Translation
		if err := rows.Scan(&t.DocumentID, &t.Locale, &t.SourceVersion, &t.Machine); err != nil {
			return nil, err
		}
		out[t.DocumentID] = append(out[t.DocumentID], t)
	}
	return out, rows.Err()
}

// SaveTranslation creates or replaces a language version.
func (d *DB) SaveTranslation(ctx context.Context, t Translation) error {
	_, err := d.Exec(ctx, `INSERT INTO document_translations (document_id, locale, tenant_id, title, content, source_version, machine, updated_by, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (document_id, locale) DO UPDATE SET title = excluded.title, content = excluded.content, source_version = excluded.source_version,
		  machine = excluded.machine, updated_by = excluded.updated_by, updated_at = excluded.updated_at`,
		t.DocumentID, t.Locale, TenantOf(ctx), t.Title, t.Content, t.SourceVersion, t.Machine, t.UpdatedBy, t.UpdatedAt)
	return err
}

// DeleteTranslation removes a language version.
func (d *DB) DeleteTranslation(ctx context.Context, documentID, locale string) error {
	res, err := d.Exec(ctx, `DELETE FROM document_translations WHERE document_id = ? AND locale = ?`, documentID, locale)
	return affected(res, err)
}
