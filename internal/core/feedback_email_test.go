package core

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/phoedox/phoedox/internal/blob"
)

const testMail = "From: =?utf-8?q?J=C3=B6rg_Player?= <Joerg@Example.org>\r\n" +
	"To: feedback@game.example\r\n" +
	"Subject: Re: Crash after level 3\r\n" +
	"Content-Language: de-DE\r\n" +
	"MIME-Version: 1.0\r\n" +
	"Content-Type: multipart/mixed; boundary=\"outer\"\r\n\r\n" +
	"--outer\r\n" +
	"Content-Type: multipart/alternative; boundary=\"alt\"\r\n\r\n" +
	"--alt\r\n" +
	"Content-Type: text/plain; charset=utf-8\r\n" +
	"Content-Transfer-Encoding: quoted-printable\r\n\r\n" +
	"The game closes when the boss appears. Gr=C3=BC=C3=9Fe\r\n\r\n" +
	"On Mon, 16 Nov 2026 the team wrote:\r\n> earlier text\r\n" +
	"--alt\r\n" +
	"Content-Type: text/html; charset=utf-8\r\n\r\n" +
	"<p>The game closes when the boss appears.</p>\r\n" +
	"--alt--\r\n" +
	"--outer\r\n" +
	"Content-Type: text/plain; name=\"game.log\"\r\n" +
	"Content-Disposition: attachment; filename=\"game.log\"\r\n" +
	"Content-Transfer-Encoding: base64\r\n\r\n" +
	"ZXJyb3I6IG51bGwgcG9pbnRlcgo=\r\n" +
	"--outer--\r\n"

func TestFeedbackByMail(t *testing.T) {
	ctx := context.Background()
	s, owner, _, _ := setup(t)
	dir := t.TempDir()
	s.Blobs, s.TempDir = blob.Local{Dir: filepath.Join(dir, "files")}, filepath.Join(dir, "tmp")
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Game"})
	settings := DefaultPortalSettings()
	settings.Accounts = true
	settings.Fields = []PortalField{{ID: "platform", Label: map[string]string{"en": "Platform"}, Type: "text", Required: true}}
	v, err := s.SaveFeedbackPortal(ctx, owner, p.Slug, "", PortalInput{Handle: "game", Name: "Game", Settings: &settings})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SubmitFeedbackMail(ctx, "fem_nope", strings.NewReader(testMail)); err != ErrNotFound {
		t.Fatalf("unknown key: %v", err)
	}
	v, err = s.RotateFeedbackKey(ctx, owner, p.Slug, v.ID, "email")
	if err != nil || !v.EmailIn || !strings.Contains(v.EmailAddress, "/api/v1/feedback/inbound/fem_") {
		t.Fatalf("key: %v %+v", err, v)
	}
	key := v.EmailAddress[strings.LastIndex(v.EmailAddress, "/")+1:]

	rec, err := s.SubmitFeedbackMail(ctx, key, strings.NewReader(testMail))
	if err != nil || rec.ID == "" {
		t.Fatalf("mail: %v", err)
	}
	topics, _ := s.FeedbackTopics(ctx, owner, p.Slug, TopicQuery{})
	d, _ := s.FeedbackTopic(ctx, owner, p.Slug, topics.Topics[0].ID)
	r := d.Reports[0]
	if r.Title != "Crash after level 3" || r.Email != "joerg@example.org" || r.Via != "email" || !strings.Contains(r.Body, "boss appears. Grüße") ||
		strings.Contains(r.Body, "earlier text") || r.Context["sender_name"] != "Jörg Player" || len(r.Attachments) != 1 || r.Attachments[0].Name != "game.log" {
		t.Fatalf("report: %+v %+v", r, r.Attachments)
	}
	// Out-of-office answers never become reports.
	auto := "From: a@example.org\r\nSubject: Out of office\r\nAuto-Submitted: auto-replied\r\n\r\nAway."
	if _, err := s.SubmitFeedbackMail(ctx, key, strings.NewReader(auto)); err != ErrIgnoredMail {
		t.Fatalf("auto reply: %v", err)
	}
	if _, err := s.SubmitFeedbackMail(ctx, key, strings.NewReader("not a mail")); invalidKey(err) != "validation.feedback_mail" {
		t.Fatalf("garbage: %v", err)
	}
	// Switching it off kills the address.
	if _, err := s.RotateFeedbackKey(ctx, owner, p.Slug, v.ID, "email_off"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SubmitFeedbackMail(ctx, key, strings.NewReader(testMail)); err != ErrNotFound {
		t.Fatalf("after off: %v", err)
	}
}
