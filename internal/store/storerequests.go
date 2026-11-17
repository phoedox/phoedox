package store

import (
	"context"
	"database/sql"
	"errors"
)

// StoreRequest asks an organization's admins for a theme or app template
// from the store that the organization's store access does not let the
// person install.
type StoreRequest struct {
	ID          string `json:"id"`
	OrgID       string `json:"orgId"`
	ProjectID   string `json:"projectId"`
	ProjectSlug string `json:"projectSlug,omitempty"`
	ProjectName string `json:"projectName,omitempty"`
	ItemType    string `json:"itemType"` // theme | app
	ItemID      string `json:"itemId"`
	ItemName    string `json:"itemName"`
	Version     string `json:"version"`
	// Title of the app created from a template.
	Title  string `json:"title,omitempty"`
	Note   string `json:"note,omitempty"`
	Status string `json:"status"` // pending | approved | rejected
	Answer string `json:"answer,omitempty"`
	// Result is the theme or document created on approval.
	Result      string `json:"result,omitempty"`
	RequestedBy string `json:"requestedBy"`
	UserName    string `json:"userName,omitempty"`
	DecidedBy   string `json:"decidedBy,omitempty"`
	CreatedAt   Time   `json:"createdAt"`
	DecidedAt   *Time  `json:"decidedAt,omitempty"`
}

const storeRequestCols = `r.id, r.org_id, r.project_id, COALESCE(p.slug, ''), COALESCE(p.name, ''), r.item_type, r.item_id, r.item_name, r.version, r.title, r.note,
	r.status, r.answer, r.result, r.requested_by, COALESCE(u.name, ''), r.decided_by, r.created_at, r.decided_at`

const storeRequestFrom = ` FROM store_requests r LEFT JOIN users u ON u.id = r.requested_by LEFT JOIN projects p ON p.id = r.project_id`

func scanStoreRequest(r interface{ Scan(...any) error }) (StoreRequest, error) {
	var q StoreRequest
	var decided sql.Null[Time]
	err := r.Scan(&q.ID, &q.OrgID, &q.ProjectID, &q.ProjectSlug, &q.ProjectName, &q.ItemType, &q.ItemID, &q.ItemName, &q.Version, &q.Title, &q.Note,
		&q.Status, &q.Answer, &q.Result, &q.RequestedBy, &q.UserName, &q.DecidedBy, &q.CreatedAt, &decided)
	if errors.Is(err, sql.ErrNoRows) {
		return q, ErrNotFound
	}
	if decided.Valid {
		q.DecidedAt = &decided.V
	}
	return q, err
}

// AddStoreRequest stores a new request.
func (d *DB) AddStoreRequest(ctx context.Context, r StoreRequest) error {
	_, err := d.Exec(ctx, `INSERT INTO store_requests (id, tenant_id, org_id, project_id, item_type, item_id, item_name, version, title, note, status, requested_by, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, TenantOf(ctx), r.OrgID, r.ProjectID, r.ItemType, r.ItemID, r.ItemName, r.Version, r.Title, r.Note, r.Status, r.RequestedBy, r.CreatedAt)
	return err
}

// StoreRequests lists an organization's requests (newest first), optionally
// of one person and one status.
func (d *DB) StoreRequests(ctx context.Context, orgID, userID, status string) ([]StoreRequest, error) {
	q := `SELECT ` + storeRequestCols + storeRequestFrom + ` WHERE r.org_id = ?`
	args := []any{orgID}
	if userID != "" {
		q += ` AND r.requested_by = ?`
		args = append(args, userID)
	}
	if status != "" {
		q += ` AND r.status = ?`
		args = append(args, status)
	}
	rows, err := d.Query(ctx, q+` ORDER BY r.created_at DESC LIMIT 200`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []StoreRequest{}
	for rows.Next() {
		r, err := scanStoreRequest(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetStoreRequest reads one request of an organization.
func (d *DB) GetStoreRequest(ctx context.Context, orgID, id string) (StoreRequest, error) {
	return scanStoreRequest(d.QueryRow(ctx, `SELECT `+storeRequestCols+storeRequestFrom+` WHERE r.org_id = ? AND r.id = ?`, orgID, id))
}

// DecideStoreRequest stores the answer of a pending request.
func (d *DB) DecideStoreRequest(ctx context.Context, r StoreRequest) error {
	res, err := d.Exec(ctx, `UPDATE store_requests SET status = ?, answer = ?, result = ?, decided_by = ?, decided_at = ? WHERE id = ? AND status = 'pending'`,
		r.Status, r.Answer, r.Result, r.DecidedBy, r.DecidedAt, r.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
