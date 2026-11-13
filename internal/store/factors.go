package store

import (
	"context"
	"database/sql"
	"errors"
)

// TOTP is the stored TOTP setup of a user.
type TOTP struct {
	UserID    string
	SecretEnc string
	Confirmed bool
	LastStep  int64
}

// SetTOTP stores a new (unconfirmed) TOTP seed, replacing any older one.
func (d *DB) SetTOTP(ctx context.Context, userID, secretEnc string) error {
	return d.InTx(ctx, func(tx *Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM user_totp WHERE user_id = ?`, userID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO user_totp (user_id, tenant_id, secret_enc, created_at) VALUES (?, ?, ?, ?)`, userID, DefaultTenant, secretEnc, Now())
		return err
	})
}

func (d *DB) GetTOTP(ctx context.Context, userID string) (TOTP, error) {
	t := TOTP{UserID: userID}
	var confirmed Time
	err := d.QueryRow(ctx, `SELECT secret_enc, confirmed_at, last_step FROM user_totp WHERE user_id = ?`, userID).Scan(&t.SecretEnc, &confirmed, &t.LastStep)
	if errors.Is(err, sql.ErrNoRows) {
		return t, ErrNotFound
	}
	t.Confirmed = !confirmed.IsZero()
	return t, err
}

// UseTOTPStep records the step of an accepted code. It fails with
// ErrConflict if that step (or a later one) was used already, so a code
// works once even with concurrent requests.
func (d *DB) UseTOTPStep(ctx context.Context, userID string, step int64, confirm bool) error {
	q := `UPDATE user_totp SET last_step = ? WHERE user_id = ? AND last_step < ?`
	args := []any{step, userID, step}
	if confirm {
		q = `UPDATE user_totp SET last_step = ?, confirmed_at = COALESCE(confirmed_at, ?) WHERE user_id = ? AND last_step < ?`
		args = []any{step, Now(), userID, step}
	}
	res, err := d.Exec(ctx, q, args...)
	if err := affected(res, err); errors.Is(err, ErrNotFound) {
		return ErrConflict
	} else if err != nil {
		return err
	}
	return nil
}

func (d *DB) DeleteTOTP(ctx context.Context, userID string) error {
	_, err := d.Exec(ctx, `DELETE FROM user_totp WHERE user_id = ?`, userID)
	return err
}

// ReplaceRecoveryCodes stores new code hashes and drops the old ones.
func (d *DB) ReplaceRecoveryCodes(ctx context.Context, userID string, hashes []string) error {
	return d.InTx(ctx, func(tx *Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM recovery_codes WHERE user_id = ?`, userID); err != nil {
			return err
		}
		now := Now()
		for _, h := range hashes {
			if _, err := tx.Exec(ctx, `INSERT INTO recovery_codes (code_hash, tenant_id, user_id, created_at) VALUES (?, ?, ?, ?)`, h, DefaultTenant, userID, now); err != nil {
				return err
			}
		}
		return nil
	})
}

// UseRecoveryCode marks a code as used; ErrNotFound if unknown or used.
func (d *DB) UseRecoveryCode(ctx context.Context, userID, hash string) error {
	res, err := d.Exec(ctx, `UPDATE recovery_codes SET used_at = ? WHERE code_hash = ? AND user_id = ? AND used_at IS NULL`, Now(), hash, userID)
	return affected(res, err)
}

func (d *DB) CountRecoveryCodes(ctx context.Context, userID string) (int, error) {
	var n int
	err := d.QueryRow(ctx, `SELECT count(*) FROM recovery_codes WHERE user_id = ? AND used_at IS NULL`, userID).Scan(&n)
	return n, err
}

// Passkey is a stored WebAuthn credential.
type Passkey struct {
	ID         string `json:"id"`
	UserID     string `json:"-"`
	Name       string `json:"name"`
	Credential string `json:"-"`
	CreatedAt  Time   `json:"createdAt"`
	LastUsedAt Time   `json:"lastUsedAt"`
}

func (d *DB) AddPasskey(ctx context.Context, p Passkey) (Passkey, error) {
	p.CreatedAt = Now()
	_, err := d.Exec(ctx, `INSERT INTO webauthn_credentials (id, tenant_id, user_id, name, credential, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		p.ID, DefaultTenant, p.UserID, p.Name, p.Credential, p.CreatedAt)
	return p, err
}

func (d *DB) Passkeys(ctx context.Context, userID string) ([]Passkey, error) {
	rows, err := d.Query(ctx, `SELECT id, user_id, name, credential, created_at, last_used_at FROM webauthn_credentials WHERE user_id = ? ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Passkey{}
	for rows.Next() {
		var p Passkey
		if err := rows.Scan(&p.ID, &p.UserID, &p.Name, &p.Credential, &p.CreatedAt, &p.LastUsedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// PasskeyByID returns a credential and its user (for discoverable login).
func (d *DB) PasskeyByID(ctx context.Context, id string) (Passkey, error) {
	var p Passkey
	err := d.QueryRow(ctx, `SELECT id, user_id, name, credential, created_at, last_used_at FROM webauthn_credentials WHERE id = ?`, id).
		Scan(&p.ID, &p.UserID, &p.Name, &p.Credential, &p.CreatedAt, &p.LastUsedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}

// UpdatePasskeyUse stores the updated credential (sign counter) after a login.
func (d *DB) UpdatePasskeyUse(ctx context.Context, id, credential string) error {
	_, err := d.Exec(ctx, `UPDATE webauthn_credentials SET credential = ?, last_used_at = ? WHERE id = ?`, credential, Now(), id)
	return err
}

func (d *DB) RenamePasskey(ctx context.Context, userID, id, name string) error {
	res, err := d.Exec(ctx, `UPDATE webauthn_credentials SET name = ? WHERE id = ? AND user_id = ?`, name, id, userID)
	return affected(res, err)
}

func (d *DB) DeletePasskey(ctx context.Context, userID, id string) error {
	res, err := d.Exec(ctx, `DELETE FROM webauthn_credentials WHERE id = ? AND user_id = ?`, id, userID)
	return affected(res, err)
}

// ResetSecondFactors removes TOTP, recovery codes and passkeys of a user.
func (d *DB) ResetSecondFactors(ctx context.Context, userID string) error {
	return d.InTx(ctx, func(tx *Tx) error {
		for _, q := range []string{`DELETE FROM user_totp WHERE user_id = ?`, `DELETE FROM recovery_codes WHERE user_id = ?`, `DELETE FROM webauthn_credentials WHERE user_id = ?`} {
			if _, err := tx.Exec(ctx, q, userID); err != nil {
				return err
			}
		}
		return nil
	})
}

// Challenge is short-lived state of a login step or WebAuthn ceremony.
type Challenge struct {
	ID       string
	UserID   string
	Kind     string
	Data     string
	Attempts int
}

func (d *DB) CreateChallenge(ctx context.Context, c Challenge, expires Time) error {
	var user any
	if c.UserID != "" {
		user = c.UserID
	}
	// Opportunistic cleanup keeps the table small.
	_, _ = d.Exec(ctx, `DELETE FROM auth_challenges WHERE expires_at < ?`, Now())
	_, err := d.Exec(ctx, `INSERT INTO auth_challenges (id, tenant_id, user_id, kind, data, expires_at) VALUES (?, ?, ?, ?, ?, ?)`,
		c.ID, DefaultTenant, user, c.Kind, c.Data, expires)
	return err
}

// GetChallenge returns an unexpired challenge of a kind and counts the attempt.
func (d *DB) GetChallenge(ctx context.Context, id, kind string) (Challenge, error) {
	var c Challenge
	var user sql.NullString
	err := d.QueryRow(ctx, `SELECT id, user_id, kind, data, attempts FROM auth_challenges WHERE id = ? AND kind = ? AND expires_at > ?`, id, kind, Now()).
		Scan(&c.ID, &user, &c.Kind, &c.Data, &c.Attempts)
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	if err != nil {
		return c, err
	}
	c.UserID = user.String
	_, err = d.Exec(ctx, `UPDATE auth_challenges SET attempts = attempts + 1 WHERE id = ?`, id)
	return c, err
}

func (d *DB) DeleteChallenge(ctx context.Context, id string) error {
	_, err := d.Exec(ctx, `DELETE FROM auth_challenges WHERE id = ?`, id)
	return err
}
