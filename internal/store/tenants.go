package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// Tenant is a customer of a hosted installation with its own domains.
type Tenant struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Domains []string `json:"domains"`
}

// CreateTenant adds a tenant with its domains (system context only).
func (d *DB) CreateTenant(ctx context.Context, t Tenant) error {
	return d.InTx(System(ctx), func(tx *Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO tenants (id, name, created_at) VALUES (?, ?, ?)`, t.ID, t.Name, Now()); err != nil {
			return err
		}
		for _, dom := range t.Domains {
			if _, err := tx.Exec(ctx, `INSERT INTO tenant_domains (domain, tenant_id) VALUES (?, ?)`, strings.ToLower(dom), t.ID); err != nil {
				return err
			}
		}
		return nil
	})
}

// AddTenantDomain adds a domain to a tenant.
func (d *DB) AddTenantDomain(ctx context.Context, tenantID, domain string) error {
	_, err := d.Exec(System(ctx), `INSERT INTO tenant_domains (domain, tenant_id) VALUES (?, ?)`, strings.ToLower(domain), tenantID)
	return err
}

// TenantByDomain finds the tenant serving a host name.
func (d *DB) TenantByDomain(ctx context.Context, domain string) (string, error) {
	var id string
	err := d.QueryRow(System(ctx), `SELECT tenant_id FROM tenant_domains WHERE domain = ?`, strings.ToLower(domain)).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		// Verified app domains of organizations belong to their tenant.
		if od, oerr := d.OrgByDomain(ctx, domain); oerr == nil {
			return od.TenantID, nil
		}
		return "", ErrNotFound
	}
	return id, err
}

// Tenants lists all tenants with their domains.
func (d *DB) Tenants(ctx context.Context) ([]Tenant, error) {
	rows, err := d.Query(System(ctx), `SELECT t.id, t.name, COALESCE(td.domain, '') FROM tenants t LEFT JOIN tenant_domains td ON td.tenant_id = t.id ORDER BY t.id, td.domain`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Tenant
	for rows.Next() {
		var id, name, dom string
		if err := rows.Scan(&id, &name, &dom); err != nil {
			return nil, err
		}
		if len(out) == 0 || out[len(out)-1].ID != id {
			out = append(out, Tenant{ID: id, Name: name, Domains: []string{}})
		}
		if dom != "" {
			out[len(out)-1].Domains = append(out[len(out)-1].Domains, dom)
		}
	}
	return out, rows.Err()
}

// RLSBypassed reports whether the database user ignores row-level security
// (superuser or BYPASSRLS); the hosted mode refuses to run then.
func (d *DB) RLSBypassed(ctx context.Context) (bool, error) {
	if d.Dialect != Postgres {
		return true, nil
	}
	var super, bypass bool
	err := d.SQL.QueryRowContext(ctx, `SELECT rolsuper, rolbypassrls FROM pg_roles WHERE rolname = current_user`).Scan(&super, &bypass)
	return super || bypass, err
}
