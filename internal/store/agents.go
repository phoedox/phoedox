package store

import (
	"context"
	"database/sql"
	"errors"
)

// Agent is an enrolled host (ADR-0033).
type Agent struct {
	ID         string `json:"id"`
	ProjectID  string `json:"projectId"`
	UserID     string `json:"-"`
	TokenHash  string `json:"-"`
	Hostname   string `json:"hostname"`
	EntitySlug string `json:"entitySlug"`
	Version    string `json:"version"`
	LastReport JSON   `json:"lastReport,omitempty"`
	LastSeen   Time   `json:"lastSeen"`
	RevokedAt  Time   `json:"revokedAt"`
	CreatedAt  Time   `json:"createdAt"`
	TenantID   string `json:"-"`
}

const agentCols = `id, project_id, user_id, token_hash, hostname, entity_slug, version, last_report, last_seen, revoked_at, created_at, tenant_id`

func scanAgent(r interface{ Scan(...any) error }) (Agent, error) {
	var a Agent
	var report sql.NullString
	err := r.Scan(&a.ID, &a.ProjectID, &a.UserID, &a.TokenHash, &a.Hostname, &a.EntitySlug, &a.Version, &report, &a.LastSeen, &a.RevokedAt, &a.CreatedAt, &a.TenantID)
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	if report.Valid {
		a.LastReport = JSON(report.String)
	}
	return a, err
}

// AddEnrollment stores an enrollment code (hashed).
func (d *DB) AddEnrollment(ctx context.Context, codeHash, projectID, userID string, expires Time, uses int) error {
	_, err := d.Exec(ctx, `INSERT INTO agent_enrollments (code_hash, tenant_id, project_id, created_by, expires_at, uses_left) VALUES (?, ?, ?, ?, ?, ?)`,
		codeHash, TenantOf(ctx), projectID, userID, expires, uses)
	return err
}

// UseEnrollment consumes one use of a valid code across tenants and returns
// tenant, project and the user who created it.
func (d *DB) UseEnrollment(ctx context.Context, codeHash string) (tenant, project, user string, err error) {
	ctx = System(ctx)
	err = d.InTx(ctx, func(tx *Tx) error {
		var exp Time
		var uses int
		if err := tx.QueryRow(ctx, `SELECT tenant_id, project_id, created_by, expires_at, uses_left FROM agent_enrollments WHERE code_hash = ?`, codeHash).
			Scan(&tenant, &project, &user, &exp, &uses); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if uses <= 0 || Now().After(exp.Time) {
			return ErrNotFound
		}
		_, err := tx.Exec(ctx, `UPDATE agent_enrollments SET uses_left = uses_left - 1 WHERE code_hash = ?`, codeHash)
		return err
	})
	return tenant, project, user, err
}

// CreateAgent inserts an agent.
func (d *DB) CreateAgent(ctx context.Context, a Agent) error {
	_, err := d.Exec(ctx, `INSERT INTO agents (id, tenant_id, project_id, user_id, token_hash, hostname, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		a.ID, TenantOf(ctx), a.ProjectID, a.UserID, a.TokenHash, a.Hostname, a.CreatedAt)
	return err
}

// AgentByToken finds a not revoked agent across tenants.
func (d *DB) AgentByToken(ctx context.Context, tokenHash string) (Agent, error) {
	return scanAgent(d.QueryRow(System(ctx), `SELECT `+agentCols+` FROM agents WHERE token_hash = ? AND revoked_at IS NULL`, tokenHash))
}

// SaveAgentReport stores the last report.
func (d *DB) SaveAgentReport(ctx context.Context, id, hostname, slug, version string, report []byte) error {
	_, err := d.Exec(ctx, `UPDATE agents SET hostname = ?, entity_slug = ?, version = ?, last_report = ?, last_seen = ? WHERE id = ?`,
		hostname, slug, version, string(report), Now(), id)
	return err
}

// ProjectAgents lists the agents of a project.
func (d *DB) ProjectAgents(ctx context.Context, projectID string) ([]Agent, error) {
	rows, err := d.Query(ctx, `SELECT `+agentCols+` FROM agents WHERE project_id = ? ORDER BY hostname`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Agent{}
	for rows.Next() {
		a, err := scanAgent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// RevokeAgent blocks an agent's token.
func (d *DB) RevokeAgent(ctx context.Context, projectID, id string) error {
	res, err := d.Exec(ctx, `UPDATE agents SET revoked_at = ? WHERE project_id = ? AND id = ? AND revoked_at IS NULL`, Now(), projectID, id)
	return affected(res, err)
}
