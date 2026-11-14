package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// OrgDomain is an organization's own address for the app.
type OrgDomain struct {
	OrgID      string `json:"-"`
	TenantID   string `json:"-"`
	Domain     string `json:"domain"`
	Token      string `json:"token"`
	VerifiedAt Time   `json:"verifiedAt"`
	CreatedAt  Time   `json:"createdAt"`
}

const orgDomainCols = `org_id, tenant_id, domain, token, verified_at, created_at`

func scanOrgDomain(r interface{ Scan(...any) error }) (OrgDomain, error) {
	var d OrgDomain
	err := r.Scan(&d.OrgID, &d.TenantID, &d.Domain, &d.Token, &d.VerifiedAt, &d.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return d, ErrNotFound
	}
	return d, err
}

// SetOrgDomain replaces an organization's domain (unverified).
func (d *DB) SetOrgDomain(ctx context.Context, orgID, domain, token, by string) error {
	return d.InTx(ctx, func(tx *Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM org_domains WHERE org_id = ?`, orgID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO org_domains (org_id, tenant_id, domain, token, created_by, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
			orgID, TenantOf(ctx), strings.ToLower(domain), token, by, Now())
		return err
	})
}

// GetOrgDomain returns an organization's domain.
func (d *DB) GetOrgDomain(ctx context.Context, orgID string) (OrgDomain, error) {
	return scanOrgDomain(d.QueryRow(ctx, `SELECT `+orgDomainCols+` FROM org_domains WHERE org_id = ?`, orgID))
}

// VerifyOrgDomain marks the domain as verified.
func (d *DB) VerifyOrgDomain(ctx context.Context, orgID string) error {
	res, err := d.Exec(ctx, `UPDATE org_domains SET verified_at = ? WHERE org_id = ?`, Now(), orgID)
	return affected(res, err)
}

// DeleteOrgDomain removes an organization's domain.
func (d *DB) DeleteOrgDomain(ctx context.Context, orgID string) error {
	_, err := d.Exec(ctx, `DELETE FROM org_domains WHERE org_id = ?`, orgID)
	return err
}

// OrgByDomain finds the organization of a verified domain (all tenants).
func (d *DB) OrgByDomain(ctx context.Context, host string) (OrgDomain, error) {
	return scanOrgDomain(d.QueryRow(System(ctx), `SELECT `+orgDomainCols+` FROM org_domains WHERE domain = ? AND verified_at IS NOT NULL`, strings.ToLower(host)))
}

// DomainTaken tells whether a domain is used by a publication, a tenant or
// another organization.
func (d *DB) DomainTaken(ctx context.Context, domain, exceptOrg string) (bool, error) {
	var n int
	domain = strings.ToLower(domain)
	err := d.QueryRow(System(ctx), `SELECT
		(SELECT COUNT(*) FROM publications WHERE domain = ?) +
		(SELECT COUNT(*) FROM tenant_domains WHERE domain = ?) +
		(SELECT COUNT(*) FROM org_domains WHERE domain = ? AND org_id <> ?)`, domain, domain, domain, exceptOrg).Scan(&n)
	return n > 0, err
}
