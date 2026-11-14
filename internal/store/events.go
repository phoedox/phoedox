package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

// AddEvent appends to the event log (history, audit, live updates).
func (d *DB) AddEvent(ctx context.Context, actor, kind, subject string, payload any) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = d.Exec(ctx, `INSERT INTO events (tenant_id, actor, kind, subject, payload, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		TenantOf(ctx), actor, kind, subject, JSON(b), Now())
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
		s.ID, TenantOf(ctx), s.ProjectID, s.Actor, s.Op, s.Summary, s.Payload, s.Status, s.CreatedAt)
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

// AddProjectEvent appends an event that belongs to a project ("" = none).
func (d *DB) AddProjectEvent(ctx context.Context, actor, kind, subject, projectID string, payload any) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = d.Exec(ctx, `INSERT INTO events (tenant_id, actor, kind, subject, payload, created_at, project_id) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		TenantOf(ctx), actor, kind, subject, JSON(b), Now(), nullable(projectID))
	return err
}

// ProjectOfSubject finds the project an event subject belongs to: a
// project, page, entry, relation, card, file, suggestion, webhook or agent.
func (d *DB) ProjectOfSubject(ctx context.Context, subject string) string {
	if subject == "" {
		return ""
	}
	var p string
	err := d.QueryRow(ctx, `SELECT p FROM (
		SELECT id AS p FROM projects WHERE id = ?
		UNION ALL SELECT project_id FROM documents WHERE id = ?
		UNION ALL SELECT project_id FROM entities WHERE id = ?
		UNION ALL SELECT f.project_id FROM edges e JOIN entities f ON f.id = e.from_id WHERE e.id = ?
		UNION ALL SELECT project_id FROM tasks WHERE id = ?
		UNION ALL SELECT project_id FROM files WHERE id = ?
		UNION ALL SELECT project_id FROM suggestions WHERE id = ?
		UNION ALL SELECT project_id FROM webhooks WHERE id = ?
		UNION ALL SELECT project_id FROM agents WHERE id = ?
		UNION ALL SELECT project_id FROM agent_jobs WHERE id = ?
		UNION ALL SELECT project_id FROM checks WHERE id = ?
	) s LIMIT 1`, subject, subject, subject, subject, subject, subject, subject, subject, subject, subject, subject).Scan(&p)
	if err != nil {
		return ""
	}
	return p
}

// Event is an audit log entry.
type Event struct {
	ID        int64  `json:"id"`
	Actor     string `json:"actor"`
	Kind      string `json:"kind"`
	Subject   string `json:"subject"`
	ProjectID string `json:"projectId,omitempty"`
	Payload   JSON   `json:"payload"`
	CreatedAt Time   `json:"createdAt"`
}

// EventFilter selects audit log entries; Before pages backwards by id.
type EventFilter struct {
	ProjectID string
	Actor     string // prefix, e.g. "mcp:" or "user:<id>"
	Kind      string // prefix, e.g. "document."
	Since     Time
	Before    int64
	Limit     int
}

// Events lists audit log entries, newest first.
func (d *DB) Events(ctx context.Context, f EventFilter) ([]Event, error) {
	q := `SELECT id, actor, kind, subject, COALESCE(project_id, ''), payload, created_at FROM events WHERE 1 = 1`
	var args []any
	if f.ProjectID != "" {
		q += ` AND project_id = ?`
		args = append(args, f.ProjectID)
	}
	if f.Actor != "" {
		q += ` AND actor LIKE ? ESCAPE '\'`
		args = append(args, likePrefix(f.Actor))
	}
	if f.Kind != "" {
		q += ` AND kind LIKE ? ESCAPE '\'`
		args = append(args, likePrefix(f.Kind))
	}
	if !f.Since.IsZero() {
		q += ` AND created_at >= ?`
		args = append(args, f.Since)
	}
	if f.Before > 0 {
		q += ` AND id < ?`
		args = append(args, f.Before)
	}
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 100
	}
	rows, err := d.Query(ctx, q+` ORDER BY id DESC LIMIT ?`, append(args, f.Limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var e Event
		var payload sql.NullString
		if err := rows.Scan(&e.ID, &e.Actor, &e.Kind, &e.Subject, &e.ProjectID, &payload, &e.CreatedAt); err != nil {
			return nil, err
		}
		e.Payload = JSON(payload.String)
		out = append(out, e)
	}
	return out, rows.Err()
}

// likePrefix escapes LIKE wildcards and appends %.
func likePrefix(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s) + "%"
}
