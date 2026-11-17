package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// ProviderLink is access a customer organization grants a service
// provider organization (ADR-0073).
type ProviderLink struct {
	ID            string   `json:"id"`
	CustomerOrgID string   `json:"customerOrgId"`
	CustomerName  string   `json:"customerName"`
	CustomerSlug  string   `json:"customerSlug"`
	ProviderOrgID string   `json:"providerOrgId"`
	ProviderName  string   `json:"providerName"`
	ProviderSlug  string   `json:"providerSlug"`
	Role          string   `json:"role"`     // reader | editor | admin
	Projects      []string `json:"projects"` // project ids; empty = all
	ExpiresAt     *Time    `json:"expiresAt,omitempty"`
	Note          string   `json:"note,omitempty"`
	CreatedBy     string   `json:"createdBy"`
	CreatedAt     Time     `json:"createdAt"`
	RevokedAt     *Time    `json:"revokedAt,omitempty"`
}

const providerCols = `pl.id, pl.customer_org_id, c.name, c.slug, pl.provider_org_id, p.name, p.slug, pl.role, pl.projects, pl.expires_at, pl.note, pl.created_by, pl.created_at, pl.revoked_at`

const providerFrom = ` FROM provider_links pl JOIN organizations c ON c.id = pl.customer_org_id JOIN organizations p ON p.id = pl.provider_org_id`

func scanProviderLink(r interface{ Scan(...any) error }) (ProviderLink, error) {
	var l ProviderLink
	var projects string
	var exp, rev sql.Null[Time]
	err := r.Scan(&l.ID, &l.CustomerOrgID, &l.CustomerName, &l.CustomerSlug, &l.ProviderOrgID, &l.ProviderName, &l.ProviderSlug, &l.Role, &projects, &exp, &l.Note, &l.CreatedBy, &l.CreatedAt, &rev)
	if errors.Is(err, sql.ErrNoRows) {
		return l, ErrNotFound
	}
	l.Projects = splitList(projects)
	if l.Projects == nil {
		l.Projects = []string{}
	}
	if exp.Valid {
		l.ExpiresAt = &exp.V
	}
	if rev.Valid {
		l.RevokedAt = &rev.V
	}
	return l, err
}

// SaveProviderLink creates or replaces the link between two organizations
// (a revoked link becomes active again).
func (d *DB) SaveProviderLink(ctx context.Context, l ProviderLink) error {
	var exp any
	if l.ExpiresAt != nil {
		exp = *l.ExpiresAt
	}
	_, err := d.Exec(ctx, `INSERT INTO provider_links (id, tenant_id, customer_org_id, provider_org_id, role, projects, expires_at, note, created_by, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (customer_org_id, provider_org_id) DO UPDATE SET role = excluded.role, projects = excluded.projects, expires_at = excluded.expires_at,
		  note = excluded.note, created_by = excluded.created_by, created_at = excluded.created_at, revoked_at = NULL`,
		l.ID, TenantOf(ctx), l.CustomerOrgID, l.ProviderOrgID, l.Role, strings.Join(l.Projects, ","), exp, l.Note, l.CreatedBy, l.CreatedAt)
	return err
}

// ProviderLinks lists the links of an organization as customer or as
// provider (revoked ones too, newest first).
func (d *DB) ProviderLinks(ctx context.Context, customerOrgID, providerOrgID string) ([]ProviderLink, error) {
	q, args := `SELECT `+providerCols+providerFrom+` WHERE 1 = 1`, []any{}
	if customerOrgID != "" {
		q += ` AND pl.customer_org_id = ?`
		args = append(args, customerOrgID)
	}
	if providerOrgID != "" {
		q += ` AND pl.provider_org_id = ?`
		args = append(args, providerOrgID)
	}
	rows, err := d.Query(ctx, q+` ORDER BY pl.created_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ProviderLink{}
	for rows.Next() {
		l, err := scanProviderLink(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// GetProviderLink reads one link.
func (d *DB) GetProviderLink(ctx context.Context, id string) (ProviderLink, error) {
	return scanProviderLink(d.QueryRow(ctx, `SELECT `+providerCols+providerFrom+` WHERE pl.id = ?`, id))
}

// RevokeProviderLink ends a link.
func (d *DB) RevokeProviderLink(ctx context.Context, id string) error {
	res, err := d.Exec(ctx, `UPDATE provider_links SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`, Now(), id)
	return affected(res, err)
}

// ProviderVia names the provider organization (slug) through which a user
// works in a project, or "" when the user has own access (member of the
// customer organization or of the project).
func (d *DB) ProviderVia(ctx context.Context, projectID, userID string) string {
	var slug string
	err := d.QueryRow(ctx, `SELECT o.slug FROM projects p
		JOIN provider_links pl ON pl.customer_org_id = p.org_id AND pl.revoked_at IS NULL
		JOIN org_members pm ON pm.org_id = pl.provider_org_id AND pm.user_id = ?
		JOIN organizations o ON o.id = pl.provider_org_id
		WHERE p.id = ? AND p.org_id <> ''
		AND NOT EXISTS (SELECT 1 FROM org_members cm WHERE cm.org_id = p.org_id AND cm.user_id = ?)
		AND NOT EXISTS (SELECT 1 FROM project_members m WHERE m.project_id = p.id AND m.user_id = ?)
		LIMIT 1`, userID, projectID, userID, userID).Scan(&slug)
	if err != nil {
		return ""
	}
	return slug
}
