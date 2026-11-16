package core

import (
	"context"
	"time"

	"github.com/phoedox/phoedox/internal/store"
)

// Usage pages: what a project, an organization, the instance and a person
// used this month – AI (charged at in-app prices, ADR-0069) and storage.
// Each report links up to the next level the viewer may open (project →
// organization → instance).

// UsageLine is one row of a breakdown.
type UsageLine struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Slug         string `json:"slug,omitempty"`
	Calls        int64  `json:"calls"`
	InputTokens  int64  `json:"inputTokens"`
	OutputTokens int64  `json:"outputTokens"`
	ChargedCents int64  `json:"chargedCents"`
	// CostCents is what the provider charged (system admins only).
	CostCents int64 `json:"costCents,omitempty"`
	// OwnKey: (part of) the charge ran on an organization's own key.
	OwnKeyCents int64 `json:"ownKeyCents,omitempty"`
}

// UsageBudget is a monthly limit with its use (cents; Limit 0 = none).
type UsageBudget struct {
	Key       string `json:"key"` // i18n key of the budget's name
	Name      string `json:"name,omitempty"`
	UsedCents int64  `json:"usedCents"`
	Limit     int64  `json:"limitCents"`
}

// UsageStorage is the storage of a project (or an organization's projects).
type UsageStorage struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Slug       string `json:"slug,omitempty"`
	UsedBytes  int64  `json:"usedBytes"`
	LimitBytes int64  `json:"limitBytes"`
}

// UsageLink points to the usage page of a higher level.
type UsageLink struct {
	Level string `json:"level"` // org | admin
	Href  string `json:"href"`
	Name  string `json:"name,omitempty"`
}

// UsageReport is the usage of one level this month.
type UsageReport struct {
	Level        string         `json:"level"` // project | org | admin | me
	Name         string         `json:"name"`
	Since        store.Time     `json:"since"`
	ChargedCents int64          `json:"chargedCents"`
	CostCents    int64          `json:"costCents,omitempty"`
	Budgets      []UsageBudget  `json:"budgets"`
	ByProject    []UsageLine    `json:"byProject,omitempty"`
	ByUser       []UsageLine    `json:"byUser,omitempty"`
	ByModel      []UsageLine    `json:"byModel,omitempty"`
	ByOrg        []UsageLine    `json:"byOrg,omitempty"`
	Storage      []UsageStorage `json:"storage,omitempty"`
	StorageBytes int64          `json:"storageBytes"`
	Up           []UsageLink    `json:"up"`
}

// usageNames resolves ids of a breakdown to names (and slugs).
type usageNames struct {
	s        *Service
	ctx      context.Context
	users    map[string]store.User
	projects map[string]store.Project
	orgs     map[string]store.Org
}

func (n *usageNames) user(id string) (string, string) {
	if u, ok := n.users[id]; ok {
		return u.Name, ""
	}
	u, err := n.s.DB.UserByID(n.ctx, id)
	if err != nil {
		return "", ""
	}
	n.users[id] = u
	return u.Name, ""
}

func (n *usageNames) project(id string) (string, string) {
	if p, ok := n.projects[id]; ok {
		return p.Name, p.Slug
	}
	p, err := n.s.DB.ProjectFor(n.ctx, id, "")
	if err != nil {
		return "", ""
	}
	n.projects[id] = p
	return p.Name, p.Slug
}

func (n *usageNames) org(id string) (string, string) {
	if id == "" {
		return "", ""
	}
	if o, ok := n.orgs[id]; ok {
		return o.Name, o.Slug
	}
	o, err := n.s.DB.OrgFor(n.ctx, id, "")
	if err != nil {
		return "", ""
	}
	n.orgs[id] = o
	return o.Name, o.Slug
}

func (s *Service) usageNames(ctx context.Context) *usageNames {
	return &usageNames{s: s, ctx: ctx, users: map[string]store.User{}, projects: map[string]store.Project{}, orgs: map[string]store.Org{}}
}

// lines turns grouped usage into named rows.
func (s *Service) lines(ctx context.Context, f store.UsageFilter, by string, names func(string) (string, string), withCost bool) ([]UsageLine, int64, int64) {
	groups, err := s.DB.AIUsageBy(ctx, f, by)
	if err != nil {
		return []UsageLine{}, 0, 0
	}
	out := make([]UsageLine, 0, len(groups))
	var charged, cost int64
	for _, g := range groups {
		l := UsageLine{ID: g.Key, Name: g.Key, Calls: g.Calls, InputTokens: g.InputTokens, OutputTokens: g.OutputTokens,
			ChargedCents: centsOf(g.ChargeMicros), OwnKeyCents: centsOf(g.OwnKey)}
		if withCost {
			l.CostCents = centsOf(g.CostMicros)
		}
		if names != nil {
			if n, slug := names(g.Key); n != "" {
				l.Name, l.Slug = n, slug
			}
		}
		out = append(out, l)
		charged += g.ChargeMicros
		cost += g.CostMicros
	}
	return out, centsOf(charged), centsOf(cost)
}

// storageOf lists the storage of projects.
func (s *Service) storageOf(ctx context.Context, ps []store.Project) ([]UsageStorage, int64) {
	out := []UsageStorage{}
	var total int64
	for _, p := range ps {
		used, _ := s.DB.FileUsage(ctx, p.ID)
		out = append(out, UsageStorage{ID: p.ID, Name: p.Name, Slug: p.Slug, UsedBytes: used, LimitBytes: s.quotaBytes(ctx, p)})
		total += used
	}
	return out, total
}

// upLinks are the usage pages above an organization the viewer can open.
func (s *Service) upLinks(ctx context.Context, a Actor, orgID string) []UsageLink {
	out := []UsageLink{}
	if orgID != "" {
		if o, err := s.DB.OrgFor(ctx, orgID, a.User.ID); err == nil && (o.Role == store.OrgAdmin || a.User.IsAdmin) {
			href := "/orgs/" + o.Slug + "/usage"
			if a.User.IsAdmin && o.Role != store.OrgAdmin {
				href = "/admin/orgs/" + o.Slug + "/usage"
			}
			out = append(out, UsageLink{Level: "org", Href: href, Name: o.Name})
		}
	}
	if a.User.IsAdmin {
		out = append(out, UsageLink{Level: "admin", Href: "/admin/usage"})
	}
	return out
}

// ProjectUsage is the usage of a project this month (owners).
func (s *Service) ProjectUsage(ctx context.Context, a Actor, ref string) (UsageReport, error) {
	if err := s.fullUser(a); err != nil {
		return UsageReport{}, err
	}
	p, err := s.project(ctx, a, ref, store.RoleOwner)
	if err != nil {
		return UsageReport{}, err
	}
	since := monthStart(time.Now())
	f := store.UsageFilter{OrgID: p.OrgID, ProjectID: p.ID, Since: since}
	names := s.usageNames(ctx)
	r := UsageReport{Level: "project", Name: p.Name, Since: store.Time{Time: since}, Budgets: []UsageBudget{}, Up: s.upLinks(ctx, a, p.OrgID)}
	r.ByUser, r.ChargedCents, _ = s.lines(ctx, f, "user", names.user, false)
	r.ByModel, _, _ = s.lines(ctx, f, "model", nil, false)
	r.Storage, r.StorageBytes = s.storageOf(ctx, []store.Project{p})
	return r, nil
}

// OrgUsage is the usage of an organization this month (its admins and
// system admins).
func (s *Service) OrgUsage(ctx context.Context, a Actor, ref string) (UsageReport, error) {
	if err := s.fullUser(a); err != nil {
		return UsageReport{}, err
	}
	o, err := s.org(ctx, a, ref, store.OrgAdmin)
	if err != nil {
		return UsageReport{}, err
	}
	since := monthStart(time.Now())
	f := store.UsageFilter{OrgID: o.ID, Since: since}
	names := s.usageNames(ctx)
	st := s.aiChatSettings(ctx)
	r := UsageReport{Level: "org", Name: o.Name, Since: store.Time{Time: since}, Up: []UsageLink{}}
	if a.User.IsAdmin {
		r.Up = append(r.Up, UsageLink{Level: "admin", Href: "/admin/usage"})
	}
	r.ByProject, r.ChargedCents, _ = s.lines(ctx, f, "project", names.project, false)
	r.ByUser, _, _ = s.lines(ctx, f, "user", names.user, false)
	r.ByModel, _, _ = s.lines(ctx, f, "model", nil, false)
	inst, _ := s.DB.AIUsageSince(ctx, o.ID, "", since, true)
	r.Budgets = []UsageBudget{{Key: "usage.budget.org_instance", UsedCents: centsOf(inst), Limit: int64(orgChatLimit(o, st))}}
	if o.AIChatBudget != nil {
		r.Budgets = append(r.Budgets, UsageBudget{Key: "usage.budget.org_own", UsedCents: r.ChargedCents, Limit: int64(*o.AIChatBudget)})
	}
	all, err := s.DB.ListProjects(ctx, a.User.ID, true)
	if err == nil {
		mine := []store.Project{}
		for _, p := range all {
			if p.OrgID == o.ID {
				mine = append(mine, p)
			}
		}
		r.Storage, r.StorageBytes = s.storageOf(ctx, mine)
	}
	return r, nil
}

// InstanceUsage is the usage of the whole instance this month (system admins).
func (s *Service) InstanceUsage(ctx context.Context, a Actor) (UsageReport, error) {
	if !a.User.IsAdmin || a.Via == "mcp" {
		return UsageReport{}, ErrForbidden
	}
	since := monthStart(time.Now())
	f := store.UsageFilter{AnyOrg: true, Since: since}
	names := s.usageNames(ctx)
	st := s.aiChatSettings(ctx)
	r := UsageReport{Level: "admin", Since: store.Time{Time: since}, Up: []UsageLink{}}
	r.ByOrg, r.ChargedCents, r.CostCents = s.lines(ctx, f, "org", names.org, true)
	r.ByModel, _, _ = s.lines(ctx, f, "model", nil, true)
	users, _, _ := s.lines(ctx, f, "user", names.user, true)
	if len(users) > 20 {
		users = users[:20]
	}
	r.ByUser = users
	instCost, _ := s.DB.AIUsageTotalSince(ctx, since)
	r.Budgets = []UsageBudget{{Key: "usage.budget.instance_cap", UsedCents: centsOf(instCost), Limit: int64(st.InstanceCap)}}
	// Storage per organization ("" = personal projects).
	all, err := s.DB.ListProjects(ctx, a.User.ID, true)
	if err == nil {
		byOrg := map[string]*UsageStorage{}
		order := []string{}
		for _, p := range all {
			used, _ := s.DB.FileUsage(ctx, p.ID)
			r.StorageBytes += used
			st, ok := byOrg[p.OrgID]
			if !ok {
				name, slug := names.org(p.OrgID)
				st = &UsageStorage{ID: p.OrgID, Name: name, Slug: slug}
				byOrg[p.OrgID] = st
				order = append(order, p.OrgID)
			}
			st.UsedBytes += used
		}
		for _, id := range order {
			r.Storage = append(r.Storage, *byOrg[id])
		}
	}
	return r, nil
}

// MyUsage is the person's own AI usage this month, with their budgets.
func (s *Service) MyUsage(ctx context.Context, a Actor) (UsageReport, error) {
	if err := s.fullUser(a); err != nil {
		return UsageReport{}, err
	}
	since := monthStart(time.Now())
	f := store.UsageFilter{AnyOrg: true, UserID: a.User.ID, Since: since}
	names := s.usageNames(ctx)
	st := s.aiChatSettings(ctx)
	r := UsageReport{Level: "me", Name: a.User.Name, Since: store.Time{Time: since}, Up: []UsageLink{}}
	r.ByProject, r.ChargedCents, _ = s.lines(ctx, f, "project", names.project, false)
	r.ByModel, _, _ = s.lines(ctx, f, "model", nil, false)
	// One budget per place: personal projects, and each organization.
	own, _ := s.DB.AIUsageSince(ctx, "", a.User.ID, since, false)
	r.Budgets = []UsageBudget{{Key: "usage.budget.personal", UsedCents: centsOf(own), Limit: int64(st.UserBudget)}}
	orgs, err := s.DB.ListOrgs(ctx, a.User.ID, false)
	if err == nil {
		for _, o := range orgs {
			limit := int64(st.UserBudget)
			if o.AIChatUserBudget != nil {
				limit = int64(*o.AIChatUserBudget)
			}
			used, _ := s.DB.AIUsageSince(ctx, o.ID, a.User.ID, since, false)
			r.Budgets = append(r.Budgets, UsageBudget{Key: "usage.budget.in_org", Name: o.Name, UsedCents: centsOf(used), Limit: limit})
		}
	}
	return r, nil
}
