package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// UserExport is one export of a person's data (ADR-0055).
type UserExport struct {
	ID        string `json:"id"`
	UserID    string `json:"-"`
	Status    string `json:"status"` // queued | done | failed
	SHA256    string `json:"-"`
	Size      int64  `json:"size"`
	Error     string `json:"-"`
	CreatedAt Time   `json:"createdAt"`
	ExpiresAt Time   `json:"expiresAt"`
}

const exportCols = `id, user_id, status, sha256, size, error, created_at, expires_at`

func scanExport(r interface{ Scan(...any) error }) (UserExport, error) {
	var e UserExport
	err := r.Scan(&e.ID, &e.UserID, &e.Status, &e.SHA256, &e.Size, &e.Error, &e.CreatedAt, &e.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return e, ErrNotFound
	}
	return e, err
}

// CreateUserExport records a queued export.
func (d *DB) CreateUserExport(ctx context.Context, e UserExport) error {
	_, err := d.Exec(ctx, `INSERT INTO user_exports (id, tenant_id, user_id, status, created_at, expires_at) VALUES (?, ?, ?, 'queued', ?, ?)`,
		e.ID, TenantOf(ctx), e.UserID, e.CreatedAt, e.ExpiresAt)
	return err
}

// GetUserExport returns one export.
func (d *DB) GetUserExport(ctx context.Context, id string) (UserExport, error) {
	return scanExport(d.QueryRow(ctx, `SELECT `+exportCols+` FROM user_exports WHERE id = ?`, id))
}

// LatestUserExport returns the newest export of a person that did not fail.
func (d *DB) LatestUserExport(ctx context.Context, userID string) (UserExport, error) {
	return scanExport(d.QueryRow(ctx, `SELECT `+exportCols+` FROM user_exports WHERE user_id = ? AND status <> 'failed' ORDER BY created_at DESC LIMIT 1`, userID))
}

// FinishUserExport stores the result of an export job.
func (d *DB) FinishUserExport(ctx context.Context, id, sha string, size int64, failure string) error {
	status := "done"
	if failure != "" {
		status = "failed"
	}
	_, err := d.Exec(ctx, `UPDATE user_exports SET status = ?, sha256 = ?, size = ?, error = ? WHERE id = ?`, status, sha, size, failure, id)
	return err
}

// ExpiredUserExports lists exports past their expiry (all tenants with a System context).
func (d *DB) ExpiredUserExports(ctx context.Context, now Time) ([]UserExport, error) {
	rows, err := d.Query(ctx, `SELECT `+exportCols+` FROM user_exports WHERE expires_at < ?`, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UserExport
	for rows.Next() {
		e, err := scanExport(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// UserExports lists all exports of a person (used when deleting the account).
func (d *DB) UserExports(ctx context.Context, userID string) ([]UserExport, error) {
	rows, err := d.Query(ctx, `SELECT `+exportCols+` FROM user_exports WHERE user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UserExport
	for rows.Next() {
		e, err := scanExport(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// DeleteUserExport removes an export record.
func (d *DB) DeleteUserExport(ctx context.Context, id string) error {
	_, err := d.Exec(ctx, `DELETE FROM user_exports WHERE id = ?`, id)
	return err
}

// Rows runs a query and returns each row as a map of column to value
// (bytes become strings). Used to write personal data exports.
func (d *DB) Rows(ctx context.Context, query string, args ...any) ([]map[string]any, error) {
	rows, err := d.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	out := []map[string]any{}
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		m := make(map[string]any, len(cols))
		for i, c := range cols {
			switch v := vals[i].(type) {
			case []byte:
				m[c] = string(v)
			case time.Time:
				m[c] = v.UTC()
			default:
				m[c] = v
			}
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// OwnedProject is a project a person owns directly, with the people who
// would remain (for account deletion).
type OwnedProject struct {
	ID, Slug, Name, OrgID, OwnerID string
	// OtherOwner is another owner (direct owner or admin of the project's
	// organization), "" if there is none.
	OtherOwner string
	// OtherMembers counts other direct members and group grants.
	OtherMembers int
}

// OwnedProjects lists the projects where userID is a direct owner.
func (d *DB) OwnedProjects(ctx context.Context, userID string, now Time) ([]OwnedProject, error) {
	rows, err := d.Query(ctx, `SELECT p.id, p.slug, p.name, COALESCE(p.org_id, ''), p.owner_id,
		COALESCE((SELECT x.user_id FROM project_members x JOIN users u ON u.id = x.user_id
			WHERE x.project_id = p.id AND x.user_id <> ? AND x.role = 'owner' AND (x.expires_at IS NULL OR x.expires_at > ?) AND u.deleted_at IS NULL AND u.disabled_at IS NULL
			ORDER BY x.user_id LIMIT 1),
		 (SELECT om.user_id FROM org_members om JOIN users u ON u.id = om.user_id
			WHERE om.org_id = p.org_id AND om.user_id <> ? AND om.role = 'admin' AND u.deleted_at IS NULL AND u.disabled_at IS NULL
			ORDER BY om.user_id LIMIT 1), ''),
		(SELECT count(*) FROM project_members x WHERE x.project_id = p.id AND x.user_id <> ?) + (SELECT count(*) FROM project_groups g WHERE g.project_id = p.id)
		FROM projects p JOIN project_members pm ON pm.project_id = p.id
		WHERE p.tenant_id = ? AND pm.user_id = ? AND pm.role = 'owner'
		ORDER BY p.name`, userID, now, userID, userID, TenantOf(ctx), userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OwnedProject
	for rows.Next() {
		var p OwnedProject
		if err := rows.Scan(&p.ID, &p.Slug, &p.Name, &p.OrgID, &p.OwnerID, &p.OtherOwner, &p.OtherMembers); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// AdminOrg is an organization a person administers, with who would remain.
type AdminOrg struct {
	ID, Slug, Name string
	OtherMembers   int
	OtherAdmins    int
}

// AdminOrgs lists the organizations where userID is an admin.
func (d *DB) AdminOrgs(ctx context.Context, userID string) ([]AdminOrg, error) {
	rows, err := d.Query(ctx, `SELECT o.id, o.slug, o.name,
		(SELECT count(*) FROM org_members x WHERE x.org_id = o.id AND x.user_id <> ?),
		(SELECT count(*) FROM org_members x JOIN users u ON u.id = x.user_id WHERE x.org_id = o.id AND x.user_id <> ? AND x.role = 'admin' AND u.deleted_at IS NULL AND u.disabled_at IS NULL)
		FROM organizations o JOIN org_members m ON m.org_id = o.id
		WHERE o.tenant_id = ? AND m.user_id = ? AND m.role = 'admin' ORDER BY o.name`, userID, userID, TenantOf(ctx), userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AdminOrg
	for rows.Next() {
		var o AdminOrg
		if err := rows.Scan(&o.ID, &o.Slug, &o.Name, &o.OtherMembers, &o.OtherAdmins); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// OrgProjectCount counts the projects of an organization.
func (d *DB) OrgProjectCount(ctx context.Context, orgID string) (int, error) {
	var n int
	err := d.QueryRow(ctx, `SELECT count(*) FROM projects WHERE org_id = ?`, orgID).Scan(&n)
	return n, err
}

// PurgeProject deletes a project with everything in it and returns the file
// contents it used, so the caller can free those no one else uses.
func (d *DB) PurgeProject(ctx context.Context, id string) ([]string, error) {
	hashes, err := d.fileHashes(ctx, `files.project_id = ?`, id)
	if err != nil {
		return nil, err
	}
	err = d.InTx(ctx, func(tx *Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM images WHERE kind = 'project' AND owner_id = ?`, id); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `DELETE FROM projects WHERE id = ?`, id)
		return err
	})
	return hashes, err
}

// DeleteOrg deletes an organization without projects (members and groups go with it).
func (d *DB) DeleteOrg(ctx context.Context, id string) error {
	_, err := d.Exec(ctx, `DELETE FROM organizations WHERE id = ?`, id)
	return err
}

// Anonymize describes how an account is anonymised.
type Anonymize struct {
	UserID string
	// Email and Name replace the person's address and name ("deleted person").
	Email, Name string
	// NewOwners moves projects whose owner_id is the person to another owner.
	NewOwners map[string]string
	At        Time
}

// AnonymizeUser removes a person's personal data but keeps the row, so
// shared content keeps an author (ADR-0055): sign-in data, memberships,
// preferences, notifications and the profile picture are deleted; name and
// address are replaced; responsibilities are cleared. Events stay (they are
// chained); they only carry the pseudonymous id.
func (d *DB) AnonymizeUser(ctx context.Context, a Anonymize) error {
	id := a.UserID
	return d.InTx(ctx, func(tx *Tx) error {
		for project, owner := range a.NewOwners {
			if _, err := tx.Exec(ctx, `UPDATE projects SET owner_id = ? WHERE id = ? AND owner_id = ?`, owner, project, id); err != nil {
				return err
			}
		}
		for _, q := range []string{
			`DELETE FROM sessions WHERE user_id = ?`,
			`DELETE FROM api_tokens WHERE user_id = ?`,
			`DELETE FROM user_totp WHERE user_id = ?`,
			`DELETE FROM recovery_codes WHERE user_id = ?`,
			`DELETE FROM webauthn_credentials WHERE user_id = ?`,
			`DELETE FROM auth_challenges WHERE user_id = ?`,
			`DELETE FROM oauth_grants WHERE user_id = ?`,
			`DELETE FROM user_identities WHERE user_id = ?`,
			`DELETE FROM account_links WHERE user_id = ?`,
			`DELETE FROM uploads WHERE user_id = ?`,
			`DELETE FROM org_members WHERE user_id = ?`,
			`DELETE FROM group_members WHERE user_id = ?`,
			`DELETE FROM project_members WHERE user_id = ?`,
			`DELETE FROM document_grants WHERE user_id = ?`,
			`DELETE FROM favorites WHERE user_id = ?`,
			`DELETE FROM notifications WHERE user_id = ?`,
			`DELETE FROM notification_settings WHERE user_id = ?`,
			`DELETE FROM push_subscriptions WHERE user_id = ?`,
			`DELETE FROM recent_views WHERE user_id = ?`,
			`DELETE FROM user_prefs WHERE user_id = ?`,
			`DELETE FROM control_grants WHERE user_id = ?`,
			`DELETE FROM signing_keys WHERE user_id = ?`,
			`DELETE FROM secret_links WHERE created_by = ?`,
			`DELETE FROM user_exports WHERE user_id = ?`,
			`DELETE FROM images WHERE kind = 'user' AND owner_id = ?`,
			`UPDATE tasks SET assignee_id = NULL WHERE assignee_id = ?`,
			`UPDATE entity_meta SET owner_id = NULL WHERE owner_id = ?`,
			`UPDATE maintenance_plans SET assignee = '' WHERE assignee = ?`,
		} {
			if _, err := tx.Exec(ctx, q, id); err != nil {
				return err
			}
		}
		_, err := tx.Exec(ctx, `UPDATE users SET email = ?, name = ?, password_hash = '', is_admin = ?, locale = '', avatar = '',
			disabled_at = ?, deleted_at = ? WHERE id = ?`, a.Email, a.Name, false, a.At, a.At, id)
		return err
	})
}

// SessionCreated returns when a session started (ErrNotFound if unknown).
func (d *DB) SessionCreated(ctx context.Context, idHash string) (Time, error) {
	var t Time
	err := d.QueryRow(ctx, `SELECT created_at FROM sessions WHERE id = ?`, idHash).Scan(&t)
	if errors.Is(err, sql.ErrNoRows) {
		return t, ErrNotFound
	}
	return t, err
}
