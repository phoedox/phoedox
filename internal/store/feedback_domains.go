package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// SetFeedbackPortalDomain sets (or clears) a portal's own domain; it is
// unverified until VerifyFeedbackPortalDomain.
func (d *DB) SetFeedbackPortalDomain(ctx context.Context, id, domain, token string) error {
	_, err := d.Exec(ctx, `UPDATE feedback_portals SET domain = ?, domain_token = ?, domain_verified_at = NULL, updated_at = ? WHERE id = ?`,
		strings.ToLower(domain), token, Now(), id)
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "unique") {
		return ErrConflict
	}
	return err
}

// VerifyFeedbackPortalDomain marks the domain as verified.
func (d *DB) VerifyFeedbackPortalDomain(ctx context.Context, id string) error {
	_, err := d.Exec(ctx, `UPDATE feedback_portals SET domain_verified_at = ? WHERE id = ?`, Now(), id)
	return err
}

// FeedbackPortalByDomain finds the portal served on a verified own domain
// (any tenant) and returns its handle and tenant.
func (d *DB) FeedbackPortalByDomain(ctx context.Context, host string) (handle, tenantID string, err error) {
	err = d.QueryRow(System(ctx), `SELECT handle, tenant_id FROM feedback_portals WHERE domain = ? AND domain_verified_at IS NOT NULL AND enabled = ?`,
		strings.ToLower(host), true).Scan(&handle, &tenantID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", ErrNotFound
	}
	return handle, tenantID, err
}

// PortalDomainTaken tells whether a domain is used by a publication, a
// tenant, an organization or another portal.
func (d *DB) PortalDomainTaken(ctx context.Context, domain, exceptPortal string) (bool, error) {
	var n int
	domain = strings.ToLower(domain)
	err := d.QueryRow(System(ctx), `SELECT
		(SELECT COUNT(*) FROM publications WHERE domain = ?) +
		(SELECT COUNT(*) FROM tenant_domains WHERE domain = ?) +
		(SELECT COUNT(*) FROM org_domains WHERE domain = ?) +
		(SELECT COUNT(*) FROM feedback_portals WHERE domain = ? AND id <> ?)`, domain, domain, domain, domain, exceptPortal).Scan(&n)
	return n > 0, err
}
