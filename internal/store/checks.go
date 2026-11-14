package store

import (
	"context"
	"database/sql"
	"errors"
)

// Check states.
const (
	CheckUnknown = "unknown"
	CheckOK      = "ok"
	CheckFail    = "fail"
)

// Check monitors a request block of a page on a schedule.
type Check struct {
	ID          string `json:"id"`
	TenantID    string `json:"-"`
	ProjectID   string `json:"projectId"`
	DocumentID  string `json:"documentId"`
	BlockIndex  int    `json:"blockIndex"`
	EntityID    string `json:"entityId,omitempty"`
	Name        string `json:"name"`
	IntervalSec int    `json:"intervalSec"`
	Enabled     bool   `json:"enabled"`
	LastStatus  string `json:"lastStatus"`
	LastRun     Time   `json:"lastRun"`
	LastChange  Time   `json:"lastChange"`
	LastMs      int    `json:"lastMs"`
	LastMessage string `json:"lastMessage"` // i18n key (check.msg.*)
	LastDetail  string `json:"lastDetail"`  // status, expectation or network error
	FailCount   int    `json:"failCount"`
	CreatedBy   string `json:"createdBy"`
	CreatedAt   Time   `json:"createdAt"`
}

const checkCols = `id, tenant_id, project_id, document_id, block_index, entity_id, name, interval_sec, enabled, last_status, last_run, last_change, last_ms, last_message, last_detail, fail_count, created_by, created_at`

func scanCheck(r interface{ Scan(...any) error }) (Check, error) {
	var c Check
	var entity sql.NullString
	err := r.Scan(&c.ID, &c.TenantID, &c.ProjectID, &c.DocumentID, &c.BlockIndex, &entity, &c.Name, &c.IntervalSec, &c.Enabled,
		&c.LastStatus, &c.LastRun, &c.LastChange, &c.LastMs, &c.LastMessage, &c.LastDetail, &c.FailCount, &c.CreatedBy, &c.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	c.EntityID = entity.String
	return c, err
}

func (d *DB) queryChecks(ctx context.Context, query string, args ...any) ([]Check, error) {
	rows, err := d.Query(ctx, `SELECT `+checkCols+` FROM checks `+query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Check{}
	for rows.Next() {
		c, err := scanCheck(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// CreateCheck stores a new check.
func (d *DB) CreateCheck(ctx context.Context, c Check) (Check, error) {
	if c.LastStatus == "" {
		c.LastStatus = CheckUnknown
	}
	_, err := d.Exec(ctx, `INSERT INTO checks (id, tenant_id, project_id, document_id, block_index, entity_id, name, interval_sec, enabled, last_status, created_by, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, TenantOf(ctx), c.ProjectID, c.DocumentID, c.BlockIndex, nullable(c.EntityID), c.Name, c.IntervalSec, c.Enabled, c.LastStatus, c.CreatedBy, c.CreatedAt)
	c.TenantID = TenantOf(ctx)
	return c, err
}

// UpdateCheckSettings changes name, interval, entry and switch of a check.
func (d *DB) UpdateCheckSettings(ctx context.Context, c Check) error {
	res, err := d.Exec(ctx, `UPDATE checks SET name = ?, interval_sec = ?, entity_id = ?, enabled = ? WHERE id = ? AND project_id = ?`,
		c.Name, c.IntervalSec, nullable(c.EntityID), c.Enabled, c.ID, c.ProjectID)
	return affected(res, err)
}

// SaveCheckResult stores the outcome of a run.
func (d *DB) SaveCheckResult(ctx context.Context, c Check) error {
	_, err := d.Exec(ctx, `UPDATE checks SET last_status = ?, last_run = ?, last_change = ?, last_ms = ?, last_message = ?, last_detail = ?, fail_count = ? WHERE id = ?`,
		c.LastStatus, c.LastRun, c.LastChange, c.LastMs, c.LastMessage, c.LastDetail, c.FailCount, c.ID)
	return err
}

// GetCheck returns a check of a project.
func (d *DB) GetCheck(ctx context.Context, projectID, id string) (Check, error) {
	return scanCheck(d.QueryRow(ctx, `SELECT `+checkCols+` FROM checks WHERE project_id = ? AND id = ?`, projectID, id))
}

// ProjectChecks lists the checks of a project.
func (d *DB) ProjectChecks(ctx context.Context, projectID string) ([]Check, error) {
	return d.queryChecks(ctx, `WHERE project_id = ? ORDER BY name, created_at`, projectID)
}

// DocumentChecks lists the checks on the blocks of one page.
func (d *DB) DocumentChecks(ctx context.Context, documentID string) ([]Check, error) {
	return d.queryChecks(ctx, `WHERE document_id = ? ORDER BY block_index`, documentID)
}

// EnabledChecks lists the switched-on checks of all tenants (system context).
func (d *DB) EnabledChecks(ctx context.Context) ([]Check, error) {
	return d.queryChecks(ctx, `WHERE enabled = ? ORDER BY last_run`, true)
}

// DeleteCheck removes a check.
func (d *DB) DeleteCheck(ctx context.Context, projectID, id string) error {
	res, err := d.Exec(ctx, `DELETE FROM checks WHERE project_id = ? AND id = ?`, projectID, id)
	return affected(res, err)
}
