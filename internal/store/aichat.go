package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// AIChat is a conversation of one user with the AI in a project (ADR-0066).
type AIChat struct {
	ID         string `json:"id"`
	ProjectID  string `json:"projectId"`
	UserID     string `json:"userId"`
	DocumentID string `json:"documentId,omitempty"`
	Title      string `json:"title"`
	CreatedAt  Time   `json:"createdAt"`
	UpdatedAt  Time   `json:"updatedAt"`
}

// AIChatMessage is one message of a chat in the provider's own format.
// Messages are only appended, never changed (reasoning blocks stay valid).
type AIChatMessage struct {
	Seq       int             `json:"seq"`
	Role      string          `json:"role"` // user | assistant
	Content   json.RawMessage `json:"content"`
	CreatedAt Time            `json:"createdAt"`
}

// AIProposal records what a person did with a proposal of the AI
// (e.g. cards to create), so it is applied only once.
type AIProposal struct {
	ToolUseID string          `json:"toolUseId"`
	Status    string          `json:"status"` // accepted | rejected
	Result    json.RawMessage `json:"result,omitempty"`
	DecidedBy string          `json:"decidedBy"`
	DecidedAt Time            `json:"decidedAt"`
}

// AIUsage is one model call in the usage ledger.
type AIUsage struct {
	ID           string
	OrgID        string
	UserID       string
	ProjectID    string
	ChatID       string
	Model        string
	InputTokens  int64
	OutputTokens int64
	CacheRead    int64
	CacheWrite   int64
	CostMicros   int64 // US dollars × 10⁶
	CreatedAt    Time
}

const aiChatCols = `id, project_id, user_id, COALESCE(document_id, ''), title, created_at, updated_at`

func scanAIChat(r interface{ Scan(...any) error }) (AIChat, error) {
	var c AIChat
	err := r.Scan(&c.ID, &c.ProjectID, &c.UserID, &c.DocumentID, &c.Title, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	return c, err
}

// CreateAIChat stores a new chat.
func (d *DB) CreateAIChat(ctx context.Context, c AIChat) error {
	var doc any
	if c.DocumentID != "" {
		doc = c.DocumentID
	}
	_, err := d.Exec(ctx, `INSERT INTO ai_chats (id, tenant_id, project_id, user_id, document_id, title, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, TenantOf(ctx), c.ProjectID, c.UserID, doc, c.Title, c.CreatedAt, c.UpdatedAt)
	return err
}

// GetAIChat returns a chat of a user.
func (d *DB) GetAIChat(ctx context.Context, userID, id string) (AIChat, error) {
	return scanAIChat(d.QueryRow(ctx, `SELECT `+aiChatCols+` FROM ai_chats WHERE id = ? AND user_id = ?`, id, userID))
}

// AIChats lists the chats of a user in a project, newest first.
func (d *DB) AIChats(ctx context.Context, userID, projectID string, limit int) ([]AIChat, error) {
	rows, err := d.Query(ctx, `SELECT `+aiChatCols+` FROM ai_chats WHERE user_id = ? AND project_id = ? ORDER BY updated_at DESC LIMIT ?`, userID, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AIChat{}
	for rows.Next() {
		c, err := scanAIChat(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// RenameAIChat changes the title of a chat.
func (d *DB) RenameAIChat(ctx context.Context, id, title string) error {
	res, err := d.Exec(ctx, `UPDATE ai_chats SET title = ? WHERE id = ?`, title, id)
	return affected(res, err)
}

// DeleteAIChat removes a chat with its messages (the usage ledger stays).
func (d *DB) DeleteAIChat(ctx context.Context, userID, id string) error {
	res, err := d.Exec(ctx, `DELETE FROM ai_chats WHERE id = ? AND user_id = ?`, id, userID)
	return affected(res, err)
}

// AppendAIChatMessage adds a message at the end of a chat and returns its number.
func (d *DB) AppendAIChatMessage(ctx context.Context, chatID, role string, content json.RawMessage) (int, error) {
	var seq int
	now := Now()
	err := d.InTx(ctx, func(tx *Tx) error {
		if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(seq), 0) FROM ai_chat_messages WHERE chat_id = ?`, chatID).Scan(&seq); err != nil {
			return err
		}
		seq++
		if _, err := tx.Exec(ctx, `INSERT INTO ai_chat_messages (chat_id, seq, tenant_id, role, content, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
			chatID, seq, TenantOf(ctx), role, string(content), now); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE ai_chats SET updated_at = ? WHERE id = ?`, now, chatID)
		return err
	})
	return seq, err
}

// AIChatMessages returns all messages of a chat in order.
func (d *DB) AIChatMessages(ctx context.Context, chatID string) ([]AIChatMessage, error) {
	rows, err := d.Query(ctx, `SELECT seq, role, content, created_at FROM ai_chat_messages WHERE chat_id = ? ORDER BY seq`, chatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AIChatMessage{}
	for rows.Next() {
		var m AIChatMessage
		var content string
		if err := rows.Scan(&m.Seq, &m.Role, &content, &m.CreatedAt); err != nil {
			return nil, err
		}
		m.Content = json.RawMessage(content)
		out = append(out, m)
	}
	return out, rows.Err()
}

// SaveAIProposal records a decision on a proposal; ErrConflict if it was
// decided before.
func (d *DB) SaveAIProposal(ctx context.Context, chatID string, p AIProposal) error {
	_, err := d.Exec(ctx, `INSERT INTO ai_chat_proposals (chat_id, tool_use_id, tenant_id, status, result, decided_by, decided_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		chatID, p.ToolUseID, TenantOf(ctx), p.Status, string(p.Result), p.DecidedBy, p.DecidedAt)
	if err != nil && isUniqueViolation(err) {
		return ErrConflict
	}
	return err
}

// AIProposals returns the decided proposals of a chat by tool use id.
func (d *DB) AIProposals(ctx context.Context, chatID string) (map[string]AIProposal, error) {
	rows, err := d.Query(ctx, `SELECT tool_use_id, status, result, decided_by, decided_at FROM ai_chat_proposals WHERE chat_id = ?`, chatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]AIProposal{}
	for rows.Next() {
		var p AIProposal
		var res string
		if err := rows.Scan(&p.ToolUseID, &p.Status, &res, &p.DecidedBy, &p.DecidedAt); err != nil {
			return nil, err
		}
		if res != "" {
			p.Result = json.RawMessage(res)
		}
		out[p.ToolUseID] = p
	}
	return out, rows.Err()
}

// AddAIUsage writes one entry of the usage ledger.
func (d *DB) AddAIUsage(ctx context.Context, u AIUsage) error {
	_, err := d.Exec(ctx, `INSERT INTO ai_usage (id, tenant_id, org_id, user_id, project_id, chat_id, model, input_tokens, output_tokens, cache_read, cache_write, cost_micros, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		u.ID, TenantOf(ctx), u.OrgID, u.UserID, u.ProjectID, u.ChatID, u.Model, u.InputTokens, u.OutputTokens, u.CacheRead, u.CacheWrite, u.CostMicros, u.CreatedAt)
	return err
}

// AIUsageSince sums the cost (micro-dollars) since a time, for an
// organization ("" = projects outside organizations) and optionally one user.
func (d *DB) AIUsageSince(ctx context.Context, orgID, userID string, since time.Time) (int64, error) {
	q := `SELECT COALESCE(SUM(cost_micros), 0) FROM ai_usage WHERE org_id = ? AND created_at >= ?`
	args := []any{orgID, Time{since}}
	if userID != "" {
		q += ` AND user_id = ?`
		args = append(args, userID)
	}
	var n int64
	err := d.QueryRow(ctx, q, args...).Scan(&n)
	return n, err
}

// AIUsageTotalSince sums the cost of the whole instance (tenant) since a time.
func (d *DB) AIUsageTotalSince(ctx context.Context, since time.Time) (int64, error) {
	var n int64
	err := d.QueryRow(ctx, `SELECT COALESCE(SUM(cost_micros), 0) FROM ai_usage WHERE created_at >= ?`, Time{since}).Scan(&n)
	return n, err
}
