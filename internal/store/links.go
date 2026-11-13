package store

import (
	"context"
	"database/sql"
	"errors"
)

// Account link purposes.
const (
	LinkInvite = "invite"
	LinkReset  = "reset"
)

// AccountLink is a one-time link for an invitation or a password reset.
// Only the sha256 of the token is stored.
type AccountLink struct {
	ID        string `json:"-"`
	UserID    string `json:"userId"`
	Purpose   string `json:"purpose"`
	CreatedBy string `json:"createdBy"`
	CreatedAt Time   `json:"createdAt"`
	ExpiresAt Time   `json:"expiresAt"`
}

// CreateLink stores a new link and invalidates older unused links of the
// same user and purpose.
func (d *DB) CreateLink(ctx context.Context, l AccountLink) (AccountLink, error) {
	l.CreatedAt = Now()
	err := d.InTx(ctx, func(tx *Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE account_links SET used_at = ? WHERE user_id = ? AND purpose = ? AND used_at IS NULL`, l.CreatedAt, l.UserID, l.Purpose); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO account_links (id, tenant_id, user_id, purpose, created_by, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			l.ID, TenantOf(ctx), l.UserID, l.Purpose, l.CreatedBy, l.CreatedAt, l.ExpiresAt)
		return err
	})
	return l, err
}

// ValidLink returns an unused, unexpired link of an active user.
func (d *DB) ValidLink(ctx context.Context, idHash string) (AccountLink, error) {
	var l AccountLink
	err := d.QueryRow(ctx, `SELECT l.id, l.user_id, l.purpose, l.created_by, l.created_at, l.expires_at
		FROM account_links l JOIN users u ON u.id = l.user_id
		WHERE l.id = ? AND l.used_at IS NULL AND l.expires_at > ? AND u.disabled_at IS NULL`, idHash, Now()).
		Scan(&l.ID, &l.UserID, &l.Purpose, &l.CreatedBy, &l.CreatedAt, &l.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return l, ErrNotFound
	}
	return l, err
}

// UseLink marks a link as used and sets the user's password in one step.
// It fails with ErrNotFound if the link was used or expired meanwhile.
func (d *DB) UseLink(ctx context.Context, idHash, passwordHash string) (AccountLink, error) {
	var l AccountLink
	err := d.InTx(ctx, func(tx *Tx) error {
		err := tx.QueryRow(ctx, `SELECT user_id, purpose FROM account_links WHERE id = ? AND used_at IS NULL AND expires_at > ?`, idHash, Now()).Scan(&l.UserID, &l.Purpose)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		res, err := tx.Exec(ctx, `UPDATE account_links SET used_at = ? WHERE id = ? AND used_at IS NULL`, Now(), idHash)
		if err := affected(res, err); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE users SET password_hash = ? WHERE id = ?`, passwordHash, l.UserID); err != nil {
			return err
		}
		// A new password ends all existing sessions.
		_, err = tx.Exec(ctx, `DELETE FROM sessions WHERE user_id = ?`, l.UserID)
		return err
	})
	l.ID = idHash
	return l, err
}
