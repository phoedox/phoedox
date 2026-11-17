package core

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/phoedox/phoedox/internal/store"
)

func TestAppShare(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, _ := setup(t)
	admin := Actor{User: store.User{ID: "sys", Name: "System", IsAdmin: true}, Via: "web"}
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Haus"})
	_ = s.SetMember(ctx, owner, p.Slug, reader.User.Email, store.RoleReader)
	board, _ := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "board", Title: "Chores"})
	_, _ = s.CreateTask(ctx, owner, p.Slug, board.Document.Slug, TaskInput{Title: "Bins out"})
	res, err := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: store.DocApp, Title: "Chores app", App: &App{
		Files: map[string]string{"index.html": "<p>x</p>"}, Level: 2, AI: true,
		Sources: []AppSource{{ID: "chores", Kind: "board", Ref: board.Document.Slug, Label: "Chores"}}}})
	if err != nil {
		t.Fatal(err)
	}
	doc := res.Document.Slug

	// Owners see a preview of the snapshot before publishing.
	v, err := s.GetAppShare(ctx, owner, p.Slug, doc, true)
	if err != nil || v.Shared || !v.CanEdit || !v.Allowed || v.Preview["chores"] == nil {
		t.Fatalf("preview: %v %+v", err, v)
	}
	// Readers may not publish; owners must confirm.
	if _, err := s.PublishApp(ctx, reader, p.Slug, doc, AppShareInput{Mode: "snapshot", Confirm: true}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("readers: %v", err)
	}
	if _, err := s.PublishApp(ctx, owner, p.Slug, doc, AppShareInput{Mode: "snapshot"}); invalidKey(err) != "validation.app_public_confirm" {
		t.Fatalf("confirm: %v", err)
	}
	v, err = s.PublishApp(ctx, owner, p.Slug, doc, AppShareInput{Mode: "snapshot", Schedule: "hourly", AILimit: 1, Confirm: true})
	if err != nil || !v.Shared || v.Share.Schedule != "hourly" || !strings.Contains(v.URL, "/shared/app/") {
		t.Fatalf("publish: %v %+v", err, v)
	}
	token := v.Share.Token

	// Visitors get level 1 from the snapshot.
	sa, err := s.OpenSharedApp(ctx, token)
	if err != nil || sa.Level != 1 || len(sa.Sources) != 1 || sa.Sources[0].Ref != "" || !sa.AI || sa.SnapshotAt == nil {
		t.Fatalf("open: %v %+v", err, sa)
	}
	data, err := s.ReadSharedApp(ctx, token, "chores")
	if err != nil || !strings.Contains(string(data), "Bins out") {
		t.Fatalf("read: %v %s", err, data)
	}
	if _, err := s.ReadSharedApp(ctx, token, "other"); invalidKey(err) != "validation.app_source_unknown" {
		t.Fatalf("unknown source: %v", err)
	}
	// Nothing is read live: a new card shows only after a refresh.
	_, _ = s.CreateTask(ctx, owner, p.Slug, board.Document.Slug, TaskInput{Title: "Water plants"})
	if data, _ := s.ReadSharedApp(ctx, token, "chores"); strings.Contains(string(data), "Water plants") {
		t.Fatal("snapshot must not change by itself")
	}
	s.RunAppShares(ctx, time.Now().Add(30*time.Minute))
	if data, _ := s.ReadSharedApp(ctx, token, "chores"); strings.Contains(string(data), "Water plants") {
		t.Fatal("hourly: not due yet")
	}
	s.RunAppShares(ctx, time.Now().Add(61*time.Minute))
	if data, _ := s.ReadSharedApp(ctx, token, "chores"); !strings.Contains(string(data), "Water plants") {
		t.Fatalf("scheduled refresh: %s", data)
	}
	// The frame serves the app only while it is public, on its own label.
	tok := strings.TrimSuffix(strings.TrimPrefix(sa.URL, "/app-frame/"), "/")
	if _, err := s.AppFrameFile(ctx, tok, "", "https://phx.test", ""); err != nil {
		t.Fatalf("frame: %v", err)
	}
	if _, err := s.AppFrameFile(ctx, tok, "", "", strings.ToLower(res.Document.ID)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("public frames must not share the app's origin: %v", err)
	}
	// The AI limit counts per month (no AI configured here: the call is
	// counted, then fails; the second is over the limit).
	_, _ = s.AskSharedApp(ctx, token, "hi", false, time.Now())
	if _, err := s.AskSharedApp(ctx, token, "hi", false, time.Now()); invalidKey(err) != "validation.app_ai_limit_reached" {
		t.Fatalf("limit: %v", err)
	}
	// Static mode: level 0, no reads.
	if _, err := s.PublishApp(ctx, owner, p.Slug, doc, AppShareInput{Mode: "static", Schedule: "daily", Confirm: true}); err != nil {
		t.Fatal(err)
	}
	if sa, _ := s.OpenSharedApp(ctx, token); sa.Level != 0 || len(sa.Sources) != 0 {
		t.Fatalf("static: %+v", sa)
	}
	if _, err := s.ReadSharedApp(ctx, token, "chores"); invalidKey(err) != "validation.app_not_allowed" {
		t.Fatalf("static read: %v", err)
	}
	// Admins can forbid public apps; the address stops working.
	if _, err := s.SetInstanceAppPolicy(ctx, admin, AppPolicy{NoPublic: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.OpenSharedApp(ctx, token); !errors.Is(err, ErrNotFound) {
		t.Fatalf("forbidden: %v", err)
	}
	if _, err := s.AppFrameFile(ctx, tok, "", "", ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("forbidden frame: %v", err)
	}
	if _, err := s.PublishApp(ctx, owner, p.Slug, doc, AppShareInput{Mode: "static", Confirm: true}); invalidKey(err) != "validation.app_public_off" {
		t.Fatalf("publish forbidden: %v", err)
	}
	_, _ = s.SetInstanceAppPolicy(ctx, admin, AppPolicy{})
	// Withdrawn: gone.
	if err := s.UnpublishApp(ctx, owner, p.Slug, doc); err != nil {
		t.Fatal(err)
	}
	if _, err := s.OpenSharedApp(ctx, token); !errors.Is(err, ErrNotFound) {
		t.Fatalf("withdrawn: %v", err)
	}
	if _, err := s.AppFrameFile(ctx, tok, "", "", ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("withdrawn frame: %v", err)
	}
}
