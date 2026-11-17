package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/phoedox/phoedox/internal/ai"
	"github.com/phoedox/phoedox/internal/store"
)

// AI in background jobs (ADR-0077): calls no person starts (feedback
// triage, later automations) count against the organization's monthly
// budget, or the owner's in personal projects. Each project has a limit:
// the organization's general limit, a limit for that project (organization
// admins) and the owner's lower limit – the lowest applies. At the limit,
// jobs fall back to their non-AI way for the rest of the month.

// BackgroundAILimits are the limits that apply to a project (cents per
// month; nil = not set).
type BackgroundAILimits struct {
	OrgGeneral *int `json:"orgGeneral,omitempty"`
	OrgProject *int `json:"orgProject,omitempty"`
	Owner      *int `json:"owner,omitempty"`
}

// BackgroundAIStatus is a project's background AI this month.
type BackgroundAIStatus struct {
	BackgroundAILimits
	// Limit is the lowest limit (0 = none), Used the charged cents.
	Limit int   `json:"limit"`
	Used  int64 `json:"used"`
	// Available: a background job may call the AI now; Reason when not.
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
	// CanSetOrg: organization admin of the project's organization.
	CanSetOrg   bool   `json:"canSetOrg"`
	CanSetOwner bool   `json:"canSetOwner"`
	Org         string `json:"org,omitempty"`
	// Percent replaces the amounts when prices are hidden.
	PricesHidden bool    `json:"pricesHidden,omitempty"`
	Percent      float64 `json:"percent,omitempty"`
}

type orgBGLimits struct {
	General  *int           `json:"general,omitempty"`
	Projects map[string]int `json:"projects,omitempty"` // project id → cents
}

func bgOrgKey(orgID string) string       { return "bgai.org:" + orgID }
func bgOwnerKey(projectID string) string { return "bgai.owner:" + projectID }
func bgChatID(job string) string         { return "bg:" + job }
func bgScope(projectID string) string    { return "bgai:" + projectID }
func intPtr(v int) *int                  { return &v }

func (s *Service) orgBGLimits(ctx context.Context, orgID string) orgBGLimits {
	var l orgBGLimits
	if raw, _ := s.DB.Setting(ctx, bgOrgKey(orgID), ""); raw != "" {
		_ = json.Unmarshal([]byte(raw), &l)
	}
	return l
}

func (s *Service) bgLimits(ctx context.Context, p store.Project) BackgroundAILimits {
	var out BackgroundAILimits
	if p.OrgID != "" {
		l := s.orgBGLimits(ctx, p.OrgID)
		out.OrgGeneral = l.General
		if v, ok := l.Projects[p.ID]; ok {
			out.OrgProject = intPtr(v)
		}
	}
	if raw, _ := s.DB.Setting(ctx, bgOwnerKey(p.ID), ""); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil {
			out.Owner = intPtr(v)
		}
	}
	return out
}

// effective is the lowest limit; the organization's project limit
// replaces its general one.
func (l BackgroundAILimits) effective() int {
	lim := 0
	take := func(v *int) {
		if v != nil && *v >= 0 && (lim == 0 || *v < lim) {
			lim = *v
			if lim == 0 {
				lim = -1 // 0 set explicitly: no background AI
			}
		}
	}
	if l.OrgProject != nil {
		take(l.OrgProject)
	} else {
		take(l.OrgGeneral)
	}
	take(l.Owner)
	if lim < 0 {
		return -1
	}
	return lim
}

func (s *Service) bgStatus(ctx context.Context, p store.Project) BackgroundAIStatus {
	st := BackgroundAIStatus{BackgroundAILimits: s.bgLimits(ctx, p)}
	since := monthStart(time.Now())
	used, _ := s.DB.AIProjectUsageSince(ctx, p.ID, "bg:", since)
	st.Used = centsOf(used)
	lim := st.effective()
	switch {
	case lim < 0:
		st.Reason = "bgai.off"
	case lim > 0 && st.Used >= int64(lim):
		st.Reason = "bgai.limit"
	case len(s.projectModels(ctx, p)) == 0 || !s.AIEnabled(ctx) || !s.aiChatSettings(ctx).Enabled:
		st.Reason = "ai_chat.unavailable.not_configured"
	case p.AIWriteMode == AIOff:
		st.Reason = "ai_chat.unavailable.project_off"
	}
	if lim > 0 {
		st.Limit = lim
	}
	if st.Reason == "" {
		// The money comes from the organization (or the owner's own budget).
		since := monthStart(time.Now())
		if p.OrgID != "" {
			if o, err := s.DB.OrgFor(ctx, p.OrgID, ""); err == nil {
				if !p.OrgAI || !o.AIChat {
					st.Reason = "ai_chat.unavailable.org_off"
				} else if o.AIChatBudget != nil {
					spent, _ := s.DB.AIUsageSince(ctx, o.ID, "", since, false)
					if centsOf(spent) >= int64(*o.AIChatBudget) {
						st.Reason = "ai_chat.unavailable.org_budget"
					}
				}
			}
		} else if limit := s.aiChatSettings(ctx).UserBudget; limit > 0 {
			used, _ := s.DB.AIUsageSince(ctx, "", p.OwnerID, since, false)
			if centsOf(used) >= int64(limit) {
				st.Reason = "ai_chat.unavailable.user_budget"
			}
		}
	}
	st.Available = st.Reason == ""
	return st
}

// BackgroundAI returns a project's background AI status (members); admins
// and owners see what they may change.
func (s *Service) BackgroundAI(ctx context.Context, a Actor, ref string) (BackgroundAIStatus, error) {
	p, err := s.project(ctx, a, ref, store.RoleReader)
	if err != nil {
		return BackgroundAIStatus{}, err
	}
	st := s.bgStatus(ctx, p)
	st.CanSetOwner = p.Role == store.RoleOwner
	if p.OrgID != "" {
		if o, err := s.org(ctx, a, p.OrgID, store.OrgAdmin); err == nil && o.Role == store.OrgAdmin {
			st.CanSetOrg, st.Org = true, o.Slug
		}
	}
	if s.pricesHidden(ctx, a, p.OrgID) {
		st.PricesHidden, st.Percent = true, percentOf(st.Used, int64(st.Limit))
		st.Used = 0
	}
	return st, nil
}

// SetOwnerBackgroundAI sets the owner's limit for a project (nil clears it).
func (s *Service) SetOwnerBackgroundAI(ctx context.Context, a Actor, ref string, cents *int) (BackgroundAIStatus, error) {
	p, err := s.project(ctx, a, ref, store.RoleOwner)
	if err != nil {
		return BackgroundAIStatus{}, err
	}
	v := ""
	if cents != nil {
		if *cents < 0 || *cents > 10_000_000 {
			return BackgroundAIStatus{}, invalid("validation.budget", nil, "invalid limit")
		}
		v = strconv.Itoa(*cents)
	}
	if err := s.DB.SetSetting(ctx, bgOwnerKey(p.ID), v, a.String()); err != nil {
		return BackgroundAIStatus{}, err
	}
	s.event(ctx, a, "project.background_ai", p.ID, map[string]any{"owner": cents})
	return s.BackgroundAI(ctx, a, ref)
}

// OrgBackgroundAIInput sets an organization's limits (cents; nil clears).
type OrgBackgroundAIInput struct {
	General *int `json:"general"`
	// Projects: project slug or id → cents (nil value removes the limit).
	Projects map[string]*int `json:"projects,omitempty"`
}

// SetOrgBackgroundAI sets an organization's general and per-project limits
// (organization admins).
func (s *Service) SetOrgBackgroundAI(ctx context.Context, a Actor, orgRef string, in OrgBackgroundAIInput) (orgBGLimits, error) {
	o, err := s.org(ctx, a, orgRef, store.OrgAdmin)
	if err != nil {
		return orgBGLimits{}, err
	}
	l := s.orgBGLimits(ctx, o.ID)
	if in.General != nil && *in.General < 0 {
		in.General = nil
	}
	l.General = in.General
	if l.Projects == nil {
		l.Projects = map[string]int{}
	}
	for ref, v := range in.Projects {
		p, err := s.DB.ProjectFor(ctx, ref, a.User.ID)
		if err != nil || p.OrgID != o.ID {
			return l, invalid("validation.bgai_project", map[string]any{"project": ref}, "%s is not a project of the organization", ref)
		}
		if v == nil || *v < 0 {
			delete(l.Projects, p.ID)
		} else {
			l.Projects[p.ID] = *v
		}
	}
	raw, _ := json.Marshal(l)
	if err := s.DB.SetSetting(ctx, bgOrgKey(o.ID), string(raw), a.String()); err != nil {
		return l, err
	}
	s.event(ctx, a, "org.background_ai", o.ID, l)
	return l, nil
}

// OrgBackgroundAI reads an organization's limits (organization admins).
func (s *Service) OrgBackgroundAI(ctx context.Context, a Actor, orgRef string) (orgBGLimits, error) {
	o, err := s.org(ctx, a, orgRef, store.OrgAdmin)
	if err != nil {
		return orgBGLimits{}, err
	}
	return s.orgBGLimits(ctx, o.ID), nil
}

// ErrNoBackgroundAI means the job must use its non-AI way.
var ErrNoBackgroundAI = errors.New("background AI not available")

// BackgroundAsk calls the AI for a background job of a project and records
// the usage; ErrNoBackgroundAI when the project may not use it now.
func (s *Service) BackgroundAsk(ctx context.Context, p store.Project, job string, system []string, prompt string, maxTokens int) (string, error) {
	st := s.bgStatus(ctx, p)
	if !st.Available {
		return "", ErrNoBackgroundAI
	}
	ms := s.projectModels(ctx, p)
	want := s.defaultModel(ctx, p, ms)
	var m ChatModel
	for _, x := range ms {
		if x.Ref == want {
			m = x
		}
	}
	client, err := s.aiClient(m.source)
	if err != nil {
		return "", err
	}
	if maxTokens <= 0 {
		maxTokens = 1500
	}
	turn, err := client.Stream(ctx, ai.Request{Model: m.Model, System: system, MaxTokens: maxTokens,
		History: []ai.Message{{Raw: ai.UserMessage(prompt, nil)}}}, func(ai.Event) {})
	if err != nil {
		return "", err
	}
	owner := ""
	if p.OrgID == "" {
		owner = p.OwnerID
	}
	cost := ai.CostMicros(turn.Usage, m.cost)
	list := ai.CostMicros(turn.Usage, m.charge)
	now := store.Now()
	charge := s.chargeFor(ctx, p.OrgID, m.Own, turn.Usage.Model, turn.Usage, list, now.Time)
	_ = s.DB.AddAIUsage(ctx, store.AIUsage{ID: newID(), OrgID: p.OrgID, UserID: owner, ProjectID: p.ID, ChatID: bgChatID(job),
		ProviderID: m.ProviderID, OwnKey: m.Own, Model: turn.Usage.Model, InputTokens: turn.Usage.Input, OutputTokens: turn.Usage.Output,
		CacheRead: turn.Usage.CacheRead, CacheWrite: turn.Usage.CacheWrite, CostMicros: cost, ChargeMicros: charge, ListMicros: list, CreatedAt: now})
	s.warnBackground(ctx, p)
	var text strings.Builder
	for _, part := range ai.Parts(turn.Message) {
		if part.Type == "text" {
			text.WriteString(part.Text)
		}
	}
	return text.String(), nil
}

// warnBackground tells the project's owners at 80 % and 100 % of the limit.
func (s *Service) warnBackground(ctx context.Context, p store.Project) {
	st := s.bgStatus(ctx, p)
	if st.Limit <= 0 {
		return
	}
	month := time.Now().UTC().Format("2006-01")
	for _, level := range []int{100, 80} {
		if st.Used*100 < int64(st.Limit)*int64(level) {
			continue
		}
		first, err := s.DB.MarkBudgetWarning(ctx, bgScope(p.ID), month, level)
		if err != nil || !first {
			return
		}
		members, _ := s.DB.ListMembers(ctx, p.ID)
		for _, m := range members {
			if m.Role != store.RoleOwner {
				continue
			}
			if u, err := s.DB.UserByID(ctx, m.UserID); err == nil {
				s.notify(ctx, u, Note{Kind: "budget.warning", ProjectID: p.ID, Project: p.Name, Subject: bgScope(p.ID),
					Title: fmt.Sprintf("%s · background AI · %d %%", p.Name, level), Link: "/p/" + p.Slug + "/settings/usage"})
			}
		}
		return
	}
}
