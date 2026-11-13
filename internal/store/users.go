package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// User is an account of the instance.
type User struct {
	ID           string `json:"id"`
	Email        string `json:"email"`
	Name         string `json:"name"`
	IsAdmin      bool   `json:"isAdmin"`
	Disabled     bool   `json:"disabled"`
	Locale       string `json:"locale"`           // "" = follow the browser
	Avatar       string `json:"avatar,omitempty"` // version of the profile picture, "" = none
	HasPassword  bool   `json:"hasPassword"`
	SecondFactor bool   `json:"secondFactor"` // filled by ListUsers
	PasswordHash string `json:"-"`
	CreatedAt    Time   `json:"createdAt"`
	LastLoginAt  Time   `json:"lastLoginAt"`
}

const userCols = `id, email, name, is_admin, password_hash, created_at, disabled_at, last_login_at, locale, avatar`

func scanUser(r interface{ Scan(...any) error }) (User, error) {
	var u User
	var disabled Time
	err := r.Scan(&u.ID, &u.Email, &u.Name, &u.IsAdmin, &u.PasswordHash, &u.CreatedAt, &disabled, &u.LastLoginAt, &u.Locale, &u.Avatar)
	if errors.Is(err, sql.ErrNoRows) {
		return u, ErrNotFound
	}
	u.Disabled = !disabled.IsZero()
	u.HasPassword = u.PasswordHash != ""
	return u, err
}

// CreateUser inserts a user. Email must be unique (ErrConflict otherwise).
func (d *DB) CreateUser(ctx context.Context, u User) (User, error) {
	u.CreatedAt = Now()
	u.Email = strings.ToLower(strings.TrimSpace(u.Email))
	u.HasPassword = u.PasswordHash != ""
	_, err := d.Exec(ctx, `INSERT INTO users (id, tenant_id, email, name, is_admin, password_hash, created_at, locale) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		u.ID, TenantOf(ctx), u.Email, u.Name, u.IsAdmin, u.PasswordHash, u.CreatedAt, u.Locale)
	return u, err
}

// UserByEmail returns an active (not disabled) user.
func (d *DB) UserByEmail(ctx context.Context, email string) (User, error) {
	return scanUser(d.QueryRow(ctx, `SELECT `+userCols+` FROM users WHERE tenant_id = ? AND email = ? AND disabled_at IS NULL`, TenantOf(ctx), strings.ToLower(strings.TrimSpace(email))))
}

// UserByID returns a user, also a disabled one.
func (d *DB) UserByID(ctx context.Context, id string) (User, error) {
	return scanUser(d.QueryRow(ctx, `SELECT `+userCols+` FROM users WHERE id = ?`, id))
}

// ListUsers returns all users of the instance, ordered by name.
func (d *DB) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := d.Query(ctx, `SELECT `+userCols+` FROM users WHERE tenant_id = ? ORDER BY lower(name), email`, TenantOf(ctx))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []User{}
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	with := map[string]bool{}
	frows, err := d.Query(ctx, `SELECT user_id FROM user_totp WHERE confirmed_at IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	defer frows.Close()
	for frows.Next() {
		var id string
		if err := frows.Scan(&id); err != nil {
			return nil, err
		}
		with[id] = true
	}
	for i := range out {
		out[i].SecondFactor = with[out[i].ID]
	}
	return out, frows.Err()
}

// UpdateUser saves name, admin flag, disabled state and locale. Disabling
// ends all sessions of the user.
func (d *DB) UpdateUser(ctx context.Context, u User) error {
	var disabled any
	if u.Disabled {
		disabled = Now()
	}
	return d.InTx(ctx, func(tx *Tx) error {
		res, err := tx.Exec(ctx, `UPDATE users SET name = ?, is_admin = ?, locale = ?,
			disabled_at = CASE WHEN ? THEN COALESCE(disabled_at, ?) ELSE NULL END WHERE id = ?`,
			u.Name, u.IsAdmin, u.Locale, u.Disabled, disabled, u.ID)
		if err := affected(res, err); err != nil {
			return err
		}
		if u.Disabled {
			_, err = tx.Exec(ctx, `DELETE FROM sessions WHERE user_id = ?`, u.ID)
		}
		return err
	})
}

// CountActiveAdmins counts system admins that are not disabled.
func (d *DB) CountActiveAdmins(ctx context.Context) (int, error) {
	var n int
	err := d.QueryRow(ctx, `SELECT count(*) FROM users WHERE tenant_id = ? AND is_admin = ? AND disabled_at IS NULL`, TenantOf(ctx), true).Scan(&n)
	return n, err
}

func (d *DB) SetPassword(ctx context.Context, userID, hash string) error {
	res, err := d.Exec(ctx, `UPDATE users SET password_hash = ? WHERE id = ?`, hash, userID)
	return affected(res, err)
}

// TouchLogin records a successful login.
func (d *DB) TouchLogin(ctx context.Context, userID string) error {
	_, err := d.Exec(ctx, `UPDATE users SET last_login_at = ? WHERE id = ?`, Now(), userID)
	return err
}

func (d *DB) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := d.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&n)
	return n, err
}

// Session is a browser login.
type Session struct {
	ID        string
	UserID    string
	ExpiresAt Time
}

func (d *DB) CreateSession(ctx context.Context, idHash, userID string, expires Time) error {
	_, err := d.Exec(ctx, `INSERT INTO sessions (id, user_id, created_at, expires_at) VALUES (?, ?, ?, ?)`, idHash, userID, Now(), expires)
	return err
}

// UserBySession returns the active user of a valid, unexpired session.
func (d *DB) UserBySession(ctx context.Context, idHash string) (User, error) {
	return scanUser(d.QueryRow(ctx, `SELECT `+prefixCols("u.", userCols)+`
		FROM sessions s JOIN users u ON u.id = s.user_id WHERE s.id = ? AND s.expires_at > ? AND u.disabled_at IS NULL`, idHash, Now()))
}

// DeleteUserSessions ends all sessions of a user except keep (a session id hash, may be empty).
func (d *DB) DeleteUserSessions(ctx context.Context, userID, keep string) error {
	_, err := d.Exec(ctx, `DELETE FROM sessions WHERE user_id = ? AND id <> ?`, userID, keep)
	return err
}

func (d *DB) DeleteSession(ctx context.Context, idHash string) error {
	_, err := d.Exec(ctx, `DELETE FROM sessions WHERE id = ?`, idHash)
	return err
}

// Token is a personal API token (REST, MCP, CLI).
type Token struct {
	ID         string `json:"id"`
	UserID     string `json:"userId"`
	Name       string `json:"name"`
	Prefix     string `json:"prefix"`
	CreatedAt  Time   `json:"createdAt"`
	LastUsedAt Time   `json:"lastUsedAt"`
	// Access is "read" or "write"; Projects limits the token to these
	// project ids (empty = all projects of the user).
	Access   string   `json:"access"`
	Projects []string `json:"projects"`
}

func (d *DB) CreateToken(ctx context.Context, t Token, hash string) (Token, error) {
	t.CreatedAt = Now()
	if t.Access == "" {
		t.Access = AccessWrite
	}
	_, err := d.Exec(ctx, `INSERT INTO api_tokens (id, user_id, name, token_hash, prefix, created_at, access, projects) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.UserID, t.Name, hash, t.Prefix, t.CreatedAt, t.Access, joinIDs(t.Projects))
	return t, err
}

// UserByToken returns the active owner of a valid token and records its use.
func (d *DB) UserByToken(ctx context.Context, hash string) (User, Token, error) {
	var t Token
	row := d.QueryRow(ctx, `SELECT t.id, t.name, t.prefix, t.access, t.projects, `+prefixCols("u.", userCols)+`
		FROM api_tokens t JOIN users u ON u.id = t.user_id WHERE t.token_hash = ? AND t.revoked_at IS NULL AND u.disabled_at IS NULL`, hash)
	var tok [5]string
	u, err := scanUser(scanFunc(func(dest ...any) error {
		return row.Scan(append([]any{&tok[0], &tok[1], &tok[2], &tok[3], &tok[4]}, dest...)...)
	}))
	if err != nil {
		return u, t, err
	}
	t.ID, t.Name, t.Prefix, t.Access, t.Projects, t.UserID = tok[0], tok[1], tok[2], tok[3], splitIDs(tok[4]), u.ID
	_, _ = d.Exec(ctx, `UPDATE api_tokens SET last_used_at = ? WHERE id = ?`, Now(), t.ID)
	return u, t, nil
}

// scanFunc adapts a function to the Scan interface.
type scanFunc func(dest ...any) error

func (f scanFunc) Scan(dest ...any) error { return f(dest...) }

func (d *DB) ListTokens(ctx context.Context, userID string) ([]Token, error) {
	rows, err := d.Query(ctx, `SELECT id, user_id, name, prefix, created_at, last_used_at, access, projects FROM api_tokens
		WHERE user_id = ? AND revoked_at IS NULL ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Token
	for rows.Next() {
		var t Token
		var projects string
		if err := rows.Scan(&t.ID, &t.UserID, &t.Name, &t.Prefix, &t.CreatedAt, &t.LastUsedAt, &t.Access, &projects); err != nil {
			return nil, err
		}
		t.Projects = splitIDs(projects)
		out = append(out, t)
	}
	return out, rows.Err()
}

func (d *DB) RevokeToken(ctx context.Context, userID, tokenID string) error {
	res, err := d.Exec(ctx, `UPDATE api_tokens SET revoked_at = ? WHERE id = ? AND user_id = ? AND revoked_at IS NULL`, Now(), tokenID, userID)
	return affected(res, err)
}

func affected(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// UserByIdentity returns the user linked to an external identity.
func (d *DB) UserByIdentity(ctx context.Context, provider, subject string) (User, error) {
	u, err := scanUser(d.QueryRow(ctx, `SELECT `+prefixCols("u.", userCols)+` FROM user_identities i JOIN users u ON u.id = i.user_id
		WHERE i.provider = ? AND i.subject = ?`, provider, subject))
	if err == nil {
		_, _ = d.Exec(ctx, `UPDATE user_identities SET last_login_at = ? WHERE provider = ? AND subject = ?`, Now(), provider, subject)
	}
	return u, err
}

// LinkIdentity connects an external identity with a user.
func (d *DB) LinkIdentity(ctx context.Context, provider, subject, userID, email string) error {
	now := Now()
	_, err := d.Exec(ctx, `INSERT INTO user_identities (provider, subject, tenant_id, user_id, email, created_at, last_login_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		provider, subject, TenantOf(ctx), userID, email, now, now)
	return err
}

// UserByEmailAny returns a user by email, also a disabled one.
func (d *DB) UserByEmailAny(ctx context.Context, email string) (User, error) {
	return scanUser(d.QueryRow(ctx, `SELECT `+userCols+` FROM users WHERE tenant_id = ? AND email = ?`, TenantOf(ctx), strings.ToLower(strings.TrimSpace(email))))
}
