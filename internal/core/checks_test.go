package core

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/phoedox/phoedox/internal/mail"
	"github.com/phoedox/phoedox/internal/requests"
	"github.com/phoedox/phoedox/internal/store"
)

func TestChecks(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, _ := setup(t)
	s.RequestPolicy = requests.Policy{AllowPrivate: true}
	mails := &mail.Capture{}
	s.Mail, s.PublicURL = mails, "https://phx.test"
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Haus", AIWriteMode: "suggest"})
	_ = s.SetMember(ctx, owner, p.Slug, "reader@x.de", store.RoleReader)

	var down atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if down.Load() {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	if err := s.SetVariable(ctx, owner, p.Slug, "base", srv.URL, false); err != nil {
		t.Fatal(err)
	}
	md := "```request\nGET {{base}}/health\n```\n\n```request\nPOST {{base}}/reboot\n```\n"
	doc, _ := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "markdown", Title: "NAS", Markdown: md})
	nas, err := s.UpsertEntity(ctx, owner, p.ID, EntityInput{Kind: "server", Name: "NAS"})
	if err != nil {
		t.Fatal(err)
	}
	slug := doc.Document.Slug

	// rules: editors only, reading requests only, sane intervals, AI only when writing directly
	if _, err := s.CreateCheck(ctx, reader, p.Slug, CheckInput{Document: slug}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("reader: %v", err)
	}
	if _, err := s.CreateCheck(ctx, owner, p.Slug, CheckInput{Document: slug, Block: 1}); invalidKey(err) != "validation.check_method" {
		t.Fatalf("POST: %v", err)
	}
	if _, err := s.CreateCheck(ctx, owner, p.Slug, CheckInput{Document: slug, IntervalSec: 10}); invalidKey(err) != "validation.check_interval" {
		t.Fatalf("interval: %v", err)
	}
	if _, err := s.CreateCheck(ctx, Actor{User: owner.User, Via: "mcp"}, p.Slug, CheckInput{Document: slug}); invalidKey(err) != "validation.ai_checks_direct" {
		t.Fatalf("AI: %v", err)
	}
	c, err := s.CreateCheck(ctx, owner, p.Slug, CheckInput{Document: slug, Entity: nas.Entity.Slug, Name: "NAS erreichbar", IntervalSec: 60})
	if err != nil || c.LastStatus != store.CheckUnknown || c.EntitySlug != nas.Entity.Slug || c.DocumentTitle != "NAS" {
		t.Fatalf("create: %v %+v", err, c)
	}

	now := time.Now()
	s.RunChecks(store.System(ctx), now)
	list, _ := s.Checks(ctx, reader, p.Slug, "")
	if len(list) != 1 || list[0].LastStatus != store.CheckOK || list[0].LastDetail != "HTTP 200" {
		t.Fatalf("first run: %+v", list)
	}
	if len(mails.Sent) != 0 {
		t.Fatal("a first good run is not news")
	}
	// not due yet
	down.Store(true)
	s.RunChecks(store.System(ctx), now.Add(30*time.Second))
	if l, _ := s.Checks(ctx, owner, p.Slug, slug); l[0].FailCount != 0 {
		t.Fatalf("ran before it was due: %+v", l[0])
	}
	// one failure is tolerated, the second turns the check red
	s.RunChecks(store.System(ctx), now.Add(61*time.Second))
	if l, _ := s.Checks(ctx, owner, p.Slug, slug); l[0].LastStatus != store.CheckOK || l[0].FailCount != 1 {
		t.Fatalf("after one failure: %+v", l[0])
	}
	s.RunChecks(store.System(ctx), now.Add(122*time.Second))
	l, _ := s.Checks(ctx, owner, p.Slug, slug)
	if l[0].LastStatus != store.CheckFail || l[0].LastMessage != "check.msg.status" || l[0].LastDetail != "HTTP 502" {
		t.Fatalf("after two failures: %+v", l[0])
	}
	if m, ok := mails.Last(); !ok || !strings.Contains(m.Subject, "NAS erreichbar") || !strings.Contains(m.Body, "HTTP 502") {
		t.Fatalf("fail mail: %+v", m)
	}
	e, _ := s.DB.GetEntity(ctx, p.ID, nas.Entity.Slug)
	var attrs map[string]any
	_ = json.Unmarshal(e.Attrs, &attrs)
	if attrs["state"] != "offline" {
		t.Fatalf("entity state: %v", attrs)
	}
	if h := stationStatus(e, time.Now()); h.Status != "down" {
		t.Fatalf("diagnosis: %+v", h)
	}
	// recovery: at once, with a mail and the entry back online
	down.Store(false)
	v, err := s.RunCheckNow(ctx, owner, p.Slug, c.ID)
	if err != nil || v.LastStatus != store.CheckOK {
		t.Fatalf("run now: %v %+v", err, v)
	}
	if len(mails.Sent) != 2 {
		t.Fatalf("mails: %d", len(mails.Sent))
	}
	e, _ = s.DB.GetEntity(ctx, p.ID, nas.Entity.Slug)
	_ = json.Unmarshal(e.Attrs, &attrs)
	if attrs["state"] != "online" {
		t.Fatalf("entity state: %v", attrs)
	}
	evs, _ := s.DB.Events(ctx, store.EventFilter{ProjectID: p.ID})
	kinds := map[string]bool{}
	for _, ev := range evs {
		kinds[ev.Kind] = true
	}
	if !kinds["check.create"] || !kinds["check.fail"] || !kinds["check.recover"] {
		t.Fatalf("events: %v", kinds)
	}

	// a block edited into a writing request stops being run
	_, err = s.UpdateDocument(ctx, owner, p.Slug, slug, DocumentPatch{Markdown: strp("```request\nDELETE {{base}}/all\n```\n")})
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := s.RunCheckNow(ctx, owner, p.Slug, c.ID); v.LastMessage != "check.msg.method" {
		t.Fatalf("method changed: %+v", v)
	}

	// switch off, then delete
	off := false
	if v, err := s.UpdateCheck(ctx, owner, p.Slug, c.ID, CheckInput{IntervalSec: 120, Enabled: &off}); err != nil || v.Enabled || v.IntervalSec != 120 {
		t.Fatalf("update: %v %+v", err, v)
	}
	if all, _ := s.DB.EnabledChecks(store.System(ctx)); len(all) != 0 {
		t.Fatalf("disabled check still scheduled: %+v", all)
	}
	if err := s.DeleteCheck(ctx, reader, p.Slug, c.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("reader delete: %v", err)
	}
	if err := s.DeleteCheck(ctx, owner, p.Slug, c.ID); err != nil {
		t.Fatal(err)
	}
}
