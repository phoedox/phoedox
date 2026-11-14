package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// Document types in the core.
const (
	DocMarkdown  = "markdown"
	DocADR       = "adr"
	DocLink      = "link"
	DocBoard     = "board"     // a task board (ADR-0030); its cards are tasks
	DocFloorplan = "floorplan" // rooms as polygons and devices as points (ADR-0034)
)

// Document is a page of any document type.
type Document struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`
	Type      string `json:"type"`
	Slug      string `json:"slug"`
	Title     string `json:"title"`
	Category  string `json:"category"`
	Position  int    `json:"position"`
	Content   JSON   `json:"content,omitempty"`
	Number    int    `json:"number,omitempty"` // decisions only, per project, never reused
	// Restricted documents are visible only to project owners and people or groups they are shared with.
	Restricted bool `json:"restricted,omitempty"`
	// ParentID is the page a document belongs to (a board made inside a
	// page); such documents stay out of the sidebar.
	ParentID string `json:"parentId,omitempty"`
	// Role is the requesting user's role for this document (set by the service).
	Role string `json:"role,omitempty"`
	// Locale is the language of title and content when the project is
	// multilingual; Translation describes the shown translation (nil = main
	// language) and Translations lists all of them (ADR-0028). Set by the service.
	Locale       string        `json:"locale,omitempty"`
	Translation  *Translation  `json:"translation,omitempty"`
	Translations []Translation `json:"translations,omitempty"`
	Version      int           `json:"version"`
	CreatedBy    string        `json:"createdBy"`
	CreatedAt    Time          `json:"createdAt"`
	UpdatedAt    Time          `json:"updatedAt"`
}

const docCols = `id, project_id, doc_type, slug, title, category, position, version, created_by, created_at, updated_at, number, restricted, parent_id`

func scanDoc(r interface{ Scan(...any) error }, withContent bool) (Document, error) {
	var doc Document
	var number sql.NullInt64
	var parent sql.NullString
	dest := []any{&doc.ID, &doc.ProjectID, &doc.Type, &doc.Slug, &doc.Title, &doc.Category, &doc.Position, &doc.Version, &doc.CreatedBy, &doc.CreatedAt, &doc.UpdatedAt, &number, &doc.Restricted, &parent}
	if withContent {
		dest = append(dest, &doc.Content)
	}
	err := r.Scan(dest...)
	if errors.Is(err, sql.ErrNoRows) {
		return doc, ErrNotFound
	}
	doc.Number = int(number.Int64)
	doc.ParentID = parent.String
	return doc, err
}

// CreateDocument inserts a document and its first version. Decisions get
// the next number of their project. Related documents (e.g. a decision
// that the new one supersedes) are saved as new versions in the same
// transaction; each fails with ErrConflict if it changed since it was read.
func (d *DB) CreateDocument(ctx context.Context, doc Document, actor string, related ...Document) (Document, error) {
	now := Now()
	doc.CreatedAt, doc.UpdatedAt, doc.Version = now, now, 1
	err := d.InTx(ctx, func(tx *Tx) error {
		var number any
		if doc.Type == DocADR {
			// The UPDATE locks the project row, so concurrent creates queue up.
			if _, err := tx.Exec(ctx, `UPDATE projects SET adr_seq = adr_seq + 1 WHERE id = ?`, doc.ProjectID); err != nil {
				return err
			}
			if err := tx.QueryRow(ctx, `SELECT adr_seq FROM projects WHERE id = ?`, doc.ProjectID).Scan(&doc.Number); err != nil {
				return err
			}
			number = doc.Number
		}
		if _, err := tx.Exec(ctx, `INSERT INTO documents (id, tenant_id, project_id, doc_type, slug, title, category, position, content, version, created_by, created_at, updated_at, number, parent_id)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, doc.ID, TenantOf(ctx), doc.ProjectID, doc.Type, doc.Slug, doc.Title, doc.Category, doc.Position, doc.Content, doc.Version, doc.CreatedBy, now, now, number, nullable(doc.ParentID)); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO document_versions (document_id, version, title, content, actor, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
			doc.ID, doc.Version, doc.Title, doc.Content, actor, now); err != nil {
			return err
		}
		return updateRelated(ctx, tx, related, actor, now)
	})
	return doc, err
}

// UpdateDocument saves a new version. It fails with ErrConflict if the
// document changed since expectVersion (0 skips the check). Related
// documents are saved as in CreateDocument.
func (d *DB) UpdateDocument(ctx context.Context, doc Document, expectVersion int, actor string, related ...Document) (Document, error) {
	now := Now()
	err := d.InTx(ctx, func(tx *Tx) error {
		var err error
		if doc, err = updateDoc(ctx, tx, doc, expectVersion, actor, now); err != nil {
			return err
		}
		return updateRelated(ctx, tx, related, actor, now)
	})
	return doc, err
}

func updateRelated(ctx context.Context, tx *Tx, related []Document, actor string, now Time) error {
	for _, r := range related {
		if _, err := updateDoc(ctx, tx, r, r.Version, actor, now); err != nil {
			return err
		}
	}
	return nil
}

func updateDoc(ctx context.Context, tx *Tx, doc Document, expectVersion int, actor string, now Time) (Document, error) {
	var cur int
	if err := tx.QueryRow(ctx, `SELECT version FROM documents WHERE id = ? AND deleted_at IS NULL`, doc.ID).Scan(&cur); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return doc, ErrNotFound
		}
		return doc, err
	}
	if expectVersion != 0 && expectVersion != cur {
		return doc, ErrConflict
	}
	doc.Version = cur + 1
	doc.UpdatedAt = now
	if _, err := tx.Exec(ctx, `UPDATE documents SET slug = ?, title = ?, category = ?, position = ?, content = ?, version = ?, updated_at = ? WHERE id = ?`,
		doc.Slug, doc.Title, doc.Category, doc.Position, doc.Content, doc.Version, now, doc.ID); err != nil {
		return doc, err
	}
	_, err := tx.Exec(ctx, `INSERT INTO document_versions (document_id, version, title, content, actor, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		doc.ID, doc.Version, doc.Title, doc.Content, actor, now)
	return doc, err
}

// GetDocument returns a document of a project by id or slug, with content.
func (d *DB) GetDocument(ctx context.Context, projectID, ref string) (Document, error) {
	return scanDoc(d.QueryRow(ctx, `SELECT `+docCols+`, content FROM documents
		WHERE project_id = ? AND (id = ? OR slug = ?) AND deleted_at IS NULL`, projectID, ref, ref), true)
}

// GetDecision returns a decision of a project by its number, with content.
func (d *DB) GetDecision(ctx context.Context, projectID string, number int) (Document, error) {
	return scanDoc(d.QueryRow(ctx, `SELECT `+docCols+`, content FROM documents
		WHERE project_id = ? AND number = ? AND deleted_at IS NULL`, projectID, number), true)
}

// DocFilter narrows ListDocuments.
type DocFilter struct {
	Type     string
	Category string
	// WithContent includes the content of all documents. Otherwise only
	// link documents carry it (the sidebar needs their URL).
	WithContent bool
	// Locale shows titles in this translation where one exists (sidebar of
	// multilingual projects); Document.Locale is then set on those.
	Locale string
}

// ListDocuments returns documents ordered for the sidebar: by category,
// position, decision number and title.
func (d *DB) ListDocuments(ctx context.Context, projectID string, f DocFilter) ([]Document, error) {
	contentCol := `CASE WHEN doc_type = 'link' THEN content ELSE NULL END`
	if f.WithContent {
		contentCol = `content`
	}
	query := `SELECT ` + docCols + `, ` + contentCol + ` FROM documents WHERE project_id = ? AND deleted_at IS NULL`
	args := []any{projectID}
	if f.Type != "" {
		query += ` AND doc_type = ?`
		args = append(args, f.Type)
	}
	if f.Category != "" {
		query += ` AND category = ?`
		args = append(args, f.Category)
	}
	rows, err := d.Query(ctx, query+` ORDER BY category, position, COALESCE(number, 0), title`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Document
	for rows.Next() {
		doc, err := scanDoc(rows, true)
		if err != nil {
			return nil, err
		}
		if string(doc.Content) == "null" {
			doc.Content = nil
		}
		out = append(out, doc)
	}
	return out, rows.Err()
}

// TranslatedTitles maps the documents of a project to their title in a locale.
func (d *DB) TranslatedTitles(ctx context.Context, projectID, locale string) (map[string]string, error) {
	rows, err := d.Query(ctx, `SELECT t.document_id, t.title FROM document_translations t JOIN documents d ON d.id = t.document_id
		WHERE d.project_id = ? AND t.locale = ?`, projectID, locale)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id, title string
		if err := rows.Scan(&id, &title); err != nil {
			return nil, err
		}
		out[id] = title
	}
	return out, rows.Err()
}

// DeleteDocument moves a document to the trash.
func (d *DB) DeleteDocument(ctx context.Context, id string) error {
	res, err := d.Exec(ctx, `UPDATE documents SET deleted_at = ? WHERE id = ? AND deleted_at IS NULL`, Now(), id)
	return affected(res, err)
}

// Version is a stored state of a document.
type Version struct {
	Version   int    `json:"version"`
	Title     string `json:"title"`
	Actor     string `json:"actor"`
	CreatedAt Time   `json:"createdAt"`
	Content   JSON   `json:"content,omitempty"`
	Kind      string `json:"kind"` // edit | live
	Coauthors string `json:"-"`    // user ids, comma separated
}

func (d *DB) ListVersions(ctx context.Context, documentID string) ([]Version, error) {
	rows, err := d.Query(ctx, `SELECT version, title, actor, created_at, kind, coauthors FROM document_versions WHERE document_id = ? ORDER BY version DESC`, documentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Version
	for rows.Next() {
		var v Version
		if err := rows.Scan(&v.Version, &v.Title, &v.Actor, &v.CreatedAt, &v.Kind, &v.Coauthors); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// SetDocumentLinks replaces the entities a document mentions.
func (d *DB) SetDocumentLinks(ctx context.Context, documentID string, entityIDs []string) error {
	return d.InTx(ctx, func(tx *Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM document_links WHERE document_id = ?`, documentID); err != nil {
			return err
		}
		seen := map[string]bool{}
		for _, id := range entityIDs {
			if seen[id] {
				continue
			}
			seen[id] = true
			if _, err := tx.Exec(ctx, `INSERT INTO document_links (document_id, entity_id) VALUES (?, ?)`, documentID, id); err != nil {
				return err
			}
		}
		return nil
	})
}

// DocumentsMentioning returns documents that mention an entity.
func (d *DB) DocumentsMentioning(ctx context.Context, entityID string) ([]Document, error) {
	rows, err := d.Query(ctx, `SELECT `+prefixCols("d.", docCols)+` FROM documents d JOIN document_links l ON l.document_id = d.id
		WHERE l.entity_id = ? AND d.deleted_at IS NULL ORDER BY d.title`, entityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Document
	for rows.Next() {
		doc, err := scanDoc(rows, false)
		if err != nil {
			return nil, err
		}
		out = append(out, doc)
	}
	return out, rows.Err()
}

func prefixCols(prefix, cols string) string {
	parts := strings.Split(cols, ",")
	for i, p := range parts {
		parts[i] = prefix + strings.TrimSpace(p)
	}
	return strings.Join(parts, ", ")
}

// MentionedEntities lists the entries that live pages of a project mention.
func (d *DB) MentionedEntities(ctx context.Context, projectID string) (map[string]bool, error) {
	rows, err := d.Query(ctx, `SELECT DISTINCT l.entity_id FROM document_links l JOIN documents d ON d.id = l.document_id
		WHERE d.project_id = ? AND d.deleted_at IS NULL`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}
