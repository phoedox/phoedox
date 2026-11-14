package store

import (
	"context"
	"database/sql"
	"errors"
)

// ProjectApproval tells whether page changes in a project need an owner's approval.
func (d *DB) ProjectApproval(ctx context.Context, projectID string) (bool, error) {
	var on bool
	err := d.QueryRow(ctx, `SELECT approval FROM projects WHERE id = ?`, projectID).Scan(&on)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrNotFound
	}
	return on, err
}

// SetProjectApproval switches the approval workflow of a project.
func (d *DB) SetProjectApproval(ctx context.Context, projectID string, on bool) error {
	_, err := d.Exec(ctx, `UPDATE projects SET approval = ? WHERE id = ?`, on, projectID)
	return err
}

// PageRule holds the review interval and required reading of a page.
type PageRule struct {
	DocumentID      string `json:"documentId"`
	TenantID        string `json:"-"`
	ProjectID       string `json:"-"`
	ReviewDays      int    `json:"reviewDays"`
	ReviewedAt      Time   `json:"reviewedAt"`
	ReviewedBy      string `json:"reviewedBy,omitempty"`
	NotifiedAt      Time   `json:"-"`
	RequiredReading bool   `json:"requiredReading"`
	UpdatedBy       string `json:"-"`
	UpdatedAt       Time   `json:"updatedAt"`
}

const pageRuleCols = `document_id, tenant_id, project_id, review_days, reviewed_at, reviewed_by, notified_at, required_reading, updated_by, updated_at`

func scanPageRule(r interface{ Scan(...any) error }) (PageRule, error) {
	var x PageRule
	err := r.Scan(&x.DocumentID, &x.TenantID, &x.ProjectID, &x.ReviewDays, &x.ReviewedAt, &x.ReviewedBy, &x.NotifiedAt, &x.RequiredReading, &x.UpdatedBy, &x.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return x, ErrNotFound
	}
	return x, err
}

// GetPageRule returns the rules of a page (ErrNotFound if none).
func (d *DB) GetPageRule(ctx context.Context, documentID string) (PageRule, error) {
	return scanPageRule(d.QueryRow(ctx, `SELECT `+pageRuleCols+` FROM page_rules WHERE document_id = ?`, documentID))
}

// SavePageRule creates or replaces the rules of a page.
func (d *DB) SavePageRule(ctx context.Context, x PageRule) error {
	_, err := d.Exec(ctx, `INSERT INTO page_rules (`+pageRuleCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (document_id) DO UPDATE SET review_days = excluded.review_days, reviewed_at = excluded.reviewed_at, reviewed_by = excluded.reviewed_by,
		  notified_at = excluded.notified_at, required_reading = excluded.required_reading, updated_by = excluded.updated_by, updated_at = excluded.updated_at`,
		x.DocumentID, TenantOf(ctx), x.ProjectID, x.ReviewDays, x.ReviewedAt, x.ReviewedBy, x.NotifiedAt, x.RequiredReading, x.UpdatedBy, x.UpdatedAt)
	return err
}

// PageRules lists the rules of a project's live pages, or of all projects
// (projectID "", e.g. with a System context for reminders).
func (d *DB) PageRules(ctx context.Context, projectID string) ([]PageRule, error) {
	q := `SELECT ` + prefixed("r.", pageRuleCols) + ` FROM page_rules r JOIN documents doc ON doc.id = r.document_id WHERE doc.deleted_at IS NULL`
	args := []any{}
	if projectID != "" {
		q += ` AND r.project_id = ?`
		args = append(args, projectID)
	}
	rows, err := d.Query(ctx, q+` ORDER BY r.document_id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PageRule{}
	for rows.Next() {
		x, err := scanPageRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// ReadConfirmation records that someone read a version of a page.
type ReadConfirmation struct {
	UserID      string `json:"userId"`
	Version     int    `json:"version"`
	ConfirmedAt Time   `json:"confirmedAt"`
}

// ConfirmRead stores (or moves forward) a read confirmation.
func (d *DB) ConfirmRead(ctx context.Context, documentID, userID string, version int, at Time) error {
	_, err := d.Exec(ctx, `INSERT INTO read_confirmations (document_id, tenant_id, user_id, version, confirmed_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (document_id, user_id) DO UPDATE SET version = excluded.version, confirmed_at = excluded.confirmed_at`,
		documentID, TenantOf(ctx), userID, version, at)
	return err
}

// ReadConfirmations lists who confirmed reading a page.
func (d *DB) ReadConfirmations(ctx context.Context, documentID string) (map[string]ReadConfirmation, error) {
	rows, err := d.Query(ctx, `SELECT user_id, version, confirmed_at FROM read_confirmations WHERE document_id = ?`, documentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]ReadConfirmation{}
	for rows.Next() {
		var c ReadConfirmation
		if err := rows.Scan(&c.UserID, &c.Version, &c.ConfirmedAt); err != nil {
			return nil, err
		}
		out[c.UserID] = c
	}
	return out, rows.Err()
}

// Maintenance plan units.
var MaintenanceUnits = []string{"day", "week", "month", "year"}

// MaintenancePlan is a recurring task for an entry.
type MaintenancePlan struct {
	ID          string `json:"id"`
	TenantID    string `json:"-"`
	ProjectID   string `json:"projectId"`
	EntityID    string `json:"entityId"`
	BoardID     string `json:"boardId"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Every       int    `json:"every"`
	Unit        string `json:"unit"`
	LeadDays    int    `json:"leadDays"`
	Assignee    string `json:"assignee,omitempty"`
	NextDue     string `json:"nextDue"` // YYYY-MM-DD
	OpenTaskID  string `json:"openTaskId,omitempty"`
	LastDone    Time   `json:"lastDone"`
	CreatedBy   string `json:"createdBy"`
	CreatedAt   Time   `json:"createdAt"`
	// Filled by joins.
	EntitySlug string `json:"entitySlug,omitempty"`
	EntityName string `json:"entityName,omitempty"`
	BoardSlug  string `json:"boardSlug,omitempty"`
}

const planCols = `m.id, m.tenant_id, m.project_id, m.entity_id, m.board_id, m.title, m.description, m.every, m.unit, m.lead_days, m.assignee, m.next_due,
	m.open_task_id, m.last_done, m.created_by, m.created_at, e.slug, e.name, b.slug`

const planFrom = ` FROM maintenance_plans m JOIN entities e ON e.id = m.entity_id JOIN documents b ON b.id = m.board_id`

func scanPlan(r interface{ Scan(...any) error }) (MaintenancePlan, error) {
	var x MaintenancePlan
	err := r.Scan(&x.ID, &x.TenantID, &x.ProjectID, &x.EntityID, &x.BoardID, &x.Title, &x.Description, &x.Every, &x.Unit, &x.LeadDays, &x.Assignee, &x.NextDue,
		&x.OpenTaskID, &x.LastDone, &x.CreatedBy, &x.CreatedAt, &x.EntitySlug, &x.EntityName, &x.BoardSlug)
	if errors.Is(err, sql.ErrNoRows) {
		return x, ErrNotFound
	}
	return x, err
}

// SaveMaintenancePlan creates or updates a plan.
func (d *DB) SaveMaintenancePlan(ctx context.Context, x MaintenancePlan) error {
	_, err := d.Exec(ctx, `INSERT INTO maintenance_plans (id, tenant_id, project_id, entity_id, board_id, title, description, every, unit, lead_days, assignee, next_due, open_task_id, last_done, created_by, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET board_id = excluded.board_id, title = excluded.title, description = excluded.description, every = excluded.every, unit = excluded.unit,
		  lead_days = excluded.lead_days, assignee = excluded.assignee, next_due = excluded.next_due, open_task_id = excluded.open_task_id, last_done = excluded.last_done`,
		x.ID, TenantOf(ctx), x.ProjectID, x.EntityID, x.BoardID, x.Title, x.Description, x.Every, x.Unit, x.LeadDays, x.Assignee, x.NextDue, x.OpenTaskID, x.LastDone, x.CreatedBy, x.CreatedAt)
	return err
}

// GetMaintenancePlan returns a plan of a project.
func (d *DB) GetMaintenancePlan(ctx context.Context, projectID, id string) (MaintenancePlan, error) {
	return scanPlan(d.QueryRow(ctx, `SELECT `+planCols+planFrom+` WHERE m.project_id = ? AND m.id = ?`, projectID, id))
}

// DeleteMaintenancePlan removes a plan (its tasks stay on the board).
func (d *DB) DeleteMaintenancePlan(ctx context.Context, projectID, id string) error {
	_, err := d.Exec(ctx, `DELETE FROM maintenance_plans WHERE project_id = ? AND id = ?`, projectID, id)
	return err
}

// MaintenancePlans lists the plans of a project, of one entry, or (both
// empty, System context) of all tenants.
func (d *DB) MaintenancePlans(ctx context.Context, projectID, entityID string) ([]MaintenancePlan, error) {
	q := `SELECT ` + planCols + planFrom + ` WHERE e.deleted_at IS NULL AND b.deleted_at IS NULL`
	args := []any{}
	if projectID != "" {
		q += ` AND m.project_id = ?`
		args = append(args, projectID)
	}
	if entityID != "" {
		q += ` AND m.entity_id = ?`
		args = append(args, entityID)
	}
	rows, err := d.Query(ctx, q+` ORDER BY m.next_due, m.title`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MaintenancePlan{}
	for rows.Next() {
		x, err := scanPlan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}
