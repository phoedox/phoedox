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
	// Model is the model ref ("provider/model") used last.
	Model     string `json:"model,omitempty"`
	CreatedAt Time   `json:"createdAt"`
	UpdatedAt Time   `json:"updatedAt"`
}

// AIChatMessage is one message of a chat in the provider's own format.
// Messages are only appended, never changed (reasoning blocks stay valid).
type AIChatMessage struct {
	Seq       int             `json:"seq"`
	Role      string          `json:"role"` // user | assistant
	Content   json.RawMessage `json:"content"`
	Model     string          `json:"model,omitempty"` // assistant messages: the model that wrote it
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
	ProviderID   string
	OwnKey       bool // paid with the organization's own key
	Model        string
	InputTokens  int64
	OutputTokens int64
	CacheRead    int64
	CacheWrite   int64
	CostMicros   int64 // what the provider charges, US dollars × 10⁶
	ChargeMicros int64 // what counts against budgets (in-app price)
	CreatedAt    Time
}

const aiChatCols = `id, project_id, user_id, COALESCE(document_id, ''), title, model, created_at, updated_at`

func scanAIChat(r interface{ Scan(...any) error }) (AIChat, error) {
	var c AIChat
	err := r.Scan(&c.ID, &c.ProjectID, &c.UserID, &c.DocumentID, &c.Title, &c.Model, &c.CreatedAt, &c.UpdatedAt)
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
	_, err := d.Exec(ctx, `INSERT INTO ai_chats (id, tenant_id, project_id, user_id, document_id, title, model, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, TenantOf(ctx), c.ProjectID, c.UserID, doc, c.Title, c.Model, c.CreatedAt, c.UpdatedAt)
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

// SetAIChatModel remembers the model a chat used last.
func (d *DB) SetAIChatModel(ctx context.Context, id, model string) error {
	_, err := d.Exec(ctx, `UPDATE ai_chats SET model = ? WHERE id = ?`, model, id)
	return err
}

// DeleteAIChat removes a chat with its messages (the usage ledger stays).
func (d *DB) DeleteAIChat(ctx context.Context, userID, id string) error {
	res, err := d.Exec(ctx, `DELETE FROM ai_chats WHERE id = ? AND user_id = ?`, id, userID)
	return affected(res, err)
}

// AppendAIChatMessage adds a message at the end of a chat and returns its number.
func (d *DB) AppendAIChatMessage(ctx context.Context, chatID, role, model string, content json.RawMessage) (int, error) {
	var seq int
	now := Now()
	err := d.InTx(ctx, func(tx *Tx) error {
		if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(seq), 0) FROM ai_chat_messages WHERE chat_id = ?`, chatID).Scan(&seq); err != nil {
			return err
		}
		seq++
		if _, err := tx.Exec(ctx, `INSERT INTO ai_chat_messages (chat_id, seq, tenant_id, role, content, model, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			chatID, seq, TenantOf(ctx), role, string(content), model, now); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE ai_chats SET updated_at = ? WHERE id = ?`, now, chatID)
		return err
	})
	return seq, err
}

// AIChatMessages returns all messages of a chat in order.
func (d *DB) AIChatMessages(ctx context.Context, chatID string) ([]AIChatMessage, error) {
	rows, err := d.Query(ctx, `SELECT seq, role, content, model, created_at FROM ai_chat_messages WHERE chat_id = ? ORDER BY seq`, chatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AIChatMessage{}
	for rows.Next() {
		var m AIChatMessage
		var content string
		if err := rows.Scan(&m.Seq, &m.Role, &content, &m.Model, &m.CreatedAt); err != nil {
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
	_, err := d.Exec(ctx, `INSERT INTO ai_usage (id, tenant_id, org_id, user_id, project_id, chat_id, provider_id, own_key, model, input_tokens, output_tokens, cache_read, cache_write, cost_micros, charge_micros, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		u.ID, TenantOf(ctx), u.OrgID, u.UserID, u.ProjectID, u.ChatID, u.ProviderID, u.OwnKey, u.Model, u.InputTokens, u.OutputTokens, u.CacheRead, u.CacheWrite, u.CostMicros, u.ChargeMicros, u.CreatedAt)
	return err
}

// AIUsageSince sums the charged amount (micro-dollars, in-app prices) since
// a time, for an organization ("" = projects outside organizations) and
// optionally one user; instanceOnly leaves out calls paid with the
// organization's own keys.
func (d *DB) AIUsageSince(ctx context.Context, orgID, userID string, since time.Time, instanceOnly bool) (int64, error) {
	q := `SELECT COALESCE(SUM(charge_micros), 0) FROM ai_usage WHERE org_id = ? AND created_at >= ?`
	args := []any{orgID, Time{since}}
	if instanceOnly {
		q += ` AND own_key = ?`
		args = append(args, false)
	}
	if userID != "" {
		q += ` AND user_id = ?`
		args = append(args, userID)
	}
	var n int64
	err := d.QueryRow(ctx, q, args...).Scan(&n)
	return n, err
}

// AIUsageTotalSince sums the cost the instance paid (without organizations'
// own keys) since a time.
func (d *DB) AIUsageTotalSince(ctx context.Context, since time.Time) (int64, error) {
	var n int64
	err := d.QueryRow(ctx, `SELECT COALESCE(SUM(cost_micros), 0) FROM ai_usage WHERE created_at >= ? AND own_key = ?`, Time{since}, false).Scan(&n)
	return n, err
}

// AIChargedTotalSince sums what was charged on the instance's models since
// a time (in-app prices).
func (d *DB) AIChargedTotalSince(ctx context.Context, since time.Time) (int64, error) {
	var n int64
	err := d.QueryRow(ctx, `SELECT COALESCE(SUM(charge_micros), 0) FROM ai_usage WHERE created_at >= ? AND own_key = ?`, Time{since}, false).Scan(&n)
	return n, err
}

// AIModel is a model of a provider with its prices in US dollars per
// million tokens (0 = free, e.g. a local model): what the provider charges,
// and optionally an own price in Phoedox (Charge, nil = the provider's).
type AIModel struct {
	ID         string   `json:"id"`
	Label      string   `json:"label,omitempty"`
	Input      float64  `json:"input"`
	Output     float64  `json:"output"`
	CacheRead  float64  `json:"cacheRead"`
	CacheWrite float64  `json:"cacheWrite"`
	Charge     *AIPrice `json:"charge,omitempty"`
}

// AIPrice is a price in US dollars per million tokens.
type AIPrice struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cacheRead"`
	CacheWrite float64 `json:"cacheWrite"`
}

// AIProvider is a model API set up by system admins (OrgID "") or an
// organization (ADR-0067). APIKey is sealed and never leaves the server.
type AIProvider struct {
	ID      string `json:"id"`
	OrgID   string `json:"orgId,omitempty"`
	Name    string `json:"name"`
	Kind    string `json:"kind"` // anthropic | openai
	BaseURL string `json:"baseUrl"`
	APIKey  string `json:"-"`
	HasKey  bool   `json:"hasKey"`
	// BuiltIn is the provider from PHOEDOX_AI_* (read only).
	BuiltIn   bool      `json:"builtIn,omitempty"`
	Effort    string    `json:"effort,omitempty"`
	Models    []AIModel `json:"models"`
	Enabled   bool      `json:"enabled"`
	CreatedBy string    `json:"createdBy"`
	CreatedAt Time      `json:"createdAt"`
	UpdatedAt Time      `json:"updatedAt"`
}

const aiProviderCols = `id, org_id, name, kind, base_url, api_key, effort, models, enabled, created_by, created_at, updated_at`

func scanAIProvider(r interface{ Scan(...any) error }) (AIProvider, error) {
	var p AIProvider
	var models string
	err := r.Scan(&p.ID, &p.OrgID, &p.Name, &p.Kind, &p.BaseURL, &p.APIKey, &p.Effort, &models, &p.Enabled, &p.CreatedBy, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	_ = json.Unmarshal([]byte(models), &p.Models)
	if p.Models == nil {
		p.Models = []AIModel{}
	}
	p.HasKey = p.APIKey != ""
	return p, err
}

// AIProviders lists the providers of the instance (orgID "") or an organization.
func (d *DB) AIProviders(ctx context.Context, orgID string) ([]AIProvider, error) {
	rows, err := d.Query(ctx, `SELECT `+aiProviderCols+` FROM ai_providers WHERE org_id = ? ORDER BY created_at`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AIProvider{}
	for rows.Next() {
		p, err := scanAIProvider(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetAIProvider returns a provider of the instance or an organization.
func (d *DB) GetAIProvider(ctx context.Context, orgID, id string) (AIProvider, error) {
	return scanAIProvider(d.QueryRow(ctx, `SELECT `+aiProviderCols+` FROM ai_providers WHERE org_id = ? AND id = ?`, orgID, id))
}

// SaveAIProvider creates or replaces a provider.
func (d *DB) SaveAIProvider(ctx context.Context, p AIProvider) error {
	models, _ := json.Marshal(p.Models)
	return d.InTx(ctx, func(tx *Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM ai_providers WHERE id = ?`, p.ID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO ai_providers (id, tenant_id, org_id, name, kind, base_url, api_key, effort, models, enabled, created_by, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			p.ID, TenantOf(ctx), p.OrgID, p.Name, p.Kind, p.BaseURL, p.APIKey, p.Effort, string(models), p.Enabled, p.CreatedBy, p.CreatedAt, p.UpdatedAt)
		return err
	})
}

// DeleteAIProvider removes a provider.
func (d *DB) DeleteAIProvider(ctx context.Context, orgID, id string) error {
	res, err := d.Exec(ctx, `DELETE FROM ai_providers WHERE org_id = ? AND id = ?`, orgID, id)
	return affected(res, err)
}

// UsageFilter limits an AI usage breakdown. Empty fields do not filter;
// AnyOrg false with OrgID "" means projects outside organizations.
type UsageFilter struct {
	OrgID     string
	AnyOrg    bool
	ProjectID string
	UserID    string
	Since     time.Time
}

// UsageGroup is one line of a breakdown: the key (a project, user, model
// or organization id) with its calls, tokens, charge and cost.
type UsageGroup struct {
	Key          string `json:"key"`
	Calls        int64  `json:"calls"`
	InputTokens  int64  `json:"inputTokens"`
	OutputTokens int64  `json:"outputTokens"`
	ChargeMicros int64  `json:"chargeMicros"`
	CostMicros   int64  `json:"costMicros"`
	OwnKey       int64  `json:"-"` // charge on the organization's own keys
}

var usageGroupCols = map[string]string{"project": "project_id", "user": "user_id", "model": "model", "org": "org_id"}

// AIUsageBy sums AI usage grouped by project, user, model or org, largest first.
func (d *DB) AIUsageBy(ctx context.Context, f UsageFilter, by string) ([]UsageGroup, error) {
	col, ok := usageGroupCols[by]
	if !ok {
		return nil, errors.New("unknown grouping")
	}
	q := `SELECT COALESCE(` + col + `, ''), COUNT(*), COALESCE(SUM(input_tokens), 0), COALESCE(SUM(output_tokens), 0), COALESCE(SUM(charge_micros), 0), COALESCE(SUM(cost_micros), 0),
		COALESCE(SUM(CASE WHEN own_key THEN charge_micros ELSE 0 END), 0) FROM ai_usage WHERE created_at >= ?`
	args := []any{Time{f.Since}}
	if !f.AnyOrg {
		q += ` AND org_id = ?`
		args = append(args, f.OrgID)
	}
	if f.ProjectID != "" {
		q += ` AND project_id = ?`
		args = append(args, f.ProjectID)
	}
	if f.UserID != "" {
		q += ` AND user_id = ?`
		args = append(args, f.UserID)
	}
	rows, err := d.Query(ctx, q+` GROUP BY `+col+` ORDER BY 5 DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []UsageGroup{}
	for rows.Next() {
		var g UsageGroup
		if err := rows.Scan(&g.Key, &g.Calls, &g.InputTokens, &g.OutputTokens, &g.ChargeMicros, &g.CostMicros, &g.OwnKey); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}
