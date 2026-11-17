package store

import (
	"context"
	"database/sql"
	"errors"
)

// Apps like artifacts: records of collections and consent per device feature.

// AppRecord is one record of an app's collection (Owner "" = shared).
type AppRecord struct {
	ID         string `json:"id"`
	DocumentID string `json:"-"`
	Collection string `json:"collection"`
	Owner      string `json:"-"`
	Data       string `json:"-"`
	Size       int    `json:"-"`
	CreatedBy  string `json:"createdBy"`
	CreatedAt  Time   `json:"createdAt"`
	UpdatedAt  Time   `json:"updatedAt"`
}

const appRecordCols = `id, document_id, collection, owner, data, size, created_by, created_at, updated_at`

func scanAppRecord(r interface{ Scan(...any) error }) (AppRecord, error) {
	var x AppRecord
	err := r.Scan(&x.ID, &x.DocumentID, &x.Collection, &x.Owner, &x.Data, &x.Size, &x.CreatedBy, &x.CreatedAt, &x.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return x, ErrNotFound
	}
	return x, err
}

// AppRecords lists the records of a collection (oldest first, at most limit).
func (d *DB) AppRecords(ctx context.Context, docID, collection, owner string, limit int) ([]AppRecord, error) {
	rows, err := d.Query(ctx, `SELECT `+appRecordCols+` FROM app_records WHERE document_id = ? AND collection = ? AND owner = ? ORDER BY created_at, id LIMIT ?`,
		docID, collection, owner, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AppRecord{}
	for rows.Next() {
		x, err := scanAppRecord(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// AppRecord reads one record.
func (d *DB) AppRecord(ctx context.Context, docID, id string) (AppRecord, error) {
	return scanAppRecord(d.QueryRow(ctx, `SELECT `+appRecordCols+` FROM app_records WHERE document_id = ? AND id = ?`, docID, id))
}

// AppCollections lists the collection names of an app with their record count.
func (d *DB) AppCollections(ctx context.Context, docID, owner string) (map[string]int, error) {
	rows, err := d.Query(ctx, `SELECT collection, COUNT(*) FROM app_records WHERE document_id = ? AND owner = ? GROUP BY collection`, docID, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var c string
		var n int
		if err := rows.Scan(&c, &n); err != nil {
			return nil, err
		}
		out[c] = n
	}
	return out, rows.Err()
}

// PutAppRecord creates or replaces a record.
func (d *DB) PutAppRecord(ctx context.Context, projectID string, x AppRecord) (AppRecord, error) {
	now := Now()
	if x.CreatedAt.IsZero() {
		x.CreatedAt = now
	}
	x.UpdatedAt, x.Size = now, len(x.Data)
	_, err := d.Exec(ctx, `INSERT INTO app_records (id, tenant_id, project_id, document_id, collection, owner, data, size, created_by, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET data = excluded.data, size = excluded.size, updated_at = excluded.updated_at`,
		x.ID, TenantOf(ctx), projectID, x.DocumentID, x.Collection, x.Owner, x.Data, x.Size, x.CreatedBy, x.CreatedAt, x.UpdatedAt)
	return x, err
}

// DeleteAppRecord removes a record.
func (d *DB) DeleteAppRecord(ctx context.Context, docID, id string) error {
	_, err := d.Exec(ctx, `DELETE FROM app_records WHERE document_id = ? AND id = ?`, docID, id)
	return err
}

// AppRecordsSize is the bytes an app's records take.
func (d *DB) AppRecordsSize(ctx context.Context, docID string) (int64, error) {
	var n sql.NullInt64
	err := d.QueryRow(ctx, `SELECT SUM(size) FROM app_records WHERE document_id = ?`, docID).Scan(&n)
	return n.Int64, err
}

// AppDeviceGrants reads a person's answers about an app's device features
// (feature → allowed); a feature not asked yet is missing.
func (d *DB) AppDeviceGrants(ctx context.Context, docID, userID string) (map[string]bool, error) {
	rows, err := d.Query(ctx, `SELECT device, allowed FROM app_device_grants WHERE document_id = ? AND user_id = ?`, docID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var dev string
		var ok int
		if err := rows.Scan(&dev, &ok); err != nil {
			return nil, err
		}
		out[dev] = ok == 1
	}
	return out, rows.Err()
}

// SetAppDeviceGrant stores a person's answer for one feature; remove
// forgets it (the app asks again).
func (d *DB) SetAppDeviceGrant(ctx context.Context, docID, userID, device string, allowed, remove bool) error {
	if remove {
		_, err := d.Exec(ctx, `DELETE FROM app_device_grants WHERE document_id = ? AND user_id = ? AND device = ?`, docID, userID, device)
		return err
	}
	v := 0
	if allowed {
		v = 1
	}
	_, err := d.Exec(ctx, `INSERT INTO app_device_grants (document_id, user_id, device, tenant_id, allowed, created_at) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (document_id, user_id, device) DO UPDATE SET allowed = excluded.allowed, created_at = excluded.created_at`,
		docID, userID, device, TenantOf(ctx), v, Now())
	return err
}
