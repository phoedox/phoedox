package store

import (
	"context"
	"database/sql"
	"errors"
)

// DeviceSetting holds per-device rules for control tools.
type DeviceSetting struct {
	EntityID  string `json:"entityId"`
	ProjectID string `json:"-"`
	AIControl string `json:"aiControl"` // suggest | direct
	UpdatedBy string `json:"-"`
	UpdatedAt Time   `json:"updatedAt"`
}

// GetDeviceSetting returns the rules of a device (ErrNotFound if none).
func (d *DB) GetDeviceSetting(ctx context.Context, entityID string) (DeviceSetting, error) {
	var x DeviceSetting
	err := d.QueryRow(ctx, `SELECT entity_id, project_id, ai_control, updated_by, updated_at FROM device_settings WHERE entity_id = ?`, entityID).
		Scan(&x.EntityID, &x.ProjectID, &x.AIControl, &x.UpdatedBy, &x.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return x, ErrNotFound
	}
	return x, err
}

// SaveDeviceSetting creates or replaces the rules of a device.
func (d *DB) SaveDeviceSetting(ctx context.Context, x DeviceSetting) error {
	_, err := d.Exec(ctx, `INSERT INTO device_settings (entity_id, tenant_id, project_id, ai_control, updated_by, updated_at) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (entity_id) DO UPDATE SET ai_control = excluded.ai_control, updated_by = excluded.updated_by, updated_at = excluded.updated_at`,
		x.EntityID, TenantOf(ctx), x.ProjectID, x.AIControl, x.UpdatedBy, x.UpdatedAt)
	return err
}

// ControlGrant lets one person switch one device without other rights in
// the project.
type ControlGrant struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`
	EntityID  string `json:"entityId"`
	UserID    string `json:"userId"`
	GrantedBy string `json:"grantedBy"`
	CreatedAt Time   `json:"createdAt"`
	ExpiresAt Time   `json:"expiresAt"`
	// Filled by the listings.
	UserName  string `json:"userName,omitempty"`
	UserEmail string `json:"userEmail,omitempty"`
}

const controlGrantCols = `g.id, g.project_id, g.entity_id, g.user_id, g.granted_by, g.created_at, g.expires_at, u.name, u.email`

func scanGrants(rows *sql.Rows) ([]ControlGrant, error) {
	defer rows.Close()
	out := []ControlGrant{}
	for rows.Next() {
		var g ControlGrant
		if err := rows.Scan(&g.ID, &g.ProjectID, &g.EntityID, &g.UserID, &g.GrantedBy, &g.CreatedAt, &g.ExpiresAt, &g.UserName, &g.UserEmail); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// SaveControlGrant creates a grant or renews the one of the same person.
func (d *DB) SaveControlGrant(ctx context.Context, g ControlGrant) error {
	_, err := d.Exec(ctx, `INSERT INTO control_grants (id, tenant_id, project_id, entity_id, user_id, granted_by, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (entity_id, user_id) DO UPDATE SET granted_by = excluded.granted_by, created_at = excluded.created_at, expires_at = excluded.expires_at`,
		g.ID, TenantOf(ctx), g.ProjectID, g.EntityID, g.UserID, g.GrantedBy, g.CreatedAt, g.ExpiresAt)
	return err
}

// DeleteControlGrant removes a grant of a device.
func (d *DB) DeleteControlGrant(ctx context.Context, entityID, id string) error {
	res, err := d.Exec(ctx, `DELETE FROM control_grants WHERE entity_id = ? AND id = ?`, entityID, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeviceGrants lists who may switch a device.
func (d *DB) DeviceGrants(ctx context.Context, entityID string) ([]ControlGrant, error) {
	rows, err := d.Query(ctx, `SELECT `+controlGrantCols+` FROM control_grants g JOIN users u ON u.id = g.user_id WHERE g.entity_id = ? ORDER BY u.name`, entityID)
	if err != nil {
		return nil, err
	}
	return scanGrants(rows)
}

// UserGrants lists the devices shared with a person (expired ones too).
func (d *DB) UserGrants(ctx context.Context, userID string) ([]ControlGrant, error) {
	rows, err := d.Query(ctx, `SELECT `+controlGrantCols+` FROM control_grants g JOIN users u ON u.id = g.user_id
		JOIN entities e ON e.id = g.entity_id WHERE g.user_id = ? AND e.deleted_at IS NULL ORDER BY g.created_at`, userID)
	if err != nil {
		return nil, err
	}
	return scanGrants(rows)
}

// GetControlGrant returns one grant.
func (d *DB) GetControlGrant(ctx context.Context, id string) (ControlGrant, error) {
	rows, err := d.Query(ctx, `SELECT `+controlGrantCols+` FROM control_grants g JOIN users u ON u.id = g.user_id WHERE g.id = ?`, id)
	if err != nil {
		return ControlGrant{}, err
	}
	list, err := scanGrants(rows)
	if err != nil {
		return ControlGrant{}, err
	}
	if len(list) == 0 {
		return ControlGrant{}, ErrNotFound
	}
	return list[0], nil
}
