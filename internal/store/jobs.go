package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Job is a unit of background work.
type Job struct {
	ID        string `json:"id"`
	Tenant    string `json:"tenant"`
	Kind      string `json:"kind"`
	Payload   JSON   `json:"payload"`
	Status    string `json:"status"`
	Attempts  int    `json:"attempts"`
	RunAt     Time   `json:"runAt"`
	LastError string `json:"lastError,omitempty"`
}

// Enqueue adds a job that runs at runAt (zero = now).
func (d *DB) Enqueue(ctx context.Context, id, kind string, payload JSON, runAt Time) error {
	now := Now()
	if runAt.IsZero() {
		runAt = now
	}
	_, err := d.Exec(ctx, `INSERT INTO jobs (id, tenant_id, kind, payload, status, attempts, run_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, 'queued', 0, ?, ?, ?)`, id, TenantOf(ctx), kind, payload, runAt, now, now)
	return err
}

// ClaimJob takes the next due job (or one whose worker died) and locks it
// for lease. Postgres uses FOR UPDATE SKIP LOCKED so several workers can
// run; SQLite has a single writer anyway.
func (d *DB) ClaimJob(ctx context.Context, lease time.Duration) (Job, error) {
	now := Now()
	until := Time{now.Add(lease)}
	lock := ""
	if d.Dialect == Postgres {
		lock = ` FOR UPDATE SKIP LOCKED`
	}
	var j Job
	err := d.QueryRow(ctx, `UPDATE jobs SET status = 'running', attempts = attempts + 1, locked_until = ?, updated_at = ?
		WHERE id = (SELECT id FROM jobs WHERE (status = 'queued' AND run_at <= ?) OR (status = 'running' AND locked_until < ?)
			ORDER BY run_at LIMIT 1`+lock+`)
		RETURNING id, tenant_id, kind, payload, status, attempts, run_at, last_error`, until, now, now, now).
		Scan(&j.ID, &j.Tenant, &j.Kind, &j.Payload, &j.Status, &j.Attempts, &j.RunAt, &j.LastError)
	if errors.Is(err, sql.ErrNoRows) {
		return j, ErrNotFound
	}
	return j, err
}

// FinishJob marks a job done, or schedules a retry (retryAt set) or gives up.
func (d *DB) FinishJob(ctx context.Context, id string, jobErr error, retryAt *Time) error {
	now := Now()
	switch {
	case jobErr == nil:
		_, err := d.Exec(ctx, `UPDATE jobs SET status = 'done', locked_until = NULL, last_error = '', updated_at = ? WHERE id = ?`, now, id)
		return err
	case retryAt != nil:
		_, err := d.Exec(ctx, `UPDATE jobs SET status = 'queued', locked_until = NULL, run_at = ?, last_error = ?, updated_at = ? WHERE id = ?`, *retryAt, jobErr.Error(), now, id)
		return err
	default:
		_, err := d.Exec(ctx, `UPDATE jobs SET status = 'failed', locked_until = NULL, last_error = ?, updated_at = ? WHERE id = ?`, jobErr.Error(), now, id)
		return err
	}
}

// PruneJobs deletes finished jobs older than before.
func (d *DB) PruneJobs(ctx context.Context, before Time) (int64, error) {
	res, err := d.Exec(ctx, `DELETE FROM jobs WHERE status IN ('done', 'failed') AND updated_at < ?`, before)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// JobCounts returns the number of jobs per status (for metrics and the admin page).
func (d *DB) JobCounts(ctx context.Context) (map[string]int, error) {
	rows, err := d.Query(ctx, `SELECT status, COUNT(*) FROM jobs GROUP BY status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{"queued": 0, "running": 0, "done": 0, "failed": 0}
	for rows.Next() {
		var s string
		var n int
		if err := rows.Scan(&s, &n); err != nil {
			return nil, err
		}
		out[s] = n
	}
	return out, rows.Err()
}
