package payments

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// StripeAPIVersion is pinned: the first version with managed_payments
// (generally available since 2026-04-22).
const StripeAPIVersion = "2026-04-22.dahlia"

// DefaultTaxCode is Stripe's tax code for SaaS sold to businesses.
const DefaultTaxCode = "txcd_10103001"

// ManagedCountries are the seller countries of Stripe Managed Payments.
var ManagedCountries = []string{"CA", "US", "AT", "BE", "BG", "CH", "CY", "CZ", "DE", "DK", "EE", "ES", "FI", "FR", "GB", "GR", "HR", "HU", "IE", "IT",
	"LI", "LT", "LU", "LV", "MT", "NL", "NO", "PL", "PT", "RO", "SE", "SI", "SK", "AU", "HK", "JP", "SG"}

// Stripe sells through Stripe Checkout.
type Stripe struct {
	SecretKey     string
	WebhookSecret string
	// Managed: Stripe Managed Payments (Stripe is the merchant of record).
	Managed bool
	// AutomaticTax (classic only): Stripe Tax calculates VAT; customers
	// give their billing address and may enter a VAT id.
	AutomaticTax bool
	// TaxCode is the default product tax code (managed mode needs one).
	TaxCode string
	// BaseURL is https://api.stripe.com unless tests change it.
	BaseURL string
	HTTP    *http.Client
	Now     func() time.Time
}

func (s *Stripe) Name() string { return "stripe" }

func (s *Stripe) Mode() string {
	if s.Managed {
		return ModeManaged
	}
	return ModeClassic
}

func (s *Stripe) base() string {
	if s.BaseURL != "" {
		return strings.TrimRight(s.BaseURL, "/")
	}
	return "https://api.stripe.com"
}

func (s *Stripe) client() *http.Client {
	if s.HTTP != nil {
		return s.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

// StripeError is an error answer of the Stripe API.
type StripeError struct {
	Status  int
	Type    string `json:"type"`
	Code    string `json:"code"`
	Message string `json:"message"`
	Param   string `json:"param"`
}

func (e *StripeError) Error() string {
	if e.Param != "" {
		return fmt.Sprintf("stripe: %s (%s, %s)", e.Message, e.Code, e.Param)
	}
	return fmt.Sprintf("stripe: %s (%s)", e.Message, e.Code)
}

// call sends a form-encoded request and reads the JSON answer into out.
func (s *Stripe) call(ctx context.Context, method, path string, form url.Values, idem string, out any) error {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, s.base()+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.SecretKey)
	req.Header.Set("Stripe-Version", StripeAPIVersion)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if idem != "" {
		req.Header.Set("Idempotency-Key", idem)
	}
	res, err := s.client().Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return err
	}
	if res.StatusCode >= 300 {
		var e struct {
			Error StripeError `json:"error"`
		}
		_ = json.Unmarshal(raw, &e)
		e.Error.Status = res.StatusCode
		if e.Error.Message == "" {
			e.Error.Message = http.StatusText(res.StatusCode)
		}
		return &e.Error
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}

// checkoutForm builds the parameters of a Checkout Session.
func (s *Stripe) checkoutForm(in CheckoutRequest) (url.Values, error) {
	if len(in.Items) == 0 {
		return nil, fmt.Errorf("payments: a checkout needs at least one item")
	}
	if in.SuccessURL == "" {
		return nil, fmt.Errorf("payments: a checkout needs a success address")
	}
	f := url.Values{}
	recurring := false
	for i, it := range in.Items {
		p := fmt.Sprintf("line_items[%d]", i)
		q := it.Quantity
		if q <= 0 {
			q = 1
		}
		if it.Amount <= 0 || it.Currency == "" || it.Name == "" {
			return nil, fmt.Errorf("payments: item %d needs a name, an amount and a currency", i+1)
		}
		f.Set(p+"[quantity]", strconv.FormatInt(q, 10))
		f.Set(p+"[price_data][currency]", strings.ToLower(it.Currency))
		f.Set(p+"[price_data][unit_amount]", strconv.FormatInt(it.Amount, 10))
		f.Set(p+"[price_data][product_data][name]", it.Name)
		if it.Description != "" {
			f.Set(p+"[price_data][product_data][description]", it.Description)
		}
		code := it.TaxCode
		if code == "" {
			code = s.TaxCode
		}
		if s.Managed && code == "" {
			code = DefaultTaxCode
		}
		if code != "" && (s.Managed || s.AutomaticTax) {
			f.Set(p+"[price_data][product_data][tax_code]", code)
		}
		if it.Recurring != "" {
			if it.Recurring != "month" && it.Recurring != "year" {
				return nil, fmt.Errorf("payments: recurring must be month or year")
			}
			f.Set(p+"[price_data][recurring][interval]", it.Recurring)
			recurring = true
		}
	}
	mode := "payment"
	if recurring {
		mode = "subscription"
	}
	f.Set("mode", mode)
	f.Set("success_url", in.SuccessURL)
	if in.CancelURL != "" {
		f.Set("cancel_url", in.CancelURL)
	}
	if in.Reference != "" {
		f.Set("client_reference_id", in.Reference)
	}
	meta := map[string]string{}
	for k, v := range in.Metadata {
		meta[k] = v
	}
	if in.Reference != "" {
		meta["reference"] = in.Reference
	}
	if in.Tenant != "" {
		meta["tenant"] = in.Tenant
	}
	keys := make([]string, 0, len(meta))
	for k := range meta {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		f.Set("metadata["+k+"]", meta[k])
		// Payments and subscriptions carry the reference too (refunds,
		// renewals and cancellations come as their own events).
		if mode == "subscription" {
			f.Set("subscription_data[metadata]["+k+"]", meta[k])
		} else {
			f.Set("payment_intent_data[metadata]["+k+"]", meta[k])
		}
	}
	if in.Customer != "" {
		f.Set("customer", in.Customer)
	} else if in.Email != "" {
		f.Set("customer_email", in.Email)
		if mode == "payment" {
			f.Set("customer_creation", "always")
		}
	}
	if in.Locale != "" {
		f.Set("locale", in.Locale)
	}
	if s.Managed {
		// Stripe is the merchant of record: it collects name and address,
		// handles tax, payment methods and receipts itself. Parameters such
		// as automatic_tax, tax_id_collection, payment_method_types or
		// shipping must not be sent.
		f.Set("managed_payments[enabled]", "true")
		return f, nil
	}
	if s.AutomaticTax {
		f.Set("automatic_tax[enabled]", "true")
		f.Set("billing_address_collection", "required")
		f.Set("tax_id_collection[enabled]", "true")
		if in.Customer != "" {
			f.Set("customer_update[address]", "auto")
			f.Set("customer_update[name]", "auto")
		}
	}
	return f, nil
}

// Checkout creates a Stripe Checkout Session.
func (s *Stripe) Checkout(ctx context.Context, in CheckoutRequest) (Checkout, error) {
	f, err := s.checkoutForm(in)
	if err != nil {
		return Checkout{}, err
	}
	var out struct {
		ID  string `json:"id"`
		URL string `json:"url"`
	}
	idem := ""
	if in.Reference != "" {
		idem = "checkout-" + in.Reference
	}
	if err := s.call(ctx, http.MethodPost, "/v1/checkout/sessions", f, idem, &out); err != nil {
		return Checkout{}, err
	}
	return Checkout{ID: out.ID, URL: out.URL}, nil
}

// Refund refunds a payment (amount 0 = everything).
func (s *Stripe) Refund(ctx context.Context, payment string, amount int64) error {
	f := url.Values{"payment_intent": {payment}}
	if amount > 0 {
		f.Set("amount", strconv.FormatInt(amount, 10))
	}
	return s.call(ctx, http.MethodPost, "/v1/refunds", f, "", nil)
}

// Portal opens Stripe's customer portal.
func (s *Stripe) Portal(ctx context.Context, customer, returnURL string) (string, error) {
	var out struct {
		URL string `json:"url"`
	}
	err := s.call(ctx, http.MethodPost, "/v1/billing_portal/sessions", url.Values{"customer": {customer}, "return_url": {returnURL}}, "", &out)
	return out.URL, err
}

// Check reads the account the key belongs to.
func (s *Stripe) Check(ctx context.Context) (Account, error) {
	var a struct {
		ID             string `json:"id"`
		Country        string `json:"country"`
		Email          string `json:"email"`
		ChargesEnabled bool   `json:"charges_enabled"`
	}
	if err := s.call(ctx, http.MethodGet, "/v1/account", nil, "", &a); err != nil {
		return Account{}, err
	}
	return Account{ID: a.ID, Country: a.Country, Email: a.Email, Charges: a.ChargesEnabled, Live: strings.HasPrefix(s.SecretKey, "sk_live_") || strings.HasPrefix(s.SecretKey, "rk_live_"),
		ManagedEligible: slices.Contains(ManagedCountries, strings.ToUpper(a.Country))}, nil
}

// webhookTolerance is how old a signed webhook may be.
const webhookTolerance = 5 * time.Minute

// verifyStripeSignature checks the Stripe-Signature header
// (t=<unix>,v1=<hex hmac>[,v1=…]) of a payload.
func verifyStripeSignature(header string, body []byte, secret string, now time.Time) error {
	if secret == "" || header == "" {
		return ErrSignature
	}
	var ts int64
	var sigs [][]byte
	for _, part := range strings.Split(header, ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		switch k {
		case "t":
			ts, _ = strconv.ParseInt(v, 10, 64)
		case "v1":
			if b, err := hex.DecodeString(v); err == nil {
				sigs = append(sigs, b)
			}
		}
	}
	if ts == 0 || len(sigs) == 0 {
		return ErrSignature
	}
	if d := now.Sub(time.Unix(ts, 0)); d > webhookTolerance || d < -webhookTolerance {
		return ErrSignature
	}
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(strconv.FormatInt(ts, 10) + "."))
	m.Write(body)
	want := m.Sum(nil)
	for _, s := range sigs {
		if hmac.Equal(s, want) {
			return nil
		}
	}
	return ErrSignature
}

// SignStripe makes a Stripe-Signature header (tests).
func SignStripe(body []byte, secret string, at time.Time) string {
	m := hmac.New(sha256.New, []byte(secret))
	ts := strconv.FormatInt(at.Unix(), 10)
	m.Write([]byte(ts + "."))
	m.Write(body)
	return "t=" + ts + ",v1=" + hex.EncodeToString(m.Sum(nil))
}

type stripeObject struct {
	ID                  string            `json:"id"`
	Object              string            `json:"object"`
	ClientReference     string            `json:"client_reference_id"`
	Metadata            map[string]string `json:"metadata"`
	PaymentStatus       string            `json:"payment_status"`
	PaymentIntent       json.RawMessage   `json:"payment_intent"`
	Subscription        json.RawMessage   `json:"subscription"`
	Customer            json.RawMessage   `json:"customer"`
	AmountTotal         int64             `json:"amount_total"`
	AmountPaid          int64             `json:"amount_paid"`
	AmountRefunded      int64             `json:"amount_refunded"`
	Currency            string            `json:"currency"`
	Status              string            `json:"status"`
	BillingReason       string            `json:"billing_reason"`
	SubscriptionDetails *struct {
		Metadata map[string]string `json:"metadata"`
	} `json:"subscription_details"`
	Parent *struct {
		SubscriptionDetails *struct {
			Metadata     map[string]string `json:"metadata"`
			Subscription json.RawMessage   `json:"subscription"`
		} `json:"subscription_details"`
	} `json:"parent"`
}

// idOf reads an expandable field (a string id or an object with id).
func idOf(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var o struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(raw, &o)
	return o.ID
}

// Webhook verifies a Stripe event and normalizes it.
func (s *Stripe) Webhook(h http.Header, body []byte) (Event, error) {
	now := time.Now()
	if s.Now != nil {
		now = s.Now()
	}
	if err := verifyStripeSignature(h.Get("Stripe-Signature"), body, s.WebhookSecret, now); err != nil {
		return Event{}, err
	}
	var ev struct {
		ID   string `json:"id"`
		Type string `json:"type"`
		Data struct {
			Object stripeObject `json:"object"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &ev); err != nil {
		return Event{}, fmt.Errorf("payments: unreadable event: %w", err)
	}
	o := ev.Data.Object
	meta := o.Metadata
	if meta == nil {
		meta = map[string]string{}
	}
	if o.SubscriptionDetails != nil {
		for k, v := range o.SubscriptionDetails.Metadata {
			if meta[k] == "" {
				meta[k] = v
			}
		}
	}
	sub := idOf(o.Subscription)
	if o.Parent != nil && o.Parent.SubscriptionDetails != nil {
		for k, v := range o.Parent.SubscriptionDetails.Metadata {
			if meta[k] == "" {
				meta[k] = v
			}
		}
		if sub == "" {
			sub = idOf(o.Parent.SubscriptionDetails.Subscription)
		}
	}
	out := Event{ID: ev.ID, Original: ev.Type, Type: EventUnknown, Reference: o.ClientReference, Tenant: meta["tenant"],
		Customer: idOf(o.Customer), Currency: o.Currency, Raw: body}
	if out.Reference == "" {
		out.Reference = meta["reference"]
	}
	switch ev.Type {
	case "checkout.session.completed":
		out.Checkout, out.Payment, out.Subscription, out.Amount = o.ID, idOf(o.PaymentIntent), sub, o.AmountTotal
		if o.PaymentStatus == "paid" || o.PaymentStatus == "no_payment_required" {
			out.Type = EventPaid
		} else {
			out.Type = EventPending
		}
	case "checkout.session.async_payment_succeeded":
		out.Type, out.Checkout, out.Payment, out.Subscription, out.Amount = EventPaid, o.ID, idOf(o.PaymentIntent), sub, o.AmountTotal
	case "checkout.session.async_payment_failed":
		out.Type, out.Checkout = EventFailed, o.ID
	case "checkout.session.expired":
		out.Type, out.Checkout = EventExpired, o.ID
	case "charge.refunded":
		out.Type, out.Payment, out.Amount = EventRefunded, idOf(o.PaymentIntent), o.AmountRefunded
	case "invoice.paid":
		out.Subscription, out.Amount = sub, o.AmountPaid
		// The first invoice is the checkout itself; later ones are renewals.
		if o.BillingReason == "subscription_cycle" {
			out.Type = EventRenewed
		}
	case "customer.subscription.updated":
		out.Type, out.Subscription = EventSubUpdated, o.ID
	case "customer.subscription.deleted":
		out.Type, out.Subscription = EventSubCanceled, o.ID
	}
	return out, nil
}
