package core

import (
	"context"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/phoedox/phoedox/internal/store"
)

// AI limits per person in an organization and requests for more usage
// (usage-prices-concept). A member's own limit overrides the organization's
// default per person; an approved request adds to it for one month or for
// good.

// baseUserLimit is a member's monthly limit in cents without this month's
// extra (0 = no limit).
func baseUserLimit(o store.Org, m store.MemberAI, st AIChatSettings) int {
	switch {
	case m.Budget != nil:
		return *m.Budget
	case o.AIChatUserBudget != nil:
		return *o.AIChatUserBudget
	}
	return st.UserBudget
}

// userLimit is a person's monthly limit in an organization, including an
// extra granted for this month.
func (s *Service) userLimit(ctx context.Context, o store.Org, userID string, st AIChatSettings) int {
	m, _ := s.DB.OrgMemberAI(ctx, o.ID, userID)
	base := baseUserLimit(o, m, st)
	if base > 0 && m.ExtraMonth == time.Now().UTC().Format("2006-01") {
		base += int(m.ExtraCents)
	}
	return base
}

// MemberBudget is a member's AI limit and use this month (organization admins).
type MemberBudget struct {
	UserID string `json:"userId"`
	Name   string `json:"name"`
	Email  string `json:"email"`
	// OwnCents is the member's own limit (nil = the organization's default);
	// OwnPercent the same as a share of the default (for hidden prices).
	OwnCents   *int     `json:"ownCents"`
	OwnPercent *float64 `json:"ownPercent,omitempty"`
	LimitCents int64    `json:"limitCents"`
	UsedCents  int64    `json:"usedCents"`
	Percent    float64  `json:"percent"`
	ExtraCents int64    `json:"extraCents,omitempty"`
	Pending    bool     `json:"pending,omitempty"`
}

// MemberBudgets lists the members' AI limits with requests (org admins).
type MemberBudgets struct {
	PricesHidden bool                  `json:"pricesHidden"`
	DefaultCents int                   `json:"defaultCents"`
	Requests     bool                  `json:"requests"`
	Members      []MemberBudget        `json:"members"`
	Pending      []store.BudgetRequest `json:"pending"`
	Recent       []store.BudgetRequest `json:"recent"`
}

func (s *Service) MemberBudgetsOf(ctx context.Context, a Actor, ref string) (MemberBudgets, error) {
	if err := s.fullUser(a); err != nil {
		return MemberBudgets{}, err
	}
	o, err := s.org(ctx, a, ref, store.OrgAdmin)
	if err != nil {
		return MemberBudgets{}, err
	}
	st := s.aiChatSettings(ctx)
	hidden := s.pricesHidden(ctx, a, o.ID)
	def := st.UserBudget
	if o.AIChatUserBudget != nil {
		def = *o.AIChatUserBudget
	}
	out := MemberBudgets{PricesHidden: hidden, DefaultCents: def, Requests: o.AIRequests, Members: []MemberBudget{}}
	ms, err := s.DB.OrgMembers(ctx, o.ID)
	if err != nil {
		return out, err
	}
	ais, _ := s.DB.OrgMemberAIs(ctx, o.ID)
	pending, _ := s.DB.BudgetRequests(ctx, o.ID, "", "pending")
	since := monthStart(time.Now())
	for _, m := range ms {
		ai := ais[m.UserID]
		limit := int64(s.userLimit(ctx, o, m.UserID, st))
		used, _ := s.DB.AIUsageSince(ctx, o.ID, m.UserID, since, false)
		b := MemberBudget{UserID: m.UserID, Name: m.Name, Email: m.Email, OwnCents: ai.Budget, LimitCents: limit, UsedCents: centsOf(used), Percent: percentOf(centsOf(used), limit)}
		if ai.ExtraMonth == time.Now().UTC().Format("2006-01") {
			b.ExtraCents = ai.ExtraCents
		}
		b.Pending = slices.ContainsFunc(pending, func(r store.BudgetRequest) bool { return r.UserID == m.UserID })
		if hidden {
			if ai.Budget != nil && def > 0 {
				p := math.Round(float64(*ai.Budget)*1000/float64(def)) / 10
				b.OwnPercent = &p
			}
			b.OwnCents, b.LimitCents, b.UsedCents, b.ExtraCents = nil, 0, 0, 0
		}
		out.Members = append(out.Members, b)
	}
	out.Pending = pending
	all, _ := s.DB.BudgetRequests(ctx, o.ID, "", "")
	for _, r := range all {
		if r.Status != "pending" && len(out.Recent) < 20 {
			if hidden {
				r.GrantedCents = 0
			}
			out.Recent = append(out.Recent, r)
		}
	}
	if out.Recent == nil {
		out.Recent = []store.BudgetRequest{}
	}
	return out, nil
}

// MemberLimitInput sets a member's own limit: Cents (-1 = back to the
// default) or, with hidden prices, Percent of the default per person.
type MemberLimitInput struct {
	Cents   *int     `json:"cents,omitempty"`
	Percent *float64 `json:"percent,omitempty"`
}

// SetMemberLimit sets a member's AI limit (organization admins).
func (s *Service) SetMemberLimit(ctx context.Context, a Actor, ref, userID string, in MemberLimitInput) (MemberBudgets, error) {
	if err := s.fullUser(a); err != nil {
		return MemberBudgets{}, err
	}
	o, err := s.org(ctx, a, ref, store.OrgAdmin)
	if err != nil {
		return MemberBudgets{}, err
	}
	m, err := s.DB.OrgMemberAI(ctx, o.ID, userID)
	if err != nil {
		return MemberBudgets{}, err
	}
	st := s.aiChatSettings(ctx)
	switch {
	case in.Percent != nil:
		def := st.UserBudget
		if o.AIChatUserBudget != nil {
			def = *o.AIChatUserBudget
		}
		if *in.Percent < 0 || *in.Percent > 100_000 {
			return MemberBudgets{}, invalid("validation.member_limit", nil, "the limit must be between 0 and 100000 percent")
		}
		c := int(math.Round(float64(def) * *in.Percent / 100))
		m.Budget = &c
	case in.Cents != nil && *in.Cents < 0:
		m.Budget = nil
	case in.Cents != nil:
		if err := validBudget(in.Cents); err != nil {
			return MemberBudgets{}, err
		}
		c := *in.Cents
		m.Budget = &c
	}
	if err := s.DB.SetOrgMemberAI(ctx, o.ID, m); err != nil {
		return MemberBudgets{}, err
	}
	s.event(ctx, a, "org.member_limit", o.ID, map[string]any{"user": userID, "cents": m.Budget})
	return s.MemberBudgetsOf(ctx, a, ref)
}

// BudgetRequestInput asks for more AI usage: Percent more (25, 50, 100 or
// 200) for this month or permanently.
type BudgetRequestInput struct {
	Percent int    `json:"percent"`
	Scope   string `json:"scope"`
	Reason  string `json:"reason"`
}

var requestPercents = []int{25, 50, 100, 200}

func validRequest(in BudgetRequestInput) error {
	if !slices.Contains(requestPercents, in.Percent) {
		return invalid("validation.request_percent", nil, "choose 25, 50, 100 or 200 %%")
	}
	if in.Scope != "month" && in.Scope != "permanent" {
		return invalid("validation.request_scope", nil, "scope must be month or permanent")
	}
	return nil
}

// RequestBudget asks the organization's admins for more AI usage.
func (s *Service) RequestBudget(ctx context.Context, a Actor, ref string, in BudgetRequestInput) (store.BudgetRequest, error) {
	if err := s.fullUser(a); err != nil {
		return store.BudgetRequest{}, err
	}
	o, err := s.org(ctx, a, ref, store.OrgMemberRole)
	if err != nil {
		return store.BudgetRequest{}, err
	}
	if !o.AIRequests {
		return store.BudgetRequest{}, invalid("validation.requests_off", nil, "the organization does not take requests for more AI usage")
	}
	if err := validRequest(in); err != nil {
		return store.BudgetRequest{}, err
	}
	if s.userLimit(ctx, o, a.User.ID, s.aiChatSettings(ctx)) == 0 {
		return store.BudgetRequest{}, invalid("validation.request_no_limit", nil, "your usage has no limit")
	}
	open, _ := s.DB.BudgetRequests(ctx, o.ID, a.User.ID, "pending")
	if len(open) > 0 {
		return store.BudgetRequest{}, invalid("validation.request_pending", nil, "a request is already waiting for an answer")
	}
	r := store.BudgetRequest{ID: newID(), OrgID: o.ID, UserID: a.User.ID, UserName: a.User.Name, Percent: in.Percent, Scope: in.Scope,
		Reason: excerpt(strings.TrimSpace(in.Reason), 1000), Status: "pending", CreatedAt: store.Now()}
	if err := s.DB.AddBudgetRequest(ctx, r); err != nil {
		return r, err
	}
	s.event(ctx, a, "org.budget_request", o.ID, r)
	ms, _ := s.DB.OrgMembers(ctx, o.ID)
	for _, m := range ms {
		if m.Role != store.OrgAdmin {
			continue
		}
		if u, err := s.DB.UserByID(ctx, m.UserID); err == nil {
			s.notify(ctx, u, Note{Kind: "budget.request", Subject: r.ID, Title: o.Name, Body: r.Reason, Actor: a.User.Name, Link: "/orgs/" + o.Slug + "#budgets"})
		}
	}
	return r, nil
}

// BudgetDecision answers a request; Percent and Scope may differ from the
// request.
type BudgetDecision struct {
	Approve bool   `json:"approve"`
	Percent int    `json:"percent,omitempty"`
	Scope   string `json:"scope,omitempty"`
	Note    string `json:"note,omitempty"`
}

// DecideBudget approves or rejects a request (organization admins).
func (s *Service) DecideBudget(ctx context.Context, a Actor, ref, id string, in BudgetDecision) (MemberBudgets, error) {
	if err := s.fullUser(a); err != nil {
		return MemberBudgets{}, err
	}
	o, err := s.org(ctx, a, ref, store.OrgAdmin)
	if err != nil {
		return MemberBudgets{}, err
	}
	r, err := s.DB.GetBudgetRequest(ctx, o.ID, id)
	if err != nil {
		return MemberBudgets{}, err
	}
	if r.Status != "pending" {
		return MemberBudgets{}, invalid("validation.request_decided", nil, "the request was already answered")
	}
	if in.Percent != 0 {
		r.Percent = in.Percent
	}
	if in.Scope != "" {
		r.Scope = in.Scope
	}
	if err := validRequest(BudgetRequestInput{Percent: r.Percent, Scope: r.Scope}); err != nil {
		return MemberBudgets{}, err
	}
	now := store.Now()
	r.Status, r.Note, r.DecidedBy, r.DecidedAt = "rejected", excerpt(strings.TrimSpace(in.Note), 1000), a.User.ID, &now
	if in.Approve {
		r.Status = "approved"
		m, err := s.DB.OrgMemberAI(ctx, o.ID, r.UserID)
		if err != nil {
			return MemberBudgets{}, err
		}
		base := baseUserLimit(o, m, s.aiChatSettings(ctx))
		extra := int64(math.Round(float64(base) * float64(r.Percent) / 100))
		r.GrantedCents = extra
		month := time.Now().UTC().Format("2006-01")
		if r.Scope == "permanent" {
			b := base + int(extra)
			m.Budget = &b
		} else {
			if m.ExtraMonth != month {
				m.ExtraCents = 0
			}
			m.ExtraCents += extra
			m.ExtraMonth = month
		}
		if err := s.DB.SetOrgMemberAI(ctx, o.ID, m); err != nil {
			return MemberBudgets{}, err
		}
	}
	if err := s.DB.DecideBudgetRequest(ctx, r); err != nil {
		return MemberBudgets{}, err
	}
	s.event(ctx, a, "org.budget_decision", o.ID, r)
	if u, err := s.DB.UserByID(ctx, r.UserID); err == nil {
		title := o.Name + " · +" + strconv.Itoa(r.Percent) + " %"
		if r.Status == "rejected" {
			title = o.Name
		}
		s.notify(ctx, u, Note{Kind: "budget.request_decided", Subject: r.ID, Title: title, Body: r.Note, Actor: a.User.Name, Link: "/settings/usage"})
	}
	return s.MemberBudgetsOf(ctx, a, ref)
}
