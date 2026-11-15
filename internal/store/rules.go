package store

import (
	"context"
	"database/sql"
	"errors"
)

// Rule is an automation rule of a project (ADR-0060). Trigger and Action
// are JSON documents interpreted by the core.
type Rule struct {
	ID         string `json:"id"`
	ProjectID  string `json:"projectId"`
	Name       string `json:"name"`
	Enabled    bool   `json:"enabled"`
	Trigger    JSON   `json:"trigger"`
	Action     JSON   `json:"action"`
	CreatedBy  string `json:"createdBy"`
	CreatedAt  Time   `json:"createdAt"`
	UpdatedAt  Time   `json:"updatedAt"`
	LastRunAt  *Time  `json:"lastRunAt,omitempty"`
	LastStatus string `json:"lastStatus,omitempty"` // ok | error
	LastError  string `json:"lastError,omitempty"`
}

const ruleCols = `id, project_id, name, enabled, trigger_def, action_def, created_by, created_at, updated_at, last_run_at, last_status, last_error`

func scanRule(r interface{ Scan(...any) error }) (Rule, error) {
	var x Rule
	var trig, act string
	var last Time
	err := r.Scan(&x.ID, &x.ProjectID, &x.Name, &x.Enabled, &trig, &act, &x.CreatedBy, &x.CreatedAt, &x.UpdatedAt, &last, &x.LastStatus, &x.LastError)
	if errors.Is(err, sql.ErrNoRows) {
		return x, ErrNotFound
	}
	x.Trigger, x.Action = JSON(trig), JSON(act)
	if !last.IsZero() {
		x.LastRunAt = &last
	}
	return x, err
}

// SaveRule creates or updates a rule.
func (d *DB) SaveRule(ctx context.Context, x Rule) error {
	now := Now()
	if x.CreatedAt.IsZero() {
		x.CreatedAt = now
	}
	_, err := d.Exec(ctx, `INSERT INTO automation_rules (id, tenant_id, project_id, name, enabled, trigger_def, action_def, created_by, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET name = excluded.name, enabled = excluded.enabled, trigger_def = excluded.trigger_def,
			action_def = excluded.action_def, updated_at = excluded.updated_at`,
		x.ID, TenantOf(ctx), x.ProjectID, x.Name, x.Enabled, string(x.Trigger), string(x.Action), x.CreatedBy, x.CreatedAt, now)
	return err
}

// Rules lists the rules of a project (all projects with projectID "").
func (d *DB) Rules(ctx context.Context, projectID string) ([]Rule, error) {
	q, args := `SELECT `+ruleCols+` FROM automation_rules WHERE `+liveProject+` ORDER BY created_at`, []any{}
	if projectID != "" {
		q, args = `SELECT `+ruleCols+` FROM automation_rules WHERE project_id = ? ORDER BY created_at`, []any{projectID}
	}
	rows, err := d.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Rule{}
	for rows.Next() {
		x, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// GetRule returns a rule of a project.
func (d *DB) GetRule(ctx context.Context, projectID, id string) (Rule, error) {
	return scanRule(d.QueryRow(ctx, `SELECT `+ruleCols+` FROM automation_rules WHERE id = ? AND project_id = ?`, id, projectID))
}

// DeleteRule removes a rule and its runs.
func (d *DB) DeleteRule(ctx context.Context, projectID, id string) error {
	res, err := d.Exec(ctx, `DELETE FROM automation_rules WHERE id = ? AND project_id = ?`, id, projectID)
	return affected(res, err)
}

// ClaimRuleRun records that a rule handled key; false if it already did.
func (d *DB) ClaimRuleRun(ctx context.Context, ruleID, key string) (bool, error) {
	res, err := d.Exec(ctx, `INSERT INTO rule_runs (rule_id, tenant_id, run_key, created_at) VALUES (?, ?, ?, ?) ON CONFLICT DO NOTHING`,
		ruleID, TenantOf(ctx), key, Now())
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// RecentRuleRuns counts the runs of a rule since a time (rate limit).
func (d *DB) RecentRuleRuns(ctx context.Context, ruleID string, since Time) (int, error) {
	var n int
	err := d.QueryRow(ctx, `SELECT COUNT(*) FROM rule_runs WHERE rule_id = ? AND created_at > ?`, ruleID, since).Scan(&n)
	return n, err
}

// RuleResult stores the outcome of the last run.
func (d *DB) RuleResult(ctx context.Context, id, status, message string) error {
	_, err := d.Exec(ctx, `UPDATE automation_rules SET last_run_at = ?, last_status = ?, last_error = ? WHERE id = ?`, Now(), status, message, id)
	return err
}
