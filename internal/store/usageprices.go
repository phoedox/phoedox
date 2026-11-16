package store

import (
	"context"
	"encoding/json"
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
