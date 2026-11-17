package payments

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func fakePaddle(t *testing.T, reply string) (*httptest.Server, *map[string]any, *string) {
	var body map[string]any
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.Method + " " + r.URL.Path
		if r.Header.Get("Authorization") != "Bearer "+testPaddleKey || r.Header.Get("Paddle-Version") != "1" {
			w.WriteHeader(403)
			_, _ = io.WriteString(w, `{"error":{"type":"request_error","code":"forbidden","detail":"You aren't permitted"}}`)
			return
		}
		b, _ := io.ReadAll(r.Body)
		body = nil
		_ = json.Unmarshal(b, &body)
		_, _ = io.WriteString(w, reply)
	}))
	t.Cleanup(srv.Close)
	return srv, &body, &path
}

// testPaddleKey is assembled so secret scanners do not take it for a real key.
var testPaddleKey = "pdl_sdbx_" + "apikey_" + strings.Repeat("0", 26) + "_" + strings.Repeat("x", 22) + "_abc"

func TestPaddleKeys(t *testing.T) {
	if !PaddleKeyRe.MatchString(testPaddleKey) || PaddleKeyRe.MatchString("sk_test_abc") || !PaddleHookRe.MatchString("pdl_ntfset_01abcdefghijk") ||
		!PaddleClientTokenRe.MatchString("test_abcdefghijk") || PaddleClientTokenRe.MatchString("pdl_live_x") {
		t.Fatal("formats")
	}
	if p := (&Paddle{APIKey: testPaddleKey}); !p.Sandbox() || p.base() != "https://sandbox-api.paddle.com" || p.Mode() != ModeManaged {
		t.Fatal("sandbox")
	}
	if p := (&Paddle{APIKey: "pdl_live_apikey_x"}); p.Sandbox() || p.base() != "https://api.paddle.com" {
		t.Fatal("live")
	}
}

func TestPaddleCheckout(t *testing.T) {
	srv, body, path := fakePaddle(t, `{"data":{"id":"txn_1","checkout":{"url":"https://phx.example/pay?_ptxn=txn_1"}}}`)
	p := &Paddle{APIKey: testPaddleKey, PayURL: "https://phx.example/pay", BaseURL: srv.URL}
	co, err := p.Checkout(context.Background(), CheckoutRequest{Reference: "pur_1", Tenant: "t1", Customer: "ctm_1",
		Items: []LineItem{{Name: "AI credits", Amount: 1000, Currency: "eur", Recurring: "month"}}})
	if err != nil || co.ID != "txn_1" || co.URL != "https://phx.example/pay?_ptxn=txn_1" || *path != "POST /transactions" {
		t.Fatalf("checkout: %v %+v %s", err, co, *path)
	}
	b := *body
	item := b["items"].([]any)[0].(map[string]any)
	price := item["price"].(map[string]any)
	if price["unit_price"].(map[string]any)["amount"] != "1000" || price["unit_price"].(map[string]any)["currency_code"] != "EUR" ||
		price["product"].(map[string]any)["tax_category"] != "saas" || price["billing_cycle"].(map[string]any)["interval"] != "month" ||
		b["custom_data"].(map[string]any)["reference"] != "pur_1" || b["custom_data"].(map[string]any)["tenant"] != "t1" ||
		b["checkout"].(map[string]any)["url"] != "https://phx.example/pay" || b["customer_id"] != "ctm_1" {
		t.Fatalf("body: %v", b)
	}
	// Without a payment link URL in the answer, the pay page still gets the transaction.
	srv2, _, _ := fakePaddle(t, `{"data":{"id":"txn_2","checkout":null}}`)
	p.BaseURL = srv2.URL
	if co, err := p.Checkout(context.Background(), CheckoutRequest{Items: []LineItem{{Name: "x", Amount: 1, Currency: "eur"}}}); err != nil || co.URL != "https://phx.example/pay?_ptxn=txn_2" {
		t.Fatalf("fallback: %v %+v", err, co)
	}
	// API errors carry Paddle's detail.
	bad := &Paddle{APIKey: "pdl_sdbx_apikey_wrong", PayURL: "https://phx.example/pay", BaseURL: srv.URL}
	_, err = bad.Checkout(context.Background(), CheckoutRequest{Items: []LineItem{{Name: "x", Amount: 1, Currency: "eur"}}})
	var pe *PaddleError
	if !errors.As(err, &pe) || pe.Status != 403 || pe.Detail != "You aren't permitted" {
		t.Fatalf("error: %v", err)
	}
	if _, err := p.Checkout(context.Background(), CheckoutRequest{Items: []LineItem{{Name: "x", Amount: 1, Currency: "eur", Recurring: "week"}}}); err == nil {
		t.Fatal("week")
	}
}

func TestPaddleRefundPortalCheck(t *testing.T) {
	srv, body, path := fakePaddle(t, `{"data":{"urls":{"general":{"overview":"https://customer-portal.paddle.com/x"}}}}`)
	p := &Paddle{APIKey: testPaddleKey, BaseURL: srv.URL}
	if err := p.Refund(context.Background(), "txn_1", 0); err != nil || *path != "POST /adjustments" || (*body)["transaction_id"] != "txn_1" || (*body)["type"] != "full" {
		t.Fatalf("refund: %v %s %v", err, *path, *body)
	}
	if u, err := p.Portal(context.Background(), "ctm_1", ""); err != nil || u != "https://customer-portal.paddle.com/x" || *path != "POST /customers/ctm_1/portal-sessions" {
		t.Fatalf("portal: %v %q", err, u)
	}
	if acc, err := p.Check(context.Background()); err != nil || acc.Live || *path != "GET /event-types" {
		t.Fatalf("check: %v %+v", err, acc)
	}
}

func TestPaddleWebhook(t *testing.T) {
	secret := "pdl_ntfset_01secretsecret"
	now := time.Unix(1_800_000_000, 0)
	p := &Paddle{WebhookSecret: secret, Now: func() time.Time { return now }}
	body := []byte(`{"event_id":"evt_1","event_type":"transaction.completed","data":{"id":"txn_1","status":"completed","origin":"web","customer_id":"ctm_1",
		"subscription_id":"sub_1","currency_code":"EUR","custom_data":{"reference":"pur_1","tenant":"t1"},"details":{"totals":{"grand_total":"1190"}}}}`)
	h := http.Header{}
	h.Set("Paddle-Signature", SignPaddle(body, secret, now))
	ev, err := p.Webhook(h, body)
	if err != nil || ev.Type != EventPaid || ev.Reference != "pur_1" || ev.Tenant != "t1" || ev.Payment != "txn_1" || ev.Customer != "ctm_1" ||
		ev.Subscription != "sub_1" || ev.Amount != 1190 || ev.Currency != "eur" || PaddleTenant(body) != "t1" {
		t.Fatalf("paid: %v %+v", err, ev)
	}
	// A second h1 (secret rotation) is fine; a wrong or old one is not.
	h.Set("Paddle-Signature", h.Get("Paddle-Signature")+";h1=00ff")
	if _, err := p.Webhook(h, body); err != nil {
		t.Fatal(err)
	}
	h.Set("Paddle-Signature", SignPaddle(body, "pdl_ntfset_other", now))
	if _, err := p.Webhook(h, body); !errors.Is(err, ErrSignature) {
		t.Fatal("forged")
	}
	h.Set("Paddle-Signature", SignPaddle(body, secret, now.Add(-time.Hour)))
	if _, err := p.Webhook(h, body); !errors.Is(err, ErrSignature) {
		t.Fatal("old")
	}
	sign := func(s string) (Event, error) {
		h := http.Header{}
		h.Set("Paddle-Signature", SignPaddle([]byte(s), secret, now))
		return p.Webhook(h, []byte(s))
	}
	if ev, _ := sign(`{"event_id":"e2","event_type":"transaction.completed","data":{"id":"txn_2","origin":"subscription_recurring","subscription_id":"sub_1"}}`); ev.Type != EventRenewed {
		t.Fatalf("renewed: %+v", ev)
	}
	if ev, _ := sign(`{"event_id":"e3","event_type":"adjustment.updated","data":{"id":"adj_1","action":"refund","status":"approved","transaction_id":"txn_1","totals":{"total":"1190"}}}`); ev.Type != EventRefunded || ev.Payment != "txn_1" {
		t.Fatalf("refund: %+v", ev)
	}
	if ev, _ := sign(`{"event_id":"e4","event_type":"adjustment.created","data":{"id":"adj_1","action":"refund","status":"pending_approval","transaction_id":"txn_1"}}`); ev.Type != EventUnknown {
		t.Fatalf("pending refund: %+v", ev)
	}
	if ev, _ := sign(`{"event_id":"e5","event_type":"subscription.canceled","data":{"id":"sub_1"}}`); ev.Type != EventSubCanceled || ev.Subscription != "sub_1" {
		t.Fatalf("canceled: %+v", ev)
	}
	if !strings.Contains(SignPaddle(body, secret, now), "ts=1800000000;h1=") {
		t.Fatal("format")
	}
}
