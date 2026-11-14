package store

import (
	"context"
	"database/sql"
	"errors"
)

// Incident states, in the order they usually follow.
const (
	IncidentInvestigating = "investigating"
	IncidentIdentified    = "identified"
	IncidentMonitoring    = "monitoring"
	IncidentResolved      = "resolved"
)

// Incident is an outage or problem with a timeline of updates.
type Incident struct {
	ID         string           `json:"id"`
	TenantID   string           `json:"-"`
	ProjectID  string           `json:"projectId"`
	CheckID    string           `json:"checkId,omitempty"`
	Title      string           `json:"title"`
	Status     string           `json:"status"`
	Public     bool             `json:"public"`
	StartedAt  Time             `json:"startedAt"`
	ResolvedAt Time             `json:"resolvedAt"`
	CreatedBy  string           `json:"createdBy"`
	Updates    []IncidentUpdate `json:"updates"`
}

// IncidentUpdate is one entry of an incident's timeline.
type IncidentUpdate struct {
	ID         string `json:"id"`
	TenantID   string `json:"-"`
	IncidentID string `json:"-"`
	Status     string `json:"status"`
	Body       string `json:"body"`
	// Auto: written by Phoedox from a check result.
	Auto      bool   `json:"auto"`
	CreatedBy string `json:"createdBy"`
	CreatedAt Time   `json:"createdAt"`
}

const incidentCols = `id, tenant_id, project_id, check_id, title, status, public, started_at, resolved_at, created_by`

func scanIncident(r interface{ Scan(...any) error }) (Incident, error) {
	var in Incident
	var check sql.NullString
	err := r.Scan(&in.ID, &in.TenantID, &in.ProjectID, &check, &in.Title, &in.Status, &in.Public, &in.StartedAt, &in.ResolvedAt, &in.CreatedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return in, ErrNotFound
	}
	in.CheckID = check.String
	in.Updates = []IncidentUpdate{}
	return in, err
}

// CreateIncident stores an incident with its first update.
func (d *DB) CreateIncident(ctx context.Context, in Incident, first IncidentUpdate) error {
	return d.InTx(ctx, func(tx *Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO incidents (`+incidentCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			in.ID, TenantOf(ctx), in.ProjectID, nullable(in.CheckID), in.Title, in.Status, in.Public, in.StartedAt, in.ResolvedAt, in.CreatedBy); err != nil {
			return err
		}
		return addIncidentUpdate(ctx, tx, first)
	})
}

func addIncidentUpdate(ctx context.Context, tx *Tx, u IncidentUpdate) error {
	_, err := tx.Exec(ctx, `INSERT INTO incident_updates (id, tenant_id, incident_id, status, body, auto, created_by, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		u.ID, TenantOf(ctx), u.IncidentID, u.Status, u.Body, u.Auto, u.CreatedBy, u.CreatedAt)
	return err
}

// AddIncidentUpdate appends to the timeline and sets the incident's status
// (resolving it sets the end time).
func (d *DB) AddIncidentUpdate(ctx context.Context, u IncidentUpdate) error {
	return d.InTx(ctx, func(tx *Tx) error {
		if err := addIncidentUpdate(ctx, tx, u); err != nil {
			return err
		}
		var resolved any
		if u.Status == IncidentResolved {
			resolved = u.CreatedAt
		}
		res, err := tx.Exec(ctx, `UPDATE incidents SET status = ?, resolved_at = ? WHERE id = ?`, u.Status, resolved, u.IncidentID)
		return affected(res, err)
	})
}

// UpdateIncident changes title and visibility.
func (d *DB) UpdateIncident(ctx context.Context, in Incident) error {
	res, err := d.Exec(ctx, `UPDATE incidents SET title = ?, public = ? WHERE id = ? AND project_id = ?`, in.Title, in.Public, in.ID, in.ProjectID)
	return affected(res, err)
}

// DeleteIncident removes an incident and its timeline.
func (d *DB) DeleteIncident(ctx context.Context, projectID, id string) error {
	res, err := d.Exec(ctx, `DELETE FROM incidents WHERE id = ? AND project_id = ?`, id, projectID)
	return affected(res, err)
}

// GetIncident returns an incident with its timeline.
func (d *DB) GetIncident(ctx context.Context, projectID, id string) (Incident, error) {
	in, err := scanIncident(d.QueryRow(ctx, `SELECT `+incidentCols+` FROM incidents WHERE id = ? AND project_id = ?`, id, projectID))
	if err != nil {
		return in, err
	}
	list := []Incident{in}
	if err := d.withUpdates(ctx, list); err != nil {
		return in, err
	}
	return list[0], nil
}

// OpenIncidentForCheck returns the unresolved incident a check opened.
func (d *DB) OpenIncidentForCheck(ctx context.Context, checkID string) (Incident, error) {
	return scanIncident(d.QueryRow(ctx, `SELECT `+incidentCols+` FROM incidents WHERE check_id = ? AND resolved_at IS NULL ORDER BY started_at DESC LIMIT 1`, checkID))
}

// Incidents lists a project's incidents that were open at some point since
// `since` (newest first), with their timelines; onlyPublic for status pages.
func (d *DB) Incidents(ctx context.Context, projectIDs []string, since Time, onlyPublic bool, limit int) ([]Incident, error) {
	if len(projectIDs) == 0 {
		return []Incident{}, nil
	}
	args := []any{}
	q := `SELECT ` + incidentCols + ` FROM incidents WHERE project_id IN (` + placeholders(len(projectIDs)) + `)`
	for _, id := range projectIDs {
		args = append(args, id)
	}
	q += ` AND (resolved_at IS NULL OR resolved_at >= ?)`
	args = append(args, since)
	if onlyPublic {
		q += ` AND public = ?`
		args = append(args, true)
	}
	q += ` ORDER BY started_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := d.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	out := []Incident{}
	for rows.Next() {
		in, err := scanIncident(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, in)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, d.withUpdates(ctx, out)
}

func (d *DB) withUpdates(ctx context.Context, list []Incident) error {
	if len(list) == 0 {
		return nil
	}
	idx := map[string]int{}
	args := []any{}
	for i, in := range list {
		idx[in.ID] = i
		args = append(args, in.ID)
	}
	rows, err := d.Query(ctx, `SELECT id, tenant_id, incident_id, status, body, auto, created_by, created_at FROM incident_updates
		WHERE incident_id IN (`+placeholders(len(list))+`) ORDER BY created_at, id`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var u IncidentUpdate
		if err := rows.Scan(&u.ID, &u.TenantID, &u.IncidentID, &u.Status, &u.Body, &u.Auto, &u.CreatedBy, &u.CreatedAt); err != nil {
			return err
		}
		i := idx[u.IncidentID]
		list[i].Updates = append(list[i].Updates, u)
	}
	return rows.Err()
}

// ProjectStatusToken returns the token of a project's public status page ("" = none).
func (d *DB) ProjectStatusToken(ctx context.Context, projectID string) (string, error) {
	var t sql.NullString
	err := d.QueryRow(ctx, `SELECT status_token FROM projects WHERE id = ?`, projectID).Scan(&t)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return t.String, err
}

// SetProjectStatusToken turns the public status page on (token) or off ("").
func (d *DB) SetProjectStatusToken(ctx context.Context, projectID, token string) error {
	res, err := d.Exec(ctx, `UPDATE projects SET status_token = ? WHERE id = ?`, nullable(token), projectID)
	return affected(res, err)
}

// ProjectByStatusToken finds the project of a public status page (system context).
func (d *DB) ProjectByStatusToken(ctx context.Context, token string) (projectID, tenantID string, err error) {
	err = d.QueryRow(ctx, `SELECT id, tenant_id FROM projects WHERE status_token = ?`, token).Scan(&projectID, &tenantID)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return
}
