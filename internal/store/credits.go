package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// AI credits: lots per person or organization (ADR-0080).

// CreditLot is credit that was bought or granted.
type CreditLot struct {
	ID        string `json:"id"`
	OwnerKind string `json:"ownerKind"` // user | org
	OwnerID   string `json:"ownerId"`
	OwnerName string `json:"ownerName,omitempty"`
	// Amount and Remaining in micro-dollars (in-app prices).
	Amount    int64  `json:"amount"`
	Remaining int64  `json:"remaining"`
	ExpiresAt *Time  `json:"expiresAt,omitempty"`
	Source    string `json:"source"` // purchase | grant
	SourceRef string `json:"sourceRef,omitempty"`
	Note      string `json:"note,omitempty"`
	CreatedBy string `json:"-"`
	CreatedAt Time   `json:"createdAt"`
}

const creditCols = `l.id, l.owner_kind, l.owner_id, COALESCE(u.name, o.name, ''), l.amount, l.remaining, l.expires_at, l.source, l.source_ref, l.note, l.created_by, l.created_at`

const creditFrom = ` FROM ai_credit_lots l LEFT JOIN users u ON l.owner_kind = 'user' AND u.id = l.owner_id LEFT JOIN organizations o ON l.owner_kind = 'org' AND o.id = l.owner_id`

func scanCreditLot(r interface{ Scan(...any) error }) (CreditLot, error) {
	var l CreditLot
	var exp sql.Null[Time]
	err := r.Scan(&l.ID, &l.OwnerKind, &l.OwnerID, &l.OwnerName, &l.Amount, &l.Remaining, &exp, &l.Source, &l.SourceRef, &l.Note, &l.CreatedBy, &l.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	if exp.Valid {
		l.ExpiresAt = &exp.V
	}
	return l, err
}

// AddCreditLot stores a lot.
func (d *DB) AddCreditLot(ctx context.Context, l CreditLot) error {
	var exp any
	if l.ExpiresAt != nil {
		exp = *l.ExpiresAt
	}
	_, err := d.Exec(ctx, `INSERT INTO ai_credit_lots (id, tenant_id, owner_kind, owner_id, amount, remaining, expires_at, source, source_ref, note, created_by, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		l.ID, TenantOf(ctx), l.OwnerKind, l.OwnerID, l.Amount, l.Remaining, exp, l.Source, l.SourceRef, l.Note, l.CreatedBy, l.CreatedAt)
	return err
}

func (d *DB) creditLots(ctx context.Context, where string, args ...any) ([]CreditLot, error) {
	rows, err := d.Query(ctx, `SELECT `+creditCols+creditFrom+` WHERE `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CreditLot{}
	for rows.Next() {
		l, err := scanCreditLot(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// CreditLots lists an owner's lots, the ones that expire first first
// (all = also used up and expired ones).
func (d *DB) CreditLots(ctx context.Context, kind, ownerID string, all bool, now time.Time) ([]CreditLot, error) {
	if all {
		return d.creditLots(ctx, `l.owner_kind = ? AND l.owner_id = ? ORDER BY l.created_at DESC LIMIT 200`, kind, ownerID)
	}
	return d.creditLots(ctx, `l.owner_kind = ? AND l.owner_id = ? AND l.remaining > 0 AND (l.expires_at IS NULL OR l.expires_at > ?)
		ORDER BY CASE WHEN l.expires_at IS NULL THEN 1 ELSE 0 END, l.expires_at, l.created_at`, kind, ownerID, Time{Time: now})
}

// CreditBalance sums an owner's usable credit.
func (d *DB) CreditBalance(ctx context.Context, kind, ownerID string, now time.Time) (int64, error) {
	var n int64
	err := d.QueryRow(ctx, `SELECT COALESCE(SUM(remaining), 0) FROM ai_credit_lots WHERE owner_kind = ? AND owner_id = ? AND remaining > 0
		AND (expires_at IS NULL OR expires_at > ?)`, kind, ownerID, Time{Time: now}).Scan(&n)
	return n, err
}

// UseCredit takes up to amount from an owner's lots (the lot that expires
// first first) and returns what it took.
func (d *DB) UseCredit(ctx context.Context, kind, ownerID string, amount int64, now time.Time) (int64, error) {
	if amount <= 0 {
		return 0, nil
	}
	taken := int64(0)
	err := d.InTx(ctx, func(tx *Tx) error {
		rows, err := tx.Query(ctx, `SELECT id, remaining FROM ai_credit_lots WHERE owner_kind = ? AND owner_id = ? AND remaining > 0 AND (expires_at IS NULL OR expires_at > ?)
			ORDER BY CASE WHEN expires_at IS NULL THEN 1 ELSE 0 END, expires_at, created_at`, kind, ownerID, Time{Time: now})
		if err != nil {
			return err
		}
		type lot struct {
			id   string
			left int64
		}
		var lots []lot
		for rows.Next() {
			var l lot
			if err := rows.Scan(&l.id, &l.left); err != nil {
				rows.Close()
				return err
			}
			lots = append(lots, l)
		}
		rows.Close()
		for _, l := range lots {
			if taken >= amount {
				break
			}
			take := min(l.left, amount-taken)
			res, err := tx.Exec(ctx, `UPDATE ai_credit_lots SET remaining = remaining - ? WHERE id = ? AND remaining >= ?`, take, l.id, take)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n == 1 {
				taken += take
			}
		}
		return nil
	})
	return taken, err
}

// CreditLotBySource finds the lot of a purchase or grant.
func (d *DB) CreditLotBySource(ctx context.Context, source, ref string) (CreditLot, error) {
	lots, err := d.creditLots(ctx, `l.source = ? AND l.source_ref = ? LIMIT 1`, source, ref)
	if err != nil {
		return CreditLot{}, err
	}
	if len(lots) == 0 {
		return CreditLot{}, ErrNotFound
	}
	return lots[0], nil
}

// EmptyCreditLot sets what is left of a lot to zero (refunds).
func (d *DB) EmptyCreditLot(ctx context.Context, id string) error {
	_, err := d.Exec(ctx, `UPDATE ai_credit_lots SET remaining = 0 WHERE id = ?`, id)
	return err
}

// RecentGrants lists the latest granted lots (administration).
func (d *DB) RecentGrants(ctx context.Context, limit int) ([]CreditLot, error) {
	return d.creditLots(ctx, `l.source = 'grant' ORDER BY l.created_at DESC LIMIT ?`, limit)
}
