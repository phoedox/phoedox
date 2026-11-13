package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// AddEvent appends to the event log (history, audit, live updates).
func (d *DB) AddEvent(ctx context.Context, actor, kind, subject string, payload any) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = d.Exec(ctx, `INSERT INTO events (tenant_id, actor, kind, subject, payload, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		DefaultTenant, actor, kind, subject, JSON(b), Now())
	return err
}

// Suggestion is a pending change proposed by an AI client.
type Suggestion struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`
	Actor     string `json:"actor"`
	Op        string `json:"op"`
	Summary   string `json:"summary"`
	Payload   JSON   `json:"payload"`
	Status    string `json:"status"`
	CreatedAt Time   `json:"createdAt"`
	DecidedBy string `json:"decidedBy,omitempty"`
}

func (d *DB) CreateSuggestion(ctx context.Context, s Suggestion) (Suggestion, error) {
	s.CreatedAt, s.Status = Now(), "pending"
	_, err := d.Exec(ctx, `INSERT INTO suggestions (id, tenant_id, project_id, actor, op, summary, payload, status, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		s.ID, DefaultTenant, s.ProjectID, s.Actor, s.Op, s.Summary, s.Payload, s.Status, s.CreatedAt)
	return s, err
}

func (d *DB) ListSuggestions(ctx context.Context, projectID, status string) ([]Suggestion, error) {
	rows, err := d.Query(ctx, `SELECT id, project_id, actor, op, summary, payload, status, created_at, COALESCE(decided_by, '') FROM suggestions
		WHERE project_id = ? AND (? = '' OR status = ?) ORDER BY created_at`, projectID, status, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Suggestion
	for rows.Next() {
		var s Suggestion
		if err := rows.Scan(&s.ID, &s.ProjectID, &s.Actor, &s.Op, &s.Summary, &s.Payload, &s.Status, &s.CreatedAt, &s.DecidedBy); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (d *DB) GetSuggestion(ctx context.Context, id string) (Suggestion, error) {
	var s Suggestion
	err := d.QueryRow(ctx, `SELECT id, project_id, actor, op, summary, payload, status, created_at, COALESCE(decided_by, '') FROM suggestions WHERE id = ?`, id).
		Scan(&s.ID, &s.ProjectID, &s.Actor, &s.Op, &s.Summary, &s.Payload, &s.Status, &s.CreatedAt, &s.DecidedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return s, ErrNotFound
	}
	return s, err
}

// DecideSuggestion marks a pending suggestion as accepted or rejected.
func (d *DB) DecideSuggestion(ctx context.Context, id, status, by string) error {
	res, err := d.Exec(ctx, `UPDATE suggestions SET status = ?, decided_by = ?, decided_at = ? WHERE id = ? AND status = 'pending'`, status, by, Now(), id)
	return affected(res, err)
}
