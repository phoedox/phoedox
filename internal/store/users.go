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
	PasswordHash string `json:"-"`
	CreatedAt    Time   `json:"createdAt"`
}

const userCols = `id, email, name, is_admin, password_hash, created_at`

func scanUser(r interface{ Scan(...any) error }) (User, error) {
	var u User
	err := r.Scan(&u.ID, &u.Email, &u.Name, &u.IsAdmin, &u.PasswordHash, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return u, ErrNotFound
	}
	return u, err
}

// CreateUser inserts a user. Email must be unique.
func (d *DB) CreateUser(ctx context.Context, u User) (User, error) {
	u.CreatedAt = Now()
	u.Email = strings.ToLower(strings.TrimSpace(u.Email))
	_, err := d.Exec(ctx, `INSERT INTO users (id, tenant_id, email, name, is_admin, password_hash, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		u.ID, DefaultTenant, u.Email, u.Name, u.IsAdmin, u.PasswordHash, u.CreatedAt)
	return u, err
}

func (d *DB) UserByEmail(ctx context.Context, email string) (User, error) {
	return scanUser(d.QueryRow(ctx, `SELECT `+userCols+` FROM users WHERE tenant_id = ? AND email = ?`, DefaultTenant, strings.ToLower(strings.TrimSpace(email))))
}

func (d *DB) UserByID(ctx context.Context, id string) (User, error) {
	return scanUser(d.QueryRow(ctx, `SELECT `+userCols+` FROM users WHERE id = ?`, id))
}

func (d *DB) SetPassword(ctx context.Context, userID, hash string) error {
	res, err := d.Exec(ctx, `UPDATE users SET password_hash = ? WHERE id = ?`, hash, userID)
	return affected(res, err)
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

// UserBySession returns the user of a valid, unexpired session.
func (d *DB) UserBySession(ctx context.Context, idHash string) (User, error) {
	return scanUser(d.QueryRow(ctx, `SELECT u.id, u.email, u.name, u.is_admin, u.password_hash, u.created_at
		FROM sessions s JOIN users u ON u.id = s.user_id WHERE s.id = ? AND s.expires_at > ?`, idHash, Now()))
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
}

func (d *DB) CreateToken(ctx context.Context, t Token, hash string) (Token, error) {
	t.CreatedAt = Now()
	_, err := d.Exec(ctx, `INSERT INTO api_tokens (id, user_id, name, token_hash, prefix, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		t.ID, t.UserID, t.Name, hash, t.Prefix, t.CreatedAt)
	return t, err
}

// UserByToken returns the owner of a valid token and records its use.
func (d *DB) UserByToken(ctx context.Context, hash string) (User, Token, error) {
	var t Token
	row := d.QueryRow(ctx, `SELECT t.id, t.name, t.prefix, u.id, u.email, u.name, u.is_admin, u.password_hash, u.created_at
		FROM api_tokens t JOIN users u ON u.id = t.user_id WHERE t.token_hash = ? AND t.revoked_at IS NULL`, hash)
	var u User
	err := row.Scan(&t.ID, &t.Name, &t.Prefix, &u.ID, &u.Email, &u.Name, &u.IsAdmin, &u.PasswordHash, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return u, t, ErrNotFound
	}
	if err != nil {
		return u, t, err
	}
	t.UserID = u.ID
	_, _ = d.Exec(ctx, `UPDATE api_tokens SET last_used_at = ? WHERE id = ?`, Now(), t.ID)
	return u, t, nil
}

func (d *DB) ListTokens(ctx context.Context, userID string) ([]Token, error) {
	rows, err := d.Query(ctx, `SELECT id, user_id, name, prefix, created_at, last_used_at FROM api_tokens
		WHERE user_id = ? AND revoked_at IS NULL ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Token
	for rows.Next() {
		var t Token
		if err := rows.Scan(&t.ID, &t.UserID, &t.Name, &t.Prefix, &t.CreatedAt, &t.LastUsedAt); err != nil {
			return nil, err
		}
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
