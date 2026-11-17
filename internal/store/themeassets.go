package store

import (
	"context"
	"database/sql"
	"errors"
)

// ThemeAsset is a font or logo of a theme (content-addressed).
type ThemeAsset struct {
	SHA256 string `json:"sha256"`
	Kind   string `json:"kind"`
	Mime   string `json:"mime"`
	Name   string `json:"name"`
	Size   int64  `json:"size"`
}

// AddThemeAsset records an uploaded asset (once per content).
func (d *DB) AddThemeAsset(ctx context.Context, a ThemeAsset, by string) error {
	_, err := d.Exec(ctx, `INSERT INTO theme_assets (tenant_id, sha256, kind, mime, name, size, created_by, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (tenant_id, sha256) DO NOTHING`, TenantOf(ctx), a.SHA256, a.Kind, a.Mime, a.Name, a.Size, by, Now())
	return err
}

// GetThemeAsset reads an asset record of the tenant.
func (d *DB) GetThemeAsset(ctx context.Context, sha string) (ThemeAsset, error) {
	var a ThemeAsset
	err := d.QueryRow(ctx, `SELECT sha256, kind, mime, name, size FROM theme_assets WHERE tenant_id = ? AND sha256 = ?`, TenantOf(ctx), sha).
		Scan(&a.SHA256, &a.Kind, &a.Mime, &a.Name, &a.Size)
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	return a, err
}
