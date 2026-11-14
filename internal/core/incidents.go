package core

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/phoedox/phoedox/internal/store"
)

// Incidents: a failing check opens one (and resolves it when it recovers);
// people open them by hand too and write updates for the timeline. Checks
// and incidents marked public appear on the project's public status page
// (/status/<token>) and on its publications (/status).

var incidentStatuses = []string{store.IncidentInvestigating, store.IncidentIdentified, store.IncidentMonitoring, store.IncidentResolved}

// statusWindow is how far back status pages and uptime look.
const statusWindow = 30 * 24 * time.Hour

// IncidentInput opens an incident or changes it.
type IncidentInput struct {
	Title  string `json:"title"`
	Public bool   `json:"public"`
	// Status and Body form the first update when opening.
	Status string `json:"status"`
	Body   string `json:"body"`
}

// IncidentUpdateInput adds to the timeline.
type IncidentUpdateInput struct {
	Status string `json:"status"`
	Body   string `json:"body"`
}

func validStatus(s string) bool { return slices.Contains(incidentStatuses, s) }

// Incidents lists a project's incidents of the last 30 days and all open ones.
func (s *Service) Incidents(ctx context.Context, a Actor, ref string) ([]store.Incident, error) {
	p, err := s.project(ctx, a, ref, store.RoleReader)
	if err != nil {
		return nil, err
	}
	return s.DB.Incidents(ctx, []string{p.ID}, store.Time{Time: time.Now().Add(-statusWindow)}, false, 200)
}

func (s *Service) incidentEditor(ctx context.Context, a Actor, ref string) (store.Project, error) {
	p, err := s.project(ctx, a, ref, store.RoleEditor)
	if err == nil && a.Restricted() {
		err = ErrForbidden
	}
	return p, err
}

// CreateIncident opens an incident by hand (editors).
func (s *Service) CreateIncident(ctx context.Context, a Actor, ref string, in IncidentInput) (store.Incident, error) {
	p, err := s.incidentEditor(ctx, a, ref)
	if err != nil {
		return store.Incident{}, err
	}
	in.Title, in.Body = strings.TrimSpace(in.Title), strings.TrimSpace(in.Body)
	if in.Status == "" {
		in.Status = store.IncidentInvestigating
	}
	if in.Title == "" || len(in.Title) > 200 {
		return store.Incident{}, invalid("validation.title_required", nil, "title is required")
	}
	if !validStatus(in.Status) || len(in.Body) > 10000 {
		return store.Incident{}, invalid("validation.incident_status", nil, "unknown incident status")
	}
	now := store.Now()
	inc := store.Incident{ID: newID(), ProjectID: p.ID, Title: in.Title, Status: in.Status, Public: in.Public, StartedAt: now, CreatedBy: a.User.ID}
	if in.Status == store.IncidentResolved {
		inc.ResolvedAt = now
	}
	first := store.IncidentUpdate{ID: newID(), IncidentID: inc.ID, Status: in.Status, Body: in.Body, CreatedBy: a.User.ID, CreatedAt: now}
	if err := s.DB.CreateIncident(ctx, inc, first); err != nil {
		return inc, err
	}
	s.event(ctx, a, "incident.create", inc.ID, map[string]any{"title": inc.Title, "status": inc.Status, "public": inc.Public})
	return s.DB.GetIncident(ctx, p.ID, inc.ID)
}

// UpdateIncident changes title and visibility (editors).
func (s *Service) UpdateIncident(ctx context.Context, a Actor, ref, id string, in IncidentInput) (store.Incident, error) {
	p, err := s.incidentEditor(ctx, a, ref)
	if err != nil {
		return store.Incident{}, err
	}
	cur, err := s.DB.GetIncident(ctx, p.ID, id)
	if err != nil {
		return cur, err
	}
	cur.Title = strings.TrimSpace(in.Title)
	if cur.Title == "" || len(cur.Title) > 200 {
		return cur, invalid("validation.title_required", nil, "title is required")
	}
	cur.Public = in.Public
	if err := s.DB.UpdateIncident(ctx, cur); err != nil {
		return cur, err
	}
	s.event(ctx, a, "incident.change", id, map[string]any{"title": cur.Title, "public": cur.Public})
	return s.DB.GetIncident(ctx, p.ID, id)
}

// AddIncidentUpdate writes to the timeline and sets the status (editors).
func (s *Service) AddIncidentUpdate(ctx context.Context, a Actor, ref, id string, in IncidentUpdateInput) (store.Incident, error) {
	p, err := s.incidentEditor(ctx, a, ref)
	if err != nil {
		return store.Incident{}, err
	}
	in.Body = strings.TrimSpace(in.Body)
	if !validStatus(in.Status) || len(in.Body) > 10000 {
		return store.Incident{}, invalid("validation.incident_status", nil, "unknown incident status")
	}
	if _, err := s.DB.GetIncident(ctx, p.ID, id); err != nil {
		return store.Incident{}, err
	}
	u := store.IncidentUpdate{ID: newID(), IncidentID: id, Status: in.Status, Body: in.Body, CreatedBy: a.User.ID, CreatedAt: store.Now()}
	if err := s.DB.AddIncidentUpdate(ctx, u); err != nil {
		return store.Incident{}, err
	}
	s.event(ctx, a, "incident.update", id, map[string]any{"status": in.Status})
	return s.DB.GetIncident(ctx, p.ID, id)
}

// DeleteIncident removes an incident (editors).
func (s *Service) DeleteIncident(ctx context.Context, a Actor, ref, id string) error {
	p, err := s.incidentEditor(ctx, a, ref)
	if err != nil {
		return err
	}
	if err := s.DB.DeleteIncident(ctx, p.ID, id); err != nil {
		return err
	}
	s.event(ctx, a, "incident.delete", id, nil)
	return nil
}

// checkIncident opens an incident when a check fails and resolves it when
// the check recovers. Public checks open public incidents.
func (s *Service) checkIncident(ctx context.Context, a Actor, c store.Check) {
	open, err := s.DB.OpenIncidentForCheck(ctx, c.ID)
	hasOpen := err == nil
	now := store.Now()
	switch {
	case c.LastStatus == store.CheckFail && !hasOpen:
		inc := store.Incident{ID: newID(), ProjectID: c.ProjectID, CheckID: c.ID, Title: c.Name, Status: store.IncidentInvestigating,
			Public: c.Public, StartedAt: now, CreatedBy: a.User.ID}
		first := store.IncidentUpdate{ID: newID(), IncidentID: inc.ID, Status: store.IncidentInvestigating, Body: c.LastDetail, Auto: true, CreatedBy: a.User.ID, CreatedAt: now}
		if err := s.DB.CreateIncident(ctx, inc, first); err == nil {
			s.event(ctx, a, "incident.create", inc.ID, map[string]any{"title": inc.Title, "check": c.ID, "auto": true})
		}
	case c.LastStatus == store.CheckOK && hasOpen:
		u := store.IncidentUpdate{ID: newID(), IncidentID: open.ID, Status: store.IncidentResolved, Body: c.LastDetail, Auto: true, CreatedBy: a.User.ID, CreatedAt: now}
		if err := s.DB.AddIncidentUpdate(ctx, u); err == nil {
			s.event(ctx, a, "incident.update", open.ID, map[string]any{"status": store.IncidentResolved, "check": c.ID, "auto": true})
		}
	}
}

// ---- public status pages ---------------------------------------------------

// StatusCheck is a check as the public sees it.
type StatusCheck struct {
	Name   string     `json:"name"`
	Status string     `json:"status"` // ok | fail | unknown
	Since  store.Time `json:"since"`
	// Uptime in percent over the last 30 days (from its incidents).
	Uptime float64 `json:"uptime"`
}

// StatusPage is what a public status page shows.
type StatusPage struct {
	Title     string           `json:"title"`
	Overall   string           `json:"overall"` // operational | degraded | outage
	Checks    []StatusCheck    `json:"checks"`
	Incidents []store.Incident `json:"incidents"`
	Updated   store.Time       `json:"updated"`
}

// PublicStatus collects the public checks and incidents of projects (ctx
// carries their tenant). Nothing personal is included.
func (s *Service) PublicStatus(ctx context.Context, title string, projectIDs []string, now time.Time) (StatusPage, error) {
	page := StatusPage{Title: title, Overall: "operational", Checks: []StatusCheck{}, Incidents: []store.Incident{}, Updated: store.Time{Time: now}}
	since := now.Add(-statusWindow)
	all, err := s.DB.Incidents(ctx, projectIDs, store.Time{Time: since}, false, 1000)
	if err != nil {
		return page, err
	}
	failing, total := 0, 0
	for _, pid := range projectIDs {
		checks, err := s.DB.ProjectChecks(ctx, pid)
		if err != nil {
			return page, err
		}
		for _, c := range checks {
			if !c.Public || !c.Enabled {
				continue
			}
			total++
			if c.LastStatus == store.CheckFail {
				failing++
			}
			page.Checks = append(page.Checks, StatusCheck{Name: c.Name, Status: c.LastStatus, Since: c.LastChange, Uptime: uptime(all, c.ID, since, now)})
		}
	}
	for _, in := range all {
		if !in.Public {
			continue
		}
		in.CreatedBy = ""
		for i := range in.Updates {
			in.Updates[i].CreatedBy = ""
		}
		page.Incidents = append(page.Incidents, in)
		if in.ResolvedAt.IsZero() && page.Overall == "operational" {
			page.Overall = "degraded"
		}
	}
	switch {
	case total > 0 && failing == total:
		page.Overall = "outage"
	case failing > 0:
		page.Overall = "degraded"
	}
	return page, nil
}

// uptime is the share of the window a check had no open incident, in percent
// with two decimals.
func uptime(incidents []store.Incident, checkID string, since, now time.Time) float64 {
	var down time.Duration
	for _, in := range incidents {
		if in.CheckID != checkID {
			continue
		}
		start, end := in.StartedAt.Time, now
		if !in.ResolvedAt.IsZero() {
			end = in.ResolvedAt.Time
		}
		if start.Before(since) {
			start = since
		}
		if end.After(start) {
			down += end.Sub(start)
		}
	}
	window := now.Sub(since)
	pct := 100 * (1 - float64(down)/float64(window))
	if pct < 0 {
		pct = 0
	}
	return float64(int(pct*100)) / 100
}

// StatusPageSettings is whether a project has a public status page.
type StatusPageSettings struct {
	Enabled bool   `json:"enabled"`
	Path    string `json:"path,omitempty"` // e.g. /status/<token>
}

// ProjectStatusPage returns the status page setting (editors see the address).
func (s *Service) ProjectStatusPage(ctx context.Context, a Actor, ref string) (StatusPageSettings, error) {
	p, err := s.project(ctx, a, ref, store.RoleEditor)
	if err != nil {
		return StatusPageSettings{}, err
	}
	token, err := s.DB.ProjectStatusToken(ctx, p.ID)
	if err != nil || token == "" {
		return StatusPageSettings{}, err
	}
	return StatusPageSettings{Enabled: true, Path: "/status/" + token}, nil
}

// SetProjectStatusPage turns the public status page on (new address) or off (owners).
func (s *Service) SetProjectStatusPage(ctx context.Context, a Actor, ref string, enabled bool) (StatusPageSettings, error) {
	p, err := s.project(ctx, a, ref, store.RoleOwner)
	if err == nil && (a.Via == "mcp" || a.Restricted()) {
		err = ErrForbidden
	}
	if err != nil {
		return StatusPageSettings{}, err
	}
	token := ""
	if enabled {
		if cur, _ := s.DB.ProjectStatusToken(ctx, p.ID); cur != "" {
			token = cur
		} else {
			token = strings.TrimPrefix(randomToken("s"), "s")
		}
	}
	if err := s.DB.SetProjectStatusToken(ctx, p.ID, token); err != nil {
		return StatusPageSettings{}, err
	}
	s.event(ctx, a, "project.status_page", p.ID, map[string]any{"enabled": enabled})
	return s.ProjectStatusPage(ctx, a, ref)
}

// StatusByToken loads the public status page of a project by its address.
func (s *Service) StatusByToken(ctx context.Context, token string, now time.Time) (StatusPage, string, error) {
	if token == "" || len(token) > 100 {
		return StatusPage{}, "", ErrNotFound
	}
	pid, tenant, err := s.DB.ProjectByStatusToken(store.System(ctx), token)
	if errors.Is(err, store.ErrNotFound) {
		return StatusPage{}, "", ErrNotFound
	}
	if err != nil {
		return StatusPage{}, "", err
	}
	ctx = store.WithTenant(ctx, tenant)
	p, err := s.DB.ProjectFor(ctx, pid, "")
	if err != nil {
		return StatusPage{}, "", err
	}
	page, err := s.PublicStatus(ctx, p.Name, []string{p.ID}, now)
	return page, p.MainLocale, err
}
