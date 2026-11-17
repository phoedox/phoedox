package core

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/phoedox/phoedox/internal/jobs"
	"github.com/phoedox/phoedox/internal/requests"
	"github.com/phoedox/phoedox/internal/store"
)

// Automation rules (ADR-0060): when something happens in a project, do
// something. Triggers: a check fails or recovers, an entry of a kind is
// created, a date of an entry comes close (warranty, "cancel by", contract
// end). Actions: create a task, notify people, send a request – in the
// syntax of request blocks, with the project's environment variables, e.g.
// to open a ticket in Zammad, Jira or GitHub.
//
// Rules act as the person who created them (they must still have the
// rights); what a rule does is recorded with the actor "rule:<id>" and never
// triggers other rules.

// Trigger types.
const (
	TriggerCheckFail    = "check.fail"
	TriggerCheckRecover = "check.recover"
	TriggerEntityCreate = "entity.create"
	TriggerDate         = "date"
	// TriggerFeedbackTopic: a feedback portal got a new topic (again after
	// the AI triage sorted it, so category rules see the category).
	TriggerFeedbackTopic = "feedback.topic"
)

// Action types.
const (
	ActionTask   = "task"
	ActionNotify = "notify"
	ActionHTTP   = "http"
)

// JobRule runs one rule action in the background.
const JobRule = "rule.run"

// ruleRunsPerHour limits how often one rule acts.
const ruleRunsPerHour = 30

// RuleTrigger says when a rule fires.
type RuleTrigger struct {
	Type string `json:"type"`
	// Check limits check triggers to one check (id); empty = any check.
	Check string `json:"check,omitempty"`
	// Kind limits entry and date triggers to one kind; empty = any kind.
	Kind string `json:"kind,omitempty"`
	// Field is the date attribute of date triggers, e.g. cancel_by.
	Field string `json:"field,omitempty"`
	// DaysBefore: date triggers fire this many days before the date (0–365).
	DaysBefore int `json:"daysBefore,omitempty"`
	// Feedback triggers: limit to a portal (id), a category and a report type.
	Portal     string `json:"portal,omitempty"`
	Category   string `json:"category,omitempty"`
	ReportType string `json:"reportType,omitempty"`
}

// RuleAction says what a rule does. Texts may contain {{variables}}:
// rule, project, link, entry, kind, check, status, message, date, days.
type RuleAction struct {
	Type string `json:"type"`
	// Task: board (slug), optional column, title, description, labels, assignee.
	Board    string   `json:"board,omitempty"`
	Column   string   `json:"column,omitempty"`
	Title    string   `json:"title,omitempty"`
	Text     string   `json:"text,omitempty"`
	Labels   []string `json:"labels,omitempty"`
	Assignee string   `json:"assignee,omitempty"`
	// Notify: "owners" (default), "members" or an e-mail address of a member.
	To string `json:"to,omitempty"`
	// HTTP: a request in the syntax of request blocks; {{NAME}} are the
	// project's environment variables, {{entry|json}} inserts a JSON string.
	Request string `json:"request,omitempty"`
}

// RuleInput creates or changes a rule.
type RuleInput struct {
	Name    string      `json:"name"`
	Enabled *bool       `json:"enabled,omitempty"`
	Trigger RuleTrigger `json:"trigger"`
	Action  RuleAction  `json:"action"`
}

func (s *Service) validRule(ctx context.Context, p store.Project, in *RuleInput) error {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len([]rune(in.Name)) > 120 {
		return invalid("validation.rule_name", nil, "a rule needs a name (up to 120 characters)")
	}
	t := &in.Trigger
	switch t.Type {
	case TriggerCheckFail, TriggerCheckRecover:
		if t.Check != "" {
			if _, err := s.DB.GetCheck(ctx, p.ID, t.Check); err != nil {
				return invalid("validation.rule_check", nil, "unknown check")
			}
		}
	case TriggerEntityCreate:
	case TriggerFeedbackTopic:
		if t.Portal != "" {
			if _, err := s.DB.FeedbackPortal(ctx, p.ID, t.Portal); err != nil {
				return invalid("validation.rule_portal", nil, "unknown feedback portal")
			}
		}
	case TriggerDate:
		if t.Field == "" {
			return invalid("validation.rule_field", nil, "a date rule needs a date field")
		}
		if t.DaysBefore < 0 || t.DaysBefore > 365 {
			return invalid("validation.rule_days", nil, "days before must be between 0 and 365")
		}
	default:
		return invalid("validation.rule_trigger", nil, "unknown trigger")
	}
	x := &in.Action
	switch x.Type {
	case ActionTask:
		if x.Board == "" {
			return invalid("validation.rule_board", nil, "a task rule needs a board")
		}
		if _, err := s.DB.GetDocument(ctx, p.ID, x.Board); err != nil {
			return invalid("validation.rule_board", nil, "unknown board")
		}
	case ActionNotify:
		if x.To == "" {
			x.To = "owners"
		}
	case ActionHTTP:
		if _, err := requests.Parse(x.Request); err != nil || strings.TrimSpace(x.Request) == "" {
			return invalid("validation.rule_request", nil, "the request cannot be read; use the syntax of request blocks")
		}
	default:
		return invalid("validation.rule_action", nil, "unknown action")
	}
	return nil
}

// Rules lists the rules of a project (editors).
func (s *Service) Rules(ctx context.Context, a Actor, ref string) ([]store.Rule, error) {
	p, err := s.project(ctx, a, ref, store.RoleEditor)
	if err != nil {
		return nil, err
	}
	return s.DB.Rules(ctx, p.ID)
}

// SaveRule creates (id "") or changes a rule. Owners only: rules act on
// their own and can use the project's secrets. Not for AI clients.
func (s *Service) SaveRule(ctx context.Context, a Actor, ref, id string, in RuleInput) (store.Rule, error) {
	p, err := s.project(ctx, a, ref, store.RoleOwner)
	if err != nil {
		return store.Rule{}, err
	}
	if a.Via == "mcp" || a.Via == "rule" {
		return store.Rule{}, ErrForbidden
	}
	if err := s.validRule(ctx, p, &in); err != nil {
		return store.Rule{}, err
	}
	r := store.Rule{ID: id, ProjectID: p.ID, CreatedBy: a.User.ID, Enabled: true}
	if id != "" {
		if r, err = s.DB.GetRule(ctx, p.ID, id); err != nil {
			return r, err
		}
	} else {
		r.ID = newID()
	}
	if in.Enabled != nil {
		r.Enabled = *in.Enabled
	}
	r.Name = in.Name
	r.Trigger, _ = json.Marshal(in.Trigger)
	r.Action, _ = json.Marshal(in.Action)
	if err := s.DB.SaveRule(ctx, r); err != nil {
		return r, err
	}
	s.event(ctx, a, "rule.save", p.ID, map[string]any{"rule": r.ID, "name": r.Name, "trigger": in.Trigger.Type, "action": in.Action.Type})
	return s.DB.GetRule(ctx, p.ID, r.ID)
}

// DeleteRule removes a rule (owners).
func (s *Service) DeleteRule(ctx context.Context, a Actor, ref, id string) error {
	p, err := s.project(ctx, a, ref, store.RoleOwner)
	if err != nil {
		return err
	}
	if a.Via == "mcp" {
		return ErrForbidden
	}
	if err := s.DB.DeleteRule(ctx, p.ID, id); err != nil {
		return err
	}
	s.event(ctx, a, "rule.delete", p.ID, map[string]any{"rule": id})
	return nil
}

// TestRule runs a rule's action once with sample values (owners).
func (s *Service) TestRule(ctx context.Context, a Actor, ref, id string) error {
	p, err := s.project(ctx, a, ref, store.RoleOwner)
	if err != nil {
		return err
	}
	r, err := s.DB.GetRule(ctx, p.ID, id)
	if err != nil {
		return err
	}
	vars := map[string]string{"entry": "Test", "kind": "device", "check": "Test", "status": "fail", "message": "test", "date": time.Now().Format(time.DateOnly), "days": "0", "link": s.PublicURL + "/p/" + p.Slug}
	return s.runRule(ctx, r, vars)
}

// --- engine ---

type ruleJob struct {
	Rule string            `json:"rule"`
	Vars map[string]string `json:"vars"`
}

func (s *Service) registerRuleJobs(q *jobs.Queue) {
	q.Handle(JobRule, func(ctx context.Context, payload json.RawMessage) error {
		var j ruleJob
		if err := json.Unmarshal(payload, &j); err != nil {
			return jobs.Permanent(err)
		}
		r, err := s.ruleByID(ctx, j.Rule)
		if err != nil {
			return nil // deleted meanwhile
		}
		if err := s.runRule(ctx, r, j.Vars); err != nil {
			return jobs.Permanent(err) // recorded on the rule; no retries that could open a ticket twice
		}
		return nil
	})
}

func (s *Service) ruleByID(ctx context.Context, id string) (store.Rule, error) {
	rules, err := s.DB.Rules(ctx, "")
	if err != nil {
		return store.Rule{}, err
	}
	for _, r := range rules {
		if r.ID == id {
			return r, nil
		}
	}
	return store.Rule{}, ErrNotFound
}

// fire runs matching rules of a project for an event (in the background).
func (s *Service) fire(ctx context.Context, a Actor, projectID, key string, match func(RuleTrigger) bool, vars map[string]string) {
	if a.Via == "rule" || projectID == "" {
		return // what rules do never triggers rules
	}
	rules, err := s.DB.Rules(ctx, projectID)
	if err != nil || len(rules) == 0 {
		return
	}
	for _, r := range rules {
		var t RuleTrigger
		if !r.Enabled || json.Unmarshal(r.Trigger, &t) != nil || !match(t) {
			continue
		}
		if !s.claim(ctx, r, key) {
			continue
		}
		if s.Jobs == nil {
			if err := s.runRule(ctx, r, vars); err != nil {
				slog.Warn("rule failed", "rule", r.ID, "err", err)
			}
			continue
		}
		if err := s.Jobs.Add(ctx, JobRule, ruleJob{Rule: r.ID, Vars: vars}); err != nil {
			slog.Warn("queueing a rule failed", "rule", r.ID, "err", err)
		}
	}
}

// claim records a run and enforces the hourly limit.
func (s *Service) claim(ctx context.Context, r store.Rule, key string) bool {
	if n, err := s.DB.RecentRuleRuns(ctx, r.ID, store.Time{Time: time.Now().Add(-time.Hour)}); err != nil || n >= ruleRunsPerHour {
		if err == nil {
			_ = s.DB.RuleResult(ctx, r.ID, "error", fmt.Sprintf("more than %d runs in an hour; paused until the next hour", ruleRunsPerHour))
		}
		return false
	}
	ok, err := s.DB.ClaimRuleRun(ctx, r.ID, key)
	return err == nil && ok
}

// ruleCheck fires check rules after a check changed its state.
func (s *Service) ruleCheck(ctx context.Context, a Actor, c store.Check, kind string) {
	vars := map[string]string{"check": c.Name, "status": c.LastStatus, "message": c.LastMessage, "date": time.Now().UTC().Format(time.DateOnly)}
	if p, err := s.DB.ProjectFor(ctx, c.ProjectID, ""); err == nil {
		vars["project"], vars["link"] = p.Name, s.PublicURL+"/p/"+p.Slug+"/monitoring"
	}
	if c.EntityID != "" {
		if e, err := s.DB.GetEntity(ctx, c.ProjectID, c.EntityID); err == nil {
			vars["entry"], vars["kind"] = e.Name, e.Kind
		}
	}
	key := kind + ":" + c.ID + ":" + time.Now().UTC().Format(time.RFC3339Nano)
	s.fire(ctx, a, c.ProjectID, key, func(t RuleTrigger) bool {
		return t.Type == kind && (t.Check == "" || t.Check == c.ID)
	}, vars)
}

// ruleEntityCreated fires entry rules after an entry was created.
func (s *Service) ruleEntityCreated(ctx context.Context, a Actor, p store.Project, e store.Entity) {
	vars := map[string]string{"entry": e.Name, "kind": e.Kind, "project": p.Name, "link": s.PublicURL + "/p/" + p.Slug + "/e/" + e.Slug, "date": time.Now().UTC().Format(time.DateOnly)}
	s.fire(ctx, a, p.ID, "entity:"+e.ID, func(t RuleTrigger) bool {
		return t.Type == TriggerEntityCreate && (t.Kind == "" || t.Kind == e.Kind)
	}, vars)
}

// RunRuleDates fires date rules once a day for every project.
func (s *Service) RunRuleDates(ctx context.Context, now time.Time) {
	sys := store.System(ctx)
	day := now.UTC().Format(time.DateOnly)
	if last, _ := s.DB.Setting(sys, "rules.day", ""); last == day {
		return
	}
	_ = s.DB.SetSetting(sys, "rules.day", day, "system")
	tenants, err := s.DB.Tenants(sys)
	if err != nil {
		return
	}
	for _, t := range tenants {
		tctx := store.WithTenant(ctx, t.ID)
		rules, err := s.DB.Rules(tctx, "")
		if err != nil {
			continue
		}
		for _, r := range rules {
			var tr RuleTrigger
			if r.Enabled && json.Unmarshal(r.Trigger, &tr) == nil && tr.Type == TriggerDate {
				s.ruleDates(tctx, r, tr, now)
			}
		}
	}
}

func (s *Service) ruleDates(ctx context.Context, r store.Rule, t RuleTrigger, now time.Time) {
	p, err := s.DB.ProjectFor(ctx, r.ProjectID, "")
	if err != nil {
		return
	}
	ents, err := s.DB.ListEntities(ctx, p.ID, t.Kind)
	if err != nil {
		return
	}
	today, _ := time.Parse(time.DateOnly, now.UTC().Format(time.DateOnly))
	for _, e := range ents {
		attrs := map[string]any{}
		_ = json.Unmarshal(e.Attrs, &attrs)
		raw, _ := attrs[t.Field].(string)
		d, err := time.Parse(time.DateOnly, raw)
		if err != nil {
			continue
		}
		days := int(d.Sub(today).Hours() / 24)
		if days < 0 || days > t.DaysBefore {
			continue
		}
		vars := map[string]string{"entry": e.Name, "kind": e.Kind, "project": p.Name, "date": raw, "days": fmt.Sprint(days),
			"link": s.PublicURL + "/p/" + p.Slug + "/e/" + e.Slug}
		if !s.claim(ctx, r, "date:"+e.ID+":"+t.Field+":"+raw) {
			continue
		}
		if err := s.runRule(ctx, r, vars); err != nil {
			slog.Warn("rule failed", "rule", r.ID, "err", err)
		}
	}
}

// runRule does a rule's action as the person who made the rule.
func (s *Service) runRule(ctx context.Context, r store.Rule, vars map[string]string) error {
	err := s.ruleAction(ctx, r, vars)
	status, msg := "ok", ""
	if err != nil {
		status, msg = "error", err.Error()
	}
	_ = s.DB.RuleResult(ctx, r.ID, status, msg)
	return err
}

func (s *Service) ruleAction(ctx context.Context, r store.Rule, vars map[string]string) error {
	u, err := s.DB.UserByID(ctx, r.CreatedBy)
	if err != nil || u.Disabled || u.Deleted {
		return invalid("validation.rule_owner", nil, "the person who made this rule can no longer act")
	}
	p, err := s.DB.ProjectFor(ctx, r.ProjectID, u.ID)
	if err != nil {
		return err
	}
	a := Actor{User: u, Via: "rule", TokenID: r.ID}
	if vars == nil {
		vars = map[string]string{}
	}
	vars["rule"] = r.Name
	if vars["project"] == "" {
		vars["project"] = p.Name
	}
	var x RuleAction
	if err := json.Unmarshal(r.Action, &x); err != nil {
		return err
	}
	switch x.Type {
	case ActionTask:
		title := fillRule(x.Title, vars, false)
		if strings.TrimSpace(title) == "" {
			title = r.Name
		}
		task, err := s.CreateTask(ctx, a, p.Slug, x.Board, TaskInput{Title: title, Description: fillRule(x.Text, vars, false), Column: x.Column, Labels: x.Labels, Assignee: x.Assignee})
		if err == nil && vars["feedback_topic"] != "" {
			// A routed feedback topic: the card becomes its ticket.
			err = s.DB.LinkTopicTaskAt(ctx, vars["feedback_topic"], task.ID, a.String(), vars["feedback_reports"])
		}
		return err
	case ActionNotify:
		members, err := s.DB.ListMembers(ctx, p.ID)
		if err != nil {
			return err
		}
		n := Note{Kind: "rule", ProjectID: p.ID, Project: p.Name, Subject: r.ID, Title: fillRule(x.Title, vars, false), Body: fillRule(x.Text, vars, false), Link: strings.TrimPrefix(vars["link"], s.PublicURL), Actor: r.Name}
		if n.Title == "" {
			n.Title = r.Name
		}
		if n.Link == "" {
			n.Link = "/p/" + p.Slug
		}
		for _, m := range members {
			if (x.To == "owners" && m.Role != store.RoleOwner) || (x.To != "owners" && x.To != "members" && !strings.EqualFold(m.Email, x.To)) {
				continue
			}
			if mu, err := s.DB.UserByID(ctx, m.UserID); err == nil {
				s.notify(ctx, mu, n)
			}
		}
		s.event(ctx, a, "rule.notify", p.ID, map[string]any{"rule": r.ID, "to": x.To})
		return nil
	case ActionHTTP:
		req, err := requests.Parse(fillRule(x.Request, vars, true))
		if err != nil {
			return invalid("validation.rule_request", nil, "the request cannot be read; use the syntax of request blocks")
		}
		res, host, err := s.execRequest(ctx, p.ID, req)
		if err != nil {
			return err
		}
		s.event(ctx, a, "rule.request", p.ID, map[string]any{"rule": r.ID, "method": req.Method, "host": host, "status": res.Status})
		if res.Error != "" {
			return invalid("validation.rule_http", map[string]any{"error": res.Error}, "the request failed: %s", res.Error)
		}
		if res.Status >= 400 {
			return invalid("validation.rule_http_status", map[string]any{"status": res.Status}, "the request was answered with status %d", res.Status)
		}
		return nil
	}
	return invalid("validation.rule_action", nil, "unknown action")
}

// ruleVar matches {{name}} and {{name|json}} of rule variables (lower case;
// upper-case names are environment variables, filled later).
var ruleVar = regexp.MustCompile(`\{\{\s*([a-z][a-z_]*)\s*(\|\s*json\s*)?\}\}`)

var ruleVarNames = []string{"rule", "project", "link", "entry", "kind", "check", "status", "message", "date", "days", "topic", "category", "type", "portal"}

// fillRule replaces rule variables; in requests, values never add new
// {{…}} that the environment step would fill (no secret injection).
func fillRule(s string, vars map[string]string, request bool) string {
	return ruleVar.ReplaceAllStringFunc(s, func(m string) string {
		g := ruleVar.FindStringSubmatch(m)
		if !slices.Contains(ruleVarNames, g[1]) {
			return m
		}
		v := vars[g[1]]
		if request {
			v = strings.NewReplacer("{{", "{ {", "}}", "} }").Replace(v)
		}
		if g[2] != "" {
			b, _ := json.Marshal(v)
			return string(b)
		}
		return v
	})
}
