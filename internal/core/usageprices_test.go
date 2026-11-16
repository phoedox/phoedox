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

func TestMemberLimitsAndRequests(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, stranger := setup(t)
	s.SyncNotify = true
	admin := Actor{User: store.User{ID: stranger.User.ID, Name: stranger.User.Name, IsAdmin: true}, Via: "web"}
	o, err := s.CreateOrg(ctx, admin, OrgInput{Name: "Firma", AdminEmail: "owner@x.de"})
	if err != nil {
		t.Fatal(err)
	}
	_ = s.SetOrgMember(ctx, owner, o.Slug, "reader@x.de", store.OrgMemberRole)
	p, _ := s.CreateProject(ctx, reader, ProjectInput{Name: "Team", Org: &o.Slug})
	def := 500
	if _, err := s.UpdateOrg(ctx, owner, o.Slug, OrgInput{AIChatUserBudget: &def}); err != nil {
		t.Fatal(err)
	}
	thousand := 1000
	mb, err := s.SetMemberLimit(ctx, owner, o.Slug, reader.User.ID, MemberLimitInput{Cents: &thousand})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetMemberLimit(ctx, reader, o.Slug, reader.User.ID, MemberLimitInput{Cents: &thousand}); err == nil {
		t.Fatal("members must not set limits")
	}
	var got *MemberBudget
	for i := range mb.Members {
		if mb.Members[i].UserID == reader.User.ID {
			got = &mb.Members[i]
		}
	}
	if got == nil || got.LimitCents != 1000 || got.OwnCents == nil {
		t.Fatalf("member budgets: %+v", mb)
	}
	cs, _ := s.ChatStatus(ctx, reader, p.Slug)
	if cs.UserLimit != 1000 || cs.CanRequest {
		t.Fatalf("own limit in chat: %+v", cs)
	}
	in := BudgetRequestInput{Percent: 50, Scope: "month", Reason: "Release week"}
	if _, err := s.RequestBudget(ctx, reader, o.Slug, in); err == nil {
		t.Fatal("requests are off by default")
	}
	on := true
	if _, err := s.UpdateOrg(ctx, owner, o.Slug, OrgInput{AIRequests: &on}); err != nil {
		t.Fatal(err)
	}
	r, err := s.RequestBudget(ctx, reader, o.Slug, in)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RequestBudget(ctx, reader, o.Slug, in); err == nil {
		t.Fatal("only one pending request")
	}
	if cs, _ := s.ChatStatus(ctx, reader, p.Slug); !cs.CanRequest || !cs.RequestPending {
		t.Fatalf("pending in chat: %+v", cs)
	}
	notes, _ := s.Notifications(ctx, owner, false, 10)
	if len(notes.Items) == 0 || notes.Items[0].Kind != "budget.request" {
		t.Fatalf("admins are notified: %+v", notes.Items)
	}
	// Approved with 100 % for this month: 1000 + 1000.
	if _, err := s.DecideBudget(ctx, owner, o.Slug, r.ID, BudgetDecision{Approve: true, Percent: 100}); err != nil {
		t.Fatal(err)
	}
	if cs, _ := s.ChatStatus(ctx, reader, p.Slug); cs.UserLimit != 2000 || cs.RequestPending {
		t.Fatalf("month extra: %+v", cs)
	}
	if _, err := s.DecideBudget(ctx, owner, o.Slug, r.ID, BudgetDecision{Approve: true}); err == nil {
		t.Fatal("a request is answered once")
	}
	notes, _ = s.Notifications(ctx, reader, false, 10)
	if len(notes.Items) == 0 || notes.Items[0].Kind != "budget.request_decided" {
		t.Fatalf("member is told: %+v", notes.Items)
	}
	// Permanent: the own limit grows (1000 + 25 %).
	r2, _ := s.RequestBudget(ctx, reader, o.Slug, BudgetRequestInput{Percent: 25, Scope: "permanent"})
	if _, err := s.DecideBudget(ctx, owner, o.Slug, r2.ID, BudgetDecision{Approve: true}); err != nil {
		t.Fatal(err)
	}
	m, _ := s.DB.OrgMemberAI(ctx, o.ID, reader.User.ID)
	if m.Budget == nil || *m.Budget != 1250 {
		t.Fatalf("permanent: %+v", m)
	}
	// Rejected requests change nothing.
	r3, _ := s.RequestBudget(ctx, reader, o.Slug, in)
	if _, err := s.DecideBudget(ctx, owner, o.Slug, r3.ID, BudgetDecision{Approve: false, Note: "Not now"}); err != nil {
		t.Fatal(err)
	}
	// With hidden prices, admins set limits in percent of the default.
	if _, err := s.UpdateOrg(ctx, admin, o.Slug, OrgInput{PricesHiddenAdmin: &on}); err != nil {
		t.Fatal(err)
	}
	pct := 300.0
	mb, err = s.SetMemberLimit(ctx, owner, o.Slug, reader.User.ID, MemberLimitInput{Percent: &pct})
	if err != nil || !mb.PricesHidden {
		t.Fatalf("percent limit: %v %+v", err, mb)
	}
	m, _ = s.DB.OrgMemberAI(ctx, o.ID, reader.User.ID)
	if m.Budget == nil || *m.Budget != 1500 {
		t.Fatalf("300 %% of 500: %+v", m)
	}
	for _, x := range mb.Members {
		if x.UserID == reader.User.ID && (x.OwnPercent == nil || *x.OwnPercent != 300 || x.LimitCents != 0) {
			t.Fatalf("hidden member view: %+v", x)
		}
	}
	back := -1
	if _, err := s.SetMemberLimit(ctx, owner, o.Slug, reader.User.ID, MemberLimitInput{Cents: &back}); err != nil {
		t.Fatal(err)
	}
	if m, _ := s.DB.OrgMemberAI(ctx, o.ID, reader.User.ID); m.Budget != nil {
		t.Fatalf("back to default: %+v", m)
	}
}
