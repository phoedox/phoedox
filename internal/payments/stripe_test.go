package payments

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

type captured struct {
	path, version, idem string
	form                url.Values
}

func fakeStripe(t *testing.T, reply string) (*httptest.Server, *captured) {
	c := &captured{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.path, c.version, c.idem = r.URL.Path, r.Header.Get("Stripe-Version"), r.Header.Get("Idempotency-Key")
		if r.Header.Get("Authorization") != "Bearer sk_test_x" {
			w.WriteHeader(401)
			_, _ = io.WriteString(w, `{"error":{"type":"invalid_request_error","code":"api_key_invalid","message":"Invalid API Key"}}`)
			return
		}
		b, _ := io.ReadAll(r.Body)
		c.form, _ = url.ParseQuery(string(b))
		_, _ = io.WriteString(w, reply)
	}))
	t.Cleanup(srv.Close)
	return srv, c
}

func TestStripeCheckoutModes(t *testing.T) {
	srv, c := fakeStripe(t, `{"id":"cs_1","url":"https://checkout.stripe.com/c/cs_1"}`)
	in := CheckoutRequest{Reference: "pur_1", Tenant: "t1", Email: "a@b.de", SuccessURL: "https://x/ok", CancelURL: "https://x/no",
		Items: []LineItem{{Name: "AI credits 10 €", Amount: 1000, Currency: "EUR"}}}

	// Managed Payments: Stripe is the merchant of record.
	m := &Stripe{SecretKey: "sk_test_x", Managed: true, BaseURL: srv.URL}
	out, err := m.Checkout(context.Background(), in)
	if err != nil || out.URL == "" || c.path != "/v1/checkout/sessions" || c.version != StripeAPIVersion || c.idem != "checkout-pur_1" {
		t.Fatalf("managed: %v %+v %+v", err, out, c)
	}
	f := c.form
	if f.Get("managed_payments[enabled]") != "true" || f.Get("mode") != "payment" || f.Get("line_items[0][price_data][product_data][tax_code]") != DefaultTaxCode ||
		f.Get("line_items[0][price_data][currency]") != "eur" || f.Get("client_reference_id") != "pur_1" || f.Get("metadata[tenant]") != "t1" ||
		f.Get("payment_intent_data[metadata][reference]") != "pur_1" {
		t.Fatalf("managed form: %v", f)
	}
	for _, forbidden := range []string{"automatic_tax[enabled]", "tax_id_collection[enabled]", "payment_method_types[0]", "billing_address_collection"} {
		if f.Has(forbidden) {
			t.Fatalf("managed must not send %s", forbidden)
		}
	}

	// Classic with Stripe Tax.
	cl := &Stripe{SecretKey: "sk_test_x", AutomaticTax: true, BaseURL: srv.URL}
	in.Items = []LineItem{{Name: "Plan", Amount: 900, Currency: "eur", Recurring: "month", TaxCode: "txcd_10103001"}}
	if _, err := cl.Checkout(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	f = c.form
	if f.Has("managed_payments[enabled]") || f.Get("automatic_tax[enabled]") != "true" || f.Get("mode") != "subscription" ||
		f.Get("line_items[0][price_data][recurring][interval]") != "month" || f.Get("subscription_data[metadata][reference]") != "pur_1" || f.Get("billing_address_collection") != "required" {
		t.Fatalf("classic form: %v", f)
	}
	// Classic without tax: no tax code sent.
	plain := &Stripe{SecretKey: "sk_test_x", BaseURL: srv.URL}
	in.Items[0].TaxCode = ""
	_, _ = plain.Checkout(context.Background(), in)
	if c.form.Has("line_items[0][price_data][product_data][tax_code]") || c.form.Has("automatic_tax[enabled]") {
		t.Fatalf("plain form: %v", c.form)
	}
	// Errors of Stripe come back readable.
	bad := &Stripe{SecretKey: "sk_test_wrong", BaseURL: srv.URL}
	var se *StripeError
	if _, err := bad.Checkout(context.Background(), in); !errors.As(err, &se) || se.Code != "api_key_invalid" || se.Status != 401 {
		t.Fatalf("error: %v", err)
	}
	if _, err := plain.Checkout(context.Background(), CheckoutRequest{SuccessURL: "x"}); err == nil {
		t.Fatal("empty checkout must fail")
	}
}

func TestStripeCheck(t *testing.T) {
	srv, _ := fakeStripe(t, `{"id":"acct_1","country":"DE","email":"x@y.de","charges_enabled":true}`)
	a, err := (&Stripe{SecretKey: "sk_test_x", BaseURL: srv.URL}).Check(context.Background())
	if err != nil || a.Country != "DE" || !a.ManagedEligible || a.Live || !a.Charges {
		t.Fatalf("check: %v %+v", err, a)
	}
}

func TestStripeWebhook(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	s := &Stripe{WebhookSecret: "whsec_1", Now: func() time.Time { return now }}
	body := []byte(`{"id":"evt_1","type":"checkout.session.completed","data":{"object":{"id":"cs_1","object":"checkout.session","client_reference_id":"pur_1",
		"metadata":{"tenant":"t1","reference":"pur_1"},"payment_status":"paid","payment_intent":"pi_1","customer":"cus_1","amount_total":1190,"currency":"eur"}}}`)
	h := http.Header{}
	h.Set("Stripe-Signature", SignStripe(body, "whsec_1", now))
	ev, err := s.Webhook(h, body)
	if err != nil || ev.Type != EventPaid || ev.Reference != "pur_1" || ev.Tenant != "t1" || ev.Payment != "pi_1" || ev.Customer != "cus_1" || ev.Amount != 1190 {
		t.Fatalf("event: %v %+v", err, ev)
	}
	// Wrong secret, changed body, old timestamp.
	h.Set("Stripe-Signature", SignStripe(body, "whsec_other", now))
	if _, err := s.Webhook(h, body); !errors.Is(err, ErrSignature) {
		t.Fatalf("wrong secret: %v", err)
	}
	h.Set("Stripe-Signature", SignStripe(body, "whsec_1", now))
	if _, err := s.Webhook(h, []byte(strings.Replace(string(body), "1190", "1", 1))); !errors.Is(err, ErrSignature) {
		t.Fatalf("changed body: %v", err)
	}
	h.Set("Stripe-Signature", SignStripe(body, "whsec_1", now.Add(-10*time.Minute)))
	if _, err := s.Webhook(h, body); !errors.Is(err, ErrSignature) {
		t.Fatalf("old: %v", err)
	}
	// A renewal invoice finds its purchase through the subscription's metadata.
	inv := []byte(`{"id":"evt_2","type":"invoice.paid","data":{"object":{"id":"in_1","object":"invoice","billing_reason":"subscription_cycle","amount_paid":900,"currency":"eur",
		"parent":{"subscription_details":{"subscription":"sub_1","metadata":{"reference":"pur_2","tenant":"t1"}}}}}}`)
	h.Set("Stripe-Signature", SignStripe(inv, "whsec_1", now))
	ev, err = s.Webhook(h, inv)
	if err != nil || ev.Type != EventRenewed || ev.Reference != "pur_2" || ev.Subscription != "sub_1" || ev.Amount != 900 {
		t.Fatalf("renewal: %v %+v", err, ev)
	}
}
