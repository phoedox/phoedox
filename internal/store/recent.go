package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
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

// PinnedProjects returns the ids of the projects a person pinned.
func (d *DB) PinnedProjects(ctx context.Context, userID string) ([]string, error) {
	var s string
	err := d.QueryRow(ctx, `SELECT pinned_projects FROM user_prefs WHERE user_id = ?`, userID).Scan(&s)
	if errors.Is(err, sql.ErrNoRows) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []string{}
	_ = json.Unmarshal([]byte(s), &out)
	return out, nil
}

// SavePinnedProjects stores the projects a person pinned.
func (d *DB) SavePinnedProjects(ctx context.Context, userID string, ids []string) error {
	b, _ := json.Marshal(ids)
	_, err := d.Exec(ctx, `INSERT INTO user_prefs (user_id, tenant_id, pinned_projects, updated_at) VALUES (?, ?, ?, ?)
		ON CONFLICT (user_id) DO UPDATE SET pinned_projects = excluded.pinned_projects, updated_at = excluded.updated_at`,
		userID, TenantOf(ctx), string(b), Now())
	return err
}

// ProjectStats are the numbers on a project's card.
type ProjectStats struct {
	Documents     int  `json:"documents"`
	OpenTasks     int  `json:"openTasks"`
	FailingChecks int  `json:"failingChecks"`
	Suggestions   int  `json:"suggestions"`
	LastActivity  Time `json:"lastActivity"`
}

// ProjectStatsFor counts per project in a few grouped queries.
// publicDocs lists projects where only unrestricted pages count.
func (d *DB) ProjectStatsFor(ctx context.Context, ids []string, publicDocs map[string]bool) (map[string]*ProjectStats, error) {
	out := map[string]*ProjectStats{}
	if len(ids) == 0 {
		return out, nil
	}
	for _, id := range ids {
		out[id] = &ProjectStats{}
	}
	in := strings.TrimSuffix(strings.Repeat("?, ", len(ids)), ", ")
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	count := func(q string, set func(*ProjectStats, int), pre ...any) error {
		rows, err := d.Query(ctx, q, append(pre, args...)...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			var n int
			if err := rows.Scan(&id, &n); err != nil {
				return err
			}
			if s := out[id]; s != nil {
				set(s, n)
			}
		}
		return rows.Err()
	}
	// Pages: all for members with full access, unrestricted ones otherwise.
	rows, err := d.Query(ctx, `SELECT project_id, restricted, COUNT(*) FROM documents WHERE deleted_at IS NULL AND project_id IN (`+in+`) GROUP BY project_id, restricted`, args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id string
		var restricted bool
		var n int
		if err := rows.Scan(&id, &restricted, &n); err != nil {
			rows.Close()
			return nil, err
		}
		if s := out[id]; s != nil && (!restricted || !publicDocs[id]) {
			s.Documents += n
		}
	}
	rows.Close()
	if err := count(`SELECT t.project_id, COUNT(*) FROM tasks t JOIN documents b ON b.id = t.board_id
		WHERE t.done_at IS NULL AND b.deleted_at IS NULL AND t.project_id IN (`+in+`) GROUP BY t.project_id`, func(s *ProjectStats, n int) { s.OpenTasks = n }); err != nil {
		return nil, err
	}
	if err := count(`SELECT project_id, COUNT(*) FROM checks WHERE enabled = ? AND last_status = 'fail' AND project_id IN (`+in+`) GROUP BY project_id`,
		func(s *ProjectStats, n int) { s.FailingChecks = n }, true); err != nil {
		return nil, err
	}
	if err := count(`SELECT project_id, COUNT(*) FROM suggestions WHERE status = 'pending' AND project_id IN (`+in+`) GROUP BY project_id`,
		func(s *ProjectStats, n int) { s.Suggestions = n }); err != nil {
		return nil, err
	}
	rows, err = d.Query(ctx, `SELECT project_id, MAX(created_at) FROM events WHERE project_id IN (`+in+`) GROUP BY project_id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var at Time
		if err := rows.Scan(&id, &at); err != nil {
			return nil, err
		}
		if s := out[id]; s != nil {
			s.LastActivity = at
		}
	}
	return out, rows.Err()
}
