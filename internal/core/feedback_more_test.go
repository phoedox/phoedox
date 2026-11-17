package core

import (
	"context"
	"testing"
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
