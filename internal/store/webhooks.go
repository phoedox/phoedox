package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// Webhook sends a project's events to a URL.
type Webhook struct {
	ID        string   `json:"id"`
	ProjectID string   `json:"projectId"`
	URL       string   `json:"url"`
	Secret    string   `json:"-"` // sealed
	Events    []string `json:"events"`
	Active    bool     `json:"active"`
	Failures  int      `json:"failures"`
	CreatedBy string   `json:"createdBy"`
	CreatedAt Time     `json:"createdAt"`
}

const hookCols = `id, project_id, url, secret, events, active, failures, created_by, created_at`

func scanHook(r interface{ Scan(...any) error }) (Webhook, error) {
	var h Webhook
	var events string
	err := r.Scan(&h.ID, &h.ProjectID, &h.URL, &h.Secret, &events, &h.Active, &h.Failures, &h.CreatedBy, &h.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return h, ErrNotFound
	}
	h.Events = strings.Split(events, ",")
	return h, err
}

// SaveWebhook creates or updates a webhook.
func (d *DB) SaveWebhook(ctx context.Context, h Webhook) error {
	if h.CreatedAt.IsZero() {
		h.CreatedAt = Now()
	}
	_, err := d.Exec(ctx, `INSERT INTO webhooks (id, tenant_id, project_id, url, secret, events, active, failures, created_by, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET url = excluded.url, events = excluded.events, active = excluded.active, failures = excluded.failures`,
		h.ID, TenantOf(ctx), h.ProjectID, h.URL, h.Secret, strings.Join(h.Events, ","), h.Active, h.Failures, h.CreatedBy, h.CreatedAt)
	return err
}

// Webhooks lists the webhooks of a project.
func (d *DB) Webhooks(ctx context.Context, projectID string) ([]Webhook, error) {
	rows, err := d.Query(ctx, `SELECT `+hookCols+` FROM webhooks WHERE project_id = ? ORDER BY created_at`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Webhook{}
	for rows.Next() {
		h, err := scanHook(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// GetWebhook returns a webhook.
func (d *DB) GetWebhook(ctx context.Context, id string) (Webhook, error) {
	return scanHook(d.QueryRow(ctx, `SELECT `+hookCols+` FROM webhooks WHERE id = ?`, id))
}

// DeleteWebhook removes a webhook and its log.
func (d *DB) DeleteWebhook(ctx context.Context, projectID, id string) error {
	res, err := d.Exec(ctx, `DELETE FROM webhooks WHERE id = ? AND project_id = ?`, id, projectID)
	return affected(res, err)
}

// HookFailed counts a failed delivery and deactivates the hook after max failures in a row.
func (d *DB) HookFailed(ctx context.Context, id string, max int) error {
	_, err := d.Exec(ctx, `UPDATE webhooks SET failures = failures + 1, active = CASE WHEN failures + 1 >= ? THEN `+d.boolFalse()+` ELSE active END WHERE id = ?`, max, id)
	return err
}

// HookOK resets the failure count.
func (d *DB) HookOK(ctx context.Context, id string) error {
	_, err := d.Exec(ctx, `UPDATE webhooks SET failures = 0 WHERE id = ?`, id)
	return err
}

func (d *DB) boolFalse() string {
	if d.Dialect == Postgres {
		return "false"
	}
	return "0"
}

// Delivery is one attempt to send an event to a webhook.
type Delivery struct {
	ID          string `json:"id"`
	WebhookID   string `json:"webhookId"`
	EventType   string `json:"eventType"`
	Body        string `json:"body"`
	Status      string `json:"status"`
	Response    int    `json:"response"`
	Error       string `json:"error,omitempty"`
	Attempts    int    `json:"attempts"`
	DurationMs  int    `json:"durationMs"`
	CreatedAt   Time   `json:"createdAt"`
	DeliveredAt Time   `json:"deliveredAt"`
}

const deliveryCols = `id, webhook_id, event_type, body, status, response, error, attempts, duration_ms, created_at, delivered_at`

func scanDelivery(r interface{ Scan(...any) error }) (Delivery, error) {
	var x Delivery
	err := r.Scan(&x.ID, &x.WebhookID, &x.EventType, &x.Body, &x.Status, &x.Response, &x.Error, &x.Attempts, &x.DurationMs, &x.CreatedAt, &x.DeliveredAt)
	if errors.Is(err, sql.ErrNoRows) {
		return x, ErrNotFound
	}
	return x, err
}

// CreateDelivery stores an event to deliver.
func (d *DB) CreateDelivery(ctx context.Context, x Delivery) error {
	_, err := d.Exec(ctx, `INSERT INTO webhook_deliveries (id, tenant_id, webhook_id, event_type, body, status, created_at) VALUES (?, ?, ?, ?, ?, 'pending', ?)`,
		x.ID, TenantOf(ctx), x.WebhookID, x.EventType, x.Body, Now())
	return err
}

// GetDelivery returns a delivery.
func (d *DB) GetDelivery(ctx context.Context, id string) (Delivery, error) {
	return scanDelivery(d.QueryRow(ctx, `SELECT `+deliveryCols+` FROM webhook_deliveries WHERE id = ?`, id))
}

// FinishDelivery records the outcome of an attempt.
func (d *DB) FinishDelivery(ctx context.Context, x Delivery) error {
	var at any
	if !x.DeliveredAt.IsZero() {
		at = x.DeliveredAt
	}
	_, err := d.Exec(ctx, `UPDATE webhook_deliveries SET status = ?, response = ?, error = ?, attempts = attempts + 1, duration_ms = ?, delivered_at = ? WHERE id = ?`,
		x.Status, x.Response, x.Error, x.DurationMs, at, x.ID)
	return err
}

// Deliveries lists the latest deliveries of a webhook and drops older ones (keeps 50).
func (d *DB) Deliveries(ctx context.Context, webhookID string) ([]Delivery, error) {
	rows, err := d.Query(ctx, `SELECT `+deliveryCols+` FROM webhook_deliveries WHERE webhook_id = ? ORDER BY created_at DESC LIMIT 50`, webhookID)
	if err != nil {
		return nil, err
	}
	out := []Delivery{}
	for rows.Next() {
		x, err := scanDelivery(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, x)
	}
	rows.Close()
	if len(out) == 50 {
		_, _ = d.Exec(ctx, `DELETE FROM webhook_deliveries WHERE webhook_id = ? AND created_at < ? AND status <> 'pending'`, webhookID, out[49].CreatedAt)
	}
	return out, rows.Err()
}

// ProjectOfObject finds the project of a document, entity, file, edge or suggestion.
func (d *DB) ProjectOfObject(ctx context.Context, id string) (Project, error) {
	var pid string
	err := d.QueryRow(ctx, `SELECT project_id FROM documents WHERE id = ?
		UNION ALL SELECT project_id FROM entities WHERE id = ?
		UNION ALL SELECT project_id FROM files WHERE id = ?
		UNION ALL SELECT project_id FROM suggestions WHERE id = ?
		UNION ALL SELECT f.project_id FROM edges e JOIN entities f ON f.id = e.from_id WHERE e.id = ?
		LIMIT 1`, id, id, id, id, id).Scan(&pid)
	if errors.Is(err, sql.ErrNoRows) {
		return Project{}, ErrNotFound
	}
	if err != nil {
		return Project{}, err
	}
	return d.ProjectFor(ctx, pid, "")
}
