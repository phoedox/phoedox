package store

import (
	"context"
	"database/sql"
	"errors"
)

// SecretLink is a one-time secret: only the ciphertext is stored, the key
// lives in the link's #fragment and never reaches the server.
type SecretLink struct {
	ID         string `json:"id"`
	TenantID   string `json:"-"`
	Ciphertext string `json:"ciphertext"`
	Passphrase bool   `json:"passphrase"`
	ExpiresAt  Time   `json:"expiresAt"`
	CreatedBy  string `json:"-"`
	CreatedAt  Time   `json:"createdAt"`
}

// CreateSecretLink stores a secret link.
func (d *DB) CreateSecretLink(ctx context.Context, l SecretLink) error {
	_, err := d.Exec(ctx, `INSERT INTO secret_links (id, tenant_id, ciphertext, passphrase, expires_at, created_by, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		l.ID, TenantOf(ctx), l.Ciphertext, l.Passphrase, l.ExpiresAt, l.CreatedBy, l.CreatedAt)
	return err
}

// TakeSecretLink returns a secret link and deletes it in the same step, so
// it can be opened only once (system context: the opener is not signed in).
func (d *DB) TakeSecretLink(ctx context.Context, id string, now Time) (SecretLink, error) {
	var l SecretLink
	err := d.InTx(ctx, func(tx *Tx) error {
		err := tx.QueryRow(ctx, `SELECT id, tenant_id, ciphertext, passphrase, expires_at, created_by, created_at FROM secret_links WHERE id = ?`, id).
			Scan(&l.ID, &l.TenantID, &l.Ciphertext, &l.Passphrase, &l.ExpiresAt, &l.CreatedBy, &l.CreatedAt)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		res, err := tx.Exec(ctx, `DELETE FROM secret_links WHERE id = ?`, id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return ErrNotFound // someone else opened it at the same moment
		}
		if l.ExpiresAt.Before(now.Time) {
			return ErrNotFound
		}
		return nil
	})
	if errors.Is(err, ErrNotFound) {
		return SecretLink{}, ErrNotFound
	}
	return l, err
}

// PurgeSecretLinks deletes expired links (system context).
func (d *DB) PurgeSecretLinks(ctx context.Context, now Time) (int64, error) {
	res, err := d.Exec(ctx, `DELETE FROM secret_links WHERE expires_at < ?`, now)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
