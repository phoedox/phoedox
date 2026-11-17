package core

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
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

// --- reporter tokens signed by the product ----------------------------------

func (s *Service) sealPlayerKey(key string) (string, error) {
	if s.Box == nil {
		return "plain:" + key, nil // tests
	}
	return s.Box.Seal([]byte(key), "feedback-player")
}

func (s *Service) openPlayerKey(sealed string) (string, error) {
	if k, ok := strings.CutPrefix(sealed, "plain:"); ok && s.Box == nil {
		return k, nil
	}
	if s.Box == nil {
		return "", ErrNotFound
	}
	raw, err := s.Box.Open(sealed, "feedback-player")
	return string(raw), err
}

// playerClaims is what a product's backend signs about a reporter.
type playerClaims struct {
	Sub   string `json:"sub"`             // the player's id in the product
	Name  string `json:"name,omitempty"`  // display name
	Build string `json:"build,omitempty"` // version or build of the product
	Exp   int64  `json:"exp"`             // unix seconds
}

// PlayerToken makes a reporter token (for tests and the documentation):
// base64url(JSON claims) "." base64url(HMAC-SHA256(key, first part)).
func PlayerToken(key, sub, name, build string, exp time.Time) string {
	raw, _ := json.Marshal(playerClaims{Sub: sub, Name: name, Build: build, Exp: exp.Unix()})
	body := base64.RawURLEncoding.EncodeToString(raw)
	m := hmac.New(sha256.New, []byte(key))
	m.Write([]byte(body))
	return body + "." + base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

func (s *Service) verifyPlayerToken(portal store.FeedbackPortal, tok string) (playerClaims, error) {
	var c playerClaims
	bad := func() (playerClaims, error) {
		return c, invalid("validation.feedback_player_token", nil, "the reporter token is not valid")
	}
	if portal.PlayerKeySealed == "" {
		return bad()
	}
	key, err := s.openPlayerKey(portal.PlayerKeySealed)
	if err != nil {
		return bad()
	}
	body, sig, ok := strings.Cut(strings.TrimSpace(tok), ".")
	if !ok || len(tok) > 4096 {
		return bad()
	}
	want := hmac.New(sha256.New, []byte(key))
	want.Write([]byte(body))
	got, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil || !hmac.Equal(got, want.Sum(nil)) {
		return bad()
	}
	raw, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil || json.Unmarshal(raw, &c) != nil || c.Sub == "" || c.Exp == 0 || time.Now().Unix() > c.Exp || c.Exp-time.Now().Unix() > 7*24*3600 {
		return bad()
	}
	return c, nil
}

// --- questions to reporters and their answers -------------------------------

// ReplyFeedbackByToken stores a reporter's answer to the moderators'
// question (status link) and tells the moderators.
func (s *Service) ReplyFeedbackByToken(ctx context.Context, token, body string) (ReportStatus, error) {
	r, portal, err := s.reportByToken(ctx, token)
	if err != nil {
		return ReportStatus{}, err
	}
	body = strings.TrimSpace(body)
	if body == "" || len(body) > 4000 {
		return ReportStatus{}, invalid("validation.feedback_reply", nil, "the answer is empty or longer than 4,000 characters")
	}
	if r.TopicID == "" || r.Deleted {
		return ReportStatus{}, ErrNotFound
	}
	t, err := s.DB.FeedbackTopic(ctx, "", r.TopicID)
	if err != nil || t.Question == "" || t.ClosedAt != nil {
		return ReportStatus{}, invalid("validation.feedback_no_question", nil, "there is no open question")
	}
	reps, _ := s.DB.FeedbackReplies(ctx, t.ID)
	if len(reps[r.ID]) >= 20 {
		return ReportStatus{}, invalid("validation.feedback_reply", nil, "too many answers")
	}
	if err := s.DB.AddFeedbackReply(ctx, t.ProjectID, store.FeedbackReply{ID: newID(), TopicID: t.ID, ReportID: r.ID, Body: scrubSecrets(body), CreatedAt: store.Now()}); err != nil {
		return ReportStatus{}, err
	}
	if p, err := s.DB.ProjectFor(ctx, t.ProjectID, ""); err == nil {
		s.notifyModerators(ctx, portal, Note{Kind: "feedback.reply", ProjectID: p.ID, Project: p.Name, Subject: t.ID, Title: t.Title,
			Body: excerpt(body, 300), Link: "/p/" + p.Slug + "/feedback?topic=" + t.ID})
	}
	s.event(ctx, Actor{User: store.User{ID: "anonymous:" + portal.Handle}, Via: "status"}, "feedback.reply", t.ID, nil)
	return s.FeedbackStatus(ctx, token)
}

// --- automatic answers ------------------------------------------------------

// autoAnswer answers a new topic at once when it matches a topic the team
// closed in the last 90 days with an answer or a fixed version (rule-based,
// by title), and tells the reporter. The answer is marked as automatic.
func (s *Service) autoAnswer(ctx context.Context, p store.Project, portal store.FeedbackPortal, ps PortalSettings, t store.FeedbackTopic) store.FeedbackTopic {
	closed, err := s.DB.RecentlyClosedTopics(ctx, portal.ID, store.Time{Time: time.Now().Add(-90 * 24 * time.Hour)})
	if err != nil {
		return t
	}
	best, score := -1, 0.0
	for i, c := range closed {
		if c.Type != t.Type || (c.Answer == "" && c.FixedIn == "") {
			continue
		}
		if sc := similarity(c.Title, t.Title); sc > score {
			best, score = i, sc
		}
	}
	if best < 0 || score < 0.5 {
		return t
	}
	return s.applyAutoAnswer(ctx, p, portal, t, closed[best])
}

func (s *Service) applyAutoAnswer(ctx context.Context, p store.Project, portal store.FeedbackPortal, t, from store.FeedbackTopic) store.FeedbackTopic {
	if t.Answer != "" {
		return t
	}
	t.Answer, t.FixedIn, t.AnswerAuto = from.Answer, from.FixedIn, true
	if t.DuplicateOf == "" {
		t.DuplicateOf = from.ID
	}
	t.UpdatedAt = store.Now()
	if s.DB.SaveFeedbackTopic(ctx, t) != nil {
		return t
	}
	s.event(ctx, Actor{Via: "system"}, "feedback.auto_answer", t.ID, map[string]string{"from": from.ID})
	s.tellReporters(ctx, p, portal, t)
	return t
}
