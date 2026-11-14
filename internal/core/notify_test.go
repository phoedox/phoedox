package core

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/phoedox/phoedox/internal/mail"
	"github.com/phoedox/phoedox/internal/requests"
	"github.com/phoedox/phoedox/internal/secret"
	"github.com/phoedox/phoedox/internal/store"
)

func TestNotifications(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, _ := setup(t)
	mails := &mail.Capture{}
	s.Mail, s.PublicURL, s.SyncNotify = mails, "https://phx.test", true
	s.RequestPolicy = requests.Policy{AllowPrivate: true}
	box, _ := secret.Load("", filepath.Join(t.TempDir(), "k"))
	s.Box = box
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Haus"})
	_ = s.SetMember(ctx, owner, p.Slug, "reader@x.de", store.RoleEditor)
	board, err := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: store.DocBoard, Title: "Aufgaben", Board: &Board{}})
	if err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var ntfy, pushed []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		defer mu.Unlock()
		if strings.HasPrefix(r.URL.Path, "/push") {
			pushed = append(pushed, r.Header.Get("Authorization"))
			w.WriteHeader(http.StatusCreated)
			return
		}
		ntfy = append(ntfy, r.Header.Get("Title")+"|"+string(b)+"|"+r.Header.Get("Click"))
	}))
	defer srv.Close()

	// instant mail by default, stored in the app
	if _, err := s.CreateTask(ctx, owner, p.Slug, board.Document.Slug, TaskInput{Title: "Router tauschen", Assignee: "reader@x.de", Description: "bis Freitag"}); err != nil {
		t.Fatal(err)
	}
	list, _ := s.Notifications(ctx, reader, false, 0)
	if list.Unread != 1 || list.Items[0].Kind != "task.assigned" || list.Items[0].Title != "Router tauschen" || !strings.Contains(list.Items[0].Link, board.Document.Slug) {
		t.Fatalf("in app: %+v", list)
	}
	if m, ok := mails.Last(); !ok || m.To != "reader@x.de" || !strings.Contains(m.Subject, "Router tauschen") || !strings.Contains(m.Body, "bis Freitag") {
		t.Fatalf("instant mail: %+v", m)
	}
	// own assignments are not news
	if _, err := s.CreateTask(ctx, reader, p.Slug, board.Document.Slug, TaskInput{Title: "selbst", Assignee: "reader@x.de"}); err != nil {
		t.Fatal(err)
	}
	if list, _ := s.Notifications(ctx, reader, true, 0); list.Unread != 1 {
		t.Fatalf("self assignment notified: %+v", list)
	}

	// validation
	if _, err := s.SaveNotificationSettings(ctx, reader, store.NotificationSettings{Email: "weekly"}); invalidKey(err) != "validation.notify_email" {
		t.Fatalf("mode: %v", err)
	}
	if _, err := s.SaveNotificationSettings(ctx, reader, store.NotificationSettings{Email: "daily", NtfyURL: "https://ntfy.sh/"}); invalidKey(err) != "validation.notify_ntfy" {
		t.Fatalf("ntfy without topic: %v", err)
	}
	// daily digest + ntfy + web push; comment replies muted
	v, err := s.SaveNotificationSettings(ctx, reader, store.NotificationSettings{Email: "daily", NtfyURL: srv.URL + "/haus", Muted: []string{"comment.reply", "bogus"}})
	if err != nil || v.Email != "daily" || len(v.Muted) != 1 || v.PushKey == "" {
		t.Fatalf("settings: %v %+v", err, v)
	}
	if err := s.DB.AddPushSubscription(ctx, store.PushSubscription{ID: newID(), UserID: reader.User.ID, Endpoint: srv.URL + "/push/1",
		P256dh: "BNcRdreALRFXTkOOUHK1EtK2wtaz5Ry4YfYCA_0QTpQtUbVlUls0VJXg7A8u-Ts1XbjhazAkj7I99e8QcYP7DkM", Auth: "tBHItJI5svbpez7KI4CCXg"}); err != nil {
		t.Fatal(err)
	}
	sent := len(mails.Sent)
	if _, err := s.CreateTask(ctx, owner, p.Slug, board.Document.Slug, TaskInput{Title: "Kabel prüfen", Assignee: reader.User.ID}); err != nil {
		t.Fatal(err)
	}
	if len(mails.Sent) != sent {
		t.Fatal("daily digest mode mailed at once")
	}
	mu.Lock()
	if len(ntfy) != 1 || !strings.Contains(ntfy[0], "Kabel prüfen") || !strings.Contains(ntfy[0], "https://phx.test/p/") || len(pushed) != 1 || !strings.HasPrefix(pushed[0], "vapid ") {
		t.Fatalf("ntfy %v push %v", ntfy, pushed)
	}
	mu.Unlock()
	// a muted kind stays in the app only
	doc, _ := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "markdown", Title: "Netz", Markdown: "x"})
	root, _ := s.AddComment(ctx, reader, p.Slug, doc.Document.Slug, CommentInput{Body: "Frage"})
	if _, err := s.AddComment(ctx, owner, p.Slug, doc.Document.Slug, CommentInput{Body: "Antwort", Parent: root.ID}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	if len(ntfy) != 1 {
		t.Fatalf("muted kind pushed: %v", ntfy)
	}
	mu.Unlock()
	list, _ = s.Notifications(ctx, reader, true, 0)
	if list.Unread != 3 || list.Items[0].Kind != "comment.reply" {
		t.Fatalf("after reply: %+v", list)
	}

	// the digest holds what was not mailed yet and not muted; once a day
	s.SendDigests(store.System(ctx), time.Date(2026, 11, 14, 5, 0, 0, 0, time.UTC))
	if len(mails.Sent) != sent {
		t.Fatal("digest before 6 o'clock")
	}
	morning := time.Date(2026, 11, 14, 7, 0, 0, 0, time.UTC)
	s.SendDigests(store.System(ctx), morning)
	m, _ := mails.Last()
	if len(mails.Sent) != sent+1 || m.To != "reader@x.de" || !strings.Contains(m.Body, "Kabel prüfen") || strings.Contains(m.Body, "Router tauschen") || strings.Contains(m.Body, "Antwort") {
		t.Fatalf("digest: %d %+v", len(mails.Sent)-sent, m)
	}
	_, _ = s.CreateTask(ctx, owner, p.Slug, board.Document.Slug, TaskInput{Title: "Später", Assignee: reader.User.ID})
	s.SendDigests(store.System(ctx), morning.Add(2*time.Hour))
	if len(mails.Sent) != sent+1 {
		t.Fatal("second digest the same day")
	}
	s.SendDigests(store.System(ctx), morning.Add(24*time.Hour))
	if m, _ := mails.Last(); len(mails.Sent) != sent+2 || !strings.Contains(m.Body, "Später") {
		t.Fatalf("next digest: %+v", m)
	}

	// read
	if err := s.MarkNotificationsRead(ctx, reader, []string{list.Items[0].ID}); err != nil {
		t.Fatal(err)
	}
	if l, _ := s.Notifications(ctx, reader, true, 0); l.Unread != 3 {
		t.Fatalf("mark one: %d", l.Unread)
	}
	_ = s.MarkNotificationsRead(ctx, reader, nil)
	if l, _ := s.Notifications(ctx, reader, true, 0); l.Unread != 0 || len(l.Items) != 0 {
		t.Fatalf("mark all: %+v", l)
	}
	if l, _ := s.Notifications(ctx, owner, false, 0); len(l.Items) != 0 {
		t.Fatalf("owner sees others' notifications: %+v", l)
	}
}
