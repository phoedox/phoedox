package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// Payments (ADR-0079): purchases and the provider's webhook events.

// Purchase is one checkout and what became of it.
type Purchase struct {
	ID           string `json:"id"`
	TenantID     string `json:"-"`
	UserID       string `json:"userId,omitempty"`
	UserName     string `json:"userName,omitempty"`
	OrgID        string `json:"orgId,omitempty"`
	Kind         string `json:"kind"` // what is sold: test, ai_credits, storage, domain …
	Item         string `json:"item,omitempty"`
	Description  string `json:"description,omitempty"`
	Amount       int64  `json:"amount"`
	Currency     string `json:"currency"`
	Recurring    string `json:"recurring,omitempty"`
	Status       string `json:"status"` // pending | paid | failed | expired | refunded | canceled
	Provider     string `json:"provider"`
	Mode         string `json:"mode"`
	CheckoutRef  string `json:"checkoutRef,omitempty"`
	PaymentRef   string `json:"paymentRef,omitempty"`
	Subscription string `json:"subscription,omitempty"`
	CustomerRef  string `json:"-"`
	// ReturnPath is where the buyer comes back after the checkout.
	ReturnPath string `json:"-"`
	CreatedAt  Time   `json:"createdAt"`
	UpdatedAt  Time   `json:"updatedAt"`
}

const purchaseCols = `p.id, p.tenant_id, p.user_id, COALESCE(u.name, ''), p.org_id, p.kind, p.item, p.description, p.amount, p.currency, p.recurring, p.status,
	p.provider, p.mode, p.checkout_ref, p.payment_ref, p.subscription_ref, p.customer_ref, p.return_path, p.created_at, p.updated_at`

const purchaseFrom = ` FROM purchases p LEFT JOIN users u ON u.id = p.user_id`

func scanPurchase(r interface{ Scan(...any) error }) (Purchase, error) {
	var p Purchase
	err := r.Scan(&p.ID, &p.TenantID, &p.UserID, &p.UserName, &p.OrgID, &p.Kind, &p.Item, &p.Description, &p.Amount, &p.Currency, &p.Recurring, &p.Status,
		&p.Provider, &p.Mode, &p.CheckoutRef, &p.PaymentRef, &p.Subscription, &p.CustomerRef, &p.ReturnPath, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return p, err
}

// SavePurchase creates or updates a purchase.
func (d *DB) SavePurchase(ctx context.Context, p Purchase) error {
	tenant := p.TenantID
	if tenant == "" {
		tenant = TenantOf(ctx)
	}
	_, err := d.Exec(ctx, `INSERT INTO purchases (id, tenant_id, user_id, org_id, kind, item, description, amount, currency, recurring, status, provider, mode,
		checkout_ref, payment_ref, subscription_ref, customer_ref, return_path, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET status = excluded.status, checkout_ref = excluded.checkout_ref, payment_ref = excluded.payment_ref,
		subscription_ref = excluded.subscription_ref, customer_ref = excluded.customer_ref, updated_at = excluded.updated_at`,
		p.ID, tenant, p.UserID, p.OrgID, p.Kind, p.Item, p.Description, p.Amount, strings.ToLower(p.Currency), p.Recurring, p.Status, p.Provider, p.Mode,
		p.CheckoutRef, p.PaymentRef, p.Subscription, p.CustomerRef, p.ReturnPath, p.CreatedAt, p.UpdatedAt)
	return err
}

// GetPurchase reads a purchase by id (in a system context also across tenants).
func (d *DB) GetPurchase(ctx context.Context, id string) (Purchase, error) {
	return scanPurchase(d.QueryRow(ctx, `SELECT `+purchaseCols+purchaseFrom+` WHERE p.id = ?`, id))
}

// PurchaseByCheckout finds a purchase by the provider's checkout reference.
func (d *DB) PurchaseByCheckout(ctx context.Context, ref string) (Purchase, error) {
	if ref == "" {
		return Purchase{}, ErrNotFound
	}
	return scanPurchase(d.QueryRow(ctx, `SELECT `+purchaseCols+purchaseFrom+` WHERE p.checkout_ref = ? ORDER BY p.created_at LIMIT 1`, ref))
}

// PurchaseBy finds a purchase by a provider reference (subscription or payment).
func (d *DB) PurchaseBy(ctx context.Context, subscription, payment string) (Purchase, error) {
	if subscription != "" {
		return scanPurchase(d.QueryRow(ctx, `SELECT `+purchaseCols+purchaseFrom+` WHERE p.subscription_ref = ? ORDER BY p.created_at LIMIT 1`, subscription))
	}
	if payment != "" {
		return scanPurchase(d.QueryRow(ctx, `SELECT `+purchaseCols+purchaseFrom+` WHERE p.payment_ref = ? ORDER BY p.created_at LIMIT 1`, payment))
	}
	return Purchase{}, ErrNotFound
}

// Purchases lists purchases, newest first (userID "" = all).
func (d *DB) Purchases(ctx context.Context, userID string, limit int) ([]Purchase, error) {
	q := `SELECT ` + purchaseCols + purchaseFrom
	args := []any{}
	if userID != "" {
		q += ` WHERE p.user_id = ?`
		args = append(args, userID)
	}
	q += ` ORDER BY p.created_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := d.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Purchase{}
	for rows.Next() {
		p, err := scanPurchase(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// CustomerOf is the provider's customer id from a person's latest purchase.
func (d *DB) CustomerOf(ctx context.Context, userID, provider string) (string, error) {
	var c string
	err := d.QueryRow(ctx, `SELECT customer_ref FROM purchases WHERE user_id = ? AND provider = ? AND customer_ref <> '' ORDER BY created_at DESC LIMIT 1`, userID, provider).Scan(&c)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return c, err
}

// RecordPaymentEvent stores a webhook event; false when it was seen before.
func (d *DB) RecordPaymentEvent(ctx context.Context, provider, eventID, tenant, typ, original, purchase string) (bool, error) {
	res, err := d.Exec(ctx, `INSERT INTO payment_events (provider, event_id, tenant_id, type, original, purchase_id, received_at) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (provider, event_id) DO NOTHING`, provider, eventID, tenant, typ, original, purchase, Now())
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}
