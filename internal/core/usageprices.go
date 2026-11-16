package core

import (
	"context"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/phoedox/phoedox/internal/ai"
	"github.com/phoedox/phoedox/internal/store"
)

// Usage and prices (usage-prices-concept): who sees prices, an
// organization's own token prices from a chosen moment on, and warnings
// at 80 % and 100 % of a budget.

const settingPricesHiddenPersonal = "ai.prices_hidden_personal"

// warnTargets are who can get an organization's budget warnings.
var warnTargets = []string{"admins", "members", "owners"}

func applyOrgPrices(a Actor, o *store.Org, in OrgInput) error {
	if in.PricesHiddenAdmin != nil || in.IncludedTokens != nil {
		if !a.User.IsAdmin {
			return ErrForbidden
		}
	}
	if in.PricesHiddenAdmin != nil {
		o.PricesHiddenAdmin = *in.PricesHiddenAdmin
	}
	if in.IncludedTokens != nil {
		if *in.IncludedTokens < 0 || *in.IncludedTokens > 1_000_000_000_000 {
			return invalid("validation.included_tokens", nil, "included tokens must be between 0 and 10^12")
		}
		o.IncludedTokens = *in.IncludedTokens
	}
	if in.PricesHidden != nil {
		o.PricesHidden = *in.PricesHidden
	}
	if in.AIRequests != nil {
		o.AIRequests = *in.AIRequests
	}
	if in.Warn != nil {
		parts := []string{}
		for _, p := range strings.Split(*in.Warn, ",") {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			if !slices.Contains(warnTargets, p) {
				return invalid("validation.warn_target", map[string]any{"value": p}, "unknown warning target %q", p)
			}
			if !slices.Contains(parts, p) {
				parts = append(parts, p)
			}
		}
		o.Warn = strings.Join(parts, ",")
		if o.Warn == "" {
			o.Warn = "none"
		}
	}
	return nil
}

// pricesHidden tells whether a person sees amounts for usage in an
// organization ("" = personal projects). System admins always see them;
// the system admins' switch also binds organization admins.
func (s *Service) pricesHidden(ctx context.Context, a Actor, orgID string) bool {
	if a.User.IsAdmin {
		return false
	}
	if orgID == "" {
		v, _ := s.DB.Setting(ctx, settingPricesHiddenPersonal, "false")
		return v == "true"
	}
	o, err := s.DB.OrgFor(ctx, orgID, a.User.ID)
	if err != nil {
		return false
	}
	return o.PricesHiddenAdmin || (o.PricesHidden && o.Role != store.OrgAdmin)
}

// percentOf is used/limit in percent (0 without a limit).
func percentOf(used, limit int64) float64 {
	if limit <= 0 {
		return 0
	}
	return math.Round(float64(used)*1000/float64(limit)) / 10
}

// OrgPriceInput changes an organization's own token prices (system admins).
type OrgPriceInput struct {
	// Percent is a discount (negative) or markup on all in-app prices.
	Percent float64 `json:"percent"`
	// Models are own prices for single models (US dollars per million tokens).
	Models map[string]store.AIPrice `json:"models,omitempty"`
	// Mode: date (From, the default), now, next_month or month_start.
	Mode string `json:"mode"`
	From string `json:"from,omitempty"` // YYYY-MM-DD (UTC) for mode date
}

// OrgPriceBudget shows how a price change affects a budget this month.
type OrgPriceBudget struct {
	Key         string `json:"key"`
	LimitCents  int64  `json:"limitCents"`
	BeforeCents int64  `json:"beforeCents"`
	AfterCents  int64  `json:"afterCents"`
	Exceeded    bool   `json:"exceeded"`
}

// OrgPricePreview is what a price change does to this month.
type OrgPricePreview struct {
	EffectiveFrom store.Time       `json:"effectiveFrom"`
	BeforeCents   int64            `json:"beforeCents"`
	AfterCents    int64            `json:"afterCents"`
	Budgets       []OrgPriceBudget `json:"budgets"`
}

// OrgPrices is an organization's price history (newest first).
type OrgPrices struct {
	Current *store.OrgAIPrice  `json:"current"`
	History []store.OrgAIPrice `json:"history"`
}

func (s *Service) adminOrg(ctx context.Context, a Actor, ref string) (store.Org, error) {
	if !a.User.IsAdmin || a.Via == "mcp" {
		return store.Org{}, ErrForbidden
	}
	return s.org(ctx, a, ref, store.OrgAdmin)
}

// effectiveFrom is the moment a price change takes effect.
func effectiveFrom(in OrgPriceInput, now time.Time) (time.Time, error) {
	switch in.Mode {
	case "now":
		return now, nil
	case "next_month":
		return monthStart(now).AddDate(0, 1, 0), nil
	case "month_start":
		return monthStart(now), nil
	case "", "date":
		d, err := time.Parse("2006-01-02", in.From)
		if err != nil {
			return time.Time{}, invalid("validation.price_date", nil, "the date must be YYYY-MM-DD")
		}
		return d.UTC(), nil
	}
	return time.Time{}, invalid("validation.price_mode", nil, "unknown mode %q", in.Mode)
}

func validPrices(in OrgPriceInput) error {
	if in.Percent < -100 || in.Percent > 1000 || math.IsNaN(in.Percent) {
		return invalid("validation.price_percent", nil, "the percentage must be between -100 and 1000")
	}
	for m, p := range in.Models {
		if strings.TrimSpace(m) == "" || p.Input < 0 || p.Output < 0 || p.CacheRead < 0 || p.CacheWrite < 0 || p.Input > 10_000 || p.Output > 10_000 {
			return invalid("validation.price_model", map[string]any{"model": m}, "invalid price for %q", m)
		}
	}
	return nil
}

// activePrice is the price change in effect at a moment (prices newest first).
func activePrice(prices []store.OrgAIPrice, at time.Time) *store.OrgAIPrice {
	for i := range prices {
		if !prices[i].EffectiveFrom.After(at) {
			return &prices[i]
		}
	}
	return nil
}

// orgCharge is what a call costs an organization under its own prices.
func orgCharge(p *store.OrgAIPrice, model string, u ai.Usage, list int64) int64 {
	if p == nil {
		return list
	}
	if mp, ok := p.Models[model]; ok {
		return ai.CostMicros(u, ai.Price{Input: mp.Input, Output: mp.Output, CacheRead: mp.CacheRead, CacheWrite: mp.CacheWrite})
	}
	if p.Percent == 0 {
		return list
	}
	return int64(math.Ceil(float64(list) * (100 + p.Percent) / 100))
}

func rowUsage(r store.AIUsageRow) ai.Usage {
	return ai.Usage{Model: r.Model, Input: r.InputTokens, Output: r.OutputTokens, CacheRead: r.CacheRead, CacheWrite: r.CacheWrite}
}

// chargeFor applies the organization's prices to a new call.
func (s *Service) chargeFor(ctx context.Context, orgID string, own bool, model string, u ai.Usage, list int64, at time.Time) int64 {
	if orgID == "" || own {
		return list
	}
	prices, err := s.DB.OrgAIPrices(ctx, orgID)
	if err != nil || len(prices) == 0 {
		return list
	}
	return orgCharge(activePrice(prices, at), model, u, list)
}

// recharge recalculates an organization's ledger since a moment with a
// list of prices; it returns the new charges that differ.
func recharge(rows []store.AIUsageRow, prices []store.OrgAIPrice) (map[string]int64, int64, int64) {
	changed := map[string]int64{}
	var before, after int64
	for _, r := range rows {
		before += r.ChargeMicros
		c := r.ChargeMicros
		if !r.OwnKey {
			c = orgCharge(activePrice(prices, r.CreatedAt.Time), r.Model, rowUsage(r), r.ListMicros)
		}
		after += c
		if c != r.ChargeMicros {
			changed[r.ID] = c
		}
	}
	return changed, before, after
}

// withChange is the price list with a new change (newest first).
func withChange(prices []store.OrgAIPrice, p store.OrgAIPrice) []store.OrgAIPrice {
	out := append([]store.OrgAIPrice{p}, prices...)
	slices.SortStableFunc(out, func(a, b store.OrgAIPrice) int { return b.EffectiveFrom.Compare(a.EffectiveFrom.Time) })
	return out
}

func (s *Service) newPrice(a Actor, o store.Org, in OrgPriceInput, now time.Time) (store.OrgAIPrice, error) {
	if err := validPrices(in); err != nil {
		return store.OrgAIPrice{}, err
	}
	from, err := effectiveFrom(in, now)
	if err != nil {
		return store.OrgAIPrice{}, err
	}
	mode := in.Mode
	if mode == "" {
		mode = "date"
	}
	models := in.Models
	if models == nil {
		models = map[string]store.AIPrice{}
	}
	return store.OrgAIPrice{ID: newID(), OrgID: o.ID, Percent: in.Percent, Models: models, Mode: mode,
		EffectiveFrom: store.Time{Time: from}, CreatedBy: a.User.ID, CreatedAt: store.Time{Time: now}}, nil
}

// PreviewOrgPrice shows what a price change would do to this month.
func (s *Service) PreviewOrgPrice(ctx context.Context, a Actor, ref string, in OrgPriceInput) (OrgPricePreview, error) {
	o, err := s.adminOrg(ctx, a, ref)
	if err != nil {
		return OrgPricePreview{}, err
	}
	now := time.Now().UTC()
	p, err := s.newPrice(a, o, in, now)
	if err != nil {
		return OrgPricePreview{}, err
	}
	prices, err := s.DB.OrgAIPrices(ctx, o.ID)
	if err != nil {
		return OrgPricePreview{}, err
	}
	rows, err := s.DB.AIUsageRows(ctx, o.ID, monthStart(now))
	if err != nil {
		return OrgPricePreview{}, err
	}
	// Only calls after the moment it takes effect change.
	after := withChange(prices, p)
	changed, before, total := recharge(rows, after)
	_ = changed
	out := OrgPricePreview{EffectiveFrom: p.EffectiveFrom, BeforeCents: centsOf(before), AfterCents: centsOf(total), Budgets: []OrgPriceBudget{}}
	st := s.aiChatSettings(ctx)
	var instBefore, instAfter int64
	for _, r := range rows {
		if r.OwnKey {
			continue
		}
		instBefore += r.ChargeMicros
		instAfter += orgCharge(activePrice(after, r.CreatedAt.Time), r.Model, rowUsage(r), r.ListMicros)
	}
	add := func(key string, limit int64, b, a int64) {
		out.Budgets = append(out.Budgets, OrgPriceBudget{Key: key, LimitCents: limit, BeforeCents: centsOf(b), AfterCents: centsOf(a), Exceeded: limit > 0 && centsOf(a) >= limit})
	}
	add("usage.budget.org_instance", int64(orgChatLimit(o, st)), instBefore, instAfter)
	if o.AIChatBudget != nil {
		add("usage.budget.org_own", int64(*o.AIChatBudget), before, total)
	}
	return out, nil
}

// SetOrgPrice stores a price change. A change that takes effect in the
// past (month start or a past date) recalculates the calls since then.
func (s *Service) SetOrgPrice(ctx context.Context, a Actor, ref string, in OrgPriceInput) (OrgPrices, error) {
	o, err := s.adminOrg(ctx, a, ref)
	if err != nil {
		return OrgPrices{}, err
	}
	now := time.Now().UTC()
	p, err := s.newPrice(a, o, in, now)
	if err != nil {
		return OrgPrices{}, err
	}
	if err := s.DB.AddOrgAIPrice(ctx, p); err != nil {
		return OrgPrices{}, err
	}
	if p.EffectiveFrom.Before(now) {
		prices, err := s.DB.OrgAIPrices(ctx, o.ID)
		if err != nil {
			return OrgPrices{}, err
		}
		rows, err := s.DB.AIUsageRows(ctx, o.ID, p.EffectiveFrom.Time)
		if err != nil {
			return OrgPrices{}, err
		}
		changed, _, _ := recharge(rows, prices)
		if err := s.DB.SetAIUsageCharges(ctx, changed); err != nil {
			return OrgPrices{}, err
		}
	}
	s.event(ctx, a, "org.prices", o.ID, p)
	return s.orgPrices(ctx, o.ID)
}

// OrgPriceHistory lists an organization's price changes (system admins).
func (s *Service) OrgPriceHistory(ctx context.Context, a Actor, ref string) (OrgPrices, error) {
	o, err := s.adminOrg(ctx, a, ref)
	if err != nil {
		return OrgPrices{}, err
	}
	return s.orgPrices(ctx, o.ID)
}

// DeleteOrgPrice removes a planned price change.
func (s *Service) DeleteOrgPrice(ctx context.Context, a Actor, ref, id string) (OrgPrices, error) {
	o, err := s.adminOrg(ctx, a, ref)
	if err != nil {
		return OrgPrices{}, err
	}
	if err := s.DB.DeleteOrgAIPrice(ctx, o.ID, id, time.Now()); err != nil {
		return OrgPrices{}, err
	}
	s.event(ctx, a, "org.prices.delete", o.ID, map[string]string{"id": id})
	return s.orgPrices(ctx, o.ID)
}

func (s *Service) orgPrices(ctx context.Context, orgID string) (OrgPrices, error) {
	prices, err := s.DB.OrgAIPrices(ctx, orgID)
	if err != nil {
		return OrgPrices{}, err
	}
	return OrgPrices{Current: activePrice(prices, time.Now()), History: prices}, nil
}

// warnBudgets sends warnings at 80 % and 100 % of the budgets a call
// counted against. Each budget warns once per month and level.
func (s *Service) warnBudgets(ctx context.Context, a Actor, cs ChatStatus) {
	now := time.Now().UTC()
	month := now.Format("2006-01")
	since := monthStart(now)
	check := func(scope string, used, limit int64, to func() []store.User, title string) {
		if limit <= 0 {
			return
		}
		for _, level := range []int{100, 80} {
			if used*100 < limit*int64(level) {
				continue
			}
			first, err := s.DB.MarkBudgetWarning(ctx, scope, month, level)
			if err != nil || !first {
				return
			}
			for _, u := range to() {
				s.notify(ctx, u, Note{Kind: "budget.warning", ProjectID: cs.ProjectID, Subject: scope,
					Title: fmt.Sprintf("%s · %d %%", title, level), Link: "/settings/usage"})
			}
			return
		}
	}
	self := func() []store.User { return []store.User{a.User} }
	// The person's own budget (personal projects or within the organization).
	userUsed, _ := s.DB.AIUsageSince(ctx, cs.OrgID, a.User.ID, since, false)
	check("user:"+a.User.ID+":"+cs.OrgID, centsOf(userUsed), int64(cs.UserLimit), self, a.User.Name)
	if cs.OrgID == "" {
		return
	}
	o, err := s.DB.OrgFor(ctx, cs.OrgID, "")
	if err != nil || o.Warn == "none" {
		return
	}
	targets := func() []store.User { return s.warnRecipients(ctx, o, since) }
	if o.AIChatBudget != nil {
		spent, _ := s.DB.AIUsageSince(ctx, o.ID, "", since, false)
		check("org:"+o.ID+":budget", centsOf(spent), int64(*o.AIChatBudget), targets, o.Name)
	}
	used, _ := s.DB.AIUsageSince(ctx, o.ID, "", since, true)
	check("org:"+o.ID+":limit", centsOf(used), int64(orgChatLimit(o, s.aiChatSettings(ctx))), targets, o.Name)
}

// warnRecipients are the people an organization's budget warnings go to.
func (s *Service) warnRecipients(ctx context.Context, o store.Org, since time.Time) []store.User {
	want := strings.Split(o.Warn, ",")
	seen := map[string]bool{}
	out := []store.User{}
	add := func(id string) {
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		if u, err := s.DB.UserByID(ctx, id); err == nil {
			out = append(out, u)
		}
	}
	if slices.Contains(want, "admins") {
		if ms, err := s.DB.OrgMembers(ctx, o.ID); err == nil {
			for _, m := range ms {
				if m.Role == store.OrgAdmin {
					add(m.UserID)
				}
			}
		}
	}
	if slices.Contains(want, "members") {
		if gs, err := s.DB.AIUsageBy(ctx, store.UsageFilter{OrgID: o.ID, Since: since}, "user"); err == nil {
			for _, g := range gs {
				add(g.Key)
			}
		}
	}
	if slices.Contains(want, "owners") {
		if gs, err := s.DB.AIUsageBy(ctx, store.UsageFilter{OrgID: o.ID, Since: since}, "project"); err == nil {
			for _, g := range gs {
				if ms, err := s.DB.ListMembers(ctx, g.Key); err == nil {
					for _, m := range ms {
						if m.Role == store.RoleOwner {
							add(m.UserID)
						}
					}
				}
			}
		}
	}
	return out
}

// ChatUsage is what one conversation used: an amount, or with hidden
// prices the share of the person's monthly budget.
type ChatUsage struct {
	Cents   int64   `json:"cents,omitempty"`
	Tokens  int64   `json:"tokens"`
	Percent float64 `json:"percent,omitempty"`
}

// ChatUsageOf sums a conversation (cs is the raw status, not its View).
func (s *Service) ChatUsageOf(ctx context.Context, cs ChatStatus, chatID string) ChatUsage {
	charge, tokens, err := s.DB.AIChatCharged(ctx, chatID)
	if err != nil {
		return ChatUsage{}
	}
	u := ChatUsage{Tokens: tokens, Percent: percentOf(centsOf(charge), int64(cs.UserLimit))}
	if !cs.PricesHidden {
		u.Cents = centsOf(charge)
	}
	return u
}
