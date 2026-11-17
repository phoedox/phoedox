package core

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/phoedox/phoedox/internal/store"
)

// Service providers (ADR-0073): a customer organization's admins grant a
// provider organization a role (reader, editor or admin) in all or chosen
// projects, optionally until a date, and can revoke it at any time. The
// provider's members then see those projects like members; what they do is
// logged "via provider". Provider members get an overview across their
// customers and the logged time per customer.

var providerRoles = []string{"reader", "editor", "admin"}

// ProviderInput grants a provider access to a customer organization.
type ProviderInput struct {
	Provider string   `json:"provider"` // organization slug or id
	Role     string   `json:"role"`     // reader | editor | admin
	Projects []string `json:"projects"` // project slugs; empty = all
	Until    string   `json:"until,omitempty"`
	Note     string   `json:"note,omitempty"`
	// MayCreateProjects lets the provider's admins create projects here
	// (admin role for all projects only).
	MayCreateProjects bool `json:"mayCreateProjects,omitempty"`
}

// ProviderLinks are an organization's links as customer and as provider.
type ProviderLinks struct {
	// Providers that work in this organization (its admins see them).
	Providers []store.ProviderLink `json:"providers"`
	// Customers this organization works for.
	Customers []store.ProviderLink `json:"customers"`
}

// GrantProvider gives a provider organization access (customer admins).
func (s *Service) GrantProvider(ctx context.Context, a Actor, customerRef string, in ProviderInput) (store.ProviderLink, error) {
	if err := s.fullUser(a); err != nil {
		return store.ProviderLink{}, err
	}
	c, err := s.org(ctx, a, customerRef, store.OrgAdmin)
	if err != nil {
		return store.ProviderLink{}, err
	}
	prov, err := s.DB.OrgFor(ctx, strings.TrimSpace(in.Provider), "")
	if err != nil {
		return store.ProviderLink{}, invalid("validation.provider_unknown", map[string]any{"provider": in.Provider}, "no organization %q", in.Provider)
	}
	if prov.ID == c.ID {
		return store.ProviderLink{}, invalid("validation.provider_self", nil, "an organization cannot be its own provider")
	}
	if !slices.Contains(providerRoles, in.Role) {
		return store.ProviderLink{}, invalid("validation.provider_role", nil, "role must be reader, editor or admin")
	}
	l := store.ProviderLink{ID: newID(), CustomerOrgID: c.ID, ProviderOrgID: prov.ID, Role: in.Role, Projects: []string{},
		Note: excerpt(strings.TrimSpace(in.Note), 500), CreatedBy: a.User.ID, CreatedAt: store.Now(), MayCreateProjects: in.MayCreateProjects && in.Role == "admin"}
	for _, ref := range in.Projects {
		p, err := s.DB.ProjectFor(ctx, ref, a.User.ID)
		if err != nil || p.OrgID != c.ID {
			return store.ProviderLink{}, invalid("validation.provider_project", map[string]any{"project": ref}, "project %q is not in the organization", ref)
		}
		if !slices.Contains(l.Projects, p.ID) {
			l.Projects = append(l.Projects, p.ID)
		}
	}
	if u := strings.TrimSpace(in.Until); u != "" {
		day, err := time.Parse("2006-01-02", u)
		if err != nil || !day.After(time.Now()) {
			return store.ProviderLink{}, invalid("validation.provider_until", nil, "the end date must be a future day (YYYY-MM-DD)")
		}
		end := store.Time{Time: day.Add(24 * time.Hour)} // to the end of that day
		l.ExpiresAt = &end
	}
	if len(l.Projects) > 0 {
		l.MayCreateProjects = false
	}
	if err := s.DB.SaveProviderLink(ctx, l); err != nil {
		return l, err
	}
	s.event(ctx, a, "org.provider_grant", c.ID, map[string]any{"provider": prov.Slug, "role": l.Role, "projects": l.Projects, "until": in.Until, "mayCreateProjects": l.MayCreateProjects})
	s.notifyOrgAdmins(ctx, prov.ID, Note{Kind: "provider.granted", Subject: c.ID, Title: c.Name, Body: l.Role, Actor: a.User.Name, Link: "/orgs/" + prov.Slug + "#customers"})
	links, _ := s.DB.ProviderLinks(ctx, c.ID, prov.ID)
	if len(links) > 0 {
		return links[0], nil
	}
	return l, nil
}

// RevokeProvider ends a provider's access: the customer's admins, or the
// provider's admins giving it up.
func (s *Service) RevokeProvider(ctx context.Context, a Actor, orgRef, id string) error {
	o, err := s.org(ctx, a, orgRef, store.OrgAdmin)
	if err != nil {
		return err
	}
	l, err := s.DB.GetProviderLink(ctx, id)
	if err != nil || (l.CustomerOrgID != o.ID && l.ProviderOrgID != o.ID) {
		return ErrNotFound
	}
	if err := s.DB.RevokeProviderLink(ctx, l.ID); err != nil {
		return err
	}
	s.event(ctx, a, "org.provider_revoke", l.CustomerOrgID, map[string]any{"provider": l.ProviderSlug, "by": o.Slug})
	return nil
}

// SetProviderCreateProjects lets a provider's admins create projects in the
// customer organization, or stops it (customer admins only).
func (s *Service) SetProviderCreateProjects(ctx context.Context, a Actor, orgRef, id string, on bool) (store.ProviderLink, error) {
	o, err := s.org(ctx, a, orgRef, store.OrgAdmin)
	if err != nil {
		return store.ProviderLink{}, err
	}
	l, err := s.DB.GetProviderLink(ctx, id)
	if err != nil || l.CustomerOrgID != o.ID || l.RevokedAt != nil {
		return store.ProviderLink{}, ErrNotFound
	}
	if on && (l.Role != "admin" || len(l.Projects) > 0) {
		return store.ProviderLink{}, invalid("validation.provider_create_role", nil, "only providers with the admin role for all projects can create projects")
	}
	if err := s.DB.SetProviderCreateProjects(ctx, l.ID, on); err != nil {
		return store.ProviderLink{}, err
	}
	s.event(ctx, a, "org.provider_create_projects", o.ID, map[string]any{"provider": l.ProviderSlug, "on": on})
	l.MayCreateProjects = on
	return l, nil
}

// OrgProviderLinks lists an organization's providers (its admins) and the
// customers it works for (its members).
func (s *Service) OrgProviderLinks(ctx context.Context, a Actor, ref string) (ProviderLinks, error) {
	out := ProviderLinks{Providers: []store.ProviderLink{}, Customers: []store.ProviderLink{}}
	o, err := s.org(ctx, a, ref, store.OrgMemberRole)
	if err != nil {
		return out, err
	}
	if o.Role == store.OrgAdmin || a.User.IsAdmin {
		if out.Providers, err = s.DB.ProviderLinks(ctx, o.ID, ""); err != nil {
			return out, err
		}
	}
	out.Customers, err = s.DB.ProviderLinks(ctx, "", o.ID)
	return out, err
}

func active(l store.ProviderLink, now time.Time) bool {
	return l.RevokedAt == nil && (l.ExpiresAt == nil || l.ExpiresAt.After(now))
}

// CustomerProject is one project in the provider overview.
type CustomerProject struct {
	Slug           string `json:"slug"`
	Name           string `json:"name"`
	Role           string `json:"role"`
	OpenTasks      int    `json:"openTasks"`
	FailingChecks  int    `json:"failingChecks"`
	MaintenanceDue int    `json:"maintenanceDue"`
	MinutesMonth   int    `json:"minutesMonth"`
}

// CustomerOverview is one customer with its projects.
type CustomerOverview struct {
	Org       string            `json:"org"` // slug
	Name      string            `json:"name"`
	Role      string            `json:"role"`
	ExpiresAt *store.Time       `json:"expiresAt,omitempty"`
	Projects  []CustomerProject `json:"projects"`
}

// ProviderOverview shows the provider's members what is open at each
// customer: open cards, failing checks, maintenance due and the time
// logged this month.
func (s *Service) ProviderOverview(ctx context.Context, a Actor, ref string) ([]CustomerOverview, error) {
	o, err := s.org(ctx, a, ref, store.OrgMemberRole)
	if err != nil {
		return nil, err
	}
	links, err := s.DB.ProviderLinks(ctx, "", o.ID)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	month := now.UTC().Format("2006-01") + "-01"
	today := now.UTC().Format("2006-01-02")
	projects, _ := s.DB.ListProjects(ctx, a.User.ID, false)
	out := []CustomerOverview{}
	for _, l := range links {
		if !active(l, now) {
			continue
		}
		co := CustomerOverview{Org: l.CustomerSlug, Name: l.CustomerName, Role: l.Role, ExpiresAt: l.ExpiresAt, Projects: []CustomerProject{}}
		for _, p := range projects {
			if p.OrgID != l.CustomerOrgID || (len(l.Projects) > 0 && !slices.Contains(l.Projects, p.ID)) {
				continue
			}
			cp := CustomerProject{Slug: p.Slug, Name: p.Name, Role: p.Role}
			if tasks, err := s.DB.ListTasks(ctx, p.ID, store.TaskFilter{Open: true}); err == nil {
				cp.OpenTasks = len(tasks)
			}
			if checks, err := s.DB.ProjectChecks(ctx, p.ID); err == nil {
				for _, c := range checks {
					if c.LastStatus == "fail" {
						cp.FailingChecks++
					}
				}
			}
			if plans, err := s.DB.MaintenancePlans(ctx, p.ID, ""); err == nil {
				for _, m := range plans {
					if m.NextDue != "" && m.NextDue <= today {
						cp.MaintenanceDue++
					}
				}
			}
			if times, err := s.DB.ListTime(ctx, p.ID, store.TimeFilter{From: month}); err == nil {
				for _, t := range times {
					cp.MinutesMonth += t.Minutes
				}
			}
			co.Projects = append(co.Projects, cp)
		}
		out = append(out, co)
	}
	return out, nil
}

// ProviderTimeCSV lists the time logged in all customers' projects in a
// month (YYYY-MM) as CSV (provider admins), one row per entry with the
// customer, for invoicing.
func (s *Service) ProviderTimeCSV(ctx context.Context, a Actor, ref, month string) ([]byte, error) {
	o, err := s.org(ctx, a, ref, store.OrgAdmin)
	if err != nil {
		return nil, err
	}
	start, err := time.Parse("2006-01", month)
	if err != nil {
		return nil, invalid("validation.month", nil, "month must be YYYY-MM")
	}
	from, to := start.Format("2006-01-02"), start.AddDate(0, 1, -1).Format("2006-01-02")
	links, err := s.DB.ProviderLinks(ctx, "", o.ID)
	if err != nil {
		return nil, err
	}
	members, _ := s.DB.OrgMembers(ctx, o.ID)
	ours := map[string]bool{}
	for _, m := range members {
		ours[m.UserID] = true
	}
	projects, _ := s.DB.ListProjects(ctx, a.User.ID, false)
	var b bytes.Buffer
	w := csv.NewWriter(&b)
	_ = w.Write([]string{"customer", "project", "day", "person", "board", "card", "minutes", "hours", "billable", "note"})
	for _, l := range links {
		for _, p := range projects {
			if p.OrgID != l.CustomerOrgID || (len(l.Projects) > 0 && !slices.Contains(l.Projects, p.ID)) {
				continue
			}
			list, err := s.DB.ListTime(ctx, p.ID, store.TimeFilter{From: from, To: to})
			if err != nil {
				continue
			}
			for _, e := range list {
				if !ours[e.UserID] {
					continue // only the provider's own people
				}
				_ = w.Write([]string{csvSafe(l.CustomerName), csvSafe(p.Name), e.Day, csvSafe(e.UserName), csvSafe(e.BoardTitle), csvSafe(e.TaskTitle),
					strconv.Itoa(e.Minutes), strconv.FormatFloat(float64(e.Minutes)/60, 'f', 2, 64), strconv.FormatBool(e.Billable), csvSafe(e.Note)})
			}
		}
	}
	w.Flush()
	s.event(ctx, a, "org.provider_time_csv", o.ID, map[string]string{"month": month})
	return b.Bytes(), w.Error()
}

// notifyOrgAdmins sends a notice to every admin of an organization.
func (s *Service) notifyOrgAdmins(ctx context.Context, orgID string, n Note) {
	ms, _ := s.DB.OrgMembers(ctx, orgID)
	for _, m := range ms {
		if m.Role != store.OrgAdmin {
			continue
		}
		if u, err := s.DB.UserByID(ctx, m.UserID); err == nil {
			s.notify(ctx, u, n)
		}
	}
}

// --- project templates of an organization ----------------------------------

func orgTemplatesKey(orgID string) string { return "org.templates:" + orgID }

// OrgTemplates lists the projects an organization offers as templates.
func (s *Service) OrgTemplates(ctx context.Context, a Actor, ref string) ([]string, error) {
	o, err := s.org(ctx, a, ref, store.OrgMemberRole)
	if err != nil {
		return nil, err
	}
	return s.orgTemplateIDs(ctx, o.ID), nil
}

func (s *Service) orgTemplateIDs(ctx context.Context, orgID string) []string {
	var ids []string
	if raw, _ := s.DB.Setting(ctx, orgTemplatesKey(orgID), ""); raw != "" {
		_ = json.Unmarshal([]byte(raw), &ids)
	}
	if ids == nil {
		ids = []string{}
	}
	return ids
}

// SetOrgTemplates chooses which of the organization's projects are
// templates (its admins), e.g. a provider's standard documentation that it
// starts every customer with.
func (s *Service) SetOrgTemplates(ctx context.Context, a Actor, ref string, projects []string) ([]string, error) {
	o, err := s.org(ctx, a, ref, store.OrgAdmin)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for _, pr := range projects {
		p, err := s.DB.ProjectFor(ctx, pr, a.User.ID)
		if err != nil || p.OrgID != o.ID {
			return nil, invalid("validation.provider_project", map[string]any{"project": pr}, "project %q is not in the organization", pr)
		}
		if !slices.Contains(ids, p.ID) {
			ids = append(ids, p.ID)
		}
	}
	raw, _ := json.Marshal(ids)
	if err := s.DB.SetSetting(ctx, orgTemplatesKey(o.ID), string(raw), a.User.ID); err != nil {
		return nil, err
	}
	s.event(ctx, a, "org.templates", o.ID, ids)
	return ids, nil
}

// ProjectTemplates are the built-in templates plus the template projects
// of the person's organizations ("project:<id>").
func (s *Service) ProjectTemplates(ctx context.Context, a Actor, locale string) []Template {
	out := Templates(locale)
	if a.User.ID == "" {
		return out
	}
	orgs, _ := s.DB.ListOrgs(ctx, a.User.ID, false)
	for _, o := range orgs {
		for _, id := range s.orgTemplateIDs(ctx, o.ID) {
			p, err := s.DB.ProjectFor(ctx, id, a.User.ID)
			if err != nil {
				continue
			}
			out = append(out, Template{ID: "project:" + p.ID, Name: p.Name, Description: o.Name, Icon: "Building2", Features: p.Features})
		}
	}
	return out
}

// copyTemplateProject fills a new project from a template project: pages,
// boards (columns, no cards), apps and the data model.
func (s *Service) copyTemplateProject(ctx context.Context, a Actor, dst store.Project, src store.Project) {
	docs, err := s.DB.ListDocuments(ctx, src.ID, store.DocFilter{WithContent: true})
	if err != nil {
		return
	}
	for _, d := range docs {
		if d.Restricted {
			continue
		}
		in := DocumentInput{Type: d.Type, Title: d.Title, Category: d.Category, Slug: d.Slug}
		switch d.Type {
		case store.DocMarkdown:
			in.Markdown = markdownOf(d)
		case store.DocBoard:
			var b Board
			_ = json.Unmarshal(d.Content, &b)
			in.Board = &b
		case store.DocApp:
			app := appOf(d)
			app.Build = nil
			in.App = &app
		default:
			continue // decisions, floor plans, whiteboards and links belong to one customer
		}
		_, _ = s.createDocument(ctx, a, dst, in)
	}
	if kinds, err := s.DB.ProjectKinds(ctx, src.ID); err == nil {
		for _, k := range kinds {
			k.ProjectID, k.UpdatedBy, k.UpdatedAt = dst.ID, a.String(), store.Now()
			_ = s.DB.SetProjectKind(ctx, k)
		}
	}
	s.event(ctx, a, "project.template", dst.ID, map[string]string{"template": "project:" + src.ID})
}

// templateSource checks a "project:<id>" template: a template project of
// an organization the person belongs to, readable by them.
func (s *Service) templateSource(ctx context.Context, a Actor, ref string) (store.Project, error) {
	id := strings.TrimPrefix(ref, "project:")
	p, err := s.project(ctx, a, id, store.RoleReader)
	if err != nil || p.OrgID == "" || !slices.Contains(s.orgTemplateIDs(ctx, p.OrgID), p.ID) {
		return store.Project{}, invalid("validation.template", map[string]any{"template": ref}, "unknown template %q", ref)
	}
	return p, nil
}
