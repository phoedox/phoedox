package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// OAuthClient is a registered OAuth client (usually via dynamic registration).
type OAuthClient struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	ClientURI    string   `json:"clientUri,omitempty"`
	RedirectURIs []string `json:"redirectUris"`
	SecretHash   string   `json:"-"`
	CreatedAt    Time     `json:"createdAt"`
}

func (d *DB) CreateOAuthClient(ctx context.Context, c OAuthClient) (OAuthClient, error) {
	c.CreatedAt = Now()
	uris, _ := json.Marshal(c.RedirectURIs)
	var secret any
	if c.SecretHash != "" {
		secret = c.SecretHash
	}
	_, err := d.Exec(ctx, `INSERT INTO oauth_clients (id, tenant_id, name, client_uri, redirect_uris, secret_hash, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		c.ID, DefaultTenant, c.Name, c.ClientURI, string(uris), secret, c.CreatedAt)
	return c, err
}

func (d *DB) OAuthClientByID(ctx context.Context, id string) (OAuthClient, error) {
	var c OAuthClient
	var uris string
	var secret sql.NullString
	err := d.QueryRow(ctx, `SELECT id, name, client_uri, redirect_uris, secret_hash, created_at FROM oauth_clients WHERE id = ?`, id).
		Scan(&c.ID, &c.Name, &c.ClientURI, &uris, &secret, &c.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	if err != nil {
		return c, err
	}
	c.SecretHash = secret.String
	err = json.Unmarshal([]byte(uris), &c.RedirectURIs)
	return c, err
}

// OAuthGrant is a user's consent for a client.
type OAuthGrant struct {
	ID         string   `json:"id"`
	ClientID   string   `json:"clientId"`
	ClientName string   `json:"clientName"`
	ClientURI  string   `json:"clientUri,omitempty"`
	UserID     string   `json:"-"`
	Scope      string   `json:"scope"`
	Resource   string   `json:"resource"`
	CreatedAt  Time     `json:"createdAt"`
	LastUsedAt Time     `json:"lastUsedAt"`
	Access     string   `json:"access"`   // read | write
	Projects   []string `json:"projects"` // empty = all
}

// UpsertGrant returns the active grant of user and client, creating it or
// updating scope and resource.
func (d *DB) UpsertGrant(ctx context.Context, g OAuthGrant) (OAuthGrant, error) {
	err := d.InTx(ctx, func(tx *Tx) error {
		var id string
		err := tx.QueryRow(ctx, `SELECT id FROM oauth_grants WHERE user_id = ? AND client_id = ? AND revoked_at IS NULL`, g.UserID, g.ClientID).Scan(&id)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			g.CreatedAt = Now()
			_, err = tx.Exec(ctx, `INSERT INTO oauth_grants (id, tenant_id, client_id, user_id, scope, resource, created_at, access, projects) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				g.ID, DefaultTenant, g.ClientID, g.UserID, g.Scope, g.Resource, g.CreatedAt, g.Access, joinIDs(g.Projects))
			return err
		case err != nil:
			return err
		}
		g.ID = id
		_, err = tx.Exec(ctx, `UPDATE oauth_grants SET scope = ?, resource = ?, access = ?, projects = ? WHERE id = ?`, g.Scope, g.Resource, g.Access, joinIDs(g.Projects), id)
		return err
	})
	return g, err
}

const grantCols = `g.id, g.client_id, c.name, c.client_uri, g.user_id, g.scope, g.resource, g.created_at, g.last_used_at, g.access, g.projects`

func scanGrant(r interface{ Scan(...any) error }) (OAuthGrant, error) {
	var g OAuthGrant
	var projects string
	err := r.Scan(&g.ID, &g.ClientID, &g.ClientName, &g.ClientURI, &g.UserID, &g.Scope, &g.Resource, &g.CreatedAt, &g.LastUsedAt, &g.Access, &projects)
	if errors.Is(err, sql.ErrNoRows) {
		return g, ErrNotFound
	}
	g.Projects = splitIDs(projects)
	return g, err
}

// Grants lists the active grants of a user.
func (d *DB) Grants(ctx context.Context, userID string) ([]OAuthGrant, error) {
	rows, err := d.Query(ctx, `SELECT `+grantCols+` FROM oauth_grants g JOIN oauth_clients c ON c.id = g.client_id
		WHERE g.user_id = ? AND g.revoked_at IS NULL ORDER BY g.created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []OAuthGrant{}
	for rows.Next() {
		g, err := scanGrant(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// RevokeGrant ends a grant and all its tokens. userID "" skips the owner check.
func (d *DB) RevokeGrant(ctx context.Context, userID, grantID string) error {
	q, args := `UPDATE oauth_grants SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`, []any{Now(), grantID}
	if userID != "" {
		q += ` AND user_id = ?`
		args = append(args, userID)
	}
	res, err := d.Exec(ctx, q, args...)
	return affected(res, err)
}

// CreateOAuthCode stores an authorization code for a grant.
func (d *DB) CreateOAuthCode(ctx context.Context, hash, grantID, redirectURI, challenge string, expires Time) error {
	_, err := d.Exec(ctx, `INSERT INTO oauth_codes (id, tenant_id, grant_id, redirect_uri, code_challenge, expires_at) VALUES (?, ?, ?, ?, ?, ?)`,
		hash, DefaultTenant, grantID, redirectURI, challenge, expires)
	return err
}

// OAuthCode is a stored authorization code with its grant.
type OAuthCode struct {
	Grant         OAuthGrant
	RedirectURI   string
	CodeChallenge string
	Used          bool
	Expired       bool
}

// UseOAuthCode loads a code and marks it used (a second use reports Used).
func (d *DB) UseOAuthCode(ctx context.Context, hash string) (OAuthCode, error) {
	var c OAuthCode
	var used, expires Time
	row := d.QueryRow(ctx, `SELECT `+grantCols+`, o.redirect_uri, o.code_challenge, o.used_at, o.expires_at
		FROM oauth_codes o JOIN oauth_grants g ON g.id = o.grant_id JOIN oauth_clients c ON c.id = g.client_id
		WHERE o.id = ? AND g.revoked_at IS NULL`, hash)
	g, err := scanGrant(scanFunc(func(dest ...any) error {
		return row.Scan(append(dest, &c.RedirectURI, &c.CodeChallenge, &used, &expires)...)
	}))
	if err != nil {
		return c, err
	}
	c.Grant = g
	c.Used = !used.IsZero()
	c.Expired = !expires.After(Now().Time)
	if !c.Used {
		res, err := d.Exec(ctx, `UPDATE oauth_codes SET used_at = ? WHERE id = ? AND used_at IS NULL`, Now(), hash)
		if err := affected(res, err); errors.Is(err, ErrNotFound) {
			c.Used = true // lost a race with a parallel request
		} else if err != nil {
			return c, err
		}
	}
	return c, nil
}

func (d *DB) CreateOAuthToken(ctx context.Context, hash, grantID, kind string, expires Time) error {
	_, err := d.Exec(ctx, `INSERT INTO oauth_tokens (id, tenant_id, grant_id, kind, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?)`,
		hash, DefaultTenant, grantID, kind, Now(), expires)
	return err
}

// OAuthToken is a stored token with its grant.
type OAuthToken struct {
	Grant     OAuthGrant
	Kind      string
	ExpiresAt Time
	Used      bool
}

// OAuthTokenByHash returns a token of an active grant (expired tokens too;
// callers check ExpiresAt).
func (d *DB) OAuthTokenByHash(ctx context.Context, hash string) (OAuthToken, error) {
	var t OAuthToken
	var used Time
	row := d.QueryRow(ctx, `SELECT `+grantCols+`, t.kind, t.expires_at, t.used_at
		FROM oauth_tokens t JOIN oauth_grants g ON g.id = t.grant_id JOIN oauth_clients c ON c.id = g.client_id
		WHERE t.id = ? AND g.revoked_at IS NULL`, hash)
	g, err := scanGrant(scanFunc(func(dest ...any) error {
		return row.Scan(append(dest, &t.Kind, &t.ExpiresAt, &used)...)
	}))
	t.Grant = g
	t.Used = !used.IsZero()
	return t, err
}

// UseRefreshToken marks a refresh token as rotated; ErrConflict if it was
// used before (a sign of theft).
func (d *DB) UseRefreshToken(ctx context.Context, hash string) error {
	res, err := d.Exec(ctx, `UPDATE oauth_tokens SET used_at = ? WHERE id = ? AND kind = 'refresh' AND used_at IS NULL`, Now(), hash)
	if err := affected(res, err); errors.Is(err, ErrNotFound) {
		return ErrConflict
	} else if err != nil {
		return err
	}
	return nil
}

// RevokeOAuthToken deletes one token (RFC 7009); unknown tokens are fine.
func (d *DB) RevokeOAuthToken(ctx context.Context, hash string) error {
	_, err := d.Exec(ctx, `DELETE FROM oauth_tokens WHERE id = ?`, hash)
	return err
}

// TouchGrant records the use of a grant and its client.
func (d *DB) TouchGrant(ctx context.Context, g OAuthGrant) {
	now := Now()
	_, _ = d.Exec(ctx, `UPDATE oauth_grants SET last_used_at = ? WHERE id = ?`, now, g.ID)
	_, _ = d.Exec(ctx, `UPDATE oauth_clients SET last_used_at = ? WHERE id = ?`, now, g.ClientID)
}
