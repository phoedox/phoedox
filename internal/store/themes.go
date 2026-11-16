package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// UI themes (ui-themes-concept).

// Theme is a token set with fonts, shape and optional own CSS.
type Theme struct {
	ID          string          `json:"id"`
	Scope       string          `json:"scope"` // user | project | org | instance
	OwnerID     string          `json:"ownerId"`
	Name        string          `json:"name"`
	Data        json.RawMessage `json:"data"`
	CSS         string          `json:"css"`
	CSSReleased string          `json:"cssReleased"` // comma-separated Phoedox versions the CSS is released for
	CreatedBy   string          `json:"createdBy"`
	CreatedAt   Time            `json:"createdAt"`
	UpdatedAt   Time            `json:"updatedAt"`
}

const themeCols = `id, scope, owner_id, name, data, css, css_released, created_by, created_at, updated_at`

func scanTheme(r interface{ Scan(...any) error }) (Theme, error) {
	var t Theme
	var data string
	err := r.Scan(&t.ID, &t.Scope, &t.OwnerID, &t.Name, &data, &t.CSS, &t.CSSReleased, &t.CreatedBy, &t.CreatedAt, &t.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return t, ErrNotFound
	}
	t.Data = json.RawMessage(data)
	return t, err
}

// SaveTheme inserts or updates a theme.
func (d *DB) SaveTheme(ctx context.Context, t Theme) error {
	_, err := d.Exec(ctx, `INSERT INTO themes (id, tenant_id, scope, owner_id, name, data, css, css_released, created_by, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET name = excluded.name, data = excluded.data, css = excluded.css, css_released = excluded.css_released, updated_at = excluded.updated_at`,
		t.ID, TenantOf(ctx), t.Scope, t.OwnerID, t.Name, string(t.Data), t.CSS, t.CSSReleased, t.CreatedBy, t.CreatedAt, t.UpdatedAt)
	return err
}

// GetTheme reads a theme.
func (d *DB) GetTheme(ctx context.Context, id string) (Theme, error) {
	return scanTheme(d.QueryRow(ctx, `SELECT `+themeCols+` FROM themes WHERE id = ?`, id))
}

// Themes lists the themes of one owner.
func (d *DB) Themes(ctx context.Context, scope, ownerID string) ([]Theme, error) {
	rows, err := d.Query(ctx, `SELECT `+themeCols+` FROM themes WHERE scope = ? AND owner_id = ? ORDER BY name`, scope, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Theme{}
	for rows.Next() {
		t, err := scanTheme(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// DeleteTheme removes a theme and clears it where it was chosen.
func (d *DB) DeleteTheme(ctx context.Context, id string) error {
	return d.InTx(ctx, func(tx *Tx) error {
		for _, q := range []string{`UPDATE users SET theme_id = '' WHERE theme_id = ?`, `UPDATE projects SET theme_id = '' WHERE theme_id = ?`, `UPDATE organizations SET theme_id = '', theme_forced = FALSE WHERE theme_id = ?`} {
			if _, err := tx.Exec(ctx, q, id); err != nil {
				return err
			}
		}
		_, err := tx.Exec(ctx, `DELETE FROM themes WHERE id = ?`, id)
		return err
	})
}

// UserTheme is a person's theme choice.
type UserTheme struct {
	ThemeID  string `json:"themeId"`
	Always   bool   `json:"always"`
	Contrast string `json:"contrast"` // "" | high | variant
}

// GetUserTheme reads a person's theme choice.
func (d *DB) GetUserTheme(ctx context.Context, userID string) (UserTheme, error) {
	var u UserTheme
	err := d.QueryRow(ctx, `SELECT theme_id, theme_always, theme_contrast FROM users WHERE id = ?`, userID).Scan(&u.ThemeID, &u.Always, &u.Contrast)
	if errors.Is(err, sql.ErrNoRows) {
		return u, ErrNotFound
	}
	return u, err
}

// SetUserTheme stores a person's theme choice.
func (d *DB) SetUserTheme(ctx context.Context, userID string, u UserTheme) error {
	_, err := d.Exec(ctx, `UPDATE users SET theme_id = ?, theme_always = ?, theme_contrast = ? WHERE id = ?`, u.ThemeID, u.Always, u.Contrast, userID)
	return err
}

// ProjectThemeID reads a project's theme.
func (d *DB) ProjectThemeID(ctx context.Context, projectID string) (string, error) {
	var id string
	err := d.QueryRow(ctx, `SELECT theme_id FROM projects WHERE id = ?`, projectID).Scan(&id)
	return id, err
}

// SetProjectTheme sets a project's theme ("" = none).
func (d *DB) SetProjectTheme(ctx context.Context, projectID, themeID string) error {
	_, err := d.Exec(ctx, `UPDATE projects SET theme_id = ? WHERE id = ?`, themeID, projectID)
	return err
}

// OrgTheme reads an organization's default theme and whether it is forced.
func (d *DB) OrgTheme(ctx context.Context, orgID string) (string, bool, error) {
	var id string
	var forced bool
	err := d.QueryRow(ctx, `SELECT theme_id, theme_forced FROM organizations WHERE id = ?`, orgID).Scan(&id, &forced)
	return id, forced, err
}

// SetOrgTheme sets an organization's default theme.
func (d *DB) SetOrgTheme(ctx context.Context, orgID, themeID string, forced bool) error {
	_, err := d.Exec(ctx, `UPDATE organizations SET theme_id = ?, theme_forced = ? WHERE id = ?`, themeID, forced, orgID)
	return err
}
