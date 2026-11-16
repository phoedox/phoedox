package store

import (
	"context"
	"time"

	"github.com/phoedox/phoedox/internal/botcheck"
)

// Storage of the bot check (internal/botcheck).

// MarkBotUsed records a one-time id; false means it was used before.
func (d *DB) MarkBotUsed(ctx context.Context, id string, until time.Time) (bool, error) {
	res, err := d.Exec(ctx, `INSERT INTO bot_used (id, tenant_id, until) VALUES (?, ?, ?) ON CONFLICT DO NOTHING`, id, TenantOf(ctx), Time{until})
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// AddBotEvent writes one outcome to the log.
func (d *DB) AddBotEvent(ctx context.Context, e botcheck.Event) error {
	_, err := d.Exec(ctx, `INSERT INTO bot_events (id, tenant_id, place, policy, outcome, reason, ip_hash, solve_ms, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID, TenantOf(ctx), e.Place, e.Policy, e.Outcome, e.Reason, e.IPHash, e.SolveMS, Time{e.At})
	return err
}

// BotEvents lists events since a time (outcome "" = all), newest first.
func (d *DB) BotEvents(ctx context.Context, since time.Time, outcome string, limit int) ([]botcheck.Event, error) {
	q := `SELECT id, place, policy, outcome, reason, ip_hash, solve_ms, created_at FROM bot_events WHERE created_at >= ?`
	args := []any{Time{since}}
	if outcome != "" {
		q += ` AND outcome = ?`
		args = append(args, outcome)
	}
	rows, err := d.Query(ctx, q+` ORDER BY created_at DESC LIMIT ?`, append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []botcheck.Event{}
	for rows.Next() {
		var e botcheck.Event
		var at Time
		if err := rows.Scan(&e.ID, &e.Place, &e.Policy, &e.Outcome, &e.Reason, &e.IPHash, &e.SolveMS, &at); err != nil {
			return nil, err
		}
		e.At = at.Time
		out = append(out, e)
	}
	return out, rows.Err()
}

// PurgeBot removes events and one-time ids older than a time.
func (d *DB) PurgeBot(ctx context.Context, before time.Time) error {
	if _, err := d.Exec(ctx, `DELETE FROM bot_events WHERE created_at < ?`, Time{before}); err != nil {
		return err
	}
	_, err := d.Exec(ctx, `DELETE FROM bot_used WHERE until < ?`, Time{time.Now()})
	return err
}
