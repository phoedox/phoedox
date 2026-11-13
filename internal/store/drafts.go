package store

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"
)

// Draft is the edit session of a Markdown page (ADR-0027). Live editing,
// API and MCP writes change the draft; the page changes on publish.
type Draft struct {
	DocumentID   string   `json:"documentId"`
	BaseVersion  int      `json:"baseVersion"`
	Title        string   `json:"title"`
	Category     string   `json:"category"`
	Markdown     string   `json:"markdown"`
	Participants []string `json:"-"` // actors
	StartedBy    string   `json:"-"`
	StartedAt    Time     `json:"startedAt"`
	UpdatedBy    string   `json:"-"`
	UpdatedAt    Time     `json:"updatedAt"`
	RemindedAt   Time     `json:"-"`
	TenantID     string   `json:"-"` // filled by DueDrafts
}

// AddParticipant records an actor (once).
func (d *Draft) AddParticipant(actor string) {
	if actor != "" && !slices.Contains(d.Participants, actor) {
		d.Participants = append(d.Participants, actor)
	}
}

const draftCols = `document_id, base_version, title, category, markdown, participants, started_by, started_at, updated_by, updated_at, reminded_at`

func scanDraft(r interface{ Scan(...any) error }, extra ...any) (Draft, error) {
	var d Draft
	var parts string
	err := r.Scan(append([]any{&d.DocumentID, &d.BaseVersion, &d.Title, &d.Category, &d.Markdown, &parts, &d.StartedBy, &d.StartedAt, &d.UpdatedBy, &d.UpdatedAt, &d.RemindedAt}, extra...)...)
	if errors.Is(err, sql.ErrNoRows) {
		return d, ErrNotFound
	}
	if parts != "" {
		d.Participants = strings.Split(parts, ",")
	}
	return d, err
}

// GetDraft returns the draft of a document.
func (d *DB) GetDraft(ctx context.Context, documentID string) (Draft, error) {
	return scanDraft(d.QueryRow(ctx, `SELECT `+draftCols+` FROM drafts WHERE document_id = ?`, documentID))
}

// SaveDraft creates or updates a draft.
func (d *DB) SaveDraft(ctx context.Context, x Draft) error {
	_, err := d.Exec(ctx, `INSERT INTO drafts (document_id, tenant_id, base_version, title, category, markdown, participants, started_by, started_at, updated_by, updated_at, reminded_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (document_id) DO UPDATE SET title = excluded.title, category = excluded.category, markdown = excluded.markdown,
		  participants = excluded.participants, updated_by = excluded.updated_by, updated_at = excluded.updated_at, reminded_at = excluded.reminded_at`,
		x.DocumentID, TenantOf(ctx), x.BaseVersion, x.Title, x.Category, x.Markdown, strings.Join(x.Participants, ","), x.StartedBy, x.StartedAt,
		x.UpdatedBy, x.UpdatedAt, x.RemindedAt)
	return err
}

// DeleteDraft ends a draft and drops its live log.
func (d *DB) DeleteDraft(ctx context.Context, documentID string) error {
	return d.InTx(ctx, func(tx *Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM doc_updates WHERE document_id = ?`, documentID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `DELETE FROM drafts WHERE document_id = ?`, documentID)
		return err
	})
}

// DraftSummary is a draft with its page, for lists.
type DraftSummary struct {
	Draft
	Slug      string `json:"slug"`
	ProjectID string `json:"projectId"`
}

// ProjectDrafts lists the drafts of a project.
func (d *DB) ProjectDrafts(ctx context.Context, projectID string) ([]DraftSummary, error) {
	rows, err := d.Query(ctx, `SELECT `+prefixed("dr.", draftCols)+`, doc.slug, doc.project_id FROM drafts dr JOIN documents doc ON doc.id = dr.document_id
		WHERE doc.project_id = ? AND doc.deleted_at IS NULL ORDER BY dr.updated_at DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DraftSummary{}
	for rows.Next() {
		var s DraftSummary
		dr, err := scanDraft(rows, &s.Slug, &s.ProjectID)
		if err != nil {
			return nil, err
		}
		s.Draft = dr
		out = append(out, s)
	}
	return out, rows.Err()
}

// DueDrafts lists drafts unchanged and not reminded since before (all
// tenants with a System context).
func (d *DB) DueDrafts(ctx context.Context, before Time) ([]DraftSummary, error) {
	rows, err := d.Query(ctx, `SELECT `+prefixed("dr.", draftCols)+`, doc.slug, doc.project_id, dr.tenant_id FROM drafts dr JOIN documents doc ON doc.id = dr.document_id
		WHERE doc.deleted_at IS NULL AND dr.updated_at < ? AND (dr.reminded_at IS NULL OR dr.reminded_at < ?) ORDER BY dr.updated_at`, before, before)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DraftSummary{}
	for rows.Next() {
		var s DraftSummary
		dr, err := scanDraft(rows, &s.Slug, &s.ProjectID, &s.TenantID)
		if err != nil {
			return nil, err
		}
		dr.TenantID = s.TenantID
		s.Draft = dr
		out = append(out, s)
	}
	return out, rows.Err()
}

// MarkDraftReminded records that participants were reminded.
func (d *DB) MarkDraftReminded(ctx context.Context, documentID string, at Time) error {
	_, err := d.Exec(ctx, `UPDATE drafts SET reminded_at = ? WHERE document_id = ?`, at, documentID)
	return err
}

// SetCoauthors stores the co-authors of a version (comma-separated user ids).
func (d *DB) SetCoauthors(ctx context.Context, documentID string, version int, userIDs []string) error {
	_, err := d.Exec(ctx, `UPDATE document_versions SET coauthors = ? WHERE document_id = ? AND version = ?`, strings.Join(userIDs, ","), documentID, version)
	return err
}

func prefixed(p, cols string) string {
	parts := strings.Split(cols, ", ")
	for i := range parts {
		parts[i] = p + parts[i]
	}
	return strings.Join(parts, ", ")
}
