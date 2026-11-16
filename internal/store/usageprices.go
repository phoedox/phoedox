package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// OrgAIPrice is an organization's own token prices from a moment on
// (usage-prices-concept): a discount or markup in percent on the in-app
// prices, plus optional own prices for single models (US dollars per
// million tokens). Rows are kept as the price history.
type OrgAIPrice struct {
	ID            string             `json:"id"`
	OrgID         string             `json:"orgId"`
	Percent       float64            `json:"percent"`
	Models        map[string]AIPrice `json:"models"`
	Mode          string             `json:"mode"` // date | now | next_month | month_start
	EffectiveFrom Time               `json:"effectiveFrom"`
	CreatedBy     string             `json:"createdBy"`
	CreatedAt     Time               `json:"createdAt"`
}

// AddOrgAIPrice stores a price change.
func (d *DB) AddOrgAIPrice(ctx context.Context, p OrgAIPrice) error {
	models, _ := json.Marshal(p.Models)
	_, err := d.Exec(ctx, `INSERT INTO org_ai_prices (id, tenant_id, org_id, percent, models, mode, effective_from, created_by, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, TenantOf(ctx), p.OrgID, p.Percent, string(models), p.Mode, p.EffectiveFrom, p.CreatedBy, p.CreatedAt)
	return err
}

// OrgAIPrices lists an organization's price changes, newest first.
func (d *DB) OrgAIPrices(ctx context.Context, orgID string) ([]OrgAIPrice, error) {
	rows, err := d.Query(ctx, `SELECT id, org_id, percent, models, mode, effective_from, created_by, created_at FROM org_ai_prices WHERE org_id = ? ORDER BY effective_from DESC, created_at DESC`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []OrgAIPrice{}
	for rows.Next() {
		var p OrgAIPrice
		var models string
		if err := rows.Scan(&p.ID, &p.OrgID, &p.Percent, &models, &p.Mode, &p.EffectiveFrom, &p.CreatedBy, &p.CreatedAt); err != nil {
			return nil, err
		}
		p.Models = map[string]AIPrice{}
		_ = json.Unmarshal([]byte(models), &p.Models)
		out = append(out, p)
	}
	return out, rows.Err()
}

// DeleteOrgAIPrice removes a planned price change (one that has not taken
// effect yet).
func (d *DB) DeleteOrgAIPrice(ctx context.Context, orgID, id string, now time.Time) error {
	res, err := d.Exec(ctx, `DELETE FROM org_ai_prices WHERE org_id = ? AND id = ? AND effective_from > ?`, orgID, id, Time{now})
	return affected(res, err)
}

// AIUsageRow is a ledger entry as needed to recalculate its charge.
type AIUsageRow struct {
	ID           string
	UserID       string
	Model        string
	OwnKey       bool
	InputTokens  int64
	OutputTokens int64
	CacheRead    int64
	CacheWrite   int64
	ListMicros   int64
	ChargeMicros int64
	CreatedAt    Time
}

// AIUsageRows lists an organization's ledger entries since a time.
func (d *DB) AIUsageRows(ctx context.Context, orgID string, since time.Time) ([]AIUsageRow, error) {
	rows, err := d.Query(ctx, `SELECT id, user_id, model, own_key, input_tokens, output_tokens, cache_read, cache_write, list_micros, charge_micros, created_at
		FROM ai_usage WHERE org_id = ? AND created_at >= ? ORDER BY created_at`, orgID, Time{since})
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AIUsageRow{}
	for rows.Next() {
		var r AIUsageRow
		if err := rows.Scan(&r.ID, &r.UserID, &r.Model, &r.OwnKey, &r.InputTokens, &r.OutputTokens, &r.CacheRead, &r.CacheWrite, &r.ListMicros, &r.ChargeMicros, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// SetAIUsageCharges changes the charged amount of ledger entries.
func (d *DB) SetAIUsageCharges(ctx context.Context, charges map[string]int64) error {
	return d.InTx(ctx, func(tx *Tx) error {
		for id, c := range charges {
			if _, err := tx.Exec(ctx, `UPDATE ai_usage SET charge_micros = ? WHERE id = ?`, c, id); err != nil {
				return err
			}
		}
		return nil
	})
}

// AIChatCharged sums what a chat was charged and its tokens.
func (d *DB) AIChatCharged(ctx context.Context, chatID string) (charge, tokens int64, err error) {
	err = d.QueryRow(ctx, `SELECT COALESCE(SUM(charge_micros), 0), COALESCE(SUM(input_tokens + output_tokens), 0) FROM ai_usage WHERE chat_id = ?`, chatID).Scan(&charge, &tokens)
	return charge, tokens, err
}

// AITokensSince sums the tokens of an organization ("" = projects outside
// organizations), optionally of one user, since a time.
func (d *DB) AITokensSince(ctx context.Context, orgID, userID string, since time.Time) (int64, error) {
	q := `SELECT COALESCE(SUM(input_tokens + output_tokens), 0) FROM ai_usage WHERE org_id = ? AND created_at >= ?`
	args := []any{orgID, Time{since}}
	if userID != "" {
		q += ` AND user_id = ?`
		args = append(args, userID)
	}
	var n int64
	err := d.QueryRow(ctx, q, args...).Scan(&n)
	return n, err
}

// MarkBudgetWarning records that a warning for a budget, month and level
// was sent; false means it was sent before.
func (d *DB) MarkBudgetWarning(ctx context.Context, scope, month string, level int) (bool, error) {
	res, err := d.Exec(ctx, `INSERT INTO budget_warnings (tenant_id, scope, month, level, sent_at) VALUES (?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`, TenantOf(ctx), scope, month, level, Now())
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// MemberAI is a member's own AI limit in an organization (Budget nil = the
// organization's default) and an extra amount granted for one month.
type MemberAI struct {
	UserID     string `json:"userId"`
	Budget     *int   `json:"budgetCents"`
	ExtraCents int64  `json:"extraCents"`
	ExtraMonth string `json:"extraMonth"`
}

// OrgMemberAIs lists the AI limits of an organization's members.
func (d *DB) OrgMemberAIs(ctx context.Context, orgID string) (map[string]MemberAI, error) {
	rows, err := d.Query(ctx, `SELECT user_id, ai_budget_cents, ai_extra_cents, ai_extra_month FROM org_members WHERE org_id = ?`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]MemberAI{}
	for rows.Next() {
		var m MemberAI
		var b sql.NullInt64
		if err := rows.Scan(&m.UserID, &b, &m.ExtraCents, &m.ExtraMonth); err != nil {
			return nil, err
		}
		m.Budget = nullInt(b)
		out[m.UserID] = m
	}
	return out, rows.Err()
}

// OrgMemberAI is one member's AI limit (ErrNotFound for non-members).
func (d *DB) OrgMemberAI(ctx context.Context, orgID, userID string) (MemberAI, error) {
	m := MemberAI{UserID: userID}
	var b sql.NullInt64
	err := d.QueryRow(ctx, `SELECT ai_budget_cents, ai_extra_cents, ai_extra_month FROM org_members WHERE org_id = ? AND user_id = ?`, orgID, userID).Scan(&b, &m.ExtraCents, &m.ExtraMonth)
	if errors.Is(err, sql.ErrNoRows) {
		return m, ErrNotFound
	}
	m.Budget = nullInt(b)
	return m, err
}

// SetOrgMemberAI stores a member's AI limit and monthly extra.
func (d *DB) SetOrgMemberAI(ctx context.Context, orgID string, m MemberAI) error {
	res, err := d.Exec(ctx, `UPDATE org_members SET ai_budget_cents = ?, ai_extra_cents = ?, ai_extra_month = ? WHERE org_id = ? AND user_id = ?`,
		intOrNull(m.Budget), m.ExtraCents, m.ExtraMonth, orgID, m.UserID)
	return affected(res, err)
}

// BudgetRequest is a member's request for more AI usage.
type BudgetRequest struct {
	ID           string `json:"id"`
	OrgID        string `json:"orgId"`
	UserID       string `json:"userId"`
	UserName     string `json:"userName,omitempty"`
	Percent      int    `json:"percent"`
	Scope        string `json:"scope"` // month | permanent
	Reason       string `json:"reason"`
	Status       string `json:"status"` // pending | approved | rejected
	GrantedCents int64  `json:"grantedCents,omitempty"`
	Note         string `json:"note,omitempty"`
	DecidedBy    string `json:"decidedBy,omitempty"`
	DecidedAt    *Time  `json:"decidedAt,omitempty"`
	CreatedAt    Time   `json:"createdAt"`
}

const budgetRequestCols = `r.id, r.org_id, r.user_id, COALESCE(u.name, ''), r.percent, r.scope, r.reason, r.status, r.granted_cents, r.note, r.decided_by, r.decided_at, r.created_at`

func scanBudgetRequest(r interface{ Scan(...any) error }) (BudgetRequest, error) {
	var b BudgetRequest
	var decided sql.Null[Time]
	err := r.Scan(&b.ID, &b.OrgID, &b.UserID, &b.UserName, &b.Percent, &b.Scope, &b.Reason, &b.Status, &b.GrantedCents, &b.Note, &b.DecidedBy, &decided, &b.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return b, ErrNotFound
	}
	if decided.Valid {
		b.DecidedAt = &decided.V
	}
	return b, err
}

// AddBudgetRequest stores a request.
func (d *DB) AddBudgetRequest(ctx context.Context, b BudgetRequest) error {
	_, err := d.Exec(ctx, `INSERT INTO ai_budget_requests (id, tenant_id, org_id, user_id, percent, scope, reason, status, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		b.ID, TenantOf(ctx), b.OrgID, b.UserID, b.Percent, b.Scope, b.Reason, b.Status, b.CreatedAt)
	return err
}

// BudgetRequests lists an organization's requests (status "" = all; userID
// "" = everyone), newest first.
func (d *DB) BudgetRequests(ctx context.Context, orgID, userID, status string) ([]BudgetRequest, error) {
	q := `SELECT ` + budgetRequestCols + ` FROM ai_budget_requests r LEFT JOIN users u ON u.id = r.user_id WHERE r.org_id = ?`
	args := []any{orgID}
	if userID != "" {
		q += ` AND r.user_id = ?`
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
	out := []BudgetRequest{}
	for rows.Next() {
		b, err := scanBudgetRequest(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// GetBudgetRequest reads one request of an organization.
func (d *DB) GetBudgetRequest(ctx context.Context, orgID, id string) (BudgetRequest, error) {
	return scanBudgetRequest(d.QueryRow(ctx, `SELECT `+budgetRequestCols+` FROM ai_budget_requests r LEFT JOIN users u ON u.id = r.user_id WHERE r.org_id = ? AND r.id = ?`, orgID, id))
}

// DecideBudgetRequest stores the decision on a pending request.
func (d *DB) DecideBudgetRequest(ctx context.Context, b BudgetRequest) error {
	res, err := d.Exec(ctx, `UPDATE ai_budget_requests SET status = ?, granted_cents = ?, note = ?, decided_by = ?, decided_at = ?, percent = ?, scope = ? WHERE id = ? AND status = 'pending'`,
		b.Status, b.GrantedCents, b.Note, b.DecidedBy, b.DecidedAt, b.Percent, b.Scope, b.ID)
	return affected(res, err)
}
