package core

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/phoedox/phoedox/internal/blob"
	"github.com/phoedox/phoedox/internal/mail"
	"github.com/phoedox/phoedox/internal/store"
	"github.com/phoedox/phoedox/resources"
)

// Moderation of feedback: topics, merging, tickets on boards, answers.

func renderMail(name, lang string, data map[string]any) (string, string, error) {
	return resources.Mail(name, lang, data)
}

// TopicQuery filters the topic list.
type TopicQuery struct {
	Portal   string `json:"portal,omitempty"`
	State    string `json:"state,omitempty"`
	Category string `json:"category,omitempty"`
	Priority int    `json:"priority,omitempty"`
	Q        string `json:"q,omitempty"`
}

// TopicList is the Feedback tab: portals, topics, and the portal settings
// moderators need (states, categories).
type TopicList struct {
	Portals []PortalView          `json:"portals"`
	Topics  []store.FeedbackTopic `json:"topics"`
	Owner   bool                  `json:"owner"`
}

// FeedbackTopics lists topics for moderators.
func (s *Service) FeedbackTopics(ctx context.Context, a Actor, ref string, q TopicQuery) (TopicList, error) {
	p, portals, err := s.moderation(ctx, a, ref)
	if err != nil {
		return TopicList{}, err
	}
	out := TopicList{Portals: []PortalView{}, Topics: []store.FeedbackTopic{}, Owner: p.Role == store.RoleOwner}
	ids := []string{}
	for _, pt := range portals {
		out.Portals = append(out.Portals, s.portalView(ctx, pt))
		if q.Portal == "" || q.Portal == pt.ID || q.Portal == pt.Handle {
			ids = append(ids, pt.ID)
		}
	}
	if len(ids) == 0 {
		return out, nil
	}
	out.Topics, err = s.DB.FeedbackTopics(ctx, store.TopicFilter{ProjectID: p.ID, PortalIDs: ids, State: q.State, Category: q.Category, Priority: q.Priority, Query: q.Q})
	return out, err
}

// TopicDetail is a topic with its reports and tickets.
type TopicDetail struct {
	store.FeedbackTopic
	Portal  PortalView             `json:"portal"`
	Reports []store.FeedbackReport `json:"reports"`
	Tickets []store.TopicTicket    `json:"tickets"`
}

func (s *Service) moderatedTopic(ctx context.Context, a Actor, ref, id string) (store.Project, store.FeedbackPortal, store.FeedbackTopic, error) {
	p, portals, err := s.moderation(ctx, a, ref)
	if err != nil {
		return p, store.FeedbackPortal{}, store.FeedbackTopic{}, err
	}
	t, err := s.DB.FeedbackTopic(ctx, p.ID, id)
	if err != nil {
		return p, store.FeedbackPortal{}, t, err
	}
	for _, pt := range portals {
		if pt.ID == t.PortalID {
			return p, pt, t, nil
		}
	}
	return p, store.FeedbackPortal{}, t, ErrNotFound
}

// FeedbackTopic returns a topic with its reports, attachments and tickets.
func (s *Service) FeedbackTopic(ctx context.Context, a Actor, ref, id string) (TopicDetail, error) {
	_, portal, t, err := s.moderatedTopic(ctx, a, ref, id)
	if err != nil {
		return TopicDetail{}, err
	}
	reports, err := s.DB.FeedbackReports(ctx, t.ID)
	if err != nil {
		return TopicDetail{}, err
	}
	ids := make([]string, len(reports))
	for i, r := range reports {
		ids[i] = r.ID
	}
	files, _ := s.DB.FeedbackFiles(ctx, ids)
	for i := range reports {
		reports[i].Attachments = files[reports[i].ID]
		// Moderators see the reporter, never secrets or the status link.
		reports[i].StatusHash = ""
	}
	tickets, _ := s.DB.TopicTickets(ctx, t.ID)
	if tickets == nil {
		tickets = []store.TopicTicket{}
	}
	return TopicDetail{FeedbackTopic: t, Portal: s.portalView(ctx, portal), Reports: reports, Tickets: tickets}, nil
}

// TopicPatch changes a topic; nil fields stay.
type TopicPatch struct {
	Title    *string   `json:"title,omitempty"`
	Summary  *string   `json:"summary,omitempty"`
	Category *string   `json:"category,omitempty"`
	Tags     *[]string `json:"tags,omitempty"`
	Priority *int      `json:"priority,omitempty"`
	State    *string   `json:"state,omitempty"`
	// Answer is shown to reporters (status link, notification).
	Answer *string `json:"answer,omitempty"`
}

// UpdateFeedbackTopic changes a topic (moderators). A new state or answer
// tells the reporters.
func (s *Service) UpdateFeedbackTopic(ctx context.Context, a Actor, ref, id string, patch TopicPatch) (TopicDetail, error) {
	p, portal, t, err := s.moderatedTopic(ctx, a, ref, id)
	if err != nil {
		return TopicDetail{}, err
	}
	ps := portalSettings(portal)
	before := t
	if patch.Title != nil {
		if v := strings.TrimSpace(*patch.Title); v != "" {
			t.Title = excerpt(v, 200)
		}
	}
	if patch.Summary != nil {
		t.Summary = excerpt(strings.TrimSpace(*patch.Summary), 4000)
	}
	if patch.Category != nil {
		if *patch.Category != "" && !slices.Contains(ps.Categories, *patch.Category) {
			return TopicDetail{}, invalid("validation.feedback_category", map[string]any{"category": *patch.Category}, "unknown category %q", *patch.Category)
		}
		t.Category = *patch.Category
	}
	if patch.Tags != nil {
		t.Tags = cleanLabels(*patch.Tags)
	}
	if patch.Priority != nil {
		t.Priority = min(max(*patch.Priority, 1), 4)
	}
	if patch.State != nil {
		if !slices.Contains(ps.States, *patch.State) {
			return TopicDetail{}, invalid("validation.feedback_state", map[string]any{"state": *patch.State}, "unknown state %q", *patch.State)
		}
		t.State = *patch.State
	}
	if patch.Answer != nil {
		t.Answer = excerpt(strings.TrimSpace(*patch.Answer), 4000)
	}
	s.setClosed(&t, ps)
	t.UpdatedAt = store.Now()
	if err := s.DB.SaveFeedbackTopic(ctx, t); err != nil {
		return TopicDetail{}, err
	}
	s.event(ctx, a, "feedback.topic", t.ID, patch)
	if t.State != before.State || t.Answer != before.Answer {
		s.tellReporters(ctx, p, portal, t)
	}
	return s.FeedbackTopic(ctx, a, ref, id)
}

func (s *Service) setClosed(t *store.FeedbackTopic, ps PortalSettings) {
	closed := t.State == ps.FinishedState || t.State == "wont_fix"
	switch {
	case closed && t.ClosedAt == nil:
		now := store.Now()
		t.ClosedAt = &now
	case !closed:
		t.ClosedAt = nil
	}
}

// tellReporters notifies signed-in reporters and mails anonymous ones who
// left an address about a topic's state and answer.
func (s *Service) tellReporters(ctx context.Context, p store.Project, portal store.FeedbackPortal, t store.FeedbackTopic) {
	reports, err := s.DB.FeedbackReports(ctx, t.ID)
	if err != nil {
		return
	}
	seen := map[string]bool{}
	for _, r := range reports {
		switch {
		case r.UserID != "" && !seen[r.UserID]:
			seen[r.UserID] = true
			if u, err := s.DB.UserByID(ctx, r.UserID); err == nil {
				s.notify(ctx, u, Note{Kind: "feedback.update", ProjectID: p.ID, Project: portal.Name, Subject: t.ID, Title: t.Title, Body: t.Answer, Link: "/settings/feedback"})
			}
		case r.Email != "" && !seen[r.Email] && s.Mail != nil && s.Mail.Enabled() && s.PublicURL != "":
			seen[r.Email] = true
			lang := r.Lang
			if lang != "de" {
				lang = "en"
			}
			subject, body, err := renderMail("feedback_update", lang, map[string]any{"Portal": portal.Name, "Title": t.Title, "State": t.State, "Answer": t.Answer})
			if err == nil {
				_ = s.Mail.Send(ctx, mail.Message{To: r.Email, Subject: subject, Body: body})
			}
		}
	}
}

// MergeFeedbackTopics moves all reports and tickets of one topic into
// another and removes the first (moderators).
func (s *Service) MergeFeedbackTopics(ctx context.Context, a Actor, ref, into, from string) (TopicDetail, error) {
	_, _, target, err := s.moderatedTopic(ctx, a, ref, into)
	if err != nil {
		return TopicDetail{}, err
	}
	_, _, src, err := s.moderatedTopic(ctx, a, ref, from)
	if err != nil {
		return TopicDetail{}, err
	}
	if src.ID == target.ID {
		return TopicDetail{}, invalid("validation.feedback_merge_self", nil, "a topic cannot be merged into itself")
	}
	if err := s.DB.MoveTopicReports(ctx, src.ID, target.ID); err != nil {
		return TopicDetail{}, err
	}
	if err := s.DB.DeleteFeedbackTopic(ctx, src.ID); err != nil {
		return TopicDetail{}, err
	}
	_ = s.DB.RecountTopic(ctx, target.ID)
	s.event(ctx, a, "feedback.merge", target.ID, map[string]string{"from": src.ID})
	return s.FeedbackTopic(ctx, a, ref, target.ID)
}

// MoveFeedbackReport puts a report into another topic, or into a new one
// when topic is "" (split; moderators).
func (s *Service) MoveFeedbackReport(ctx context.Context, a Actor, ref, reportID, topicID string) (TopicDetail, error) {
	p, portals, err := s.moderation(ctx, a, ref)
	if err != nil {
		return TopicDetail{}, err
	}
	r, err := s.DB.FeedbackReport(ctx, p.ID, reportID)
	if err != nil {
		return TopicDetail{}, err
	}
	idx := slices.IndexFunc(portals, func(x store.FeedbackPortal) bool { return x.ID == r.PortalID })
	if idx < 0 {
		return TopicDetail{}, ErrNotFound
	}
	old := r.TopicID
	var t store.FeedbackTopic
	if topicID == "" {
		ps := portalSettings(portals[idx])
		now := store.Now()
		t = store.FeedbackTopic{ID: newID(), ProjectID: p.ID, PortalID: r.PortalID, Title: r.Title, Summary: excerpt(r.Body, 600), Type: r.Type, Tags: []string{},
			Priority: 3, State: ps.DefaultState, CreatedAt: now, UpdatedAt: now}
		if err := s.DB.SaveFeedbackTopic(ctx, t); err != nil {
			return TopicDetail{}, err
		}
	} else if t, err = s.DB.FeedbackTopic(ctx, p.ID, topicID); err != nil || t.PortalID != r.PortalID {
		return TopicDetail{}, ErrNotFound
	}
	if err := s.DB.MoveFeedbackReport(ctx, r.ID, t.ID); err != nil {
		return TopicDetail{}, err
	}
	_ = s.DB.RecountTopic(ctx, t.ID)
	if old != "" {
		_ = s.DB.RecountTopic(ctx, old)
		if ot, err := s.DB.FeedbackTopic(ctx, p.ID, old); err == nil && ot.ReportCount == 0 && ot.Tickets == 0 {
			_ = s.DB.DeleteFeedbackTopic(ctx, old)
		}
	}
	s.event(ctx, a, "feedback.move", r.ID, map[string]string{"from": old, "to": t.ID})
	return s.FeedbackTopic(ctx, a, ref, t.ID)
}

// TicketInput puts a topic onto a board.
type TicketInput struct {
	Board  string `json:"board"`
	Column string `json:"column,omitempty"`
	Title  string `json:"title,omitempty"`
}

// FeedbackToBoard creates a card from a topic on a board of the project
// and links it (moderators who may edit the board). A topic can have
// several tickets.
func (s *Service) FeedbackToBoard(ctx context.Context, a Actor, ref, id string, in TicketInput) (TopicDetail, error) {
	p, portal, t, err := s.moderatedTopic(ctx, a, ref, id)
	if err != nil {
		return TopicDetail{}, err
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		title = t.Title
	}
	var b strings.Builder
	fmt.Fprintf(&b, "From the feedback portal **%s** (%s, %d reports).\n\n", portal.Name, t.Type, t.ReportCount)
	if t.Summary != "" {
		b.WriteString(t.Summary + "\n\n")
	}
	fmt.Fprintf(&b, "[Open the topic](/p/%s/feedback?topic=%s)", p.Slug, t.ID)
	labels := []string{"feedback"}
	if t.Category != "" {
		labels = append(labels, t.Category)
	}
	prio := map[int]int{1: 3, 2: 2, 3: 1, 4: 0}[t.Priority]
	task, err := s.CreateTask(ctx, a, p.Slug, in.Board, TaskInput{Title: title, Description: b.String(), Column: in.Column, Labels: labels, Priority: prio})
	if err != nil {
		return TopicDetail{}, err
	}
	if err := s.DB.LinkTopicTask(ctx, t.ID, task.ID, a.User.ID); err != nil {
		return TopicDetail{}, err
	}
	s.event(ctx, a, "feedback.ticket", t.ID, map[string]string{"task": task.ID})
	return s.FeedbackTopic(ctx, a, ref, id)
}

// feedbackTaskDone closes topics whose tickets are all done (called when a
// card moves into or out of a done column).
func (s *Service) feedbackTaskDone(ctx context.Context, taskID string) {
	topics, err := s.DB.TopicsOfTask(ctx, taskID)
	if err != nil {
		return
	}
	for _, id := range topics {
		t, err := s.DB.FeedbackTopic(ctx, "", id)
		if err != nil {
			continue
		}
		portal, err := s.DB.FeedbackPortal(ctx, "", t.PortalID)
		if err != nil {
			continue
		}
		ps := portalSettings(portal)
		tickets, err := s.DB.TopicTickets(ctx, id)
		if err != nil || len(tickets) == 0 {
			continue
		}
		all := true
		for _, k := range tickets {
			all = all && k.Done
		}
		before := t.State
		switch {
		case all && t.State != ps.FinishedState:
			t.State = ps.FinishedState
		case !all && t.State == ps.FinishedState:
			t.State = ps.DefaultState
			if slices.Contains(ps.States, "in_progress") {
				t.State = "in_progress"
			}
		default:
			continue
		}
		s.setClosed(&t, ps)
		t.UpdatedAt = store.Now()
		if s.DB.SaveFeedbackTopic(ctx, t) == nil && t.State != before {
			if p, err := s.DB.ProjectFor(ctx, t.ProjectID, ""); err == nil {
				s.tellReporters(ctx, p, portal, t)
			}
		}
	}
}

// FeedbackAttachment opens an attachment for moderators.
func (s *Service) FeedbackAttachment(ctx context.Context, a Actor, ref, id string) (store.FeedbackFile, blob.ReadSeekCloser, error) {
	p, portals, err := s.moderation(ctx, a, ref)
	if err != nil {
		return store.FeedbackFile{}, nil, err
	}
	f, err := s.DB.FeedbackFile(ctx, p.ID, id)
	if err != nil {
		return f, nil, err
	}
	r, err := s.DB.FeedbackReport(ctx, p.ID, f.ReportID)
	if err != nil || !slices.ContainsFunc(portals, func(x store.FeedbackPortal) bool { return x.ID == r.PortalID }) {
		return f, nil, ErrNotFound
	}
	if s.Blobs == nil {
		return f, nil, errNoBlobs
	}
	rc, err := s.Blobs.Open(ctx, f.SHA256)
	return f, rc, err
}

// FeedbackQuotaInput sets a project's feedback storage (system admins).
func (s *Service) SetFeedbackQuota(ctx context.Context, a Actor, ref string, mb *int) error {
	if !a.User.IsAdmin {
		return ErrForbidden
	}
	p, err := s.project(ctx, a, ref, store.RoleReader)
	if err != nil {
		return err
	}
	if mb != nil && (*mb < 0 || *mb > 10_000_000) {
		return invalid("validation.quota_above_max", map[string]any{"max": 10_000_000}, "invalid quota")
	}
	return s.DB.SetFeedbackQuota(ctx, p.ID, mb)
}

// PurgeFeedback deletes closed anonymous reports after each portal's
// retention (a daily job).
func (s *Service) PurgeFeedback(ctx context.Context, projectID string) {
	portals, err := s.DB.FeedbackPortals(ctx, projectID)
	if err != nil {
		return
	}
	for _, p := range portals {
		ps := portalSettings(p)
		hashes, err := s.DB.PurgeFeedbackReports(ctx, p.ID, time.Now().AddDate(0, -ps.RetentionMonths, 0))
		if err == nil {
			s.freeBlobs(ctx, hashes)
		}
	}
}

// Housekeeping runs the retention of every portal and prunes the bot
// check's log (every six hours).
func (s *Service) FeedbackHousekeeping(ctx context.Context) {
	if ids, err := s.DB.AllFeedbackProjects(ctx); err == nil {
		for _, id := range ids {
			s.PurgeFeedback(ctx, id)
		}
	}
	_ = s.DB.PurgeBot(ctx, time.Now().Add(-7*24*time.Hour))
}
