package store

import "context"

// EnvVar is a project variable or secret. For secrets Value holds the
// ciphertext and must never leave the server.
type EnvVar struct {
	Name      string `json:"name"`
	Value     string `json:"value,omitempty"`
	Secret    bool   `json:"secret"`
	UpdatedBy string `json:"updatedBy"`
	UpdatedAt Time   `json:"updatedAt"`
}

// Env lists the variables of a project by name.
func (d *DB) Env(ctx context.Context, projectID string) ([]EnvVar, error) {
	rows, err := d.Query(ctx, `SELECT name, value, secret, updated_by, updated_at FROM project_env WHERE project_id = ? ORDER BY name`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []EnvVar{}
	for rows.Next() {
		var v EnvVar
		if err := rows.Scan(&v.Name, &v.Value, &v.Secret, &v.UpdatedBy, &v.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// SetEnv creates or replaces a variable.
func (d *DB) SetEnv(ctx context.Context, projectID string, v EnvVar) error {
	return d.InTx(ctx, func(tx *Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM project_env WHERE project_id = ? AND name = ?`, projectID, v.Name); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO project_env (project_id, name, tenant_id, value, secret, updated_by, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			projectID, v.Name, DefaultTenant, v.Value, v.Secret, v.UpdatedBy, Now())
		return err
	})
}

// DeleteEnv removes a variable.
func (d *DB) DeleteEnv(ctx context.Context, projectID, name string) error {
	res, err := d.Exec(ctx, `DELETE FROM project_env WHERE project_id = ? AND name = ?`, projectID, name)
	return affected(res, err)
}
