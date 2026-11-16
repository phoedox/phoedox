package core

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/phoedox/phoedox/internal/ai"
	"github.com/phoedox/phoedox/internal/store"
)

// AI chat (ADR-0066): switches and monthly budgets. System admins switch the
// chat on or off for the instance and set default budgets and an instance
// cap; organization admins switch it for their organization and set a lower
// budget (system admins can raise or lower an organization's limit). Budgets
// are in cents per month (UTC); 0 means no limit.

const (
	settingAIChat           = "ai_chat_enabled"
	settingAIChatOrgBudget  = "ai_chat_org_budget"
	settingAIChatUserBudget = "ai_chat_user_budget"
	settingAIChatCap        = "ai_chat_instance_cap"
	settingAIChatModel      = "ai_chat_model"
)

// AIChatSettings are the instance settings of the AI chat.
type AIChatSettings struct {
	Enabled bool `json:"enabled"`
	// Configured tells whether the instance has a model (read only).
	Configured bool `json:"configured"`
	// DefaultModel is the instance's default model ("provider/model").
	DefaultModel string `json:"defaultModel"`
	// OrgBudget is the default monthly limit of each organization, UserBudget
	// the default limit of each person, InstanceCap the limit of all chats.
	OrgBudget   int `json:"orgBudget"`
	UserBudget  int `json:"userBudget"`
	InstanceCap int `json:"instanceCap"`
	// UsedCents is what the providers cost this month, ChargedCents what
	// people were charged (in-app prices; read only).
	UsedCents    int64 `json:"usedCents"`
	ChargedCents int64 `json:"chargedCents"`
	// PricesHiddenPersonal hides amounts from people for their personal
	// projects (usage-prices-concept); they see percentages instead.
	PricesHiddenPersonal bool `json:"pricesHiddenPersonal"`
}

// AIChatPatch changes the instance settings of the AI chat.
type AIChatPatch struct {
	Enabled     *bool `json:"enabled,omitempty"`
	OrgBudget   *int  `json:"orgBudget,omitempty"`
	UserBudget  *int  `json:"userBudget,omitempty"`
	InstanceCap *int  `json:"instanceCap,omitempty"`
	// DefaultModel is a model ref of an instance provider ("" = the first).
	DefaultModel         *string `json:"defaultModel,omitempty"`
	PricesHiddenPersonal *bool   `json:"pricesHiddenPersonal,omitempty"`
}

// monthStart is the first moment of the current month (UTC).
func monthStart(now time.Time) time.Time {
	now = now.UTC()
	return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
}

func (s *Service) intSetting(ctx context.Context, key string, def int) int {
	v, err := s.DB.Setting(ctx, key, strconv.Itoa(def))
	if err != nil {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func (s *Service) aiChatSettings(ctx context.Context) AIChatSettings {
	v, _ := s.DB.Setting(ctx, settingAIChat, "true")
	used, _ := s.DB.AIUsageTotalSince(ctx, monthStart(time.Now()))
	model, _ := s.DB.Setting(ctx, settingAIChatModel, "")
	out := AIChatSettings{
		Enabled: v != "false", Configured: len(s.instanceModels(ctx)) > 0, DefaultModel: model,
		OrgBudget:   s.intSetting(ctx, settingAIChatOrgBudget, 2000),
		UserBudget:  s.intSetting(ctx, settingAIChatUserBudget, 500),
		InstanceCap: s.intSetting(ctx, settingAIChatCap, 0),
		UsedCents:   centsOf(used),
	}
	charged, _ := s.DB.AIChargedTotalSince(ctx, monthStart(time.Now()))
	out.ChargedCents = centsOf(charged)
	hidden, _ := s.DB.Setting(ctx, settingPricesHiddenPersonal, "false")
	out.PricesHiddenPersonal = hidden == "true"
	return out
}

func centsOf(micros int64) int64 { return (micros + 9_999) / 10_000 }

func validBudget(v *int) error {
	if v != nil && (*v < 0 || *v > 100_000_000) {
		return invalid("validation.ai_budget", nil, "budgets must be between 0 and 100000000 cents")
	}
	return nil
}

func (s *Service) saveAIChatSettings(ctx context.Context, a Actor, in *AIChatPatch) error {
	if in == nil {
		return nil
	}
	for _, v := range []*int{in.OrgBudget, in.UserBudget, in.InstanceCap} {
		if err := validBudget(v); err != nil {
			return err
		}
	}
	if in.DefaultModel != nil {
		if *in.DefaultModel != "" && !hasModel(s.instanceModels(ctx), *in.DefaultModel) {
			return invalid("validation.ai_model_unknown", nil, "unknown model %q", *in.DefaultModel)
		}
		if err := s.DB.SetSetting(ctx, settingAIChatModel, *in.DefaultModel, a.String()); err != nil {
			return err
		}
	}
	if in.Enabled != nil {
		if err := s.DB.SetSetting(ctx, settingAIChat, strconv.FormatBool(*in.Enabled), a.String()); err != nil {
			return err
		}
	}
	if in.PricesHiddenPersonal != nil {
		if err := s.DB.SetSetting(ctx, settingPricesHiddenPersonal, strconv.FormatBool(*in.PricesHiddenPersonal), a.String()); err != nil {
			return err
		}
	}
	for key, v := range map[string]*int{settingAIChatOrgBudget: in.OrgBudget, settingAIChatUserBudget: in.UserBudget, settingAIChatCap: in.InstanceCap} {
		if v != nil {
			if err := s.DB.SetSetting(ctx, key, strconv.Itoa(*v), a.String()); err != nil {
				return err
			}
		}
	}
	return nil
}

// lower returns the smaller of two limits where 0 means no limit.
func lower(a, b int) int {
	switch {
	case a == 0:
		return b
	case b == 0:
		return a
	}
	return min(a, b)
}

// orgChatLimit is the limit system admins allow an organization: its own
// cap if set, otherwise the instance default.
func orgChatLimit(o store.Org, st AIChatSettings) int {
	if o.AIChatCap != nil {
		return *o.AIChatCap
	}
	return st.OrgBudget
}

// OrgAIChat describes the chat budget of an organization for its admins.
type OrgAIChat struct {
	Enabled bool `json:"enabled"`
	// Limit is what the organization may spend on the instance's models per
	// month (from system admins; 0 = no limit); Budget its own limit for
	// all models including its own keys (nil = none); UserBudget the limit
	// per person (nil = the instance default).
	Limit             int   `json:"limit"`
	Budget            *int  `json:"budget"`
	UserBudget        *int  `json:"userBudget"`
	DefaultUserBudget int   `json:"defaultUserBudget"`
	UsedCents         int64 `json:"usedCents"`
	// UsedInstanceCents is the part paid by the instance (not own keys).
	UsedInstanceCents int64 `json:"usedInstanceCents"`
	// DefaultModel is the organization's default model ("" = the instance's).
	DefaultModel string `json:"defaultModel"`
	// Available is false when the instance has no provider or switched the chat off.
	Available bool `json:"available"`
	// Price visibility, included tokens and budget warnings (usage-prices-concept).
	PricesHiddenAdmin bool   `json:"pricesHiddenAdmin"`
	PricesHidden      bool   `json:"pricesHidden"`
	IncludedTokens    int64  `json:"includedTokens"`
	Warn              string `json:"warn"`
	AIRequests        bool   `json:"aiRequests"`
}

// OrgAIChatInfo returns the chat settings of an organization (its admins).
func (s *Service) OrgAIChatInfo(ctx context.Context, a Actor, ref string) (OrgAIChat, error) {
	if err := s.fullUser(a); err != nil {
		return OrgAIChat{}, err
	}
	o, err := s.org(ctx, a, ref, store.OrgAdmin)
	if err != nil {
		return OrgAIChat{}, err
	}
	st := s.aiChatSettings(ctx)
	used, _ := s.DB.AIUsageSince(ctx, o.ID, "", monthStart(time.Now()), false)
	inst, _ := s.DB.AIUsageSince(ctx, o.ID, "", monthStart(time.Now()), true)
	own, _ := s.DB.AIProviders(ctx, o.ID)
	return OrgAIChat{Enabled: o.AIChat, Limit: orgChatLimit(o, st), Budget: o.AIChatBudget, UserBudget: o.AIChatUserBudget,
		DefaultUserBudget: st.UserBudget, UsedCents: centsOf(used), UsedInstanceCents: centsOf(inst), DefaultModel: o.AIChatModel,
		Available:         (st.Configured || len(own) > 0) && st.Enabled && s.AIEnabled(ctx),
		PricesHiddenAdmin: o.PricesHiddenAdmin, PricesHidden: o.PricesHidden, IncludedTokens: o.IncludedTokens, Warn: o.Warn, AIRequests: o.AIRequests}, nil
}

// applyOrgAIChat applies the chat fields of an OrgInput. A negative budget
// clears it (back to the default).
func (s *Service) applyOrgAIChat(ctx context.Context, a Actor, o *store.Org, in OrgInput) error {
	if in.AIChat != nil {
		o.AIChat = *in.AIChat
	}
	clear := func(v *int) *int {
		if v == nil || *v < 0 {
			return nil
		}
		n := *v
		return &n
	}
	if in.AIChatCap != nil {
		if !a.User.IsAdmin {
			return ErrForbidden
		}
		if err := validBudget(clear(in.AIChatCap)); err != nil {
			return err
		}
		o.AIChatCap = clear(in.AIChatCap)
	}
	if in.AIChatBudget != nil {
		b := clear(in.AIChatBudget)
		if err := validBudget(b); err != nil {
			return err
		}
		o.AIChatBudget = b
	}
	if in.AIChatModel != nil {
		if *in.AIChatModel != "" && !hasModel(append(s.orgModels(ctx, o.ID), s.instanceModels(ctx)...), *in.AIChatModel) {
			return invalid("validation.ai_model_unknown", nil, "unknown model %q", *in.AIChatModel)
		}
		o.AIChatModel = *in.AIChatModel
	}
	if in.AIChatUserBudget != nil {
		b := clear(in.AIChatUserBudget)
		if err := validBudget(b); err != nil {
			return err
		}
		o.AIChatUserBudget = b
	}
	return nil
}

// ChatStatus tells a person whether they can use the AI chat in a project
// and how much of their budget is left.
type ChatStatus struct {
	Available bool `json:"available"`
	// Reason is an i18n key when the chat is not available.
	Reason string `json:"reason,omitempty"`
	// Budgets in cents per month (0 = no limit) and what was used. UserLimit
	// and OrgBudget count every model; OrgLimit and the instance cap only
	// the instance's models (not the organization's own keys).
	UserLimit int   `json:"userLimit"`
	UserUsed  int64 `json:"userUsed"`
	OrgLimit  int   `json:"orgLimit"`
	OrgUsed   int64 `json:"orgUsed"`
	OrgBudget int   `json:"orgBudget"`
	OrgSpent  int64 `json:"orgSpent"`
	// InstanceBlocked: the instance's models are used up for this month;
	// the organization's own models still work.
	InstanceBlocked bool `json:"instanceBlocked,omitempty"`
	// PricesHidden: amounts are left out; the percentages below show how
	// much of each budget is used (usage-prices-concept).
	PricesHidden     bool    `json:"pricesHidden,omitempty"`
	UserPercent      float64 `json:"userPercent,omitempty"`
	OrgPercent       float64 `json:"orgPercent,omitempty"`
	OrgBudgetPercent float64 `json:"orgBudgetPercent,omitempty"`
	// CanRequest: the person may ask the organization (OrgSlug) for more;
	// RequestPending: a request waits for an answer.
	OrgSlug        string `json:"orgSlug,omitempty"`
	OrgName        string `json:"orgName,omitempty"`
	CanRequest     bool   `json:"canRequest,omitempty"`
	RequestPending bool   `json:"requestPending,omitempty"`
	// WriteMode is the project's AI write mode (read: the AI only reads).
	WriteMode string `json:"writeMode"`
	OrgID     string `json:"-"`
	ProjectID string `json:"-"`
}

// View is the status as the person sees it: with hidden prices, amounts
// become percentages of the budgets.
func (cs ChatStatus) View() ChatStatus {
	if !cs.PricesHidden {
		return cs
	}
	cs.UserPercent = percentOf(cs.UserUsed, int64(cs.UserLimit))
	cs.OrgPercent = percentOf(cs.OrgUsed, int64(cs.OrgLimit))
	cs.OrgBudgetPercent = percentOf(cs.OrgSpent, int64(cs.OrgBudget))
	cs.UserUsed, cs.OrgUsed, cs.OrgSpent = 0, 0, 0
	cs.UserLimit = min(cs.UserLimit, 1)
	cs.OrgLimit = min(cs.OrgLimit, 1)
	cs.OrgBudget = min(cs.OrgBudget, 1)
	return cs
}

// ChatStatus checks switches and budgets for a person in a project.
func (s *Service) ChatStatus(ctx context.Context, a Actor, ref string) (ChatStatus, error) {
	if err := s.fullUser(a); err != nil {
		return ChatStatus{}, err
	}
	p, err := s.project(ctx, a, ref, store.RoleReader)
	if err != nil {
		return ChatStatus{}, err
	}
	st := s.aiChatSettings(ctx)
	cs := ChatStatus{OrgID: p.OrgID, ProjectID: p.ID, WriteMode: p.AIWriteMode}
	models := s.projectModels(ctx, p)
	switch {
	case len(models) == 0:
		cs.Reason = "ai_chat.unavailable.not_configured"
	case !st.Enabled || !s.AIEnabled(ctx):
		cs.Reason = "ai_chat.unavailable.instance_off"
	case p.AIWriteMode == AIOff:
		cs.Reason = "ai_chat.unavailable.project_off"
	}
	now := monthStart(time.Now())
	cs.UserLimit = st.UserBudget
	if p.OrgID != "" {
		o, err := s.DB.OrgFor(ctx, p.OrgID, a.User.ID)
		if err != nil {
			return cs, err
		}
		if cs.Reason == "" && (!p.OrgAI || !o.AIChat) {
			cs.Reason = "ai_chat.unavailable.org_off"
		}
		cs.OrgLimit = orgChatLimit(o, st)
		if o.AIChatBudget != nil {
			cs.OrgBudget = *o.AIChatBudget
		}
		cs.UserLimit = s.userLimit(ctx, o, a.User.ID, st)
		if o.AIRequests && o.Role != "" && cs.UserLimit > 0 {
			cs.OrgSlug, cs.OrgName, cs.CanRequest = o.Slug, o.Name, true
			open, _ := s.DB.BudgetRequests(ctx, o.ID, a.User.ID, "pending")
			cs.RequestPending = len(open) > 0
		}
		used, _ := s.DB.AIUsageSince(ctx, p.OrgID, "", now, true)
		spent, _ := s.DB.AIUsageSince(ctx, p.OrgID, "", now, false)
		cs.OrgUsed, cs.OrgSpent = centsOf(used), centsOf(spent)
	}
	used, _ := s.DB.AIUsageSince(ctx, p.OrgID, a.User.ID, now, false)
	cs.UserUsed = centsOf(used)
	total, _ := s.DB.AIUsageTotalSince(ctx, now)
	cs.InstanceBlocked = (st.InstanceCap > 0 && centsOf(total) >= int64(st.InstanceCap)) || (cs.OrgLimit > 0 && cs.OrgUsed >= int64(cs.OrgLimit))
	ownModels := false
	for _, m := range models {
		ownModels = ownModels || m.Own
	}
	if cs.Reason == "" {
		switch {
		case cs.OrgBudget > 0 && cs.OrgSpent >= int64(cs.OrgBudget):
			cs.Reason = "ai_chat.unavailable.org_budget"
		case cs.UserLimit > 0 && cs.UserUsed >= int64(cs.UserLimit):
			cs.Reason = "ai_chat.unavailable.user_budget"
		case cs.InstanceBlocked && !ownModels:
			if st.InstanceCap > 0 && centsOf(total) >= int64(st.InstanceCap) {
				cs.Reason = "ai_chat.unavailable.instance_budget"
			} else {
				cs.Reason = "ai_chat.unavailable.org_budget"
			}
		}
	}
	cs.Available = cs.Reason == ""
	cs.PricesHidden = s.pricesHidden(ctx, a, p.OrgID)
	return cs, nil
}

// RecordAIUsage writes a model call to the usage ledger and returns the
// charged amount in micro-dollars (the in-app price).
func (s *Service) RecordAIUsage(ctx context.Context, a Actor, cs ChatStatus, chatID string, m ChatModel, u ai.Usage) int64 {
	cost := ai.CostMicros(u, m.cost)
	list := ai.CostMicros(u, m.charge)
	now := store.Now()
	charge := s.chargeFor(ctx, cs.OrgID, m.Own, u.Model, u, list, now.Time)
	_ = s.DB.AddAIUsage(ctx, store.AIUsage{ID: newID(), OrgID: cs.OrgID, UserID: a.User.ID, ProjectID: cs.ProjectID, ChatID: chatID,
		ProviderID: m.ProviderID, OwnKey: m.Own, Model: u.Model,
		InputTokens: u.Input, OutputTokens: u.Output, CacheRead: u.CacheRead, CacheWrite: u.CacheWrite, CostMicros: cost, ChargeMicros: charge, ListMicros: list, CreatedAt: now})
	s.warnBudgets(ctx, a, cs)
	return charge
}

// AIDraftEdit lets the AI chat write a page into its draft: the draft is
// opened if nobody edits the page, people editing it see the change live,
// and publishing stays with people (ADR-0027, ADR-0066). Unlike other AI
// writes this is no suggestion, because the draft itself is reviewed before
// it is published. Projects where AI only reads refuse it.
func (s *Service) AIDraftEdit(ctx context.Context, a Actor, ref, docRef string, markdown, title *string) (DraftInfo, error) {
	if a.Via != "mcp" {
		return DraftInfo{}, ErrForbidden
	}
	p, doc, err := s.docAccess(ctx, a, ref, docRef, store.RoleEditor)
	if err != nil {
		return DraftInfo{}, err
	}
	if p.AIWriteMode != AISuggest && p.AIWriteMode != AIDirect {
		return DraftInfo{}, ErrForbidden
	}
	if !hasDrafts(doc.Type) || doc.Type != store.DocMarkdown {
		return DraftInfo{}, invalid("validation.ai_edit_pages_only", nil, "only text pages can be edited in the chat")
	}
	if _, err := s.DB.GetSkill(ctx, doc.ID); err == nil {
		return DraftInfo{}, invalid("validation.ai_edit_skill", nil, "pages with instructions for AI are changed by people only")
	}
	if _, err := s.StartDraft(ctx, a, ref, docRef); err != nil {
		return DraftInfo{}, err
	}
	res, _, err := s.draftWrite(ctx, a, p, doc, DocumentPatch{Markdown: markdown, Title: title})
	if err != nil {
		return DraftInfo{}, err
	}
	if res.Draft == nil {
		return DraftInfo{}, invalid("validation.draft_ended", nil, "the draft was published or cancelled")
	}
	return *res.Draft, nil
}

// DraftText returns the text an AI edit starts from: the open draft or the
// published page.
func (s *Service) DraftText(ctx context.Context, a Actor, ref, docRef string) (string, string, error) {
	_, doc, err := s.docAccess(ctx, a, ref, docRef, store.RoleReader)
	if err != nil {
		return "", "", err
	}
	if d, err := s.DB.GetDraft(ctx, doc.ID); err == nil {
		return d.Markdown, d.Title, nil
	}
	return draftBody(doc), doc.Title, nil
}

// trimTitle shortens the first message to a chat title.
func trimTitle(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 60 {
		return string(r[:59]) + "…"
	}
	return s
}

// ChatTitle is the title of a new chat from its first message.
func ChatTitle(first string) string { return trimTitle(first) }
