package store

import (
	"context"
	"database/sql"
	"errors"
)

// SigningKey is a public key a person signs agent jobs with (ADR-0036).
type SigningKey struct {
	ID        string `json:"id"`
	UserID    string `json:"-"`
	Name      string `json:"name"`
	PublicKey string `json:"publicKey"`
	CreatedAt Time   `json:"createdAt"`
}

// AddSigningKey stores a public key.
func (d *DB) AddSigningKey(ctx context.Context, k SigningKey) error {
	_, err := d.Exec(ctx, `INSERT INTO signing_keys (id, tenant_id, user_id, name, public_key, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		k.ID, TenantOf(ctx), k.UserID, k.Name, k.PublicKey, k.CreatedAt)
	return err
}

// SigningKeys lists a user's keys.
func (d *DB) SigningKeys(ctx context.Context, userID string) ([]SigningKey, error) {
	rows, err := d.Query(ctx, `SELECT id, user_id, name, public_key, created_at FROM signing_keys WHERE user_id = ? ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SigningKey{}
	for rows.Next() {
		var k SigningKey
		if err := rows.Scan(&k.ID, &k.UserID, &k.Name, &k.PublicKey, &k.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// DeleteSigningKey removes a user's key.
func (d *DB) DeleteSigningKey(ctx context.Context, userID, id string) error {
	res, err := d.Exec(ctx, `DELETE FROM signing_keys WHERE user_id = ? AND id = ?`, userID, id)
	return affected(res, err)
}

// AgentJob is an action for an agent.
type AgentJob struct {
	ID         string `json:"id"`
	ProjectID  string `json:"projectId"`
	AgentID    string `json:"agentId"`
	EntitySlug string `json:"entitySlug"`
	Action     string `json:"action"`
	Payload    string `json:"payload"`
	Signature  string `json:"-"`
	KeyID      string `json:"keyId,omitempty"`
	Status     string `json:"status"`
	OK         bool   `json:"ok"`
	Output     string `json:"output,omitempty"`
	CreatedBy  string `json:"createdBy"`
	CreatedAt  Time   `json:"createdAt"`
	ExpiresAt  Time   `json:"expiresAt"`
	FinishedAt Time   `json:"finishedAt"`
}

const jobCols = `id, project_id, agent_id, entity_slug, action, payload, signature, key_id, status, ok, output, created_by, created_at, expires_at, finished_at`

func scanJob(r interface{ Scan(...any) error }) (AgentJob, error) {
	var j AgentJob
	err := r.Scan(&j.ID, &j.ProjectID, &j.AgentID, &j.EntitySlug, &j.Action, &j.Payload, &j.Signature, &j.KeyID, &j.Status, &j.OK, &j.Output, &j.CreatedBy, &j.CreatedAt, &j.ExpiresAt, &j.FinishedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return j, ErrNotFound
	}
	return j, err
}

func (d *DB) jobs(ctx context.Context, where string, args ...any) ([]AgentJob, error) {
	rows, err := d.Query(ctx, `SELECT `+jobCols+` FROM agent_jobs WHERE `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AgentJob{}
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// CreateAgentJob stores an unsigned job.
func (d *DB) CreateAgentJob(ctx context.Context, j AgentJob) error {
	_, err := d.Exec(ctx, `INSERT INTO agent_jobs (id, tenant_id, project_id, agent_id, entity_slug, action, payload, status, created_by, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, j.ID, TenantOf(ctx), j.ProjectID, j.AgentID, j.EntitySlug, j.Action, j.Payload, j.Status, j.CreatedBy, j.CreatedAt, j.ExpiresAt)
	return err
}

// GetAgentJob loads a job of a project.
func (d *DB) GetAgentJob(ctx context.Context, projectID, id string) (AgentJob, error) {
	return scanJob(d.QueryRow(ctx, `SELECT `+jobCols+` FROM agent_jobs WHERE project_id = ? AND id = ?`, projectID, id))
}

// ApproveAgentJob stores the signature of an unsigned job.
func (d *DB) ApproveAgentJob(ctx context.Context, id, keyID, signature string) error {
	res, err := d.Exec(ctx, `UPDATE agent_jobs SET status = 'approved', key_id = ?, signature = ? WHERE id = ? AND status = 'unsigned'`, keyID, signature, id)
	return affected(res, err)
}

// TakeAgentJobs returns approved, unexpired jobs of an agent and marks them sent.
func (d *DB) TakeAgentJobs(ctx context.Context, agentID string, now Time) ([]AgentJob, error) {
	var out []AgentJob
	err := d.InTx(ctx, func(tx *Tx) error {
		rows, err := tx.Query(ctx, `SELECT `+jobCols+` FROM agent_jobs WHERE agent_id = ? AND status = 'approved' AND expires_at > ? ORDER BY created_at`, agentID, now)
		if err != nil {
			return err
		}
		for rows.Next() {
			j, err := scanJob(rows)
			if err != nil {
				rows.Close()
				return err
			}
			out = append(out, j)
		}
		rows.Close()
		for _, j := range out {
			if _, err := tx.Exec(ctx, `UPDATE agent_jobs SET status = 'sent' WHERE id = ?`, j.ID); err != nil {
				return err
			}
		}
		return nil
	})
	return out, err
}

// FinishAgentJob stores the result an agent reported for its own job.
func (d *DB) FinishAgentJob(ctx context.Context, agentID, id string, ok bool, output string, now Time) error {
	status := "failed"
	if ok {
		status = "done"
	}
	res, err := d.Exec(ctx, `UPDATE agent_jobs SET status = ?, ok = ?, output = ?, finished_at = ? WHERE agent_id = ? AND id = ? AND status = 'sent'`,
		status, ok, output, now, agentID, id)
	return affected(res, err)
}

// EntityJobs lists the latest jobs of an entry.
func (d *DB) EntityJobs(ctx context.Context, projectID, slug string, limit int) ([]AgentJob, error) {
	return d.jobs(ctx, `project_id = ? AND entity_slug = ? ORDER BY created_at DESC LIMIT ?`, projectID, slug, limit)
}

// AgentByID loads an agent of a project.
func (d *DB) AgentByID(ctx context.Context, projectID, id string) (Agent, error) {
	return scanAgent(d.QueryRow(ctx, `SELECT `+agentCols+` FROM agents WHERE project_id = ? AND id = ?`, projectID, id))
}

// AgentHostname returns the hostname of an agent of the tenant ("" if unknown).
func (d *DB) AgentHostname(ctx context.Context, id string) string {
	var h string
	_ = d.QueryRow(ctx, `SELECT hostname FROM agents WHERE id = ?`, id).Scan(&h)
	return h
}
