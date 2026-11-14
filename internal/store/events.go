package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
)

// AddEvent appends to the event log (history, audit, live updates).
func (d *DB) AddEvent(ctx context.Context, actor, kind, subject string, payload any) error {
	return d.AddProjectEvent(ctx, actor, kind, subject, "", payload)
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
// The event is chained: it gets the next number of its tenant's chain and a
// hash over the previous event's hash and its own content, so later
// changes or deletions show up in VerifyEventChain.
func (d *DB) AddProjectEvent(ctx context.Context, actor, kind, subject, projectID string, payload any) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	tenant := TenantOf(ctx)
	e := ChainedEvent{Tenant: tenant, Actor: actor, Kind: kind, Subject: subject, ProjectID: projectID, Payload: canonicalJSON(b), CreatedAt: Now()}
	// Two writers may pick the same number; the unique index refuses one, which tries again.
	for attempt := 0; ; attempt++ {
		err = d.InTx(ctx, func(tx *Tx) error {
			var last sql.NullInt64
			var prev string
			err := tx.QueryRow(ctx, `SELECT seq, hash FROM events WHERE tenant_id = ? AND seq IS NOT NULL ORDER BY seq DESC LIMIT 1`, tenant).Scan(&last, &prev)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			e.Seq, e.PrevHash = last.Int64+1, prev
			_, err = tx.Exec(ctx, `INSERT INTO events (tenant_id, actor, kind, subject, payload, created_at, project_id, seq, prev_hash, hash) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				tenant, actor, kind, subject, JSON(b), e.CreatedAt, nullable(projectID), e.Seq, e.PrevHash, e.Hash())
			return err
		})
		if err == nil || !isUniqueViolation(err) || attempt >= 20 {
			return err
		}
	}
}

// ChainedEvent is what an audit log hash covers.
type ChainedEvent struct {
	Tenant, Actor, Kind, Subject, ProjectID string
	Payload                                 string // canonical JSON
	CreatedAt                               Time
	Seq                                     int64
	PrevHash                                string
}

// Hash is SHA-256 over the previous hash and the event's fields.
func (e ChainedEvent) Hash() string {
	h := sha256.New()
	for _, f := range []string{e.PrevHash, e.Tenant, strconv.FormatInt(e.Seq, 10), e.Actor, e.Kind, e.Subject, e.ProjectID, e.Payload,
		e.CreatedAt.UTC().Truncate(time.Millisecond).Format(time.RFC3339Nano)} {
		h.Write([]byte(f))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// canonicalJSON gives JSON a stable form (sorted keys, no spacing), so the
// hash survives Postgres jsonb's own formatting.
func canonicalJSON(b []byte) string {
	var v any
	if json.Unmarshal(b, &v) != nil {
		return string(b)
	}
	out, _ := json.Marshal(v)
	return string(out)
}

// ChainReport is the result of checking a tenant's audit chain.
type ChainReport struct {
	OK      bool   `json:"ok"`
	Checked int64  `json:"checked"`
	Head    string `json:"head"`             // hash of the newest event
	HeadSeq int64  `json:"headSeq"`          // its number
	Broken  int64  `json:"broken,omitempty"` // first event that does not match
	Reason  string `json:"reason,omitempty"` // changed | missing | order
	Before  int64  `json:"unchained"`        // events from before the chain started
}

// VerifyEventChain recomputes the chain of the current tenant.
func (d *DB) VerifyEventChain(ctx context.Context) (ChainReport, error) {
	tenant := TenantOf(ctx)
	r := ChainReport{OK: true}
	if err := d.QueryRow(ctx, `SELECT count(*) FROM events WHERE tenant_id = ? AND seq IS NULL`, tenant).Scan(&r.Before); err != nil {
		return r, err
	}
	rows, err := d.Query(ctx, `SELECT seq, prev_hash, hash, actor, kind, subject, COALESCE(project_id, ''), payload, created_at FROM events
		WHERE tenant_id = ? AND seq IS NOT NULL ORDER BY seq`, tenant)
	if err != nil {
		return r, err
	}
	defer rows.Close()
	prev := ""
	for rows.Next() {
		var e ChainedEvent
		var hash string
		var payload JSON
		if err := rows.Scan(&e.Seq, &e.PrevHash, &hash, &e.Actor, &e.Kind, &e.Subject, &e.ProjectID, &payload, &e.CreatedAt); err != nil {
			return r, err
		}
		e.Tenant, e.Payload = tenant, canonicalJSON(payload)
		switch {
		case e.Seq != r.HeadSeq+1:
			r.OK, r.Broken, r.Reason = false, r.HeadSeq+1, "missing"
		case e.PrevHash != prev:
			r.OK, r.Broken, r.Reason = false, e.Seq, "order"
		case e.Hash() != hash:
			r.OK, r.Broken, r.Reason = false, e.Seq, "changed"
		}
		if !r.OK {
			return r, nil
		}
		r.Checked++
		r.HeadSeq, r.Head, prev = e.Seq, hash, hash
	}
	return r, rows.Err()
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
		UNION ALL SELECT project_id FROM comments WHERE id = ?
	) s LIMIT 1`, subject, subject, subject, subject, subject, subject, subject, subject, subject, subject, subject, subject).Scan(&p)
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
	Subject   string // exact
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
	if f.Subject != "" {
		q += ` AND subject = ?`
		args = append(args, f.Subject)
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
