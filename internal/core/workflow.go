package core

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/phoedox/phoedox/internal/store"
)

// Approval workflow (ADR-0042): in projects that require approval, editors
// change pages in drafts and ask for a review; owners publish (approve) or
// send the draft back with a note. New pages, boards, decisions and links
// are not affected.

// Approval returns whether a project requires approval (readers).
func (s *Service) Approval(ctx context.Context, a Actor, ref string) (bool, error) {
	p, err := s.project(ctx, a, ref, store.RoleReader)
	if err != nil {
		return false, err
	}
	return s.DB.ProjectApproval(ctx, p.ID)
}

// SetApproval switches the approval workflow (owners).
func (s *Service) SetApproval(ctx context.Context, a Actor, ref string, on bool) error {
	p, err := s.project(ctx, a, ref, store.RoleOwner)
	if err != nil {
		return err
	}
	if err := s.DB.SetProjectApproval(ctx, p.ID, on); err != nil {
		return err
	}
	s.event(ctx, a, "project.approval", p.ID, map[string]bool{"enabled": on})
	return nil
}

// needsApproval: in an approval project only owners publish page changes.
func (s *Service) needsApproval(ctx context.Context, p store.Project) bool {
	if p.Role == store.RoleOwner {
		return false
	}
	on, _ := s.DB.ProjectApproval(ctx, p.ID)
	return on
}

// openDraft starts the draft of a page (used when an approval project turns
// a direct change into a draft).
func (s *Service) openDraft(ctx context.Context, a Actor, doc store.Document) error {
	now := store.Now()
	d := store.Draft{DocumentID: doc.ID, BaseVersion: doc.Version, Title: doc.Title, Category: doc.Category, Markdown: draftBody(doc),
		StartedBy: a.String(), StartedAt: now, UpdatedBy: a.String(), UpdatedAt: now}
	d.AddParticipant(a.String())
	if err := s.DB.SaveDraft(ctx, d); err != nil {
		return err
	}
	s.event(ctx, a, "document.draft_start", doc.ID, map[string]any{"base": doc.Version, "approval": true})
	return nil
}

// RequestReview asks the owners to approve a draft (participants).
func (s *Service) RequestReview(ctx context.Context, a Actor, ref, docRef, note string) (DraftInfo, error) {
	p, doc, err := s.docAccess(ctx, a, ref, docRef, store.RoleEditor)
	if err != nil {
		return DraftInfo{}, err
	}
	d, err := s.DB.GetDraft(ctx, doc.ID)
	if err != nil {
		return DraftInfo{}, err
	}
	if !s.mayEndDraft(a, p, d) {
		return DraftInfo{}, ErrForbidden
	}
	d.ReviewBy, d.ReviewAt, d.ReviewNote = a.String(), store.Now(), strings.TrimSpace(note)
	if err := s.DB.SaveDraft(ctx, d); err != nil {
		return DraftInfo{}, err
	}
	s.event(ctx, a, "document.review_request", doc.ID, map[string]string{"note": d.ReviewNote})
	members, _ := s.DB.ListMembers(ctx, p.ID)
	for _, m := range members {
		if m.Role != store.RoleOwner || m.UserID == a.User.ID {
			continue
		}
		if u, err := s.DB.UserByID(ctx, m.UserID); err == nil {
			s.notify(ctx, u, Note{Kind: "draft.review", ProjectID: p.ID, Project: p.Name, Subject: doc.ID, Title: doc.Title, Body: d.ReviewNote,
				Link: "/p/" + p.Slug + "/d/" + doc.Slug, Actor: a.User.Name})
		}
	}
	return s.draftInfo(ctx, a, p, doc, d), nil
}

// RejectReview sends a draft back to its participants with a note (owners).
func (s *Service) RejectReview(ctx context.Context, a Actor, ref, docRef, note string) (DraftInfo, error) {
	p, doc, err := s.docAccess(ctx, a, ref, docRef, store.RoleOwner)
	if err != nil {
		return DraftInfo{}, err
	}
	if a.Via == "mcp" || a.Plugin != "" {
		return DraftInfo{}, ErrForbidden
	}
	d, err := s.DB.GetDraft(ctx, doc.ID)
	if err != nil {
		return DraftInfo{}, err
	}
	d.ReviewBy, d.ReviewAt, d.ReviewNote = "", store.Time{}, strings.TrimSpace(note)
	if err := s.DB.SaveDraft(ctx, d); err != nil {
		return DraftInfo{}, err
	}
	s.event(ctx, a, "document.review_reject", doc.ID, map[string]string{"note": d.ReviewNote})
	s.notifyParticipants(ctx, a, p, doc, d, "draft.changes_requested", d.ReviewNote)
	return s.draftInfo(ctx, a, p, doc, d), nil
}

func (s *Service) notifyParticipants(ctx context.Context, a Actor, p store.Project, doc store.Document, d store.Draft, kind, body string) {
	sent := map[string]bool{a.User.ID: true}
	for _, x := range d.Participants {
		id := store.ActorUserID(x)
		if id == "" || sent[id] {
			continue
		}
		sent[id] = true
		if u, err := s.DB.UserByID(ctx, id); err == nil {
			s.notify(ctx, u, Note{Kind: kind, ProjectID: p.ID, Project: p.Name, Subject: doc.ID, Title: doc.Title, Body: body,
				Link: "/p/" + p.Slug + "/d/" + doc.Slug, Actor: a.User.Name})
		}
	}
}

// PageRuleInfo is what the page view shows about review dates and required reading.
type PageRuleInfo struct {
	ReviewDays int        `json:"reviewDays"`
	ReviewedAt store.Time `json:"reviewedAt"`
	ReviewedBy string     `json:"reviewedBy,omitempty"`
	// ReviewDue is the date the page should be checked again (YYYY-MM-DD).
	ReviewDue       string `json:"reviewDue,omitempty"`
	Overdue         bool   `json:"overdue"`
	RequiredReading bool   `json:"requiredReading"`
	// Confirmed: the caller confirmed the current version.
	Confirmed bool `json:"confirmed"`
	// Readers is for editors: who confirmed which version.
	Readers []ReaderStatus `json:"readers,omitempty"`
}

// ReaderStatus is one member's read confirmation.
type ReaderStatus struct {
	Name        string     `json:"name"`
	Version     int        `json:"version"`
	Current     bool       `json:"current"`
	ConfirmedAt store.Time `json:"confirmedAt"`
}

// PageRulesInput changes the rules of a page; nil fields stay.
type PageRulesInput struct {
	ReviewDays      *int  `json:"reviewDays,omitempty"`
	RequiredReading *bool `json:"requiredReading,omitempty"`
}

// reviewDue is when a page needs its next review: after the interval since
// the last review or change, whichever is later.
func reviewDue(r store.PageRule, doc store.Document) time.Time {
	if r.ReviewDays <= 0 {
		return time.Time{}
	}
	base := doc.UpdatedAt.Time
	if r.ReviewedAt.After(base) {
		base = r.ReviewedAt.Time
	}
	return base.AddDate(0, 0, r.ReviewDays)
}

func (s *Service) pageRule(ctx context.Context, p store.Project, doc store.Document) (store.PageRule, error) {
	r, err := s.DB.GetPageRule(ctx, doc.ID)
	if errors.Is(err, store.ErrNotFound) {
		return store.PageRule{DocumentID: doc.ID, ProjectID: p.ID}, nil
	}
	return r, err
}

// PageRules returns review date and required reading of a page (readers).
func (s *Service) PageRules(ctx context.Context, a Actor, ref, docRef string) (PageRuleInfo, error) {
	p, doc, err := s.docAccess(ctx, a, ref, docRef, store.RoleReader)
	if err != nil {
		return PageRuleInfo{}, err
	}
	r, err := s.pageRule(ctx, p, doc)
	if err != nil {
		return PageRuleInfo{}, err
	}
	info := PageRuleInfo{ReviewDays: r.ReviewDays, ReviewedAt: r.ReviewedAt, RequiredReading: r.RequiredReading}
	if r.ReviewedBy != "" {
		if ps := s.participants(ctx, []string{r.ReviewedBy}); len(ps) == 1 {
			info.ReviewedBy = ps[0].Name
		}
	}
	if due := reviewDue(r, doc); !due.IsZero() {
		info.ReviewDue, info.Overdue = due.UTC().Format(time.DateOnly), !due.After(time.Now())
	}
	if !r.RequiredReading {
		return info, nil
	}
	confirmed, err := s.DB.ReadConfirmations(ctx, doc.ID)
	if err != nil {
		return info, err
	}
	info.Confirmed = confirmed[a.User.ID].Version == doc.Version
	if rank[p.Role] >= rank[store.RoleEditor] {
		members, _ := s.DB.ListMembers(ctx, p.ID)
		info.Readers = []ReaderStatus{}
		for _, m := range members {
			c := confirmed[m.UserID]
			info.Readers = append(info.Readers, ReaderStatus{Name: m.Name, Version: c.Version, Current: c.Version == doc.Version, ConfirmedAt: c.ConfirmedAt})
		}
	}
	return info, nil
}

// SetPageRules changes review interval and required reading (editors).
// Turning required reading on notifies the members.
func (s *Service) SetPageRules(ctx context.Context, a Actor, ref, docRef string, in PageRulesInput) (PageRuleInfo, error) {
	p, doc, err := s.docAccess(ctx, a, ref, docRef, store.RoleEditor)
	if err != nil {
		return PageRuleInfo{}, err
	}
	r, err := s.pageRule(ctx, p, doc)
	if err != nil {
		return PageRuleInfo{}, err
	}
	wasRequired := r.RequiredReading
	if in.ReviewDays != nil {
		if *in.ReviewDays < 0 || *in.ReviewDays > 3660 {
			return PageRuleInfo{}, invalid("validation.review_days", map[string]any{"max": 3660}, "review interval must be between 0 and 3660 days")
		}
		r.ReviewDays, r.NotifiedAt = *in.ReviewDays, store.Time{}
	}
	if in.RequiredReading != nil {
		r.RequiredReading = *in.RequiredReading
	}
	r.UpdatedBy, r.UpdatedAt = a.String(), store.Now()
	if err := s.DB.SavePageRule(ctx, r); err != nil {
		return PageRuleInfo{}, err
	}
	s.event(ctx, a, "document.rules", doc.ID, map[string]any{"reviewDays": r.ReviewDays, "requiredReading": r.RequiredReading})
	if r.RequiredReading && !wasRequired {
		members, _ := s.DB.ListMembers(ctx, p.ID)
		for _, m := range members {
			if m.UserID == a.User.ID {
				continue
			}
			if u, err := s.DB.UserByID(ctx, m.UserID); err == nil {
				if _, err := s.docRole(ctx, Actor{User: u, Via: "web"}, p, doc); err == nil {
					s.notify(ctx, u, Note{Kind: "page.required_reading", ProjectID: p.ID, Project: p.Name, Subject: doc.ID, Title: doc.Title,
						Link: "/p/" + p.Slug + "/d/" + doc.Slug, Actor: a.User.Name})
				}
			}
		}
	}
	return s.PageRules(ctx, a, ref, doc.ID)
}

// MarkReviewed records that someone checked a page and found it accurate (editors).
func (s *Service) MarkReviewed(ctx context.Context, a Actor, ref, docRef string) (PageRuleInfo, error) {
	p, doc, err := s.docAccess(ctx, a, ref, docRef, store.RoleEditor)
	if err != nil {
		return PageRuleInfo{}, err
	}
	r, err := s.pageRule(ctx, p, doc)
	if err != nil {
		return PageRuleInfo{}, err
	}
	now := store.Now()
	r.ReviewedAt, r.ReviewedBy, r.NotifiedAt = now, a.String(), store.Time{}
	if r.UpdatedBy == "" {
		r.UpdatedBy, r.UpdatedAt = a.String(), now
	}
	if err := s.DB.SavePageRule(ctx, r); err != nil {
		return PageRuleInfo{}, err
	}
	s.event(ctx, a, "document.reviewed", doc.ID, map[string]int{"version": doc.Version})
	return s.PageRules(ctx, a, ref, doc.ID)
}

// ConfirmRead records that the caller read the current version (readers).
func (s *Service) ConfirmRead(ctx context.Context, a Actor, ref, docRef string) (PageRuleInfo, error) {
	_, doc, err := s.docAccess(ctx, a, ref, docRef, store.RoleReader)
	if err != nil {
		return PageRuleInfo{}, err
	}
	if a.Via == "mcp" || a.Plugin != "" || a.User.ID == "" {
		return PageRuleInfo{}, ErrForbidden // only people confirm reading
	}
	if err := s.DB.ConfirmRead(ctx, doc.ID, a.User.ID, doc.Version, store.Now()); err != nil {
		return PageRuleInfo{}, err
	}
	s.event(ctx, a, "document.read_confirm", doc.ID, map[string]int{"version": doc.Version})
	return s.PageRules(ctx, a, ref, doc.ID)
}

// RemindReviews notifies once when a page's review is due: its owners and
// whoever set the interval (System context, all tenants).
func (s *Service) RemindReviews(ctx context.Context, now time.Time) {
	rules, err := s.DB.PageRules(store.System(ctx), "")
	if err != nil {
		return
	}
	for _, r := range rules {
		if r.ReviewDays <= 0 {
			continue
		}
		tctx := store.WithTenant(ctx, r.TenantID)
		doc, err := s.DB.GetDocument(tctx, r.ProjectID, r.DocumentID)
		if err != nil {
			continue
		}
		due := reviewDue(r, doc)
		if due.After(now) || !r.NotifiedAt.IsZero() && r.NotifiedAt.After(due) {
			continue
		}
		p, err := s.DB.ProjectFor(tctx, r.ProjectID, "")
		if err != nil {
			continue
		}
		ids := []string{}
		if id := store.ActorUserID(r.UpdatedBy); id != "" {
			ids = append(ids, id)
		}
		members, _ := s.DB.ListMembers(tctx, p.ID)
		for _, m := range members {
			if m.Role == store.RoleOwner && !slices.Contains(ids, m.UserID) {
				ids = append(ids, m.UserID)
			}
		}
		for _, id := range ids {
			if u, err := s.DB.UserByID(tctx, id); err == nil && !u.Disabled {
				s.notify(tctx, u, Note{Kind: "page.review_due", ProjectID: p.ID, Project: p.Name, Subject: doc.ID + ":" + due.Format(time.DateOnly), Title: doc.Title,
					Link: "/p/" + p.Slug + "/d/" + doc.Slug})
			}
		}
		r.NotifiedAt = store.Time{Time: now}
		_ = s.DB.SavePageRule(tctx, r)
		_ = s.DB.AddEvent(tctx, "system", "document.review_due", doc.ID, map[string]string{"due": due.Format(time.DateOnly)})
	}
}
