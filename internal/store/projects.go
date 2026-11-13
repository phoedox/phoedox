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
	Role         string `json:"role,omitempty"` // role of the requesting user
}

const projectCols = `p.id, p.slug, p.name, p.short, p.description, p.primary_color, p.ai_write_mode, p.owner_id, p.created_at`

func scanProject(r interface{ Scan(...any) error }, withRole bool) (Project, error) {
	var p Project
	dest := []any{&p.ID, &p.Slug, &p.Name, &p.Short, &p.Description, &p.PrimaryColor, &p.AIWriteMode, &p.OwnerID, &p.CreatedAt}
	var role sql.NullString
	if withRole {
		dest = append(dest, &role)
	}
	err := r.Scan(dest...)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	p.Role = role.String
	return p, err
}

// CreateProject inserts the project and makes the owner a member.
func (d *DB) CreateProject(ctx context.Context, p Project) (Project, error) {
	p.CreatedAt = Now()
	err := d.InTx(ctx, func(tx *Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO projects (id, tenant_id, owner_id, slug, name, short, description, primary_color, ai_write_mode, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, p.ID, DefaultTenant, p.OwnerID, p.Slug, p.Name, p.Short, p.Description, p.PrimaryColor, p.AIWriteMode, p.CreatedAt); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO project_members (project_id, user_id, role, can_control) VALUES (?, ?, ?, ?)`, p.ID, p.OwnerID, RoleOwner, true)
		return err
	})
	p.Role = RoleOwner
	return p, err
}

// ProjectFor returns a project by id or slug together with the user's role
// (empty if the user is not a member).
func (d *DB) ProjectFor(ctx context.Context, ref, userID string) (Project, error) {
	return scanProject(d.QueryRow(ctx, `SELECT `+projectCols+`, m.role FROM projects p
		LEFT JOIN project_members m ON m.project_id = p.id AND m.user_id = ?
		WHERE p.tenant_id = ? AND (p.id = ? OR p.slug = ?) AND p.deleted_at IS NULL`, userID, DefaultTenant, ref, ref), true)
}

// ListProjects returns the projects a user is member of (all for admins).
func (d *DB) ListProjects(ctx context.Context, userID string, all bool) ([]Project, error) {
	query := `SELECT ` + projectCols + `, m.role FROM projects p
		LEFT JOIN project_members m ON m.project_id = p.id AND m.user_id = ?
		WHERE p.tenant_id = ? AND p.deleted_at IS NULL`
	if !all {
		query += ` AND m.role IS NOT NULL`
	}
	rows, err := d.Query(ctx, query+` ORDER BY p.name`, userID, DefaultTenant)
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
	res, err := d.Exec(ctx, `UPDATE projects SET name = ?, short = ?, description = ?, primary_color = ?, ai_write_mode = ? WHERE id = ?`,
		p.Name, p.Short, p.Description, p.PrimaryColor, p.AIWriteMode, p.ID)
	return affected(res, err)
}

// Member of a project.
type Member struct {
	UserID string `json:"userId"`
	Email  string `json:"email"`
	Name   string `json:"name"`
	Role   string `json:"role"`
}

func (d *DB) SetMember(ctx context.Context, projectID, userID, role string) error {
	_, err := d.Exec(ctx, `INSERT INTO project_members (project_id, user_id, role, can_control) VALUES (?, ?, ?, ?)
		ON CONFLICT (project_id, user_id) DO UPDATE SET role = excluded.role`, projectID, userID, role, false)
	return err
}

func (d *DB) ListMembers(ctx context.Context, projectID string) ([]Member, error) {
	rows, err := d.Query(ctx, `SELECT u.id, u.email, u.name, m.role FROM project_members m JOIN users u ON u.id = m.user_id
		WHERE m.project_id = ? ORDER BY u.name`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Member
	for rows.Next() {
		var m Member
		if err := rows.Scan(&m.UserID, &m.Email, &m.Name, &m.Role); err != nil {
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
