package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// FeedbackStorageLot is extra feedback storage of a project for a time.
type FeedbackStorageLot struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`
	MB        int    `json:"mb"`
	Source    string `json:"source"` // purchase | grant
	SourceRef string `json:"sourceRef,omitempty"`
	Note      string `json:"note,omitempty"`
	CreatedBy string `json:"createdBy"`
	CreatedAt Time   `json:"createdAt"`
	ExpiresAt *Time  `json:"expiresAt,omitempty"`
	Revoked   bool   `json:"revoked,omitempty"`
}

const storageLotCols = `id, project_id, mb, source, source_ref, note, created_by, created_at, expires_at, revoked`

func scanStorageLot(r interface{ Scan(...any) error }) (FeedbackStorageLot, error) {
	var l FeedbackStorageLot
	var exp Time
	err := r.Scan(&l.ID, &l.ProjectID, &l.MB, &l.Source, &l.SourceRef, &l.Note, &l.CreatedBy, &l.CreatedAt, &exp, &l.Revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return l, ErrNotFound
	}
	if !exp.IsZero() {
		l.ExpiresAt = &exp
	}
	return l, err
}

// AddFeedbackStorageLot records extra storage.
func (d *DB) AddFeedbackStorageLot(ctx context.Context, l FeedbackStorageLot) error {
	var exp any
	if l.ExpiresAt != nil {
		exp = *l.ExpiresAt
	}
	_, err := d.Exec(ctx, `INSERT INTO feedback_storage_lots (`+storageLotCols+`, tenant_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		l.ID, l.ProjectID, l.MB, l.Source, l.SourceRef, l.Note, l.CreatedBy, l.CreatedAt, exp, false, TenantOf(ctx))
	return err
}

// FeedbackStorageLots lists a project's lots, newest first.
func (d *DB) FeedbackStorageLots(ctx context.Context, projectID string) ([]FeedbackStorageLot, error) {
	rows, err := d.Query(ctx, `SELECT `+storageLotCols+` FROM feedback_storage_lots WHERE project_id = ? ORDER BY created_at DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FeedbackStorageLot{}
	for rows.Next() {
		l, err := scanStorageLot(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// FeedbackExtraMB is the extra storage a project has at a time.
func (d *DB) FeedbackExtraMB(ctx context.Context, projectID string, at time.Time) (int, error) {
	var n sql.NullInt64
	err := d.QueryRow(ctx, `SELECT SUM(mb) FROM feedback_storage_lots WHERE project_id = ? AND revoked = ? AND (expires_at IS NULL OR expires_at > ?)`,
		projectID, false, Time{at}).Scan(&n)
	return int(n.Int64), err
}

// FeedbackStorageLotBySource finds the lot of a purchase.
func (d *DB) FeedbackStorageLotBySource(ctx context.Context, source, ref string) (FeedbackStorageLot, error) {
	return scanStorageLot(d.QueryRow(ctx, `SELECT `+storageLotCols+` FROM feedback_storage_lots WHERE source = ? AND source_ref = ?`, source, ref))
}

// RevokeFeedbackStorageLot ends a lot (refunds).
func (d *DB) RevokeFeedbackStorageLot(ctx context.Context, id string) error {
	_, err := d.Exec(ctx, `UPDATE feedback_storage_lots SET revoked = ? WHERE id = ?`, true, id)
	return err
}
