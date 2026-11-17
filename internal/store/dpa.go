package store

import (
	"context"
	"database/sql"
	"errors"
)

// Data processing agreement (DPA): versions of the text and acceptances by
// organizations.

// DPAVersion is one published text of the agreement.
type DPAVersion struct {
	ID          string `json:"id"`
	Version     string `json:"version"`
	BodyEN      string `json:"bodyEn"`
	BodyDE      string `json:"bodyDe"`
	PublishedAt Time   `json:"publishedAt"`
	CreatedBy   string `json:"createdBy"`
}

// DPAAcceptance records who accepted which text for an organization.
type DPAAcceptance struct {
	ID         string `json:"id"`
	OrgID      string `json:"orgId"`
	VersionID  string `json:"versionId"`
	Version    string `json:"version"`
	Lang       string `json:"lang"`
	TextSHA256 string `json:"textSha256"`
	UserID     string `json:"userId"`
	SignerName string `json:"signerName"`
	SignerRole string `json:"signerRole"`
	Company    string `json:"company"`
	AcceptedAt Time   `json:"acceptedAt"`
}

// AddDPAVersion publishes a new text.
func (d *DB) AddDPAVersion(ctx context.Context, v DPAVersion) error {
	_, err := d.Exec(ctx, `INSERT INTO dpa_versions (id, tenant_id, version, body_en, body_de, published_at, created_by) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		v.ID, TenantOf(ctx), v.Version, v.BodyEN, v.BodyDE, v.PublishedAt, v.CreatedBy)
	return err
}

func scanDPAVersion(r interface{ Scan(...any) error }) (DPAVersion, error) {
	var v DPAVersion
	err := r.Scan(&v.ID, &v.Version, &v.BodyEN, &v.BodyDE, &v.PublishedAt, &v.CreatedBy)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return v, err
}

const dpaVersionCols = `id, version, body_en, body_de, published_at, created_by`

// CurrentDPAVersion is the newest published text.
func (d *DB) CurrentDPAVersion(ctx context.Context) (DPAVersion, error) {
	return scanDPAVersion(d.QueryRow(ctx, `SELECT `+dpaVersionCols+` FROM dpa_versions ORDER BY published_at DESC, id DESC LIMIT 1`))
}

// GetDPAVersion reads one text.
func (d *DB) GetDPAVersion(ctx context.Context, id string) (DPAVersion, error) {
	return scanDPAVersion(d.QueryRow(ctx, `SELECT `+dpaVersionCols+` FROM dpa_versions WHERE id = ?`, id))
}

// DPAVersions lists all texts, newest first.
func (d *DB) DPAVersions(ctx context.Context) ([]DPAVersion, error) {
	rows, err := d.Query(ctx, `SELECT `+dpaVersionCols+` FROM dpa_versions ORDER BY published_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DPAVersion{}
	for rows.Next() {
		v, err := scanDPAVersion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// AddDPAAcceptance records an acceptance.
func (d *DB) AddDPAAcceptance(ctx context.Context, a DPAAcceptance) error {
	_, err := d.Exec(ctx, `INSERT INTO dpa_acceptances (id, tenant_id, org_id, version_id, version, lang, text_sha256, user_id, signer_name, signer_role, company, accepted_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, TenantOf(ctx), a.OrgID, a.VersionID, a.Version, a.Lang, a.TextSHA256, a.UserID, a.SignerName, a.SignerRole, a.Company, a.AcceptedAt)
	return err
}

const dpaAcceptCols = `id, org_id, version_id, version, lang, text_sha256, user_id, signer_name, signer_role, company, accepted_at`

func scanDPAAcceptance(r interface{ Scan(...any) error }) (DPAAcceptance, error) {
	var a DPAAcceptance
	err := r.Scan(&a.ID, &a.OrgID, &a.VersionID, &a.Version, &a.Lang, &a.TextSHA256, &a.UserID, &a.SignerName, &a.SignerRole, &a.Company, &a.AcceptedAt)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return a, err
}

// DPAAcceptances lists an organization's acceptances, newest first.
func (d *DB) DPAAcceptances(ctx context.Context, orgID string) ([]DPAAcceptance, error) {
	rows, err := d.Query(ctx, `SELECT `+dpaAcceptCols+` FROM dpa_acceptances WHERE org_id = ? ORDER BY accepted_at DESC`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DPAAcceptance{}
	for rows.Next() {
		a, err := scanDPAAcceptance(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// GetDPAAcceptance reads one acceptance.
func (d *DB) GetDPAAcceptance(ctx context.Context, id string) (DPAAcceptance, error) {
	return scanDPAAcceptance(d.QueryRow(ctx, `SELECT `+dpaAcceptCols+` FROM dpa_acceptances WHERE id = ?`, id))
}
