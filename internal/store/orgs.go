package store

import (
	"context"
	"database/sql"
	"errors"
)

// Org roles.
const (
	OrgAdmin      = "admin"
	OrgMemberRole = "member"
)

// Org is an organization: projects, members and groups belong to it.
type Org struct {
	ID        string `json:"id"`
	Slug      string `json:"slug"`
	Name      string `json:"name"`
	UsageType string `json:"usageType"` // commercial | noncommercial (set by system admins)
	AIEnabled bool   `json:"aiEnabled"`
	QuotaMB   int    `json:"quotaMb"`
	CreatedAt Time   `json:"createdAt"`
	Role      string `json:"role,omitempty"` // role of the requesting user
}

const orgCols = `o.id, o.slug, o.name, o.usage_type, o.ai_enabled, o.quota_mb, o.created_at`

func scanOrg(r interface{ Scan(...any) error }, withRole bool) (Org, error) {
	var o Org
	var role sql.NullString
	dest := []any{&o.ID, &o.Slug, &o.Name, &o.UsageType, &o.AIEnabled, &o.QuotaMB, &o.CreatedAt}
	if withRole {
		dest = append(dest, &role)
	}
	err := r.Scan(dest...)
	if errors.Is(err, sql.ErrNoRows) {
		return o, ErrNotFound
	}
	o.Role = role.String
	return o, err
}

// CreateOrg creates an organization with its first admin.
func (d *DB) CreateOrg(ctx context.Context, o Org, adminID string) (Org, error) {
	o.CreatedAt = Now()
	err := d.InTx(ctx, func(tx *Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO organizations (id, tenant_id, name, slug, usage_type, ai_enabled, quota_mb, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			o.ID, DefaultTenant, o.Name, o.Slug, o.UsageType, true, 0, o.CreatedAt); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO org_members (org_id, user_id, tenant_id, role, created_at) VALUES (?, ?, ?, 'admin', ?)`, o.ID, adminID, DefaultTenant, o.CreatedAt)
		return err
	})
	o.AIEnabled, o.Role = true, OrgAdmin
	return o, err
}

// OrgFor returns an organization by id or slug with the user's role.
func (d *DB) OrgFor(ctx context.Context, ref, userID string) (Org, error) {
	return scanOrg(d.QueryRow(ctx, `SELECT `+orgCols+`, m.role FROM organizations o
		LEFT JOIN org_members m ON m.org_id = o.id AND m.user_id = ?
		WHERE o.tenant_id = ? AND (o.id = ? OR o.slug = ?)`, userID, DefaultTenant, ref, ref), true)
}

// ListOrgs lists the organizations of a user (all for system admins).
func (d *DB) ListOrgs(ctx context.Context, userID string, all bool) ([]Org, error) {
	q := `SELECT ` + orgCols + `, m.role FROM organizations o LEFT JOIN org_members m ON m.org_id = o.id AND m.user_id = ? WHERE o.tenant_id = ?`
	if !all {
		q += ` AND m.role IS NOT NULL`
	}
	rows, err := d.Query(ctx, q+` ORDER BY o.name`, userID, DefaultTenant)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Org{}
	for rows.Next() {
		o, err := scanOrg(rows, true)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// UpdateOrg stores name, usage type, AI switch and quota.
func (d *DB) UpdateOrg(ctx context.Context, o Org) error {
	res, err := d.Exec(ctx, `UPDATE organizations SET name = ?, usage_type = ?, ai_enabled = ?, quota_mb = ? WHERE id = ?`, o.Name, o.UsageType, o.AIEnabled, o.QuotaMB, o.ID)
	return affected(res, err)
}

// OrgMember is a member of an organization.
type OrgMember struct {
	UserID string `json:"userId"`
	Email  string `json:"email"`
	Name   string `json:"name"`
	Role   string `json:"role"`
}

// OrgMembers lists the members of an organization.
func (d *DB) OrgMembers(ctx context.Context, orgID string) ([]OrgMember, error) {
	rows, err := d.Query(ctx, `SELECT u.id, u.email, u.name, m.role FROM org_members m JOIN users u ON u.id = m.user_id WHERE m.org_id = ? ORDER BY u.name`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []OrgMember{}
	for rows.Next() {
		var m OrgMember
		if err := rows.Scan(&m.UserID, &m.Email, &m.Name, &m.Role); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// SetOrgMember adds or changes a member.
func (d *DB) SetOrgMember(ctx context.Context, orgID, userID, role string) error {
	_, err := d.Exec(ctx, `INSERT INTO org_members (org_id, user_id, tenant_id, role, created_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (org_id, user_id) DO UPDATE SET role = excluded.role`, orgID, userID, DefaultTenant, role, Now())
	return err
}

// RemoveOrgMember removes a member and their group memberships in the organization.
func (d *DB) RemoveOrgMember(ctx context.Context, orgID, userID string) error {
	return d.InTx(ctx, func(tx *Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM group_members WHERE user_id = ? AND group_id IN (SELECT id FROM groups WHERE org_id = ?)`, userID, orgID); err != nil {
			return err
		}
		res, err := tx.Exec(ctx, `DELETE FROM org_members WHERE org_id = ? AND user_id = ?`, orgID, userID)
		return affected(res, err)
	})
}

// CountOrgAdmins counts the admins of an organization.
func (d *DB) CountOrgAdmins(ctx context.Context, orgID string) (int, error) {
	var n int
	err := d.QueryRow(ctx, `SELECT COUNT(*) FROM org_members WHERE org_id = ? AND role = 'admin'`, orgID).Scan(&n)
	return n, err
}

// Group is a set of users of an organization.
type Group struct {
	ID        string      `json:"id"`
	OrgID     string      `json:"orgId"`
	Name      string      `json:"name"`
	OIDCGroup string      `json:"oidcGroup"` // members come from this OIDC group claim value
	Members   []OrgMember `json:"members"`
}

// Groups lists the groups of an organization with members.
func (d *DB) Groups(ctx context.Context, orgID string) ([]Group, error) {
	rows, err := d.Query(ctx, `SELECT id, org_id, name, oidc_group FROM groups WHERE org_id = ? ORDER BY name`, orgID)
	if err != nil {
		return nil, err
	}
	var out []Group
	for rows.Next() {
		var g Group
		if err := rows.Scan(&g.ID, &g.OrgID, &g.Name, &g.OIDCGroup); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, g)
	}
	rows.Close()
	for i := range out {
		mrows, err := d.Query(ctx, `SELECT u.id, u.email, u.name, gm.source FROM group_members gm JOIN users u ON u.id = gm.user_id WHERE gm.group_id = ? ORDER BY u.name`, out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].Members = []OrgMember{}
		for mrows.Next() {
			var m OrgMember
			if err := mrows.Scan(&m.UserID, &m.Email, &m.Name, &m.Role); err != nil {
				mrows.Close()
				return nil, err
			}
			out[i].Members = append(out[i].Members, m)
		}
		mrows.Close()
	}
	if out == nil {
		out = []Group{}
	}
	return out, nil
}

// GetGroup returns a group.
func (d *DB) GetGroup(ctx context.Context, id string) (Group, error) {
	var g Group
	err := d.QueryRow(ctx, `SELECT id, org_id, name, oidc_group FROM groups WHERE id = ?`, id).Scan(&g.ID, &g.OrgID, &g.Name, &g.OIDCGroup)
	if errors.Is(err, sql.ErrNoRows) {
		return g, ErrNotFound
	}
	return g, err
}

// SaveGroup creates or updates a group.
func (d *DB) SaveGroup(ctx context.Context, g Group) error {
	_, err := d.Exec(ctx, `INSERT INTO groups (id, tenant_id, org_id, name, oidc_group, created_at) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET name = excluded.name, oidc_group = excluded.oidc_group`, g.ID, DefaultTenant, g.OrgID, g.Name, g.OIDCGroup, Now())
	return err
}

// DeleteGroup deletes a group (and its grants).
func (d *DB) DeleteGroup(ctx context.Context, id string) error {
	res, err := d.Exec(ctx, `DELETE FROM groups WHERE id = ?`, id)
	return affected(res, err)
}

// SetGroupMember adds (on) or removes a user.
func (d *DB) SetGroupMember(ctx context.Context, groupID, userID, source string, on bool) error {
	if !on {
		_, err := d.Exec(ctx, `DELETE FROM group_members WHERE group_id = ? AND user_id = ?`, groupID, userID)
		return err
	}
	_, err := d.Exec(ctx, `INSERT INTO group_members (group_id, user_id, source) VALUES (?, ?, ?) ON CONFLICT (group_id, user_id) DO NOTHING`, groupID, userID, source)
	return err
}

// SyncOIDCGroups sets the OIDC-managed group memberships of a user: groups
// whose oidc_group is in claims are joined, other OIDC memberships are left.
// Users are added to the organization of a group as members if needed.
func (d *DB) SyncOIDCGroups(ctx context.Context, userID string, claims []string) error {
	return d.InTx(ctx, func(tx *Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM group_members WHERE user_id = ? AND source = 'oidc'`, userID); err != nil {
			return err
		}
		for _, c := range claims {
			if c == "" {
				continue
			}
			rows, err := tx.Query(ctx, `SELECT id, org_id FROM groups WHERE oidc_group = ?`, c)
			if err != nil {
				return err
			}
			var gs [][2]string
			for rows.Next() {
				var id, org string
				if err := rows.Scan(&id, &org); err != nil {
					rows.Close()
					return err
				}
				gs = append(gs, [2]string{id, org})
			}
			rows.Close()
			for _, g := range gs {
				if _, err := tx.Exec(ctx, `INSERT INTO group_members (group_id, user_id, source) VALUES (?, ?, 'oidc') ON CONFLICT (group_id, user_id) DO NOTHING`, g[0], userID); err != nil {
					return err
				}
				if _, err := tx.Exec(ctx, `INSERT INTO org_members (org_id, user_id, tenant_id, role, created_at) VALUES (?, ?, ?, 'member', ?) ON CONFLICT (org_id, user_id) DO NOTHING`,
					g[1], userID, DefaultTenant, Now()); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// ProjectGroup is a group's access to a project.
type ProjectGroup struct {
	GroupID string `json:"groupId"`
	Name    string `json:"name"`
	Role    string `json:"role"`
}

// ProjectGroups lists the groups with access to a project.
func (d *DB) ProjectGroups(ctx context.Context, projectID string) ([]ProjectGroup, error) {
	rows, err := d.Query(ctx, `SELECT g.id, g.name, pg.role FROM project_groups pg JOIN groups g ON g.id = pg.group_id WHERE pg.project_id = ? ORDER BY g.name`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ProjectGroup{}
	for rows.Next() {
		var g ProjectGroup
		if err := rows.Scan(&g.GroupID, &g.Name, &g.Role); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// SetProjectGroup grants a group a role in a project ("" removes it).
func (d *DB) SetProjectGroup(ctx context.Context, projectID, groupID, role string) error {
	if role == "" {
		_, err := d.Exec(ctx, `DELETE FROM project_groups WHERE project_id = ? AND group_id = ?`, projectID, groupID)
		return err
	}
	_, err := d.Exec(ctx, `INSERT INTO project_groups (project_id, group_id, role) VALUES (?, ?, ?)
		ON CONFLICT (project_id, group_id) DO UPDATE SET role = excluded.role`, projectID, groupID, role)
	return err
}

// DocGrant shares a document with a user or a group.
type DocGrant struct {
	ID        string `json:"id"`
	UserID    string `json:"userId,omitempty"`
	GroupID   string `json:"groupId,omitempty"`
	Name      string `json:"name"`            // user or group name
	Email     string `json:"email,omitempty"` // users only
	Role      string `json:"role"`
	CreatedAt Time   `json:"createdAt"`
}

// DocGrants lists the grants of a document.
func (d *DB) DocGrants(ctx context.Context, docID string) ([]DocGrant, error) {
	rows, err := d.Query(ctx, `SELECT dg.id, COALESCE(dg.user_id, ''), COALESCE(dg.group_id, ''), COALESCE(u.name, g.name, ''), COALESCE(u.email, ''), dg.role, dg.created_at
		FROM document_grants dg LEFT JOIN users u ON u.id = dg.user_id LEFT JOIN groups g ON g.id = dg.group_id
		WHERE dg.document_id = ? ORDER BY 4`, docID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DocGrant{}
	for rows.Next() {
		var g DocGrant
		if err := rows.Scan(&g.ID, &g.UserID, &g.GroupID, &g.Name, &g.Email, &g.Role, &g.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// SetDocGrant shares a document with a user or group (replacing an earlier role).
func (d *DB) SetDocGrant(ctx context.Context, id, docID, userID, groupID, role string) error {
	return d.InTx(ctx, func(tx *Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM document_grants WHERE document_id = ? AND (user_id = ? OR group_id = ?)`, docID, userID, groupID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO document_grants (id, tenant_id, document_id, user_id, group_id, role, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			id, DefaultTenant, docID, nullable(userID), nullable(groupID), role, Now())
		return err
	})
}

// DeleteDocGrant removes a grant of a document.
func (d *DB) DeleteDocGrant(ctx context.Context, docID, grantID string) error {
	res, err := d.Exec(ctx, `DELETE FROM document_grants WHERE id = ? AND document_id = ?`, grantID, docID)
	return affected(res, err)
}

// SetRestricted makes a document visible only to owners and grants (on) or to the whole project.
func (d *DB) SetRestricted(ctx context.Context, docID string, on bool) error {
	res, err := d.Exec(ctx, `UPDATE documents SET restricted = ? WHERE id = ?`, on, docID)
	return affected(res, err)
}

// DocRoles returns, for documents of a project, the strongest role granted
// to the user directly or through groups (docID -> role).
func (d *DB) DocRoles(ctx context.Context, projectID, userID string) (map[string]string, error) {
	rows, err := d.Query(ctx, `SELECT dg.document_id, dg.role FROM document_grants dg JOIN documents dd ON dd.id = dg.document_id
		WHERE dd.project_id = ? AND (dg.user_id = ? OR dg.group_id IN (SELECT group_id FROM group_members WHERE user_id = ?))`, projectID, userID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id, role string
		if err := rows.Scan(&id, &role); err != nil {
			return nil, err
		}
		if out[id] != RoleEditor {
			out[id] = role
		}
	}
	return out, rows.Err()
}

// RestrictedDocs returns the ids of restricted documents in the given projects.
func (d *DB) RestrictedDocs(ctx context.Context, projectIDs []string) (map[string]bool, error) {
	out := map[string]bool{}
	if len(projectIDs) == 0 {
		return out, nil
	}
	args := make([]any, len(projectIDs))
	for i, id := range projectIDs {
		args[i] = id
	}
	rows, err := d.Query(ctx, `SELECT id FROM documents WHERE restricted = `+d.boolTrue()+` AND project_id IN (`+placeholders(len(args))+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

func (d *DB) boolTrue() string {
	if d.Dialect == Postgres {
		return "true"
	}
	return "1"
}


// ShareLink gives read access without an account.
type ShareLink struct {
	ID         string `json:"id"`
	ProjectID  string `json:"projectId"`
	DocumentID string `json:"documentId,omitempty"`
	Label      string `json:"label"`
	CreatedBy  string `json:"createdBy"`
	CreatedAt  Time   `json:"createdAt"`
	ExpiresAt  Time   `json:"expiresAt"`
	RevokedAt  Time   `json:"revokedAt"`
	LastUsedAt Time   `json:"lastUsedAt"`
}

const shareCols = `id, project_id, COALESCE(document_id, ''), label, created_by, created_at, expires_at, revoked_at, last_used_at`

func scanShare(r interface{ Scan(...any) error }) (ShareLink, error) {
	var l ShareLink
	err := r.Scan(&l.ID, &l.ProjectID, &l.DocumentID, &l.Label, &l.CreatedBy, &l.CreatedAt, &l.ExpiresAt, &l.RevokedAt, &l.LastUsedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return l, ErrNotFound
	}
	return l, err
}

// CreateShareLink stores a link by the hash of its token.
func (d *DB) CreateShareLink(ctx context.Context, l ShareLink, tokenHash string) (ShareLink, error) {
	l.CreatedAt = Now()
	var exp any
	if !l.ExpiresAt.IsZero() {
		exp = l.ExpiresAt
	}
	_, err := d.Exec(ctx, `INSERT INTO share_links (id, tenant_id, project_id, document_id, token_hash, label, created_by, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		l.ID, DefaultTenant, l.ProjectID, nullable(l.DocumentID), tokenHash, l.Label, l.CreatedBy, l.CreatedAt, exp)
	return l, err
}

// ShareLinks lists the links of a project that are not revoked.
func (d *DB) ShareLinks(ctx context.Context, projectID string) ([]ShareLink, error) {
	rows, err := d.Query(ctx, `SELECT `+shareCols+` FROM share_links WHERE project_id = ? AND revoked_at IS NULL ORDER BY created_at DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ShareLink{}
	for rows.Next() {
		l, err := scanShare(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// ShareLinkByHash returns a valid (not revoked, not expired) link and marks it used.
func (d *DB) ShareLinkByHash(ctx context.Context, hash string) (ShareLink, error) {
	now := Now()
	l, err := scanShare(d.QueryRow(ctx, `SELECT `+shareCols+` FROM share_links WHERE token_hash = ? AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > ?)`, hash, now))
	if err == nil {
		_, _ = d.Exec(ctx, `UPDATE share_links SET last_used_at = ? WHERE id = ?`, now, l.ID)
	}
	return l, err
}

// RevokeShareLink ends a link.
func (d *DB) RevokeShareLink(ctx context.Context, projectID, id string) error {
	res, err := d.Exec(ctx, `UPDATE share_links SET revoked_at = ? WHERE id = ? AND project_id = ? AND revoked_at IS NULL`, Now(), id, projectID)
	return affected(res, err)
}
