package store

import (
	"context"
	"database/sql"
	"errors"
)

// Apps (apps-definition): grants of the data bridge and the apps' own storage.

// AppGrant reads the bridge level a person allowed an app (0 = none yet).
func (d *DB) AppGrant(ctx context.Context, docID, userID string) (int, error) {
	var lvl int
	err := d.QueryRow(ctx, `SELECT level FROM app_grants WHERE document_id = ? AND user_id = ?`, docID, userID).Scan(&lvl)
	if errors.Is(err, sql.ErrNoRows) {
		return -1, nil
	}
	return lvl, err
}

// SetAppGrant stores the level a person allowed an app.
func (d *DB) SetAppGrant(ctx context.Context, docID, userID string, level int) error {
	_, err := d.Exec(ctx, `INSERT INTO app_grants (document_id, user_id, tenant_id, level, created_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (document_id, user_id) DO UPDATE SET level = excluded.level, created_at = excluded.created_at`, docID, userID, TenantOf(ctx), level, Now())
	return err
}

// AppItem is one value in an app's storage.
type AppItem struct {
	Key       string `json:"key"`
	Value     string `json:"value"`
	Size      int    `json:"size"`
	UpdatedAt Time   `json:"updatedAt"`
}

// AppItems lists an app's values (userID "" = shared).
func (d *DB) AppItems(ctx context.Context, docID, userID string) ([]AppItem, error) {
	rows, err := d.Query(ctx, `SELECT item_key, value, size, updated_at FROM app_storage WHERE document_id = ? AND user_id = ? ORDER BY item_key`, docID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AppItem{}
	for rows.Next() {
		var it AppItem
		if err := rows.Scan(&it.Key, &it.Value, &it.Size, &it.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// AppItem reads one value.
func (d *DB) AppItem(ctx context.Context, docID, userID, key string) (AppItem, error) {
	var it AppItem
	err := d.QueryRow(ctx, `SELECT item_key, value, size, updated_at FROM app_storage WHERE document_id = ? AND user_id = ? AND item_key = ?`, docID, userID, key).
		Scan(&it.Key, &it.Value, &it.Size, &it.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return it, ErrNotFound
	}
	return it, err
}

// SetAppItem stores one value.
func (d *DB) SetAppItem(ctx context.Context, projectID, docID, userID, key, value string) error {
	_, err := d.Exec(ctx, `INSERT INTO app_storage (document_id, user_id, item_key, tenant_id, project_id, value, size, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (document_id, user_id, item_key) DO UPDATE SET value = excluded.value, size = excluded.size, updated_at = excluded.updated_at`,
		docID, userID, key, TenantOf(ctx), projectID, value, len(value), Now())
	return err
}

// DeleteAppItem removes one value.
func (d *DB) DeleteAppItem(ctx context.Context, docID, userID, key string) error {
	_, err := d.Exec(ctx, `DELETE FROM app_storage WHERE document_id = ? AND user_id = ? AND item_key = ?`, docID, userID, key)
	return err
}

// AppStorageSize is the bytes an app stores (all people), and of a project's apps.
func (d *DB) AppStorageSize(ctx context.Context, docID string) (int64, error) {
	var n sql.NullInt64
	err := d.QueryRow(ctx, `SELECT COALESCE((SELECT SUM(size) FROM app_storage WHERE document_id = ?), 0) + COALESCE((SELECT SUM(size) FROM app_records WHERE document_id = ?), 0)`, docID, docID).Scan(&n)
	return n.Int64, err
}

// ProjectAppStorage is the bytes all apps of a project store.
func (d *DB) ProjectAppStorage(ctx context.Context, projectID string) (int64, error) {
	var n sql.NullInt64
	err := d.QueryRow(ctx, `SELECT COALESCE((SELECT SUM(size) FROM app_storage WHERE project_id = ?), 0) + COALESCE((SELECT SUM(size) FROM app_records WHERE project_id = ?), 0)`, projectID, projectID).Scan(&n)
	return n.Int64, err
}

// AppStorageByDocument sums the storage of a project's apps (document id → bytes).
func (d *DB) AppStorageByDocument(ctx context.Context, projectID string) (map[string]int64, error) {
	rows, err := d.Query(ctx, `SELECT document_id, SUM(size) FROM (SELECT document_id, size FROM app_storage WHERE project_id = ?
		UNION ALL SELECT document_id, size FROM app_records WHERE project_id = ?) x GROUP BY document_id`, projectID, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var id string
		var n int64
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

// AppShare is the public release of an app (apps-definition "Sharing").
type AppShare struct {
	DocumentID    string `json:"documentId"`
	ProjectID     string `json:"projectId"`
	TenantID      string `json:"-"`
	Token         string `json:"token"`
	Mode          string `json:"mode"`     // static | snapshot
	Schedule      string `json:"schedule"` // manual | hourly | daily | weekly
	AILimit       int    `json:"aiLimit"`
	AIMonth       string `json:"aiMonth"`
	AIUsed        int    `json:"aiUsed"`
	Snapshot      string `json:"-"`
	SnapshotAt    Time   `json:"snapshotAt"`
	SnapshotError string `json:"snapshotError,omitempty"`
	PublishedBy   string `json:"publishedBy"`
	PublishedAt   Time   `json:"publishedAt"`
}

const appShareCols = `document_id, project_id, tenant_id, token, mode, schedule, ai_limit, ai_month, ai_used, snapshot, snapshot_at, snapshot_error, published_by, published_at`

func scanAppShare(r interface{ Scan(...any) error }) (AppShare, error) {
	var s AppShare
	err := r.Scan(&s.DocumentID, &s.ProjectID, &s.TenantID, &s.Token, &s.Mode, &s.Schedule, &s.AILimit, &s.AIMonth, &s.AIUsed, &s.Snapshot, &s.SnapshotAt, &s.SnapshotError, &s.PublishedBy, &s.PublishedAt)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return s, err
}

// AppShare reads the public release of an app.
func (d *DB) AppShare(ctx context.Context, docID string) (AppShare, error) {
	return scanAppShare(d.QueryRow(ctx, `SELECT `+appShareCols+` FROM app_shares WHERE document_id = ?`, docID))
}

// AppShareByToken finds a public app by its address (system context: the
// visitor has no tenant yet).
func (d *DB) AppShareByToken(ctx context.Context, token string) (AppShare, error) {
	return scanAppShare(d.QueryRow(ctx, `SELECT `+appShareCols+` FROM app_shares WHERE token = ?`, token))
}

// AppSharesDue lists releases with a schedule (all tenants in a system context).
func (d *DB) AppSharesDue(ctx context.Context) ([]AppShare, error) {
	rows, err := d.Query(ctx, `SELECT `+appShareCols+` FROM app_shares WHERE mode = 'snapshot' AND schedule <> 'manual'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AppShare{}
	for rows.Next() {
		s, err := scanAppShare(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ProjectAppShares lists the public apps of a project.
func (d *DB) ProjectAppShares(ctx context.Context, projectID string) ([]AppShare, error) {
	rows, err := d.Query(ctx, `SELECT `+appShareCols+` FROM app_shares WHERE project_id = ? ORDER BY published_at`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AppShare{}
	for rows.Next() {
		s, err := scanAppShare(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// SaveAppShare creates or replaces the public release of an app.
func (d *DB) SaveAppShare(ctx context.Context, s AppShare) error {
	_, err := d.Exec(ctx, `INSERT INTO app_shares (`+appShareCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (document_id) DO UPDATE SET mode = excluded.mode, schedule = excluded.schedule, ai_limit = excluded.ai_limit,
		snapshot = excluded.snapshot, snapshot_at = excluded.snapshot_at, snapshot_error = excluded.snapshot_error,
		published_by = excluded.published_by, published_at = excluded.published_at`,
		s.DocumentID, s.ProjectID, TenantOf(ctx), s.Token, s.Mode, s.Schedule, s.AILimit, s.AIMonth, s.AIUsed, s.Snapshot, s.SnapshotAt, s.SnapshotError, s.PublishedBy, s.PublishedAt)
	return err
}

// SetAppShareSnapshot stores a new snapshot (or the error of a failed one,
// keeping the old data).
func (d *DB) SetAppShareSnapshot(ctx context.Context, docID, snapshot string, at Time, errText string) error {
	if errText != "" {
		_, err := d.Exec(ctx, `UPDATE app_shares SET snapshot_error = ? WHERE document_id = ?`, errText, docID)
		return err
	}
	_, err := d.Exec(ctx, `UPDATE app_shares SET snapshot = ?, snapshot_at = ?, snapshot_error = '' WHERE document_id = ?`, snapshot, at, docID)
	return err
}

// UseAppShareAI counts one AI call against the monthly limit; false when
// the limit is reached.
func (d *DB) UseAppShareAI(ctx context.Context, docID, month string) (bool, error) {
	res, err := d.Exec(ctx, `UPDATE app_shares SET ai_used = CASE WHEN ai_month = ? THEN ai_used + 1 ELSE 1 END, ai_month = ?
		WHERE document_id = ? AND ai_limit > 0 AND (ai_month <> ? OR ai_used < ai_limit)`, month, month, docID, month)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// DeleteAppShare withdraws the public release.
func (d *DB) DeleteAppShare(ctx context.Context, docID string) error {
	_, err := d.Exec(ctx, `DELETE FROM app_shares WHERE document_id = ?`, docID)
	return err
}
