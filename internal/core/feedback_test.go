package core

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"path/filepath"
	"strings"
	"testing"

	"github.com/phoedox/phoedox/internal/blob"
	"github.com/phoedox/phoedox/internal/store"
)

func feedbackPNG(t *testing.T) []byte {
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	img.Set(1, 1, color.RGBA{255, 0, 0, 255})
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestFeedbackFlow(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, stranger := setup(t)
	s.SyncNotify = true
	dir := t.TempDir()
	s.Blobs, s.TempDir = blob.Local{Dir: filepath.Join(dir, "files")}, filepath.Join(dir, "tmp")
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Starfall"})
	_ = s.SetMember(ctx, owner, p.Slug, reader.User.Email, store.RoleReader)
	board, _ := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "board", Title: "Frontend"})

	settings := DefaultPortalSettings()
	settings.Anonymous = true
	settings.Categories = []string{"UI", "Server"}
	settings.Fields = []PortalField{{ID: "version", Label: map[string]string{"en": "Game version"}, Type: "version", Required: true, ReportTypes: []string{"bug"}},
		{ID: "platform", Label: map[string]string{"en": "Platform"}, Type: "choice", Options: []string{"PC", "Switch"}}}
	if _, err := s.SaveFeedbackPortal(ctx, reader, p.Slug, "", PortalInput{Handle: "starfall", Name: "Starfall", Settings: &settings}); err == nil {
		t.Fatal("only owners create portals")
	}
	if _, err := s.SaveFeedbackPortal(ctx, owner, p.Slug, "", PortalInput{Handle: "status", Name: "x", Settings: &settings}); invalidKey(err) != "validation.feedback_handle" {
		t.Fatalf("reserved handle: %v", err)
	}
	portal, err := s.SaveFeedbackPortal(ctx, owner, p.Slug, "", PortalInput{Handle: "Starfall", Name: "Starfall", Settings: &settings})
	if err != nil || portal.Handle != "starfall" {
		t.Fatalf("portal: %v %+v", err, portal)
	}
	pub, err := s.PublicFeedbackPortal(ctx, "starfall", "en")
	if err != nil || !pub.Accepting || len(pub.Fields) != 2 || pub.Privacy == "" {
		t.Fatalf("public: %v %+v", err, pub)
	}
	// Required fields and the choice list are checked.
	in := ReportInput{Type: "bug", Title: "Crash when opening the map", Body: "It crashes. token=abc123secret", Fields: map[string]any{"platform": "PC"}}
	if _, err := s.SubmitFeedback(ctx, nil, "starfall", in, nil, Intake{Via: "form"}); invalidKey(err) != "validation.feedback_field_required" {
		t.Fatalf("required field: %v", err)
	}
	in.Fields["version"] = "1.4.2"
	in.Fields["platform"] = "Xbox"
	if _, err := s.SubmitFeedback(ctx, nil, "starfall", in, nil, Intake{Via: "form"}); invalidKey(err) != "validation.feedback_field_value" {
		t.Fatalf("choice: %v", err)
	}
	in.Fields["platform"] = "PC"
	log := "start\nAuthorization: Bearer abcdefghijklmnopqrstuvwxyz\nok\n"
	files := []ReportFile{{Name: "shot.png", R: bytes.NewReader(feedbackPNG(t))}, {Name: "game.log", R: strings.NewReader(log)}}
	anon, err := s.SubmitFeedback(ctx, nil, "starfall", in, files, Intake{Via: "form", Check: "normal"})
	if err != nil || anon.StatusToken == "" {
		t.Fatalf("anonymous report: %v %+v", err, anon)
	}
	// Signed in, same bug in other words: joins the topic.
	in2 := ReportInput{Type: "bug", Title: "crash opening the map", Body: "Same here", Fields: map[string]any{"version": "1.4.2"}}
	mine, err := s.SubmitFeedback(ctx, &reader, "starfall", in2, nil, Intake{Via: "form"})
	if err != nil || mine.TopicID != anon.TopicID {
		t.Fatalf("duplicate: %v %+v %+v", err, mine, anon)
	}
	// Files the portal does not accept are refused by content and ending.
	bad := []ReportFile{{Name: "tool.exe", R: strings.NewReader("MZ\x90\x00binary")}}
	if _, err := s.SubmitFeedback(ctx, nil, "starfall", ReportInput{Type: "idea", Title: "Idea"}, bad, Intake{Via: "form"}); invalidKey(err) != "validation.feedback_file_type" {
		t.Fatalf("exe: %v", err)
	}
	fake := []ReportFile{{Name: "shot.png", R: strings.NewReader("this is text, not a picture")}}
	if _, err := s.SubmitFeedback(ctx, nil, "starfall", ReportInput{Type: "idea", Title: "Idea"}, fake, Intake{Via: "form"}); invalidKey(err) != "validation.feedback_file_type" {
		t.Fatalf("fake png: %v", err)
	}
	// Moderators see the topic with both reports; secrets are gone.
	list, err := s.FeedbackTopics(ctx, owner, p.Slug, TopicQuery{})
	if err != nil || len(list.Topics) != 1 || list.Topics[0].ReportCount != 2 || list.Topics[0].Priority != 2 {
		t.Fatalf("topics: %v %+v", err, list.Topics)
	}
	if _, err := s.FeedbackTopics(ctx, stranger, p.Slug, TopicQuery{}); err == nil {
		t.Fatal("strangers must not moderate")
	}
	topic, err := s.FeedbackTopic(ctx, owner, p.Slug, anon.TopicID)
	if err != nil || len(topic.Reports) != 2 || len(topic.Reports[0].Attachments) != 2 {
		t.Fatalf("topic detail: %v %+v", err, topic)
	}
	if strings.Contains(topic.Reports[0].Body, "abc123secret") {
		t.Fatalf("secret not removed: %q", topic.Reports[0].Body)
	}
	for _, f := range topic.Reports[0].Attachments {
		if f.Name == "game.log" {
			_, rc, err := s.FeedbackAttachment(ctx, owner, p.Slug, f.ID)
			if err != nil {
				t.Fatal(err)
			}
			var b bytes.Buffer
			_, _ = b.ReadFrom(rc)
			rc.Close()
			if strings.Contains(b.String(), "abcdefghijklmnop") {
				t.Fatalf("secret in log: %q", b.String())
			}
		}
	}
	if used, _ := s.DB.FeedbackUsage(ctx, p.ID); used == 0 {
		t.Fatal("feedback storage not counted")
	}
	// External moderator: sees only this portal's topics.
	if _, err := s.SetFeedbackModerators(ctx, owner, p.Slug, portal.ID, []string{"stranger@x.de"}); err != nil {
		t.Fatal(err)
	}
	if l, err := s.FeedbackTopics(ctx, stranger, p.Slug, TopicQuery{}); err != nil || len(l.Topics) != 1 {
		t.Fatalf("external moderator: %v %+v", err, l)
	}
	// Category and answer; the signed-in reporter is notified.
	cat, answer := "UI", "Thanks, we are on it."
	if _, err := s.UpdateFeedbackTopic(ctx, owner, p.Slug, anon.TopicID, TopicPatch{Category: &cat, Answer: &answer}); err != nil {
		t.Fatal(err)
	}
	wrong := "Nope"
	if _, err := s.UpdateFeedbackTopic(ctx, owner, p.Slug, anon.TopicID, TopicPatch{Category: &wrong}); invalidKey(err) != "validation.feedback_category" {
		t.Fatalf("unknown category: %v", err)
	}
	notes, _ := s.Notifications(ctx, reader, false, 10)
	if len(notes.Items) == 0 || notes.Items[0].Kind != "feedback.update" {
		t.Fatalf("reporter told: %+v", notes.Items)
	}
	// Two tickets; when both are done, the topic is done.
	d, err := s.FeedbackToBoard(ctx, owner, p.Slug, anon.TopicID, TicketInput{Board: board.Document.Slug})
	if err != nil || len(d.Tickets) != 1 {
		t.Fatalf("ticket: %v %+v", err, d.Tickets)
	}
	d, _ = s.FeedbackToBoard(ctx, owner, p.Slug, anon.TopicID, TicketInput{Board: board.Document.Slug, Title: "Server side"})
	if len(d.Tickets) != 2 {
		t.Fatalf("second ticket: %+v", d.Tickets)
	}
	done := boardOf(*board.Document).Columns[2].ID
	for _, k := range d.Tickets {
		if _, err := s.UpdateTask(ctx, owner, p.Slug, k.TaskID, TaskPatch{Column: &done}); err != nil {
			t.Fatal(err)
		}
	}
	st, err := s.FeedbackStatus(ctx, anon.StatusToken)
	if err != nil || !st.Finished || st.Answer != answer || st.Reports != 2 || st.Files != 2 {
		t.Fatalf("status: %v %+v", err, st)
	}
	// Split: a report moves into a new topic; merge brings it back.
	var mineID string
	for _, r := range d.Reports {
		if r.UserID == reader.User.ID {
			mineID = r.ID
		}
	}
	split, err := s.MoveFeedbackReport(ctx, owner, p.Slug, mineID, "")
	if err != nil || split.ID == anon.TopicID || split.ReportCount != 1 {
		t.Fatalf("split: %v %+v", err, split)
	}
	merged, err := s.MergeFeedbackTopics(ctx, owner, p.Slug, anon.TopicID, split.ID)
	if err != nil || merged.ReportCount != 2 {
		t.Fatalf("merge: %v %+v", err, merged)
	}
	// The reporter deletes their report: files and e-mail go, the text stays.
	if err := s.DeleteFeedbackByToken(ctx, anon.StatusToken); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FeedbackStatus(ctx, anon.StatusToken); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("status link ends: %v", err)
	}
	if used, _ := s.DB.FeedbackUsage(ctx, p.ID); used != 0 {
		t.Fatalf("files removed: %d", used)
	}
	if my, err := s.MyFeedback(ctx, reader); err != nil || len(my) != 1 || !my[0].Finished {
		t.Fatalf("my feedback: %v %+v", err, my)
	}
	// Storage full: "send without files" refuses attachments.
	zero := 0
	_ = s.DB.SetFeedbackQuota(ctx, p.ID, &zero)
	one := 1
	_ = s.DB.SetFeedbackQuota(ctx, p.ID, &one)
	big := []ReportFile{{Name: "big.log", R: strings.NewReader(strings.Repeat("x", 1<<20+10))}}
	if _, err := s.SubmitFeedback(ctx, nil, "starfall", ReportInput{Type: "idea", Title: "Big"}, big, Intake{Via: "form"}); invalidKey(err) != "validation.feedback_storage_full" {
		t.Fatalf("storage full: %v", err)
	}
	// Accounts only: anonymous reports are refused, keys still work.
	settings.Anonymous, settings.ServerKeyOn = false, true
	pv, err := s.SaveFeedbackPortal(ctx, owner, p.Slug, portal.ID, PortalInput{Settings: &settings})
	if err != nil || pv.ServerKey == "" {
		t.Fatalf("server key: %v %+v", err, pv)
	}
	if _, err := s.SubmitFeedback(ctx, nil, "starfall", ReportInput{Type: "idea", Title: "Anon"}, nil, Intake{Via: "form"}); invalidKey(err) != "validation.feedback_signin" {
		t.Fatalf("accounts only: %v", err)
	}
	if _, via, err := s.PortalByKey(ctx, "starfall", pv.ServerKey); err != nil || via != "server_key" {
		t.Fatalf("server key check: %v %s", err, via)
	}
	if _, _, err := s.PortalByKey(ctx, "starfall", "sfk_wrong"); err == nil {
		t.Fatal("wrong key")
	}
	if _, err := s.SubmitFeedback(ctx, nil, "starfall", ReportInput{Type: "idea", Title: "From the game server"}, nil, Intake{Via: "server_key"}); err != nil {
		t.Fatal(err)
	}
}
