package core

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/phoedox/phoedox/internal/store"
	"github.com/phoedox/phoedox/resources"
)

// Feedback portals, more (feedback-concept): routing new topics to boards
// with automation rules, "+n new reports" on cards, topics linked to
// entries of the knowledge graph, urgent notes for moderators and answer
// drafts by the AI.

// attachFeedback adds the feedback topic behind each card.
func (s *Service) attachFeedback(ctx context.Context, projectID string, tasks []store.Task) {
	if len(tasks) == 0 {
		return
	}
	m, err := s.DB.TaskFeedbackOf(ctx, projectID)
	if err != nil || len(m) == 0 {
		return
	}
	for i := range tasks {
		if f, ok := m[tasks[i].ID]; ok {
			tasks[i].Feedback = &f
		}
	}
}

// SeenTaskFeedback resets "+n new reports" of a card (editors of its board).
func (s *Service) SeenTaskFeedback(ctx context.Context, a Actor, ref, taskID string) error {
	p, err := s.project(ctx, a, ref, store.RoleEditor)
	if err != nil {
		return err
	}
	t, err := s.DB.GetTask(ctx, p.ID, taskID)
	if err != nil {
		return err
	}
	if _, _, err := s.boardAccess(ctx, a, p.Slug, t.BoardID, store.RoleEditor); err != nil {
		return err
	}
	return s.DB.SeenTaskFeedback(ctx, t.ID)
}

// ruleFeedbackTopic fires feedback rules for a topic. Each rule acts once
// per topic: before the triage only rules without a category match, after
// it those with the topic's category.
func (s *Service) ruleFeedbackTopic(ctx context.Context, p store.Project, portal store.FeedbackPortal, t store.FeedbackTopic) {
	if t.Spam {
		return
	}
	vars := map[string]string{"topic": t.Title, "category": t.Category, "type": t.Type, "portal": portal.Name, "message": t.Summary,
		"project": p.Name, "link": s.PublicURL + "/p/" + p.Slug + "/feedback?topic=" + t.ID, "date": time.Now().UTC().Format(time.DateOnly),
		"feedback_topic": t.ID, "feedback_reports": fmt.Sprint(t.ReportCount)}
	s.fire(ctx, Actor{Via: "feedback"}, p.ID, "feedback:"+t.ID, func(r RuleTrigger) bool {
		return r.Type == TriggerFeedbackTopic && (r.Portal == "" || r.Portal == portal.ID) &&
			(r.Category == "" || r.Category == t.Category) && (r.ReportType == "" || r.ReportType == t.Type)
	}, vars)
}

// notifyUrgent tells moderators at once about a topic that became urgent.
func (s *Service) notifyUrgent(ctx context.Context, p store.Project, portal store.FeedbackPortal, t store.FeedbackTopic) {
	s.notifyModerators(ctx, portal, Note{Kind: "feedback.urgent", ProjectID: p.ID, Project: p.Name, Subject: t.ID, Title: t.Title,
		Body: t.AINote, Link: "/p/" + p.Slug + "/feedback?topic=" + t.ID})
}

// EntityFeedback is a topic as an entry page lists it.
type EntityFeedback struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	State    string `json:"state"`
	Open     bool   `json:"open"`
	Priority int    `json:"priority"`
	Reports  int    `json:"reports"`
	Portal   string `json:"portal"`
}

// EntityFeedbackTopics lists the feedback topics linked to an entry, for
// people who moderate one of the portals (others get an empty list).
func (s *Service) EntityFeedbackTopics(ctx context.Context, a Actor, ref, entRef string) ([]EntityFeedback, error) {
	out := []EntityFeedback{}
	p, err := s.project(ctx, a, ref, store.RoleReader)
	if err != nil {
		return nil, err
	}
	e, err := s.DB.GetEntity(ctx, p.ID, entRef)
	if err != nil {
		return nil, err
	}
	_, portals, err := s.moderation(ctx, a, ref)
	if err != nil || len(portals) == 0 {
		return out, nil
	}
	names := map[string]string{}
	for _, pt := range portals {
		names[pt.ID] = pt.Name
	}
	topics, err := s.DB.EntityTopics(ctx, e.ID)
	if err != nil {
		return nil, err
	}
	for _, t := range topics {
		if n, ok := names[t.PortalID]; ok {
			out = append(out, EntityFeedback{ID: t.ID, Title: t.Title, State: t.State, Open: t.ClosedAt == nil, Priority: t.Priority, Reports: t.ReportCount, Portal: n})
		}
	}
	return out, nil
}

// suggestEntities links the entries the AI named to a topic (marked as
// suggestions; moderators remove them by editing the list).
func (s *Service) suggestEntities(ctx context.Context, p store.Project, topicID string, slugs []string) {
	for i, sl := range slugs {
		if i >= 5 {
			break
		}
		if e, err := s.DB.GetEntity(ctx, p.ID, strings.TrimPrefix(strings.TrimSpace(sl), "@")); err == nil {
			_ = s.DB.AddTopicEntity(ctx, p.ID, topicID, e.ID, "ai")
		}
	}
}

// entityList is the part of the triage prompt that names the project's
// entries (at most 150), so the AI can say which ones a report is about.
func (s *Service) entityList(ctx context.Context, p store.Project) string {
	ents, err := s.DB.ListEntities(ctx, p.ID, "")
	if err != nil || len(ents) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\nEntries of the project (slug: name, kind):\n")
	for i, e := range ents {
		if i >= 150 {
			break
		}
		fmt.Fprintf(&b, "- %s: %s, %s\n", e.Slug, e.Name, e.Kind)
	}
	return b.String()
}

// FeedbackAnswerDraft is an answer the AI proposes for moderators.
type FeedbackAnswerDraft struct {
	Text string `json:"text"`
}

// DraftFeedbackAnswer lets the AI write an answer to the reporters of a
// topic (moderators; on the project's background AI budget). Nothing is
// sent: the moderator edits and saves it as the topic's answer.
func (s *Service) DraftFeedbackAnswer(ctx context.Context, a Actor, ref, id, lang string) (FeedbackAnswerDraft, error) {
	p, portal, t, err := s.moderatedTopic(ctx, a, ref, id)
	if err != nil {
		return FeedbackAnswerDraft{}, err
	}
	reports, _ := s.DB.FeedbackReports(ctx, t.ID)
	if lang == "" && len(reports) > 0 {
		lang = reports[0].Lang
	}
	if lang != "de" {
		lang = "en"
	}
	tickets, _ := s.DB.TopicTickets(ctx, t.ID)
	var b strings.Builder
	fmt.Fprintf(&b, "Portal: %s\nAnswer in language: %s\nTopic: %s\nState: %s\n", portal.Name, lang, t.Title, t.State)
	if t.FixedIn != "" {
		fmt.Fprintf(&b, "Fixed in version: %s\n", t.FixedIn)
	}
	if t.Summary != "" {
		fmt.Fprintf(&b, "Summary: %s\n", t.Summary)
	}
	for _, k := range tickets {
		fmt.Fprintf(&b, "Ticket: %s (%s, done: %v)\n", k.Title, k.Column, k.Done)
	}
	if t.Answer != "" {
		fmt.Fprintf(&b, "Earlier answer: %s\n", t.Answer)
	}
	b.WriteString("\nReports:\n")
	for i, r := range reports {
		if i >= 5 {
			break
		}
		fmt.Fprintf(&b, "<report>\n%s\n%s\n</report>\n", r.Title, excerpt(r.Body, 1500))
	}
	text, err := s.BackgroundAsk(ctx, p, "feedback-answer", []string{resources.Prompt("feedback-answer")}, b.String(), 700)
	if err != nil {
		if err == ErrNoBackgroundAI {
			return FeedbackAnswerDraft{}, invalid("validation.no_background_ai", nil, "the AI is not available for this project")
		}
		return FeedbackAnswerDraft{}, err
	}
	s.event(ctx, a, "feedback.answer_draft", t.ID, nil)
	return FeedbackAnswerDraft{Text: excerpt(strings.TrimSpace(text), 4000)}, nil
}
