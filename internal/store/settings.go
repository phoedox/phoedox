package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// Setting returns an instance setting or def if unset.
func (d *DB) Setting(ctx context.Context, key, def string) (string, error) {
	var v string
	err := d.QueryRow(ctx, `SELECT value FROM instance_settings WHERE tenant_id = ? AND key = ?`, DefaultTenant, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return def, nil
	}
	return v, err
}

// SetSetting stores an instance setting.
func (d *DB) SetSetting(ctx context.Context, key, value, by string) error {
	return d.InTx(ctx, func(tx *Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM instance_settings WHERE tenant_id = ? AND key = ?`, DefaultTenant, key); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO instance_settings (tenant_id, key, value, updated_by, updated_at) VALUES (?, ?, ?, ?, ?)`, DefaultTenant, key, value, by, Now())
		return err
	})
}

// Access levels of tokens and grants.
const (
	AccessRead  = "read"
	AccessWrite = "write"
)

func joinIDs(ids []string) string { return strings.Join(ids, ",") }

func splitIDs(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}
