package core

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/phoedox/phoedox/internal/ai"
	"github.com/phoedox/phoedox/internal/payments"
	"github.com/phoedox/phoedox/internal/store"
)

func TestAICredits(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, _ := setup(t)
	s.AI = &fakeAI{text: "hi"}
	admin := Actor{User: store.User{ID: "sys", Name: "System", Email: "sys@x.de", IsAdmin: true}, Via: "web"}
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Shop", AIWriteMode: AISuggest})
	_ = s.SetMember(ctx, owner, p.Slug, reader.User.Email, store.RoleEditor)
	// A monthly budget of 1 cent per person, used up by one call.
	one := 1
	if _, err := s.UpdateSettings(ctx, admin, SettingsPatch{AIChat: &AIChatPatch{UserBudget: &one}}); err != nil {
		t.Fatal(err)
	}
	cs, err := s.ChatStatus(ctx, reader, p.Slug)
	if err != nil || !cs.Available || cs.OnCredits {
		t.Fatalf("within the budget: %v %+v", err, cs)
	}
	m := ChatModel{Model: "m1", charge: ai.Price{Input: 10, Output: 10}}
	s.RecordAIUsage(ctx, reader, cs, "", m, ai.Usage{Model: "m1", Input: 1000, Output: 1000}) // 2 cents
	if cs, _ = s.ChatStatus(ctx, reader, p.Slug); cs.Available || cs.Reason != "ai_chat.unavailable.user_budget" {
		t.Fatalf("budget used up: %+v", cs)
	}

	// Grants: only system admins; a filter is required; dry run counts.
	if _, err := s.GrantCredits(ctx, owner, CreditGrantInput{Target: "users", All: true, Credit: 100}); err == nil {
		t.Fatal("only system admins grant")
	}
	if _, err := s.GrantCredits(ctx, admin, CreditGrantInput{Target: "users", Credit: 100}); invalidKey(err) != "validation.credits_filter_empty" {
		t.Fatalf("filter: %v", err)
	}
	dry, err := s.GrantCredits(ctx, admin, CreditGrantInput{Target: "users", Emails: []string{reader.User.Email}, Credit: 100, DryRun: true})
	if err != nil || dry.Count != 1 {
		t.Fatalf("dry run: %v %+v", err, dry)
	}
	if b, _ := s.MyCredits(ctx, reader); b.Balance != 0 {
		t.Fatal("dry run must not grant")
	}
	exp := time.Now().AddDate(0, 1, 0).Format("2006-01-02")
	if _, err := s.GrantCredits(ctx, admin, CreditGrantInput{Target: "users", Emails: []string{reader.User.Email}, Credit: 100, ExpiresAt: exp, Note: "Welcome"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GrantCredits(ctx, admin, CreditGrantInput{Target: "users", Emails: []string{reader.User.Email}, Credit: 50}); err != nil {
		t.Fatal(err)
	}
	info, err := s.MyCredits(ctx, reader)
	if err != nil || info.Balance != 150 || len(info.Lots) != 2 || info.Lots[0].ExpiresAt == nil {
		t.Fatalf("balance: %v %+v", err, info)
	}

	// Beyond the budget the chat goes on with credits; the lot that expires first is used first.
	cs, _ = s.ChatStatus(ctx, reader, p.Slug)
	if !cs.Available || !cs.OnCredits || cs.Credits != 150 {
		t.Fatalf("on credits: %+v", cs)
	}
	s.RecordAIUsage(ctx, reader, cs, "", m, ai.Usage{Model: "m1", Input: 300_000, Output: 300_000}) // $6.00 = 600 cents > 150
	info, _ = s.MyCredits(ctx, reader)
	if info.Balance != 0 || len(info.Lots) != 0 {
		t.Fatalf("used up: %+v", info)
	}
	if cs, _ = s.ChatStatus(ctx, reader, p.Slug); cs.Available || cs.CanBuyCredits {
		t.Fatalf("no credits, nothing to buy: %+v", cs)
	}

	// Offers and a purchase through the payment provider.
	if _, err := s.SetCreditOffers(ctx, admin, []CreditOffer{{ID: "Bad ID", Name: "x", Price: 1, Credit: 1}}); invalidKey(err) != "validation.credits_offer_invalid" {
		t.Fatalf("offer check: %v", err)
	}
	if _, err := s.SetCreditOffers(ctx, admin, []CreditOffer{{ID: "pack-10", Name: "AI credits 10", Price: 1000, Credit: 1000, ValidMonths: 12, For: "user"}}); err != nil {
		t.Fatal(err)
	}
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		_, _ = io.WriteString(w, `{"id":"cs_9","url":"https://checkout.stripe.com/c/cs_9"}`)
	}))
	defer fake.Close()
	s.PaymentsBaseURL = fake.URL
	key, hook := "sk_test_abcdefghijklmnop", "whsec_abcdefghijklmnop"
	if _, err := s.SetAdminPayments(ctx, admin, PaymentSettingsInput{Provider: "stripe", Mode: payments.ModeManaged, SecretKey: &key, WebhookSecret: &hook}); err != nil {
		t.Fatal(err)
	}
	if cs, _ = s.ChatStatus(ctx, reader, p.Slug); !cs.CanBuyCredits {
		t.Fatalf("can buy: %+v", cs)
	}
	if info, _ = s.MyCredits(ctx, reader); len(info.Offers) != 1 {
		t.Fatalf("offers: %+v", info.Offers)
	}
	co, err := s.BuyCredits(ctx, reader, "", "pack-10")
	if err != nil || co.URL == "" {
		t.Fatalf("buy: %v %+v", err, co)
	}
	send := func(body string) {
		t.Helper()
		h := http.Header{}
		h.Set("Stripe-Signature", payments.SignStripe([]byte(body), hook, time.Now()))
		if err := s.HandlePaymentWebhook(ctx, "stripe", h, []byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	send(`{"id":"evt_c1","type":"checkout.session.completed","data":{"object":{"id":"cs_9","metadata":{"tenant":"default","reference":"` + co.ID + `"},"payment_status":"paid","payment_intent":"pi_9","amount_total":1000,"currency":"eur"}}}`)
	info, _ = s.MyCredits(ctx, reader)
	if info.Balance != 1000 || len(info.Lots) != 1 || info.Lots[0].Source != "purchase" || info.Lots[0].ExpiresAt == nil ||
		info.Lots[0].ExpiresAt.Before(time.Now().AddDate(0, 11, 0)) {
		t.Fatalf("bought: %+v", info)
	}
	// A refund takes what is left.
	send(`{"id":"evt_c2","type":"charge.refunded","data":{"object":{"id":"ch_9","payment_intent":"pi_9","amount_refunded":1000,"currency":"eur"}}}`)
	if info, _ = s.MyCredits(ctx, reader); info.Balance != 0 {
		t.Fatalf("refunded: %+v", info)
	}
	if a, err := s.AdminCredits(ctx, admin); err != nil || len(a.Grants) != 2 || !a.PaymentsReady {
		t.Fatalf("admin view: %v %+v", err, a)
	}
}
