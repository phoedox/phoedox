package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// Publication is a site made of one or more projects (ADR-0006).
type Publication struct {
	ID           string   `json:"id"`
	TenantID     string   `json:"-"`
	Slug         string   `json:"slug"`
	Title        string   `json:"title"`
	Description  string   `json:"description"`
	Projects     []string `json:"projects"` // project ids in menu order
	Access       string   `json:"access"`   // public | password
	PasswordHash string   `json:"-"`
	Domain       string   `json:"domain"`
	Color        string   `json:"color"`
	CSS          string   `json:"css"`
	NoIndex      bool     `json:"noindex"`
	CreatedBy    string   `json:"createdBy"`
	CreatedAt    Time     `json:"createdAt"`
	UpdatedAt    Time     `json:"updatedAt"`
}

const pubCols = `id, tenant_id, slug, title, description, projects, access, password_hash, domain, color, css, noindex, created_by, created_at, updated_at`

func scanPublication(r interface{ Scan(...any) error }) (Publication, error) {
	var p Publication
	var projects string
	err := r.Scan(&p.ID, &p.TenantID, &p.Slug, &p.Title, &p.Description, &projects, &p.Access, &p.PasswordHash, &p.Domain, &p.Color, &p.CSS, &p.NoIndex, &p.CreatedBy, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	p.Projects = splitList(projects)
	return p, err
}

// SavePublication inserts or updates a publication.
func (d *DB) SavePublication(ctx context.Context, p Publication) error {
	_, err := d.Exec(ctx, `INSERT INTO publications (`+pubCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET slug = excluded.slug, title = excluded.title, description = excluded.description, projects = excluded.projects,
		  access = excluded.access, password_hash = excluded.password_hash, domain = excluded.domain, color = excluded.color, css = excluded.css,
		  noindex = excluded.noindex, updated_at = excluded.updated_at`,
		p.ID, TenantOf(ctx), p.Slug, p.Title, p.Description, strings.Join(p.Projects, ","), p.Access, p.PasswordHash, p.Domain, p.Color, p.CSS, p.NoIndex,
		p.CreatedBy, p.CreatedAt, p.UpdatedAt)
	if err != nil && isUniqueViolation(err) {
		return ErrConflict
	}
	return err
}

// GetPublication returns a publication by id or slug.
func (d *DB) GetPublication(ctx context.Context, ref string) (Publication, error) {
	return scanPublication(d.QueryRow(ctx, `SELECT `+pubCols+` FROM publications WHERE tenant_id = ? AND (id = ? OR slug = ?)`, TenantOf(ctx), ref, ref))
}

// ListPublications returns all publications of the tenant.
func (d *DB) ListPublications(ctx context.Context) ([]Publication, error) {
	rows, err := d.Query(ctx, `SELECT `+pubCols+` FROM publications WHERE tenant_id = ? ORDER BY title`, TenantOf(ctx))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Publication{}
	for rows.Next() {
		p, err := scanPublication(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// PublicationByDomain finds the publication for a host across all tenants.
func (d *DB) PublicationByDomain(ctx context.Context, host string) (Publication, error) {
	return scanPublication(d.QueryRow(System(ctx), `SELECT `+pubCols+` FROM publications WHERE domain = ?`, strings.ToLower(host)))
}

// DeletePublication removes a publication.
func (d *DB) DeletePublication(ctx context.Context, id string) error {
	res, err := d.Exec(ctx, `DELETE FROM publications WHERE tenant_id = ? AND id = ?`, TenantOf(ctx), id)
	return affected(res, err)
}
