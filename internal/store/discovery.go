package store

import (
	"context"
	"strings"
)

// Discovered is a device an agent saw in its network (ADR-0072).
type Discovered struct {
	ID        string   `json:"id"`
	AgentID   string   `json:"agentId"`
	IP        string   `json:"ip"`
	MAC       string   `json:"mac,omitempty"`
	Hostname  string   `json:"hostname,omitempty"`
	Services  []string `json:"services"`
	Via       []string `json:"via"`
	FirstSeen Time     `json:"firstSeen"`
	LastSeen  Time     `json:"lastSeen"`
	Dismissed bool     `json:"dismissed,omitempty"`
}

const discoveredCols = `id, agent_id, ip, mac, hostname, services, via, first_seen, last_seen, dismissed_at IS NOT NULL`

// SaveDiscovered records what an agent saw; a device is the same by MAC,
// else by IP. Hostname and services are kept when a later round lacks them.
func (d *DB) SaveDiscovered(ctx context.Context, projectID, agentID string, list []Discovered) error {
	now := Now()
	return d.InTx(ctx, func(tx *Tx) error {
		for _, x := range list {
			key := strings.ToLower(x.MAC)
			if key == "" {
				key = "ip:" + x.IP
			}
			_, err := tx.Exec(ctx, `INSERT INTO discovered_devices (id, tenant_id, project_id, agent_id, key, ip, mac, hostname, services, via, first_seen, last_seen)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
				ON CONFLICT (project_id, key) DO UPDATE SET agent_id = excluded.agent_id, ip = excluded.ip, last_seen = excluded.last_seen,
				  hostname = CASE WHEN excluded.hostname <> '' THEN excluded.hostname ELSE discovered_devices.hostname END,
				  services = CASE WHEN excluded.services <> '' THEN excluded.services ELSE discovered_devices.services END,
				  via = excluded.via`,
				x.ID, TenantOf(ctx), projectID, agentID, key, x.IP, strings.ToLower(x.MAC), x.Hostname, strings.Join(x.Services, ","), strings.Join(x.Via, ","), now, now)
			if err != nil {
				return err
			}
		}
		return nil
	})
}

// ListDiscovered lists the devices of a project, newest first.
func (d *DB) ListDiscovered(ctx context.Context, projectID string, withDismissed bool) ([]Discovered, error) {
	q := `SELECT ` + discoveredCols + ` FROM discovered_devices WHERE project_id = ?`
	if !withDismissed {
		q += ` AND dismissed_at IS NULL`
	}
	rows, err := d.Query(ctx, q+` ORDER BY last_seen DESC, ip LIMIT 2000`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Discovered{}
	for rows.Next() {
		var x Discovered
		var services, via string
		if err := rows.Scan(&x.ID, &x.AgentID, &x.IP, &x.MAC, &x.Hostname, &services, &via, &x.FirstSeen, &x.LastSeen, &x.Dismissed); err != nil {
			return nil, err
		}
		x.Services, x.Via = splitList(services), splitList(via)
		out = append(out, x)
	}
	return out, rows.Err()
}

// GetDiscovered returns one device.
func (d *DB) GetDiscovered(ctx context.Context, projectID, id string) (Discovered, error) {
	list, err := d.ListDiscovered(ctx, projectID, true)
	if err != nil {
		return Discovered{}, err
	}
	for _, x := range list {
		if x.ID == id {
			return x, nil
		}
	}
	return Discovered{}, ErrNotFound
}

// DismissDiscovered hides a device (or shows it again).
func (d *DB) DismissDiscovered(ctx context.Context, projectID, id string, dismissed bool) error {
	var at any
	if dismissed {
		at = Now()
	}
	res, err := d.Exec(ctx, `UPDATE discovered_devices SET dismissed_at = ? WHERE project_id = ? AND id = ?`, at, projectID, id)
	return affected(res, err)
}

// DeleteDiscovered removes devices of a project (all when ids is empty).
func (d *DB) DeleteDiscovered(ctx context.Context, projectID string) error {
	_, err := d.Exec(ctx, `DELETE FROM discovered_devices WHERE project_id = ?`, projectID)
	return err
}
