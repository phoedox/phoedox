package store

import (
	"context"
	"database/sql"
	"errors"
)

// Entitlement is the right of an organization or user to a paid feature
// (ADR-0061). Subject is "org:<id>" or "user:<id>".
type Entitlement struct {
	Subject   string `json:"subject"`
	Feature   string `json:"feature"`
	Source    string `json:"source"` // grandfathered | plan | grant
	GrantedAt Time   `json:"grantedAt"`
	GrantedBy string `json:"grantedBy,omitempty"`
	Note      string `json:"note,omitempty"`
}

const entCols = `subject, feature, source, granted_at, granted_by, note`

// GetEntitlement returns the subject's entitlement to a feature.
func (d *DB) GetEntitlement(ctx context.Context, subject, feature string) (Entitlement, error) {
	var e Entitlement
	err := d.QueryRow(ctx, `SELECT `+entCols+` FROM entitlements WHERE subject = ? AND feature = ?`, subject, feature).
		Scan(&e.Subject, &e.Feature, &e.Source, &e.GrantedAt, &e.GrantedBy, &e.Note)
	if errors.Is(err, sql.ErrNoRows) {
		return e, ErrNotFound
	}
	return e, err
}

// Entitlements lists a subject's entitlements.
func (d *DB) Entitlements(ctx context.Context, subject string) ([]Entitlement, error) {
	rows, err := d.Query(ctx, `SELECT `+entCols+` FROM entitlements WHERE subject = ? ORDER BY feature`, subject)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Entitlement{}
	for rows.Next() {
		var e Entitlement
		if err := rows.Scan(&e.Subject, &e.Feature, &e.Source, &e.GrantedAt, &e.GrantedBy, &e.Note); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// SetEntitlement records a plan or grant. A grandfathered entitlement is
// never replaced: it is already the strongest right.
func (d *DB) SetEntitlement(ctx context.Context, e Entitlement) error {
	_, err := d.Exec(ctx, `INSERT INTO entitlements (tenant_id, subject, feature, source, granted_at, granted_by, note) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (subject, feature) DO UPDATE SET source = excluded.source, granted_at = excluded.granted_at, granted_by = excluded.granted_by, note = excluded.note
		WHERE entitlements.source <> 'grandfathered'`,
		TenantOf(ctx), e.Subject, e.Feature, e.Source, Now(), e.GrantedBy, e.Note)
	return err
}

// RevokeEntitlement removes a plan or grant; grandfathered rights stay.
func (d *DB) RevokeEntitlement(ctx context.Context, subject, feature string) (bool, error) {
	res, err := d.Exec(ctx, `DELETE FROM entitlements WHERE subject = ? AND feature = ? AND source <> 'grandfathered'`, subject, feature)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// Grandfather writes a grandfathered entitlement for every organization and
// user created before the cutoff (all tenants; idempotent). It returns the
// number of new rows.
func (d *DB) Grandfather(ctx context.Context, feature string, cutoff Time) (int64, error) {
	ctx = System(ctx)
	var total int64
	for _, q := range []string{
		`INSERT INTO entitlements (tenant_id, subject, feature, source, granted_at, granted_by, note)
			SELECT tenant_id, 'org:' || id, ?, 'grandfathered', ?, 'system', '' FROM organizations WHERE created_at < ?
			ON CONFLICT (subject, feature) DO UPDATE SET source = 'grandfathered', granted_at = excluded.granted_at, granted_by = 'system'
			WHERE entitlements.source <> 'grandfathered'`,
		`INSERT INTO entitlements (tenant_id, subject, feature, source, granted_at, granted_by, note)
			SELECT tenant_id, 'user:' || id, ?, 'grandfathered', ?, 'system', '' FROM users WHERE created_at < ?
			ON CONFLICT (subject, feature) DO UPDATE SET source = 'grandfathered', granted_at = excluded.granted_at, granted_by = 'system'
			WHERE entitlements.source <> 'grandfathered'`,
	} {
		res, err := d.Exec(ctx, q, feature, Now(), cutoff)
		if err != nil {
			return total, err
		}
		n, _ := res.RowsAffected()
		total += n
	}
	return total, nil
}
