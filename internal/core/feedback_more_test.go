package core

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestFeedbackRoutingAndLinks(t *testing.T) {
	ctx := context.Background()
	s, owner, _, _ := setup(t)
	s.SyncNotify = true
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Starfall"})
	board, _ := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "board", Title: "Server team"})
	_, _ = s.UpsertEntity(ctx, owner, p.Slug, EntityInput{Kind: "server", Name: "Game server"})
	settings := DefaultPortalSettings()
	settings.Anonymous = true
	settings.Categories = []string{"UI", "Server"}
	settings.AITriage = true
	portal, err := s.SaveFeedbackPortal(ctx, owner, p.Slug, "", PortalInput{Handle: "starfall", Name: "Starfall", Settings: &settings})
	if err != nil {
		t.Fatal(err)
	}
	// Server topics go to the server team's board.
	if _, err := s.SaveRule(ctx, owner, p.Slug, "", RuleInput{Name: "Server bugs", Trigger: RuleTrigger{Type: TriggerFeedbackTopic, Category: "Server", Portal: portal.ID},
		Action: RuleAction{Type: ActionTask, Board: board.Document.Slug, Title: "{{topic}}", Text: "{{message}} – {{link}}"}}); err != nil {
		t.Fatal(err)
	}
	s.AI = &fakeAI{text: `{"summary":"The server drops players.","category":"Server","tags":[],"priority":1,"reason":"many blocked","duplicate_of":"","spam":false,"entities":["game-server","nope"]}`}
	rec, err := s.SubmitFeedback(ctx, nil, "starfall", ReportInput{Type: "bug", Title: "Disconnected every minute", Body: "The server kicks me out."}, nil, Intake{Via: "form"})
	if err != nil {
		t.Fatal(err)
	}
	topic, err := s.FeedbackTopic(ctx, owner, p.Slug, rec.TopicID)
	if err != nil || len(topic.Tickets) != 1 || topic.Tickets[0].BoardSlug != board.Document.Slug {
		t.Fatalf("routed: %v %+v", err, topic.Tickets)
	}
	if len(topic.Entities) != 1 || topic.Entities[0].Slug != "game-server" || topic.Entities[0].Source != "ai" {
		t.Fatalf("suggested entries: %+v", topic.Entities)
	}
	// Moderators were told it is urgent.
	notes, _ := s.Notifications(ctx, owner, false, 20)
	urgent := false
	for _, n := range notes.Items {
		urgent = urgent || n.Kind == "feedback.urgent"
	}
	if !urgent {
		t.Fatalf("urgent note: %+v", notes.Items)
	}
	// The entry page lists the topic for moderators.
	if l, err := s.EntityFeedbackTopics(ctx, owner, p.Slug, "game-server"); err != nil || len(l) != 1 || !l[0].Open {
		t.Fatalf("entry topics: %v %+v", err, l)
	}
	// A second report on the topic shows as "+1" on the card, until seen.
	s.AI = nil
	if _, err := s.SubmitFeedback(ctx, nil, "starfall", ReportInput{Type: "bug", Title: "Disconnected every minute again", Body: "Still."}, nil, Intake{Via: "form"}); err != nil {
		t.Fatal(err)
	}
	tasks, _ := s.Tasks(ctx, owner, p.Slug, board.Document.Slug, "", "", false)
	if len(tasks) != 1 || tasks[0].Feedback == nil || tasks[0].Feedback.New != 1 || tasks[0].Feedback.Reports != 2 {
		t.Fatalf("+n: %+v", tasks)
	}
	if err := s.SeenTaskFeedback(ctx, owner, p.Slug, tasks[0].ID); err != nil {
		t.Fatal(err)
	}
	tasks, _ = s.Tasks(ctx, owner, p.Slug, board.Document.Slug, "", "", false)
	if tasks[0].Feedback.New != 0 {
		t.Fatalf("seen: %+v", tasks[0].Feedback)
	}
	// Fixed in a version; the entries are edited by moderators.
	v, ents := "1.5.0", []string{}
	d, err := s.UpdateFeedbackTopic(ctx, owner, p.Slug, rec.TopicID, TopicPatch{FixedIn: &v, Entities: &ents})
	if err != nil || d.FixedIn != "1.5.0" || len(d.Entities) != 0 {
		t.Fatalf("fixed in: %v %+v", err, d.FeedbackTopic)
	}
	bad := []string{"unknown-thing"}
	if _, err := s.UpdateFeedbackTopic(ctx, owner, p.Slug, rec.TopicID, TopicPatch{Entities: &bad}); invalidKey(err) != "validation.feedback_entity" {
		t.Fatalf("unknown entry: %v", err)
	}
	st, _ := s.FeedbackStatus(ctx, rec.StatusToken)
	if st.FixedIn != "1.5.0" {
		t.Fatalf("status: %+v", st)
	}
	// The AI drafts an answer; nothing is saved.
	s.AI = &fakeAI{text: "Thanks! This is fixed in version 1.5.0."}
	dr, err := s.DraftFeedbackAnswer(ctx, owner, p.Slug, rec.TopicID, "")
	if err != nil || dr.Text == "" {
		t.Fatalf("draft: %v %+v", err, dr)
	}
	if again, _ := s.DB.FeedbackTopic(ctx, p.ID, rec.TopicID); again.Answer != "" {
		t.Fatalf("draft must not be saved: %q", again.Answer)
	}
}

func TestFeedbackPlayerQuestionsAutoAnswer(t *testing.T) {
	ctx := context.Background()
	s, owner, _, _ := setup(t)
	s.SyncNotify = true
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Starfall"})
	settings := DefaultPortalSettings()
	settings.Anonymous = true
	settings.AutoAnswers = true
	portal, err := s.SaveFeedbackPortal(ctx, owner, p.Slug, "", PortalInput{Handle: "starfall", Name: "Starfall", Settings: &settings})
	if err != nil {
		t.Fatal(err)
	}
	// Reporter tokens signed by the product's backend.
	v, err := s.RotateFeedbackKey(ctx, owner, p.Slug, portal.ID, "player")
	if err != nil || v.PlayerKey == "" || !v.HasPlayerKey {
		t.Fatalf("player key: %v %+v", err, v)
	}
	tok := PlayerToken(v.PlayerKey, "player-42", "Nova", "1.4.2", time.Now().Add(time.Hour))
	rec, err := s.SubmitFeedback(ctx, nil, "starfall", ReportInput{Type: "bug", Title: "Map crashes on open", Body: "x", PlayerToken: tok}, nil, Intake{Via: "form"})
	if err != nil {
		t.Fatal(err)
	}
	d, _ := s.FeedbackTopic(ctx, owner, p.Slug, rec.TopicID)
	if c := d.Reports[0].Context; c["player"] != "player-42" || c["build"] != "1.4.2" || c["player_verified"] != "yes" {
		t.Fatalf("player: %+v", c)
	}
	if _, err := s.SubmitFeedback(ctx, nil, "starfall", ReportInput{Type: "bug", Title: "x", PlayerToken: tok + "x"}, nil, Intake{Via: "form"}); invalidKey(err) != "validation.feedback_player_token" {
		t.Fatalf("bad token: %v", err)
	}
	expired := PlayerToken(v.PlayerKey, "p", "", "", time.Now().Add(-time.Minute))
	if _, err := s.SubmitFeedback(ctx, nil, "starfall", ReportInput{Type: "bug", Title: "x", PlayerToken: expired}, nil, Intake{Via: "form"}); invalidKey(err) != "validation.feedback_player_token" {
		t.Fatalf("expired token: %v", err)
	}
	// Moderators ask; the reporter answers on the status link.
	if _, err := s.ReplyFeedbackByToken(ctx, rec.StatusToken, "hello"); invalidKey(err) != "validation.feedback_no_question" {
		t.Fatalf("no question: %v", err)
	}
	q := "Which graphics card do you have?"
	if _, err := s.UpdateFeedbackTopic(ctx, owner, p.Slug, rec.TopicID, TopicPatch{Question: &q}); err != nil {
		t.Fatal(err)
	}
	st, err := s.ReplyFeedbackByToken(ctx, rec.StatusToken, "An RTX 9070, token=abcdef123456")
	if err != nil || st.Question != q || len(st.Replies) != 1 || strings.Contains(st.Replies[0].Body, "abcdef123456") {
		t.Fatalf("reply: %v %+v", err, st)
	}
	d, _ = s.FeedbackTopic(ctx, owner, p.Slug, rec.TopicID)
	if len(d.Reports[0].Replies) != 1 {
		t.Fatalf("moderators see replies: %+v", d.Reports[0])
	}
	// Answered and closed: a later report of the same issue gets the answer at once.
	ans, state, fixed := "Fixed, please update.", "done", "1.5.0"
	if _, err := s.UpdateFeedbackTopic(ctx, owner, p.Slug, rec.TopicID, TopicPatch{Answer: &ans, State: &state, FixedIn: &fixed}); err != nil {
		t.Fatal(err)
	}
	rec2, err := s.SubmitFeedback(ctx, nil, "starfall", ReportInput{Type: "bug", Title: "Map crashes when I open it", Body: "y"}, nil, Intake{Via: "form"})
	if err != nil {
		t.Fatal(err)
	}
	st2, _ := s.FeedbackStatus(ctx, rec2.StatusToken)
	if rec2.TopicID == rec.TopicID || st2.Answer != ans || !st2.AnswerAuto || st2.FixedIn != "1.5.0" {
		t.Fatalf("auto answer: %+v %+v", rec2, st2)
	}
}
