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
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Paddle sells through Paddle Billing. Paddle is the merchant of record:
// it handles tax, invoices and refunds towards the customer.
//
// Checkout: a transaction with inline (non-catalog) prices; its checkout.url
// is the instance's /pay page (approved as default payment link in Paddle)
// with ?_ptxn=<transaction>, where Paddle.js opens the overlay checkout.
type Paddle struct {
	APIKey        string
	WebhookSecret string
	// TaxCategory of the products (saas, standard, digital-goods …).
	TaxCategory string
	// PayURL is the instance's checkout page (…/pay).
	PayURL string
	// BaseURL replaces the API address (tests); otherwise live or sandbox
	// by the key.
	BaseURL string
	HTTP    *http.Client
	Now     func() time.Time
}

// Paddle key formats and settings.
var (
	PaddleKeyRe         = regexp.MustCompile(`^pdl_(live|sdbx)_apikey_[A-Za-z0-9_]{20,}$`)
	PaddleHookRe        = regexp.MustCompile(`^pdl_ntfset_[A-Za-z0-9_]{10,}$`)
	PaddleClientTokenRe = regexp.MustCompile(`^(live|test)_[A-Za-z0-9]{10,}$`)
	PaddleTaxCategories = []string{"saas", "standard", "digital-goods", "ebooks", "implementation-services", "professional-services",
		"software-programming-services", "training-services", "website-hosting"}
	PaddleWebhookEvents = []string{"transaction.completed", "transaction.payment_failed", "subscription.updated", "subscription.canceled",
		"adjustment.created", "adjustment.updated"}
)

// PaddleDefaultTaxCategory is right for Phoedox's own products.
const PaddleDefaultTaxCategory = "saas"

func (p *Paddle) Name() string { return "paddle" }

// Mode: Paddle is always the merchant of record.
func (p *Paddle) Mode() string { return ModeManaged }

// Sandbox reports whether the key is a sandbox key.
func (p *Paddle) Sandbox() bool { return strings.HasPrefix(p.APIKey, "pdl_sdbx_") }

func (p *Paddle) base() string {
	if p.BaseURL != "" {
		return strings.TrimRight(p.BaseURL, "/")
	}
	if p.Sandbox() {
		return "https://sandbox-api.paddle.com"
	}
	return "https://api.paddle.com"
}

func (p *Paddle) client() *http.Client {
	if p.HTTP != nil {
		return p.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

// PaddleError is an error answer of the Paddle API.
type PaddleError struct {
	Status int
	Code   string `json:"code"`
	Detail string `json:"detail"`
}

func (e *PaddleError) Error() string { return fmt.Sprintf("paddle: %s (%s)", e.Detail, e.Code) }

func (p *Paddle) call(ctx context.Context, method, path string, body any, out any) error {
	var r io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = strings.NewReader(string(raw))
	}
	req, err := http.NewRequestWithContext(ctx, method, p.base()+path, r)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+p.APIKey)
	req.Header.Set("Paddle-Version", "1")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := p.client().Do(req)
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
			Error PaddleError `json:"error"`
		}
		_ = json.Unmarshal(raw, &e)
		e.Error.Status = res.StatusCode
		if e.Error.Detail == "" {
			e.Error.Detail = http.StatusText(res.StatusCode)
		}
		return &e.Error
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}

type paddleMoney struct {
	Amount       string `json:"amount"`
	CurrencyCode string `json:"currency_code"`
}

type paddleCycle struct {
	Interval  string `json:"interval"`
	Frequency int    `json:"frequency"`
}

// transactionBody builds the request of a transaction.
func (p *Paddle) transactionBody(in CheckoutRequest) (map[string]any, error) {
	if len(in.Items) == 0 {
		return nil, fmt.Errorf("payments: a checkout needs at least one item")
	}
	if p.PayURL == "" {
		return nil, fmt.Errorf("payments: Paddle needs the instance's public address")
	}
	items := []map[string]any{}
	for i, it := range in.Items {
		if it.Amount <= 0 || it.Currency == "" || it.Name == "" {
			return nil, fmt.Errorf("payments: item %d needs a name, an amount and a currency", i+1)
		}
		q := it.Quantity
		if q <= 0 {
			q = 1
		}
		cat := p.TaxCategory
		if cat == "" {
			cat = PaddleDefaultTaxCategory
		}
		desc := it.Description
		if len(desc) < 2 {
			desc = it.Name
		}
		price := map[string]any{
			"description": desc,
			"name":        it.Name,
			"unit_price":  paddleMoney{Amount: strconv.FormatInt(it.Amount, 10), CurrencyCode: strings.ToUpper(it.Currency)},
			"product":     map[string]any{"name": it.Name, "tax_category": cat},
		}
		switch it.Recurring {
		case "":
		case "month", "year":
			price["billing_cycle"] = paddleCycle{Interval: it.Recurring, Frequency: 1}
		default:
			return nil, fmt.Errorf("payments: recurring must be month or year")
		}
		items = append(items, map[string]any{"quantity": q, "price": price})
	}
	custom := map[string]string{}
	for k, v := range in.Metadata {
		custom[k] = v
	}
	if in.Reference != "" {
		custom["reference"] = in.Reference
	}
	if in.Tenant != "" {
		custom["tenant"] = in.Tenant
	}
	body := map[string]any{"items": items, "custom_data": custom, "checkout": map[string]string{"url": p.PayURL}}
	if in.Customer != "" {
		body["customer_id"] = in.Customer
	}
	return body, nil
}

// Checkout creates a transaction; its checkout URL opens the instance's
// /pay page, where Paddle.js shows the checkout.
func (p *Paddle) Checkout(ctx context.Context, in CheckoutRequest) (Checkout, error) {
	body, err := p.transactionBody(in)
	if err != nil {
		return Checkout{}, err
	}
	var out struct {
		Data struct {
			ID       string `json:"id"`
			Checkout *struct {
				URL string `json:"url"`
			} `json:"checkout"`
		} `json:"data"`
	}
	if err := p.call(ctx, http.MethodPost, "/transactions", body, &out); err != nil {
		return Checkout{}, err
	}
	u := ""
	if out.Data.Checkout != nil {
		u = out.Data.Checkout.URL
	}
	if u == "" {
		// Without an approved default payment link Paddle sends no URL;
		// the /pay page still works with the transaction id.
		sep := "?"
		if strings.Contains(p.PayURL, "?") {
			sep = "&"
		}
		u = p.PayURL + sep + "_ptxn=" + out.Data.ID
	}
	return Checkout{ID: out.Data.ID, URL: u}, nil
}

// Refund asks Paddle for a full refund of a transaction (live refunds may
// wait for Paddle's approval).
func (p *Paddle) Refund(ctx context.Context, payment string, amount int64) error {
	body := map[string]any{"action": "refund", "transaction_id": payment, "reason": "Refund requested by the seller", "type": "full"}
	if amount > 0 {
		return fmt.Errorf("payments: Paddle refunds here are full refunds only")
	}
	return p.call(ctx, http.MethodPost, "/adjustments", body, nil)
}

// Portal opens Paddle's customer portal.
func (p *Paddle) Portal(ctx context.Context, customer, _ string) (string, error) {
	var out struct {
		Data struct {
			URLs struct {
				General struct {
					Overview string `json:"overview"`
				} `json:"general"`
			} `json:"urls"`
		} `json:"data"`
	}
	err := p.call(ctx, http.MethodPost, "/customers/"+customer+"/portal-sessions", map[string]any{}, &out)
	return out.Data.URLs.General.Overview, err
}

// Check verifies the key (Paddle has no account endpoint; the event types
// answer with any valid key).
func (p *Paddle) Check(ctx context.Context) (Account, error) {
	var out json.RawMessage
	if err := p.call(ctx, http.MethodGet, "/event-types", nil, &out); err != nil {
		return Account{}, err
	}
	return Account{ID: "paddle", Live: !p.Sandbox(), Charges: true, ManagedEligible: true}, nil
}

// verifyPaddleSignature checks the Paddle-Signature header
// (ts=<unix>;h1=<hex hmac>[;h1=…]) over ts + ":" + body.
func verifyPaddleSignature(header string, body []byte, secret string, now time.Time) error {
	if secret == "" || header == "" {
		return ErrSignature
	}
	var ts int64
	var sigs [][]byte
	for _, part := range strings.Split(header, ";") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		switch k {
		case "ts":
			ts, _ = strconv.ParseInt(v, 10, 64)
		case "h1":
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
	m.Write([]byte(strconv.FormatInt(ts, 10) + ":"))
	m.Write(body)
	want := m.Sum(nil)
	for _, s := range sigs {
		if hmac.Equal(s, want) {
			return nil
		}
	}
	return ErrSignature
}

// SignPaddle makes a Paddle-Signature header (tests).
func SignPaddle(body []byte, secret string, at time.Time) string {
	m := hmac.New(sha256.New, []byte(secret))
	ts := strconv.FormatInt(at.Unix(), 10)
	m.Write([]byte(ts + ":"))
	m.Write(body)
	return "ts=" + ts + ";h1=" + hex.EncodeToString(m.Sum(nil))
}

type paddleData struct {
	ID             string         `json:"id"`
	Status         string         `json:"status"`
	Origin         string         `json:"origin"`
	CustomerID     string         `json:"customer_id"`
	SubscriptionID string         `json:"subscription_id"`
	TransactionID  string         `json:"transaction_id"`
	Action         string         `json:"action"`
	CustomData     map[string]any `json:"custom_data"`
	CurrencyCode   string         `json:"currency_code"`
	Details        *struct {
		Totals struct {
			GrandTotal string `json:"grand_total"`
			Total      string `json:"total"`
		} `json:"totals"`
	} `json:"details"`
	Totals *struct {
		Total string `json:"total"`
	} `json:"totals"`
}

func (d paddleData) custom(k string) string {
	if v, ok := d.CustomData[k].(string); ok {
		return v
	}
	return ""
}

// PaddleTenant reads the tenant an event names (unverified – it only
// chooses whose secret checks the signature).
func PaddleTenant(body []byte) string {
	var ev struct {
		Data paddleData `json:"data"`
	}
	if json.Unmarshal(body, &ev) != nil {
		return ""
	}
	return ev.Data.custom("tenant")
}

// Webhook verifies a Paddle event and normalizes it.
func (p *Paddle) Webhook(h http.Header, body []byte) (Event, error) {
	now := time.Now()
	if p.Now != nil {
		now = p.Now()
	}
	if err := verifyPaddleSignature(h.Get("Paddle-Signature"), body, p.WebhookSecret, now); err != nil {
		return Event{}, err
	}
	var ev struct {
		EventID   string     `json:"event_id"`
		EventType string     `json:"event_type"`
		Data      paddleData `json:"data"`
	}
	if err := json.Unmarshal(body, &ev); err != nil {
		return Event{}, fmt.Errorf("payments: unreadable event: %w", err)
	}
	d := ev.Data
	out := Event{ID: ev.EventID, Original: ev.EventType, Type: EventUnknown, Reference: d.custom("reference"), Tenant: d.custom("tenant"),
		Customer: d.CustomerID, Subscription: d.SubscriptionID, Currency: strings.ToLower(d.CurrencyCode), Raw: body}
	switch ev.EventType {
	case "transaction.completed", "transaction.paid":
		out.Checkout, out.Payment = d.ID, d.ID
		if d.Details != nil {
			out.Amount, _ = strconv.ParseInt(d.Details.Totals.GrandTotal, 10, 64)
		}
		out.Type = EventPaid
		if d.Origin == "subscription_recurring" {
			out.Type = EventRenewed
		}
	case "transaction.payment_failed":
		out.Type, out.Checkout = EventFailed, d.ID
	case "subscription.updated":
		out.Type, out.Subscription = EventSubUpdated, d.ID
	case "subscription.canceled":
		out.Type, out.Subscription = EventSubCanceled, d.ID
	case "adjustment.created", "adjustment.updated":
		if d.Action == "refund" && d.Status == "approved" {
			out.Type, out.Payment = EventRefunded, d.TransactionID
			if d.Totals != nil {
				out.Amount, _ = strconv.ParseInt(d.Totals.Total, 10, 64)
			}
		}
	}
	return out, nil
}
