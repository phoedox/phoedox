package store

import (
	"context"
	"database/sql"
	"errors"
)

// Plugin is an installed plugin of a tenant.
type Plugin struct {
	ID          string `json:"id"`
	Version     string `json:"version"`
	Name        string `json:"name"`
	Manifest    string `json:"-"`
	SHA256      string `json:"sha256"`
	Size        int64  `json:"size"`
	Source      string `json:"source"` // store | upload
	SignedBy    string `json:"signedBy"`
	Enabled     bool   `json:"enabled"`
	Recall      string `json:"recall"` // "" | warning | critical
	RecallNote  string `json:"recallNote,omitempty"`
	Latest      string `json:"latest,omitempty"`
	License     string `json:"-"`
	InstalledBy string `json:"installedBy"`
	InstalledAt Time   `json:"installedAt"`
	UpdatedAt   Time   `json:"updatedAt"`
}

const pluginCols = `id, version, name, manifest, sha256, size, source, signed_by, enabled, recall, recall_note, latest, license, installed_by, installed_at, updated_at`

func scanPlugin(r interface{ Scan(...any) error }) (Plugin, error) {
	var p Plugin
	err := r.Scan(&p.ID, &p.Version, &p.Name, &p.Manifest, &p.SHA256, &p.Size, &p.Source, &p.SignedBy, &p.Enabled, &p.Recall, &p.RecallNote, &p.Latest, &p.License, &p.InstalledBy, &p.InstalledAt, &p.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}

// SavePlugin installs or updates a plugin (keeps license and recall state
// unless set).
func (d *DB) SavePlugin(ctx context.Context, p Plugin) error {
	now := Now()
	if p.InstalledAt.IsZero() {
		p.InstalledAt = now
	}
	_, err := d.Exec(ctx, `INSERT INTO plugins (tenant_id, id, version, name, manifest, sha256, size, source, signed_by, enabled, recall, recall_note, latest, license, installed_by, installed_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (tenant_id, id) DO UPDATE SET version = excluded.version, name = excluded.name, manifest = excluded.manifest,
		  sha256 = excluded.sha256, size = excluded.size, source = excluded.source, signed_by = excluded.signed_by,
		  enabled = excluded.enabled, recall = excluded.recall, recall_note = excluded.recall_note, latest = excluded.latest,
		  license = excluded.license, updated_at = excluded.updated_at`,
		TenantOf(ctx), p.ID, p.Version, p.Name, p.Manifest, p.SHA256, p.Size, p.Source, p.SignedBy, p.Enabled, p.Recall, p.RecallNote, p.Latest, p.License, p.InstalledBy, p.InstalledAt, now)
	return err
}

// Plugins lists the installed plugins of the tenant.
func (d *DB) Plugins(ctx context.Context) ([]Plugin, error) {
	rows, err := d.Query(ctx, `SELECT `+pluginCols+` FROM plugins WHERE tenant_id = ? ORDER BY name`, TenantOf(ctx))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Plugin{}
	for rows.Next() {
		p, err := scanPlugin(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetPlugin returns one installed plugin.
func (d *DB) GetPlugin(ctx context.Context, id string) (Plugin, error) {
	return scanPlugin(d.QueryRow(ctx, `SELECT `+pluginCols+` FROM plugins WHERE tenant_id = ? AND id = ?`, TenantOf(ctx), id))
}

// DeletePlugin uninstalls a plugin with its project settings and data.
func (d *DB) DeletePlugin(ctx context.Context, id string) error {
	return d.InTx(ctx, func(tx *Tx) error {
		for _, q := range []string{
			`DELETE FROM plugin_kv WHERE tenant_id = ? AND plugin_id = ?`,
			`DELETE FROM project_plugins WHERE tenant_id = ? AND plugin_id = ?`,
		} {
			if _, err := tx.Exec(ctx, q, TenantOf(ctx), id); err != nil {
				return err
			}
		}
		res, err := tx.Exec(ctx, `DELETE FROM plugins WHERE tenant_id = ? AND id = ?`, TenantOf(ctx), id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// PluginHashInUse tells whether any tenant still has a package installed.
func (d *DB) PluginHashInUse(ctx context.Context, hash string) (bool, error) {
	var one int
	err := d.QueryRow(ctx, `SELECT 1 FROM plugins WHERE sha256 = ? LIMIT 1`, hash).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// ProjectPlugin is a plugin enabled for a project.
type ProjectPlugin struct {
	ProjectID string `json:"projectId"`
	PluginID  string `json:"pluginId"`
	EnabledBy string `json:"enabledBy"`
	EnabledAt Time   `json:"enabledAt"`
	Secrets   string `json:"-"` // sealed JSON object
	LastRun   Time   `json:"lastRun"`
	LastError string `json:"lastError,omitempty"`
}

const ppCols = `project_id, plugin_id, enabled_by, enabled_at, secrets, last_run, last_error`

func scanPP(r interface{ Scan(...any) error }) (ProjectPlugin, error) {
	var p ProjectPlugin
	err := r.Scan(&p.ProjectID, &p.PluginID, &p.EnabledBy, &p.EnabledAt, &p.Secrets, &p.LastRun, &p.LastError)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}

// EnablePlugin enables a plugin for a project (keeps stored secrets).
func (d *DB) EnablePlugin(ctx context.Context, projectID, pluginID, by string) error {
	_, err := d.Exec(ctx, `INSERT INTO project_plugins (tenant_id, project_id, plugin_id, enabled_by, enabled_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (project_id, plugin_id) DO UPDATE SET enabled_by = excluded.enabled_by, enabled_at = excluded.enabled_at`,
		TenantOf(ctx), projectID, pluginID, by, Now())
	return err
}

// DisablePlugin disables a plugin for a project; its data stays.
func (d *DB) DisablePlugin(ctx context.Context, projectID, pluginID string) error {
	_, err := d.Exec(ctx, `DELETE FROM project_plugins WHERE project_id = ? AND plugin_id = ?`, projectID, pluginID)
	return err
}

// ProjectPlugins lists the plugins enabled for a project.
func (d *DB) ProjectPlugins(ctx context.Context, projectID string) ([]ProjectPlugin, error) {
	return d.listPP(ctx, `SELECT `+ppCols+` FROM project_plugins WHERE project_id = ? ORDER BY plugin_id`, projectID)
}

// PluginProjects lists the projects a plugin is enabled for (all tenants
// with a System context).
func (d *DB) PluginProjects(ctx context.Context, pluginID string) ([]ProjectPlugin, error) {
	return d.listPP(ctx, `SELECT `+ppCols+` FROM project_plugins WHERE plugin_id = ? ORDER BY project_id`, pluginID)
}

// AllProjectPlugins lists every enablement visible in the context (the
// scheduler uses a System context and reads tenant_id separately).
func (d *DB) AllProjectPlugins(ctx context.Context) ([]TenantProjectPlugin, error) {
	rows, err := d.Query(ctx, `SELECT tenant_id, `+ppCols+` FROM project_plugins ORDER BY tenant_id, project_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TenantProjectPlugin{}
	for rows.Next() {
		var t TenantProjectPlugin
		if err := rows.Scan(&t.TenantID, &t.ProjectID, &t.PluginID, &t.EnabledBy, &t.EnabledAt, &t.Secrets, &t.LastRun, &t.LastError); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// TenantProjectPlugin is a ProjectPlugin with its tenant.
type TenantProjectPlugin struct {
	TenantID string
	ProjectPlugin
}

func (d *DB) listPP(ctx context.Context, q string, args ...any) ([]ProjectPlugin, error) {
	rows, err := d.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ProjectPlugin{}
	for rows.Next() {
		p, err := scanPP(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetProjectPlugin returns the enablement of a plugin in a project.
func (d *DB) GetProjectPlugin(ctx context.Context, projectID, pluginID string) (ProjectPlugin, error) {
	return scanPP(d.QueryRow(ctx, `SELECT `+ppCols+` FROM project_plugins WHERE project_id = ? AND plugin_id = ?`, projectID, pluginID))
}

// SetPluginSecrets stores the sealed secrets of a plugin in a project.
func (d *DB) SetPluginSecrets(ctx context.Context, projectID, pluginID, sealed string) error {
	res, err := d.Exec(ctx, `UPDATE project_plugins SET secrets = ? WHERE project_id = ? AND plugin_id = ?`, sealed, projectID, pluginID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// PluginRan records the outcome of a scheduled or event run.
func (d *DB) PluginRan(ctx context.Context, projectID, pluginID, errText string) error {
	_, err := d.Exec(ctx, `UPDATE project_plugins SET last_run = ?, last_error = ? WHERE project_id = ? AND plugin_id = ?`, Now(), errText, projectID, pluginID)
	return err
}

// PluginKV reads a value of a plugin's key-value store in a project.
func (d *DB) PluginKV(ctx context.Context, projectID, pluginID, key string) (string, error) {
	var v string
	err := d.QueryRow(ctx, `SELECT value FROM plugin_kv WHERE project_id = ? AND plugin_id = ? AND key = ?`, projectID, pluginID, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return v, err
}

// SetPluginKV writes a value; an empty value deletes the key. At most
// maxKeys keys per plugin and project.
func (d *DB) SetPluginKV(ctx context.Context, projectID, pluginID, key, value string, maxKeys int) error {
	return d.InTx(ctx, func(tx *Tx) error {
		if value == "" || value == "null" {
			_, err := tx.Exec(ctx, `DELETE FROM plugin_kv WHERE project_id = ? AND plugin_id = ? AND key = ?`, projectID, pluginID, key)
			return err
		}
		var n int
		if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM plugin_kv WHERE project_id = ? AND plugin_id = ? AND key <> ?`, projectID, pluginID, key).Scan(&n); err != nil {
			return err
		}
		if n >= maxKeys {
			return ErrQuota
		}
		_, err := tx.Exec(ctx, `INSERT INTO plugin_kv (tenant_id, project_id, plugin_id, key, value, updated_at) VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT (project_id, plugin_id, key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
			TenantOf(ctx), projectID, pluginID, key, value, Now())
		return err
	})
}
