package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/phoedox/phoedox/internal/requests"
	"github.com/phoedox/phoedox/internal/store"
	"github.com/phoedox/phoedox/resources"
)

// Monitoring (concept "Überwachung"): a reading request block of a page
// runs on a schedule as the person who set it up. A check turns "fail"
// after CheckFailAfter failed runs in a row and "ok" after one good run;
// each change writes an event (and so webhooks), mails the creator and
// sets the state of a linked entry, which diagnosis and topology show.
const (
	CheckMinInterval = 60
	CheckMaxInterval = 24 * 60 * 60
	CheckFailAfter   = 2
	checkWorkers     = 8
)

// CheckInput creates or changes a check.
type CheckInput struct {
	Document    string `json:"document"`
	Block       int    `json:"block"`
	Entity      string `json:"entity,omitempty"` // slug or id, "" = none
	Name        string `json:"name"`
	IntervalSec int    `json:"intervalSec"`
	Enabled     *bool  `json:"enabled,omitempty"`
}

// CheckView is a check with the page and entry it belongs to.
type CheckView struct {
	store.Check
	DocumentSlug  string `json:"documentSlug"`
	DocumentTitle string `json:"documentTitle"`
	EntitySlug    string `json:"entitySlug,omitempty"`
	EntityName    string `json:"entityName,omitempty"`
}

func (in *CheckInput) normalize() error {
	in.Name = strings.TrimSpace(in.Name)
	if len(in.Name) > 120 {
		return invalid("validation.too_long", map[string]any{"field": "name", "max": 120}, "the name is too long")
	}
	if in.IntervalSec == 0 {
		in.IntervalSec = 300
	}
	if in.IntervalSec < CheckMinInterval || in.IntervalSec > CheckMaxInterval {
		return invalid("validation.check_interval", map[string]any{"min": CheckMinInterval / 60, "max": CheckMaxInterval / 60},
			"the interval must be between %d and %d seconds", CheckMinInterval, CheckMaxInterval)
	}
	return nil
}

// aiChecks lets AI clients set up checks only where they write directly:
// checks have no suggestions.
func aiChecks(a Actor, p store.Project) error {
	if a.Via == "mcp" && p.AIWriteMode != AIDirect {
		return invalid("validation.ai_checks_direct", nil, "AI clients change checks only in projects where AI writes directly")
	}
	return nil
}

func (s *Service) checkEntity(ctx context.Context, projectID, ref string) (string, error) {
	if ref == "" {
		return "", nil
	}
	e, err := s.DB.GetEntity(ctx, projectID, ref)
	if err != nil {
		return "", invalid("validation.entity_not_found", map[string]any{"entity": ref}, "there is no entry %s", ref)
	}
	return e.ID, nil
}

// CreateCheck sets up monitoring for a request block. Only reading methods
// may run unattended, so a check never changes anything by itself.
func (s *Service) CreateCheck(ctx context.Context, a Actor, ref string, in CheckInput) (CheckView, error) {
	if err := in.normalize(); err != nil {
		return CheckView{}, err
	}
	p, doc, err := s.docAccess(ctx, a, ref, in.Document, store.RoleEditor)
	if err != nil {
		return CheckView{}, err
	}
	if err := aiChecks(a, p); err != nil {
		return CheckView{}, err
	}
	req, err := blockRequest(doc, in.Block)
	if err != nil {
		return CheckView{}, err
	}
	if !requests.Safe(req.Method) {
		return CheckView{}, invalid("validation.check_method", map[string]any{"method": req.Method}, "only reading requests can be monitored, not %s", req.Method)
	}
	entityID, err := s.checkEntity(ctx, p.ID, in.Entity)
	if err != nil {
		return CheckView{}, err
	}
	if in.Name == "" {
		in.Name = req.Method + " " + req.URL
		if len(in.Name) > 120 {
			in.Name = in.Name[:120]
		}
	}
	c := store.Check{ID: newID(), ProjectID: p.ID, DocumentID: doc.ID, BlockIndex: in.Block, EntityID: entityID, Name: in.Name,
		IntervalSec: in.IntervalSec, Enabled: in.Enabled == nil || *in.Enabled, CreatedBy: a.User.ID, CreatedAt: store.Now()}
	if c, err = s.DB.CreateCheck(ctx, c); err != nil {
		return CheckView{}, err
	}
	s.event(ctx, a, "check.create", c.ID, map[string]any{"document": doc.ID, "block": in.Block, "interval": c.IntervalSec})
	return s.checkView(ctx, c), nil
}

// UpdateCheck changes name, interval, entry or switch of a check.
func (s *Service) UpdateCheck(ctx context.Context, a Actor, ref, id string, in CheckInput) (CheckView, error) {
	if err := in.normalize(); err != nil {
		return CheckView{}, err
	}
	p, err := s.project(ctx, a, ref, store.RoleEditor)
	if err != nil {
		return CheckView{}, err
	}
	if err := aiChecks(a, p); err != nil {
		return CheckView{}, err
	}
	c, err := s.DB.GetCheck(ctx, p.ID, id)
	if err != nil {
		return CheckView{}, err
	}
	if c.EntityID, err = s.checkEntity(ctx, p.ID, in.Entity); err != nil {
		return CheckView{}, err
	}
	if in.Name != "" {
		c.Name = in.Name
	}
	c.IntervalSec = in.IntervalSec
	if in.Enabled != nil {
		c.Enabled = *in.Enabled
	}
	if err := s.DB.UpdateCheckSettings(ctx, c); err != nil {
		return CheckView{}, err
	}
	s.event(ctx, a, "check.update", c.ID, map[string]any{"interval": c.IntervalSec, "enabled": c.Enabled})
	return s.checkView(ctx, c), nil
}

// DeleteCheck removes a check.
func (s *Service) DeleteCheck(ctx context.Context, a Actor, ref, id string) error {
	p, err := s.project(ctx, a, ref, store.RoleEditor)
	if err != nil {
		return err
	}
	if err := aiChecks(a, p); err != nil {
		return err
	}
	if err := s.DB.DeleteCheck(ctx, p.ID, id); err != nil {
		return err
	}
	s.event(ctx, a, "check.delete", id, map[string]any{"project": p.ID})
	return nil
}

// Checks lists the checks of a project, or of one page when docRef is set.
// Checks on pages the actor cannot see are left out.
func (s *Service) Checks(ctx context.Context, a Actor, ref, docRef string) ([]CheckView, error) {
	var list []store.Check
	if docRef != "" {
		_, doc, err := s.docAccess(ctx, a, ref, docRef, store.RoleReader)
		if err != nil {
			return nil, err
		}
		if list, err = s.DB.DocumentChecks(ctx, doc.ID); err != nil {
			return nil, err
		}
	} else {
		p, err := s.project(ctx, a, ref, store.RoleReader)
		if err != nil {
			return nil, err
		}
		if list, err = s.DB.ProjectChecks(ctx, p.ID); err != nil {
			return nil, err
		}
	}
	out := []CheckView{}
	for _, c := range list {
		if docRef == "" {
			if _, _, err := s.docAccess(ctx, a, ref, c.DocumentID, store.RoleReader); err != nil {
				continue
			}
		}
		out = append(out, s.checkView(ctx, c))
	}
	return out, nil
}

// RunCheckNow runs a check at once and returns its new state.
func (s *Service) RunCheckNow(ctx context.Context, a Actor, ref, id string) (CheckView, error) {
	p, err := s.project(ctx, a, ref, store.RoleEditor)
	if err != nil {
		return CheckView{}, err
	}
	c, err := s.DB.GetCheck(ctx, p.ID, id)
	if err != nil {
		return CheckView{}, err
	}
	c = s.runCheck(ctx, c, time.Now())
	return s.checkView(ctx, c), nil
}

func (s *Service) checkView(ctx context.Context, c store.Check) CheckView {
	v := CheckView{Check: c}
	if doc, err := s.DB.GetDocument(ctx, c.ProjectID, c.DocumentID); err == nil {
		v.DocumentSlug, v.DocumentTitle = doc.Slug, doc.Title
	}
	if c.EntityID != "" {
		if e, err := s.DB.GetEntity(ctx, c.ProjectID, c.EntityID); err == nil {
			v.EntitySlug, v.EntityName = e.Slug, e.Name
		}
	}
	return v
}

// RunChecks runs every due check of all tenants (called by the scheduler).
// A run that is still busy when the next tick comes is not started twice.
func (s *Service) RunChecks(ctx context.Context, now time.Time) {
	if !s.checkMu.TryLock() {
		return
	}
	defer s.checkMu.Unlock()
	all, err := s.DB.EnabledChecks(ctx)
	if err != nil {
		slog.Warn("checks", "err", err)
		return
	}
	sem := make(chan struct{}, checkWorkers)
	var wg sync.WaitGroup
	for _, c := range all {
		if !c.LastRun.IsZero() && now.Sub(c.LastRun.Time) < time.Duration(c.IntervalSec)*time.Second {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(c store.Check) {
			defer func() { <-sem; wg.Done() }()
			s.runCheck(store.WithTenant(ctx, c.TenantID), c, now)
		}(c)
	}
	wg.Wait()
}

// runCheck runs one check as its creator and stores the outcome.
func (s *Service) runCheck(ctx context.Context, c store.Check, now time.Time) store.Check {
	start := time.Now()
	ok, msg, detail := s.probe(ctx, c)
	prev := c.LastStatus
	c.LastRun = store.Time{Time: now}
	c.LastMs = int(time.Since(start).Milliseconds())
	c.LastMessage, c.LastDetail = msg, detail
	if ok {
		c.FailCount = 0
		c.LastStatus = store.CheckOK
	} else {
		c.FailCount++
		if c.FailCount >= CheckFailAfter {
			c.LastStatus = store.CheckFail
		}
	}
	if c.LastStatus != prev {
		c.LastChange = c.LastRun
	}
	if err := s.DB.SaveCheckResult(ctx, c); err != nil {
		slog.Warn("check result", "check", c.ID, "err", err)
	}
	if c.LastStatus != prev {
		s.checkChanged(ctx, c, prev)
	}
	return c
}

// probe sends the request of a check. The creator must still be able to
// read the page; a block that turned into a writing request is refused.
func (s *Service) probe(ctx context.Context, c store.Check) (ok bool, key, detail string) {
	u, err := s.DB.UserByID(ctx, c.CreatedBy)
	if err != nil || u.Disabled {
		return false, "check.msg.creator_gone", ""
	}
	a := Actor{User: u, Via: "web"}
	p, err := s.DB.ProjectFor(ctx, c.ProjectID, "")
	if err != nil {
		return false, "check.msg.page_gone", ""
	}
	_, doc, err := s.docAccess(ctx, a, p.Slug, c.DocumentID, store.RoleReader)
	if err != nil {
		return false, "check.msg.page_gone", ""
	}
	req, err := blockRequest(doc, c.BlockIndex)
	if err != nil {
		return false, "check.msg.block_gone", ""
	}
	if !requests.Safe(req.Method) {
		return false, "check.msg.method", req.Method
	}
	rctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	res, _, err := s.execRequest(rctx, p.ID, req)
	if err != nil {
		var ve *InvalidError
		if errors.As(err, &ve) && ve.Key == "validation.request_variable" {
			return false, "check.msg.variable", fmt.Sprint(ve.Params["name"])
		}
		return false, "check.msg.network", err.Error()
	}
	return judge(res)
}

// judge decides whether a response is good: no network error and all
// expectations met; without expectations a status below 400.
func judge(res requests.Result) (bool, string, string) {
	if res.Error != "" {
		return false, "check.msg.network", res.Error
	}
	status := fmt.Sprintf("HTTP %d", res.Status)
	if len(res.Checks) > 0 {
		for _, ch := range res.Checks {
			if !ch.OK {
				if ch.Actual != "" {
					return false, "check.msg.expect", fmt.Sprintf("%s → %s", ch.Expr, ch.Actual)
				}
				return false, "check.msg.expect", ch.Expr
			}
		}
		return true, "check.msg.ok", status
	}
	if res.Status == 0 || res.Status >= 400 {
		return false, "check.msg.status", status
	}
	return true, "check.msg.ok", status
}

func (s *Service) checkChanged(ctx context.Context, c store.Check, prev string) {
	u, err := s.DB.UserByID(ctx, c.CreatedBy)
	if err != nil {
		return
	}
	a := Actor{User: u, Via: "web"}
	kind := "check.recover"
	if c.LastStatus == store.CheckFail {
		kind = "check.fail"
	}
	payload := map[string]any{"name": c.Name, "status": c.LastStatus, "previous": prev, "message": c.LastMessage, "detail": c.LastDetail, "document": c.DocumentID}
	if c.EntityID != "" {
		if e, err := s.DB.GetEntity(ctx, c.ProjectID, c.EntityID); err == nil {
			state := "online"
			if c.LastStatus == store.CheckFail {
				state = "offline"
			}
			attrs := map[string]any{}
			_ = json.Unmarshal(e.Attrs, &attrs)
			attrs["state"] = state
			attrs["checked_at"] = c.LastRun.UTC().Format(time.RFC3339)
			if _, err := s.UpsertEntity(ctx, a, c.ProjectID, EntityInput{Kind: e.Kind, Name: e.Name, Slug: e.Slug, Attrs: attrs}); err != nil {
				slog.Warn("check entity state", "check", c.ID, "err", err)
			}
			payload["entity"] = e.Slug
		}
	}
	// A first good run is not news; everything else is.
	if prev == store.CheckUnknown && c.LastStatus == store.CheckOK {
		return
	}
	s.event(ctx, a, kind, c.ID, payload)
	s.mailCheck(ctx, u, c)
}

func (s *Service) mailCheck(ctx context.Context, u store.User, c store.Check) {
	p, err := s.DB.ProjectFor(ctx, c.ProjectID, "")
	if err != nil {
		return
	}
	link := "/p/" + p.Slug
	if doc, err := s.DB.GetDocument(ctx, c.ProjectID, c.DocumentID); err == nil {
		link += "/d/" + doc.Slug
	}
	kind := "check.recover"
	if c.LastStatus == store.CheckFail {
		kind = "check.fail"
	}
	s.notify(ctx, u, Note{Kind: kind, ProjectID: p.ID, Project: p.Name, Subject: c.ID, Title: c.Name, Body: c.LastDetail, Link: link,
		Mail: func(u store.User, locale string) (string, string, error) {
			return resources.Mail("check", locale, map[string]any{
				"Name": u.Name, "Check": c.Name, "Project": p.Name, "Failed": c.LastStatus == store.CheckFail,
				"Detail": c.LastDetail, "Time": c.LastRun.UTC().Format("2006-01-02 15:04 UTC"), "Link": s.PublicURL + link,
			})
		}})
}
