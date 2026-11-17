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
	err := d.QueryRow(ctx, `SELECT SUM(size) FROM app_storage WHERE document_id = ?`, docID).Scan(&n)
	return n.Int64, err
}

// ProjectAppStorage is the bytes all apps of a project store.
func (d *DB) ProjectAppStorage(ctx context.Context, projectID string) (int64, error) {
	var n sql.NullInt64
	err := d.QueryRow(ctx, `SELECT SUM(size) FROM app_storage WHERE project_id = ?`, projectID).Scan(&n)
	return n.Int64, err
}

// AppStorageByDocument sums the storage of a project's apps (document id → bytes).
func (d *DB) AppStorageByDocument(ctx context.Context, projectID string) (map[string]int64, error) {
	rows, err := d.Query(ctx, `SELECT document_id, SUM(size) FROM app_storage WHERE project_id = ? GROUP BY document_id`, projectID)
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
