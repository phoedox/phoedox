package store

import (
	"context"
	"database/sql"
	"errors"
)

// RecentView is a page or entry a person opened.
type RecentView struct {
	ProjectID string `json:"projectId"`
	Kind      string `json:"kind"` // document | entity
	RefID     string `json:"refId"`
	ViewedAt  Time   `json:"viewedAt"`
}

// recentKeep is how many views are kept per person.
const recentKeep = 200

// RecordView remembers that a person opened something (newest wins) and
// keeps only the latest views.
func (d *DB) RecordView(ctx context.Context, userID string, v RecentView) error {
	return d.InTx(ctx, func(tx *Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO recent_views (user_id, tenant_id, project_id, kind, ref_id, viewed_at) VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT (user_id, ref_id) DO UPDATE SET viewed_at = excluded.viewed_at, project_id = excluded.project_id, kind = excluded.kind`,
			userID, TenantOf(ctx), v.ProjectID, v.Kind, v.RefID, v.ViewedAt); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `DELETE FROM recent_views WHERE user_id = ? AND ref_id NOT IN
			(SELECT ref_id FROM recent_views WHERE user_id = ? ORDER BY viewed_at DESC LIMIT ?)`, userID, userID, recentKeep)
		return err
	})
}

// RecentViews lists a person's views, newest first.
func (d *DB) RecentViews(ctx context.Context, userID string, limit int) ([]RecentView, error) {
	rows, err := d.Query(ctx, `SELECT project_id, kind, ref_id, viewed_at FROM recent_views WHERE user_id = ? ORDER BY viewed_at DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RecentView{}
	for rows.Next() {
		var v RecentView
		if err := rows.Scan(&v.ProjectID, &v.Kind, &v.RefID, &v.ViewedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ClearRecentViews forgets a person's history (one entry or all).
func (d *DB) ClearRecentViews(ctx context.Context, userID, refID string) error {
	if refID != "" {
		_, err := d.Exec(ctx, `DELETE FROM recent_views WHERE user_id = ? AND ref_id = ?`, userID, refID)
		return err
	}
	_, err := d.Exec(ctx, `DELETE FROM recent_views WHERE user_id = ?`, userID)
	return err
}

// UserShortcuts returns a person's keyboard shortcuts (JSON object, "{}" = defaults).
func (d *DB) UserShortcuts(ctx context.Context, userID string) (string, error) {
	var s string
	err := d.QueryRow(ctx, `SELECT shortcuts FROM user_prefs WHERE user_id = ?`, userID).Scan(&s)
	if errors.Is(err, sql.ErrNoRows) {
		return "{}", nil
	}
	return s, err
}

// SaveUserShortcuts stores a person's keyboard shortcuts.
func (d *DB) SaveUserShortcuts(ctx context.Context, userID, shortcuts string) error {
	_, err := d.Exec(ctx, `INSERT INTO user_prefs (user_id, tenant_id, shortcuts, updated_at) VALUES (?, ?, ?, ?)
		ON CONFLICT (user_id) DO UPDATE SET shortcuts = excluded.shortcuts, updated_at = excluded.updated_at`,
		userID, TenantOf(ctx), shortcuts, Now())
	return err
}
