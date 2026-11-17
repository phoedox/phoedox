package core

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/phoedox/phoedox/internal/payments"
	"github.com/phoedox/phoedox/internal/store"
)

func TestPayments(t *testing.T) {
	ctx := context.Background()
	s, owner, _, _ := setup(t)
	admin := Actor{User: store.User{ID: "sys", Name: "System", Email: "sys@x.de", IsAdmin: true}, Via: "web"}
	var form url.Values
	var jsonBody map[string]any
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		form, _ = url.ParseQuery(string(b))
		jsonBody = nil
		_ = json.Unmarshal(b, &jsonBody)
		switch r.URL.Path {
		case "/transactions":
			_, _ = io.WriteString(w, `{"data":{"id":"txn_1","checkout":{"url":"https://phx.test/pay?_ptxn=txn_1"}}}`)
		case "/adjustments":
			_, _ = io.WriteString(w, `{"data":{"id":"adj_1"}}`)
		case "/event-types":
			_, _ = io.WriteString(w, `{"data":[]}`)
		case "/v1/checkout/sessions":
			_, _ = io.WriteString(w, `{"id":"cs_1","url":"https://checkout.stripe.com/c/cs_1"}`)
		case "/v1/account":
			_, _ = io.WriteString(w, `{"id":"acct_1","country":"DE","charges_enabled":true}`)
		case "/v1/refunds":
			_, _ = io.WriteString(w, `{"id":"re_1"}`)
		case "/v1/billing_portal/sessions":
			_, _ = io.WriteString(w, `{"url":"https://billing.stripe.com/p/1"}`)
		}
	}))
	defer fake.Close()
	s.PaymentsBaseURL = fake.URL

	// Off by default; only system admins configure it.
	if _, err := s.StartPurchase(ctx, owner, PurchaseInput{Kind: "test", Name: "x", Amount: 100}); invalidKey(err) != "validation.payments_off" {
		t.Fatalf("off: %v", err)
	}
	key, hook := "sk_test_abcdefghijklmnop", "whsec_abcdefghijklmnop"
	if _, err := s.SetAdminPayments(ctx, owner, PaymentSettingsInput{Provider: "stripe", SecretKey: &key}); err == nil {
		t.Fatal("only system admins")
	}
	bad := "pk_test_123"
	if _, err := s.SetAdminPayments(ctx, admin, PaymentSettingsInput{Provider: "stripe", SecretKey: &bad}); invalidKey(err) != "validation.payments_key" {
		t.Fatalf("publishable key: %v", err)
	}
	v, err := s.SetAdminPayments(ctx, admin, PaymentSettingsInput{Provider: "stripe", Mode: payments.ModeManaged, SecretKey: &key, WebhookSecret: &hook, AutomaticTax: true, Currency: "EUR"})
	if err != nil || !v.HasSecretKey || !v.HasWebhookSecret || v.KeyHint != "sk_test_…mnop" || v.AutomaticTax || v.Mode != "managed" || v.TaxCode != payments.DefaultTaxCode {
		t.Fatalf("settings: %v %+v", err, v)
	}
	// Secrets never come back.
	if v2, _ := s.AdminPayments(ctx, admin); strings.Contains(v2.KeyHint, "abcdefghijkl") {
		t.Fatalf("leak: %+v", v2)
	}
	chk, err := s.CheckPayments(ctx, admin)
	if err != nil || chk.Country != "DE" || !chk.ManagedEligible || len(chk.Warnings) != 1 || chk.Warnings[0] != "test_mode" {
		t.Fatalf("check: %v %+v", err, chk)
	}

	// A purchase in managed mode.
	co, err := s.StartPurchase(ctx, owner, PurchaseInput{Kind: "ai_credits", Item: "pack10", Name: "AI credits 10 €", Amount: 1000, ReturnPath: "/settings/billing"})
	if err != nil || co.URL == "" || form.Get("managed_payments[enabled]") != "true" || form.Get("client_reference_id") != co.ID || form.Get("metadata[tenant]") != store.DefaultTenant {
		t.Fatalf("checkout: %v %+v %v", err, co, form)
	}
	got := []string{}
	s.OnPayment("ai_credits", func(ctx context.Context, p store.Purchase, ev payments.Event) {
		got = append(got, p.Status+":"+ev.Type)
	})

	send := func(body string) error {
		h := http.Header{}
		h.Set("Stripe-Signature", payments.SignStripe([]byte(body), hook, time.Now()))
		return s.HandlePaymentWebhook(ctx, "stripe", h, []byte(body))
	}
	paid := `{"id":"evt_1","type":"checkout.session.completed","data":{"object":{"id":"cs_1","client_reference_id":"` + co.ID + `","metadata":{"tenant":"default","reference":"` + co.ID + `"},"payment_status":"paid","payment_intent":"pi_1","customer":"cus_1","amount_total":1190,"currency":"eur"}}}`
	if err := send(paid); err != nil {
		t.Fatal(err)
	}
	if err := send(paid); err != nil { // a repeated delivery changes nothing
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "paid:payment.succeeded" {
		t.Fatalf("handler: %v", got)
	}
	p, _ := s.DB.GetPurchase(ctx, co.ID)
	if p.Status != "paid" || p.PaymentRef != "pi_1" || p.CustomerRef != "cus_1" || p.Mode != "managed" {
		t.Fatalf("purchase: %+v", p)
	}
	// Forged events are refused.
	h := http.Header{}
	h.Set("Stripe-Signature", payments.SignStripe([]byte(paid), "whsec_wrongwrongwrong", time.Now()))
	if err := s.HandlePaymentWebhook(ctx, "stripe", h, []byte(paid)); invalidKey(err) != "validation.payments_signature" {
		t.Fatalf("forged: %v", err)
	}
	// Refund (admin) and the provider's refund event.
	if err := s.RefundPurchase(ctx, admin, co.ID); err != nil || form.Get("payment_intent") != "pi_1" {
		t.Fatalf("refund: %v %v", err, form)
	}
	if err := send(`{"id":"evt_2","type":"charge.refunded","data":{"object":{"id":"ch_1","payment_intent":"pi_1","amount_refunded":1190,"currency":"eur"}}}`); err != nil {
		t.Fatal(err)
	}
	if p, _ = s.DB.GetPurchase(ctx, co.ID); p.Status != "refunded" || len(got) != 2 {
		t.Fatalf("refunded: %+v %v", p, got)
	}
	// The buyer's portal and list.
	if u, err := s.PaymentPortal(ctx, owner, "/settings/billing"); err != nil || u == "" || form.Get("customer") != "cus_1" {
		t.Fatalf("portal: %v %q", err, u)
	}
	if l, _ := s.MyPurchases(ctx, owner); len(l) != 1 {
		t.Fatalf("mine: %+v", l)
	}
	// Classic mode with Stripe Tax: no managed flag, automatic tax on.
	if _, err := s.SetAdminPayments(ctx, admin, PaymentSettingsInput{Provider: "stripe", Mode: payments.ModeClassic, AutomaticTax: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.TestPurchase(ctx, admin); err != nil || form.Has("managed_payments[enabled]") || form.Get("automatic_tax[enabled]") != "true" {
		t.Fatalf("classic: %v %v", err, form)
	}

	// Paddle: own keys (Stripe's stay), client token, checkout on /pay.
	pk, ph, tok := "pdl_sdbx_"+"apikey_"+strings.Repeat("0", 26)+"_"+strings.Repeat("x", 22)+"_abc", "pdl_ntfset_01abcdefghijk", "test_abcdefghijklmn"
	if _, err := s.SetAdminPayments(ctx, admin, PaymentSettingsInput{Provider: "paddle", SecretKey: &key}); invalidKey(err) != "validation.payments_paddle_key" {
		t.Fatalf("stripe key for paddle: %v", err)
	}
	v, err = s.SetAdminPayments(ctx, admin, PaymentSettingsInput{Provider: "paddle", Mode: payments.ModeManaged, TaxCategory: "standard"})
	if err != nil || v.HasSecretKey || v.Ready || v.Mode != payments.ModeClassic || v.PayURL == "" || v.WebhookURL != s.PublicURL+"/api/v1/payments/webhook/paddle" {
		t.Fatalf("paddle chosen: %v %+v", err, v)
	}
	v, err = s.SetAdminPayments(ctx, admin, PaymentSettingsInput{Provider: "paddle", SecretKey: &pk, WebhookSecret: &ph, ClientToken: &tok, TaxCategory: "standard"})
	if err != nil || !v.Ready || v.ClientToken != tok || v.TaxCategory != "standard" || v.KeyHint != "pdl_sdbx_apikey_…_abc" {
		t.Fatalf("paddle keys: %v %+v", err, v)
	}
	co, err = s.StartPurchase(ctx, owner, PurchaseInput{Kind: "ai_credits", Name: "AI credits 5 €", Amount: 500, ReturnPath: "/settings/billing"})
	if err != nil || co.URL != "https://phx.test/pay?_ptxn=txn_1" || jsonBody["custom_data"].(map[string]any)["reference"] != co.ID {
		t.Fatalf("paddle checkout: %v %+v %v", err, co, jsonBody)
	}
	info, err := s.PaymentPayInfo(ctx, "txn_1")
	if err != nil || info.ClientToken != tok || !info.Sandbox || !strings.HasSuffix(info.SuccessURL, "/settings/billing?purchase="+co.ID+"&result=ok") {
		t.Fatalf("pay info: %v %+v", err, info)
	}
	if _, err := s.PaymentPayInfo(ctx, "txn_unknown"); err != ErrNotFound {
		t.Fatalf("unknown txn: %v", err)
	}
	psend := func(body string) error {
		h := http.Header{}
		h.Set("Paddle-Signature", payments.SignPaddle([]byte(body), ph, time.Now()))
		return s.HandlePaymentWebhook(ctx, "paddle", h, []byte(body))
	}
	if err := psend(`{"event_id":"evt_p1","event_type":"transaction.completed","data":{"id":"txn_1","origin":"web","customer_id":"ctm_1","currency_code":"EUR",
		"custom_data":{"reference":"` + co.ID + `","tenant":"default"},"details":{"totals":{"grand_total":"500"}}}}`); err != nil {
		t.Fatal(err)
	}
	if p, _ = s.DB.GetPurchase(ctx, co.ID); p.Status != "paid" || p.Provider != "paddle" || p.PaymentRef != "txn_1" || p.CustomerRef != "ctm_1" || len(got) != 3 {
		t.Fatalf("paddle paid: %+v %v", p, got)
	}
	// Stripe events no longer count while Paddle is chosen.
	if err := send(paid); err != ErrNotFound {
		t.Fatalf("stripe while paddle: %v", err)
	}
	if err := s.RefundPurchase(ctx, admin, co.ID); err != nil || jsonBody["transaction_id"] != "txn_1" {
		t.Fatalf("paddle refund: %v %v", err, jsonBody)
	}
	if err := psend(`{"event_id":"evt_p2","event_type":"adjustment.updated","data":{"id":"adj_1","action":"refund","status":"approved","transaction_id":"txn_1"}}`); err != nil {
		t.Fatal(err)
	}
	if p, _ = s.DB.GetPurchase(ctx, co.ID); p.Status != "refunded" {
		t.Fatalf("paddle refunded: %+v", p)
	}
	if chk, err := s.CheckPayments(ctx, admin); err != nil || chk.Live {
		t.Fatalf("paddle check: %v %+v", err, chk)
	}
	// Back to Stripe: its keys were kept.
	if v, err = s.SetAdminPayments(ctx, admin, PaymentSettingsInput{Provider: "stripe", Mode: payments.ModeManaged}); err != nil || !v.Ready || v.KeyHint != "sk_test_…mnop" {
		t.Fatalf("stripe again: %v %+v", err, v)
	}
}
