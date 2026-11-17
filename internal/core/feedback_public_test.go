package core

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/phoedox/phoedox/internal/blob"
	"github.com/phoedox/phoedox/internal/mail"
	"github.com/phoedox/phoedox/internal/store"
)

func TestFeedbackPublicView(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, _ := setup(t)
	s.SyncNotify = true
	mc := &mail.Capture{}
	s.Mail, s.PublicURL = mc, "https://docs.example"
	dir := t.TempDir()
	s.Blobs, s.TempDir = blob.Local{Dir: filepath.Join(dir, "files")}, filepath.Join(dir, "tmp")
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Starfall"})
	settings := DefaultPortalSettings()
	settings.Anonymous = true
	if _, err := s.SaveFeedbackPortal(ctx, owner, p.Slug, "", PortalInput{Handle: "starfall", Name: "Starfall", Settings: &settings}); err != nil {
		t.Fatal(err)
	}
	rec, err := s.SubmitFeedback(ctx, nil, "starfall", ReportInput{Type: "idea", Title: "Dark mode", Body: "Please add a dark mode."}, nil, Intake{Via: "form"})
	if err != nil {
		t.Fatal(err)
	}
	topics, _ := s.FeedbackTopics(ctx, owner, p.Slug, TopicQuery{})
	topicID := topics.Topics[0].ID
	_ = rec

	// Off by default: nothing public.
	if _, err := s.PublicFeedbackTopics(ctx, nil, "starfall", "", "", "", ""); err != ErrNotFound {
		t.Fatalf("public view off: %v", err)
	}
	settings.PublicView, settings.PublicComments = true, true
	if _, err := s.SaveFeedbackPortal(ctx, owner, p.Slug, topics.Topics[0].PortalID, PortalInput{Handle: "starfall", Name: "Starfall", Settings: &settings}); err != nil {
		t.Fatal(err)
	}
	if l, _ := s.PublicFeedbackTopics(ctx, nil, "starfall", "", "", "", ""); len(l) != 0 {
		t.Fatal("topics are public only when released")
	}
	on := true
	if _, err := s.UpdateFeedbackTopic(ctx, owner, p.Slug, topicID, TopicPatch{Public: &on}); err != nil {
		t.Fatal(err)
	}
	key := "visitor-key-0123456789abcdef"
	l, err := s.PublicFeedbackTopics(ctx, nil, "starfall", key, "votes", "", "")
	if err != nil || len(l) != 1 || l[0].Title != "Dark mode" || l[0].Voted {
		t.Fatalf("released: %v %+v", err, l)
	}
	// Votes: anonymous with a key (toggle), and by account.
	if _, err := s.VoteFeedbackTopic(ctx, nil, "starfall", topicID, "short"); invalidKey(err) != "validation.feedback_vote_signin" {
		t.Fatalf("anonymous without key: %v", err)
	}
	if v, err := s.VoteFeedbackTopic(ctx, nil, "starfall", topicID, key); err != nil || !v.Voted || v.Votes != 1 {
		t.Fatalf("vote: %v %+v", err, v)
	}
	if v, _ := s.VoteFeedbackTopic(ctx, &reader, "starfall", topicID, ""); !v.Voted || v.Votes != 2 {
		t.Fatalf("vote by account: %+v", v)
	}
	if v, _ := s.VoteFeedbackTopic(ctx, nil, "starfall", topicID, key); v.Voted || v.Votes != 1 {
		t.Fatalf("take back: %+v", v)
	}
	// Following: by e-mail (confirmation mail with link to stop) and by account.
	if err := s.FollowFeedbackTopic(ctx, nil, "starfall", topicID, FollowInput{Email: "fan@example.org"}); err != nil {
		t.Fatal(err)
	}
	m, _ := mc.Last()
	if m.To != "fan@example.org" || !strings.Contains(m.Body, "/feedback/unfollow/phxf_") {
		t.Fatalf("follow mail: %+v", m)
	}
	if err := s.FollowFeedbackTopic(ctx, &reader, "starfall", topicID, FollowInput{}); err != nil {
		t.Fatal(err)
	}
	// Comments wait for a moderator.
	c, err := s.CommentFeedbackTopic(ctx, nil, "starfall", topicID, TopicCommentInput{Name: "Fan", Body: "Yes please! password=hunter2"})
	if err != nil || c.Status != "pending" {
		t.Fatalf("comment: %v %+v", err, c)
	}
	if d, _ := s.PublicFeedbackTopic(ctx, nil, "starfall", topicID, key); len(d.Comments) != 0 {
		t.Fatal("pending comments stay hidden")
	}
	det, err := s.DecideFeedbackComment(ctx, owner, p.Slug, topicID, c.ID, true)
	if err != nil || len(det.Comments) != 1 || det.Followers != 2 {
		t.Fatalf("moderated: %v %+v", err, det.Comments)
	}
	if _, err := s.TeamCommentFeedbackTopic(ctx, owner, p.Slug, topicID, TopicCommentInput{Body: "Planned for 2.0"}); err != nil {
		t.Fatal(err)
	}
	d, _ := s.PublicFeedbackTopic(ctx, nil, "starfall", topicID, key)
	if len(d.Comments) != 2 || strings.Contains(d.Comments[0].Body, "hunter2") || !d.Comments[1].Team {
		t.Fatalf("public comments: %+v", d.Comments)
	}
	// News reach the followers; the link stops it.
	before := len(mc.Sent)
	planned := "planned"
	if _, err := s.UpdateFeedbackTopic(ctx, owner, p.Slug, topicID, TopicPatch{State: &planned}); err != nil {
		t.Fatal(err)
	}
	var news mail.Message
	for _, x := range mc.Sent[before:] {
		if x.To == "fan@example.org" {
			news = x
		}
	}
	if !strings.Contains(news.Body, "planned") {
		t.Fatalf("follower mail: %+v", mc.Sent[before:])
	}
	i := strings.Index(news.Body, "/feedback/unfollow/")
	tok := news.Body[i+len("/feedback/unfollow/"):]
	tok = tok[:strings.Index(tok+"\n", "\n")]
	if err := s.UnfollowFeedbackTopic(ctx, nil, tok, "", ""); err != nil {
		t.Fatalf("unfollow: %v", err)
	}
	if err := s.UnfollowFeedbackTopic(ctx, nil, tok, "", ""); err != ErrNotFound {
		t.Fatal("link works once")
	}
	if notes, _ := s.Notifications(ctx, reader, false, 50); !hasKind(notes.Items, "feedback.update") {
		t.Fatalf("account follower notified: %+v", notes)
	}
}

func hasKind(notes []store.Notification, kind string) bool {
	for _, n := range notes {
		if n.Kind == kind {
			return true
		}
	}
	return false
}
