package core

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/phoedox/phoedox/internal/ai"
	"github.com/phoedox/phoedox/internal/store"
)

func TestOrgPrices(t *testing.T) {
	ctx := context.Background()
	s, owner, _, _ := setup(t)
	admin := Actor{User: store.User{ID: owner.User.ID, Name: owner.User.Name, IsAdmin: true}, Via: "web"}
	o, err := s.CreateOrg(ctx, admin, OrgInput{Name: "Kunde"})
	if err != nil {
		t.Fatal(err)
	}
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Shop", Org: &o.Slug})
	m := ChatModel{Model: "m1", charge: ai.Price{Input: 10, Output: 10}}
	cs := ChatStatus{OrgID: o.ID, ProjectID: p.ID}
	u := ai.Usage{Model: "m1", Input: 50_000, Output: 50_000} // 100k tokens × $10/M = $1.00
	if got := s.RecordAIUsage(ctx, owner, cs, "", m, u); got != 1_000_000 {
		t.Fatalf("list price: %d", got)
	}
	// Only system admins set prices.
	if _, err := s.SetOrgPrice(ctx, owner, o.Slug, OrgPriceInput{Percent: -20, Mode: "now"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("org admins must not set prices: %v", err)
	}
	// A planned change does not apply yet and can be removed.
	planned, err := s.SetOrgPrice(ctx, admin, o.Slug, OrgPriceInput{Percent: 50, Mode: "next_month"})
	if err != nil || planned.Current != nil || len(planned.History) != 1 {
		t.Fatalf("planned: %v %+v", err, planned)
	}
	if got := s.RecordAIUsage(ctx, owner, cs, "", m, u); got != 1_000_000 {
		t.Fatalf("planned price must not apply: %d", got)
	}
	if _, err := s.DeleteOrgPrice(ctx, admin, o.Slug, planned.History[0].ID); err != nil {
		t.Fatal(err)
	}
	// Preview of a retroactive discount: $2.00 becomes $1.60.
	pv, err := s.PreviewOrgPrice(ctx, admin, o.Slug, OrgPriceInput{Percent: -20, Mode: "month_start"})
	if err != nil || pv.BeforeCents != 200 || pv.AfterCents != 160 || len(pv.Budgets) == 0 {
		t.Fatalf("preview: %v %+v", err, pv)
	}
	// Back to the month start: the month is recalculated.
	if _, err := s.SetOrgPrice(ctx, admin, o.Slug, OrgPriceInput{Percent: -20, Mode: "month_start"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.DB.AIUsageSince(ctx, o.ID, "", monthStart(time.Now()), false); got != 1_600_000 {
		t.Fatalf("recalculated month: %d", got)
	}
	// New calls use the discount; a model with an own price uses that price.
	if got := s.RecordAIUsage(ctx, owner, cs, "", m, u); got != 800_000 {
		t.Fatalf("discounted: %d", got)
	}
	if _, err := s.SetOrgPrice(ctx, admin, o.Slug, OrgPriceInput{Mode: "now", Models: map[string]store.AIPrice{"m1": {Input: 1, Output: 1}}}); err != nil {
		t.Fatal(err)
	}
	if got := s.RecordAIUsage(ctx, owner, cs, "", m, u); got != 100_000 {
		t.Fatalf("own model price: %d", got)
	}
	// Own keys keep their charge.
	own := m
	own.Own = true
	if got := s.RecordAIUsage(ctx, owner, cs, "", own, u); got != 1_000_000 {
		t.Fatalf("own key: %d", got)
	}
	h, err := s.OrgPriceHistory(ctx, admin, o.Slug)
	if err != nil || len(h.History) != 2 || h.Current == nil || h.Current.Models["m1"].Input != 1 {
		t.Fatalf("history: %v %+v", err, h)
	}
	if _, err := s.SetOrgPrice(ctx, admin, o.Slug, OrgPriceInput{Percent: -200, Mode: "now"}); err == nil {
		t.Fatal("percent below -100 must fail")
	}
	if _, err := s.SetOrgPrice(ctx, admin, o.Slug, OrgPriceInput{Mode: "date", From: "morgen"}); err == nil {
		t.Fatal("bad date must fail")
	}
}

func TestHiddenPrices(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, stranger := setup(t)
	admin := Actor{User: store.User{ID: stranger.User.ID, Name: stranger.User.Name, IsAdmin: true}, Via: "web"}
	o, err := s.CreateOrg(ctx, admin, OrgInput{Name: "Firma", AdminEmail: "owner@x.de"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetOrgMember(ctx, owner, o.Slug, "reader@x.de", store.OrgMemberRole); err != nil {
		t.Fatal(err)
	}
	p, _ := s.CreateProject(ctx, reader, ProjectInput{Name: "Team", Org: &o.Slug})
	_ = s.DB.AddAIUsage(ctx, store.AIUsage{ID: newID(), OrgID: o.ID, UserID: reader.User.ID, ProjectID: p.ID, Model: "m1", InputTokens: 700, OutputTokens: 300,
		ChargeMicros: 500_000, ListMicros: 500_000, CreatedAt: store.Now()})
	budget := 1000
	on := true
	// Organization admins hide prices from members; only system admins may
	// hide them from organization admins.
	if _, err := s.UpdateOrg(ctx, owner, o.Slug, OrgInput{PricesHidden: &on, AIChatBudget: &budget}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateOrg(ctx, owner, o.Slug, OrgInput{PricesHiddenAdmin: &on}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("org admins must not set the admin switch: %v", err)
	}
	pr, err := s.ProjectUsage(ctx, reader, p.Slug)
	if err != nil || !pr.PricesHidden || pr.ChargedCents != 0 || pr.Tokens != 1000 || pr.Calls != 1 {
		t.Fatalf("member view: %v %+v", err, pr)
	}
	var own *UsageBudget
	for i := range pr.Budgets {
		if pr.Budgets[i].Key == "usage.budget.org_own" {
			own = &pr.Budgets[i]
		}
	}
	if own == nil || own.Percent != 5 || own.UsedCents != 0 || own.Limit != 0 || !own.HasLimit {
		t.Fatalf("budget as percent: %+v", pr.Budgets)
	}
	for _, l := range pr.ByUser {
		if l.ChargedCents != 0 {
			t.Fatalf("lines must not show amounts: %+v", l)
		}
	}
	or, err := s.OrgUsage(ctx, owner, o.Slug)
	if err != nil || or.PricesHidden || or.ChargedCents != 50 {
		t.Fatalf("org admins see prices: %v %+v", err, or)
	}
	tokens := int64(10_000)
	if _, err := s.UpdateOrg(ctx, admin, o.Slug, OrgInput{PricesHiddenAdmin: &on, IncludedTokens: &tokens}); err != nil {
		t.Fatal(err)
	}
	or, err = s.OrgUsage(ctx, owner, o.Slug)
	if err != nil || !or.PricesHidden || or.Budgets[0].LimitTokens != 10_000 || or.Budgets[0].UsedTokens != 1000 || or.Budgets[0].Percent != 10 {
		t.Fatalf("admin switch binds org admins, tokens: %v %+v", err, or)
	}
	cs, err := s.ChatStatus(ctx, reader, p.Slug)
	if err != nil || !cs.PricesHidden {
		t.Fatalf("chat status: %v %+v", err, cs)
	}
	v := cs.View()
	if v.OrgSpent != 0 || v.OrgBudgetPercent != 5 {
		t.Fatalf("chat view: %+v", v)
	}
	bad := "admins,everyone"
	if _, err := s.UpdateOrg(ctx, owner, o.Slug, OrgInput{Warn: &bad}); err == nil {
		t.Fatal("unknown warning target must fail")
	}
}

func TestBudgetWarnings(t *testing.T) {
	ctx := context.Background()
	s, owner, _, _ := setup(t)
	s.SyncNotify = true
	q, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Home"})
	m := ChatModel{Model: "m1", charge: ai.Price{Input: 10, Output: 10}}
	cs := ChatStatus{ProjectID: q.ID, UserLimit: 100}         // $1.00
	u := ai.Usage{Model: "m1", Input: 42_500, Output: 42_500} // $0.85
	s.RecordAIUsage(ctx, owner, cs, "", m, u)
	s.RecordAIUsage(ctx, owner, cs, "", m, ai.Usage{Model: "m1", Input: 100}) // still 80 %: no second warning
	list, err := s.Notifications(ctx, owner, false, 20)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, x := range list.Items {
		if x.Kind == "budget.warning" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("one 80 %% warning expected, got %d: %+v", n, list.Items)
	}
	s.RecordAIUsage(ctx, owner, cs, "", m, u) // over 100 %
	list, _ = s.Notifications(ctx, owner, false, 20)
	n = 0
	for _, x := range list.Items {
		if x.Kind == "budget.warning" {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("100 %% warning expected, got %d", n)
	}
}
