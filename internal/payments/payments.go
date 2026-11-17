// Package payments is the provider interface for selling things (ADR-0079):
// checkout pages, verified webhooks, refunds and the customer portal. The
// core only talks to a Provider; providers call their APIs over plain HTTP
// (no SDKs). Providers:
//
//   - Stripe classic: we are the merchant (optionally with Stripe Tax);
//   - Stripe managed: Stripe Managed Payments, Stripe is the merchant of
//     record. Only digital products, each with an eligible tax code;
//     marketplace sales are not allowed;
//   - Paddle (Paddle Billing): Paddle is the merchant of record; own
//     products only, checkout with Paddle.js on our own pay page.
package payments

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
)

// Modes of a provider.
const (
	ModeClassic = "classic"
	ModeManaged = "managed"
)

// Normalized event types.
const (
	EventPaid        = "payment.succeeded"
	EventPending     = "payment.pending"
	EventFailed      = "payment.failed"
	EventExpired     = "payment.expired"
	EventRefunded    = "payment.refunded"
	EventRenewed     = "subscription.renewed"
	EventSubUpdated  = "subscription.updated"
	EventSubCanceled = "subscription.canceled"
	EventUnknown     = "other"
)

// Errors.
var (
	ErrSignature     = errors.New("payments: webhook signature does not match")
	ErrNotConfigured = errors.New("payments: no provider configured")
)

// LineItem is one thing on a checkout.
type LineItem struct {
	Name        string
	Description string
	// Amount per unit in the smallest currency unit (cents).
	Amount   int64
	Currency string // ISO 4217, lower case
	Quantity int64
	// TaxCode of the product (Stripe: txcd_…); required in managed mode.
	TaxCode string
	// Recurring makes it a subscription: "month" or "year".
	Recurring string
}

// CheckoutRequest starts a hosted checkout.
type CheckoutRequest struct {
	// Reference is our purchase id; it comes back with every event.
	Reference string
	// Tenant is passed through so webhooks find the right tenant.
	Tenant     string
	Customer   string // provider customer id, if known
	Email      string
	Items      []LineItem
	SuccessURL string
	CancelURL  string
	Locale     string
	Metadata   map[string]string
}

// Checkout is a hosted checkout page.
type Checkout struct {
	ID  string `json:"id"`
	URL string `json:"url"`
}

// Event is a provider webhook, normalized.
type Event struct {
	ID           string          `json:"id"`
	Type         string          `json:"type"`     // one of the Event* constants
	Original     string          `json:"original"` // provider's own type
	Reference    string          `json:"reference,omitempty"`
	Tenant       string          `json:"tenant,omitempty"`
	Checkout     string          `json:"checkout,omitempty"`
	Payment      string          `json:"payment,omitempty"`      // payment intent
	Subscription string          `json:"subscription,omitempty"` // subscription id
	Customer     string          `json:"customer,omitempty"`
	Amount       int64           `json:"amount,omitempty"`
	Currency     string          `json:"currency,omitempty"`
	Raw          json.RawMessage `json:"-"`
}

// Account is what a key check reports.
type Account struct {
	ID      string `json:"id"`
	Country string `json:"country"`
	Email   string `json:"email,omitempty"`
	Live    bool   `json:"live"`
	Charges bool   `json:"charges"`
	// ManagedEligible: the country is one where Managed Payments sells.
	ManagedEligible bool `json:"managedEligible"`
}

// Provider sells through one payment service.
type Provider interface {
	Name() string
	Mode() string
	Checkout(ctx context.Context, in CheckoutRequest) (Checkout, error)
	// Webhook verifies and reads an incoming event.
	Webhook(h http.Header, body []byte) (Event, error)
	Refund(ctx context.Context, payment string, amount int64) error
	// Portal is the customer's self-service page (payment methods, invoices, cancel).
	Portal(ctx context.Context, customer, returnURL string) (string, error)
	Check(ctx context.Context) (Account, error)
}
