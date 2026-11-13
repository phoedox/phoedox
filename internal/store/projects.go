package store

import (
	"context"
	"database/sql"
	"errors"
)

// Roles in a project, ordered by power.
const (
	RoleReader = "reader"
	RoleEditor = "editor"
	RoleOwner  = "owner"
)

// Project groups documents and entities.
type Project struct {
	ID           string `json:"id"`
	Slug         string `json:"slug"`
	Name         string `json:"name"`
	Short        string `json:"short"`
	Description  string `json:"description"`
	PrimaryColor string `json:"primaryColor"`
	AIWriteMode  string `json:"aiWriteMode"` // suggest | direct
	OwnerID      string `json:"ownerId"`
	CreatedAt    Time   `json:"createdAt"`
	// OrgID is the organization (empty = personal project).
	OrgID   string `json:"orgId,omitempty"`
	OrgName string `json:"orgName,omitempty"`
	// OrgAI is false when the organization switched off AI access.
	OrgAI bool `json:"-"`
	// Role of the requesting user: owner, editor, reader, or "doc" when only
	// single documents are shared with them.
	Role string `json:"role,omitempty"`
}

// RoleDoc means access to single shared documents only.
const RoleDoc = "doc"

const projectCols = `p.id, p.slug, p.name, p.short, p.description, p.primary_color, p.ai_write_mode, p.owner_id, p.created_at,
	COALESCE(p.org_id, ''), COALESCE(o.name, ''), COALESCE(o.ai_enabled, ` + "true" + `)`

const projectFrom = ` FROM projects p LEFT JOIN organizations o ON o.id = p.org_id`

var rankRoles = map[int]string{1: RoleDoc, 2: RoleReader, 3: RoleEditor, 4: RoleOwner}

// rankExpr computes the strongest role of a user in project p as a number:
// direct membership (unless expired), group grants, organization admin
// (owner) and shared documents ("doc"). Arguments: see rankArgs.
func (d *DB) rankExpr() string {
	greatest := "MAX"
	if d.Dialect == Postgres {
		greatest = "GREATEST"
	}
	rank := func(col string) string {
		return `CASE ` + col + ` WHEN 'owner' THEN 4 WHEN 'editor' THEN 3 WHEN 'reader' THEN 2 ELSE 0 END`
	}
	return greatest + `(
		COALESCE((SELECT ` + rank("m.role") + ` FROM project_members m WHERE m.project_id = p.id AND m.user_id = ? AND (m.expires_at IS NULL OR m.expires_at > ?)), 0),
		COALESCE((SELECT MAX(` + rank("pg.role") + `) FROM project_groups pg JOIN group_members gm ON gm.group_id = pg.group_id WHERE pg.project_id = p.id AND gm.user_id = ?), 0),
		CASE WHEN EXISTS (SELECT 1 FROM org_members om WHERE om.org_id = p.org_id AND om.user_id = ? AND om.role = 'admin') THEN 4 ELSE 0 END,
		CASE WHEN EXISTS (SELECT 1 FROM document_grants dg JOIN documents dd ON dd.id = dg.document_id
			WHERE dd.project_id = p.id AND dd.deleted_at IS NULL
			AND (dg.user_id = ? OR dg.group_id IN (SELECT group_id FROM group_members WHERE user_id = ?))) THEN 1 ELSE 0 END)`
}

func rankArgs(userID string) []any { return []any{userID, Now(), userID, userID, userID, userID} }

func scanProject(r interface{ Scan(...any) error }, withRole bool) (Project, error) {
	var p Project
	dest := []any{&p.ID, &p.Slug, &p.Name, &p.Short, &p.Description, &p.PrimaryColor, &p.AIWriteMode, &p.OwnerID, &p.CreatedAt, &p.OrgID, &p.OrgName, &p.OrgAI}
	var rank int
	if withRole {
		dest = append(dest, &rank)
	}
	err := r.Scan(dest...)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	p.Role = rankRoles[rank]
	return p, err
}

// CreateProject inserts the project and makes the owner a member.
func (d *DB) CreateProject(ctx context.Context, p Project) (Project, error) {
	p.CreatedAt = Now()
	err := d.InTx(ctx, func(tx *Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO projects (id, tenant_id, owner_id, org_id, slug, name, short, description, primary_color, ai_write_mode, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, p.ID, DefaultTenant, p.OwnerID, nullable(p.OrgID), p.Slug, p.Name, p.Short, p.Description, p.PrimaryColor, p.AIWriteMode, p.CreatedAt); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO project_members (project_id, user_id, role, can_control) VALUES (?, ?, ?, ?)`, p.ID, p.OwnerID, RoleOwner, true)
		return err
	})
	p.Role = RoleOwner
	return p, err
}

// ProjectFor returns a project by id or slug together with the user's
// effective role (empty if the user has no access).
func (d *DB) ProjectFor(ctx context.Context, ref, userID string) (Project, error) {
	args := append(rankArgs(userID), DefaultTenant, ref, ref)
	return scanProject(d.QueryRow(ctx, `SELECT `+projectCols+`, `+d.rankExpr()+projectFrom+`
		WHERE p.tenant_id = ? AND (p.id = ? OR p.slug = ?) AND p.deleted_at IS NULL`, args...), true)
}

// ListProjects returns the projects a user can see (all for admins).
func (d *DB) ListProjects(ctx context.Context, userID string, all bool) ([]Project, error) {
	args := append(rankArgs(userID), DefaultTenant)
	query := `SELECT ` + projectCols + `, ` + d.rankExpr() + projectFrom + ` WHERE p.tenant_id = ? AND p.deleted_at IS NULL`
	if !all {
		query += ` AND ` + d.rankExpr() + ` > 0`
		args = append(args, rankArgs(userID)...)
	}
	rows, err := d.Query(ctx, query+` ORDER BY p.name`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Project
	for rows.Next() {
		p, err := scanProject(rows, true)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// UpdateProject updates the editable fields.
func (d *DB) UpdateProject(ctx context.Context, p Project) error {
	res, err := d.Exec(ctx, `UPDATE projects SET name = ?, short = ?, description = ?, primary_color = ?, ai_write_mode = ?, org_id = ? WHERE id = ?`,
		p.Name, p.Short, p.Description, p.PrimaryColor, p.AIWriteMode, nullable(p.OrgID), p.ID)
	return affected(res, err)
}

// Member of a project. ExpiresAt is set for guests.
type Member struct {
	UserID    string `json:"userId"`
	Email     string `json:"email"`
	Name      string `json:"name"`
	Role      string `json:"role"`
	ExpiresAt Time   `json:"expiresAt"`
}

// SetMember adds or changes a member; a non-zero expires makes a guest.
func (d *DB) SetMember(ctx context.Context, projectID, userID, role string, expires Time) error {
	var exp any
	if !expires.IsZero() {
		exp = expires
	}
	_, err := d.Exec(ctx, `INSERT INTO project_members (project_id, user_id, role, can_control, expires_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (project_id, user_id) DO UPDATE SET role = excluded.role, expires_at = excluded.expires_at`, projectID, userID, role, false, exp)
	return err
}

// RemoveMember removes a direct member.
func (d *DB) RemoveMember(ctx context.Context, projectID, userID string) error {
	res, err := d.Exec(ctx, `DELETE FROM project_members WHERE project_id = ? AND user_id = ?`, projectID, userID)
	return affected(res, err)
}

// CountOwners counts direct owners that do not expire.
func (d *DB) CountOwners(ctx context.Context, projectID string) (int, error) {
	var n int
	err := d.QueryRow(ctx, `SELECT COUNT(*) FROM project_members WHERE project_id = ? AND role = 'owner' AND expires_at IS NULL`, projectID).Scan(&n)
	return n, err
}

func (d *DB) ListMembers(ctx context.Context, projectID string) ([]Member, error) {
	rows, err := d.Query(ctx, `SELECT u.id, u.email, u.name, m.role, m.expires_at FROM project_members m JOIN users u ON u.id = m.user_id
		WHERE m.project_id = ? ORDER BY u.name`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Member
	for rows.Next() {
		var m Member
		if err := rows.Scan(&m.UserID, &m.Email, &m.Name, &m.Role, &m.ExpiresAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// VisibleProjectIDs returns ids of projects a user can read.
func (d *DB) VisibleProjectIDs(ctx context.Context, userID string, all bool) ([]string, error) {
	ps, err := d.ListProjects(ctx, userID, all)
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(ps))
	for i, p := range ps {
		ids[i] = p.ID
	}
	return ids, nil
}
