package core

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/phoedox/phoedox/internal/requests"
	"github.com/phoedox/phoedox/internal/secret"
	"github.com/phoedox/phoedox/internal/store"
)

func TestWebhooks(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, _ := setup(t)
	s.Box, _ = secret.New(make([]byte, 32))
	s.RequestPolicy = requests.Policy{AllowPrivate: true}
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Haus", AIWriteMode: "direct"})
	_ = s.SetMember(ctx, owner, p.Slug, reader.User.Email, store.RoleReader)

	var mu sync.Mutex
	var got []*http.Request
	var bodies [][]byte
	fail := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		got, bodies = append(got, r), append(bodies, b)
		f := fail
		mu.Unlock()
		if f {
			w.WriteHeader(500)
		}
	}))
	defer srv.Close()

	if _, err := s.CreateWebhook(ctx, reader, p.Slug, WebhookInput{URL: srv.URL}); err == nil {
		t.Fatal("readers must not add webhooks")
	}
	if _, err := s.CreateWebhook(ctx, owner, p.Slug, WebhookInput{URL: "ftp://x"}); invalidKey(err) != "validation.webhook_url" {
		t.Fatalf("url: %v", err)
	}
	if _, err := s.CreateWebhook(ctx, owner, p.Slug, WebhookInput{URL: srv.URL, Events: []string{"nope"}}); invalidKey(err) != "validation.webhook_event" {
		t.Fatalf("event: %v", err)
	}
	h, err := s.CreateWebhook(ctx, owner, p.Slug, WebhookInput{URL: srv.URL, Events: []string{"document.created", "document.deleted"}})
	if err != nil || !strings.HasPrefix(h.Secret, "whsec_") {
		t.Fatalf("create: %v %+v", err, h)
	}
	doc, _ := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "markdown", Title: "Netz", Markdown: "x"})
	_, _ = s.UpdateDocument(ctx, owner, p.Slug, doc.Document.Slug, DocumentPatch{Markdown: new("y")}) // not subscribed
	if len(got) != 1 {
		t.Fatalf("deliveries: %d", len(got))
	}
	r := got[0]
	var e WebhookEvent
	_ = json.Unmarshal(bodies[0], &e)
	if r.Header.Get("Phoedox-Event") != "document.created" || e.Version != 1 || e.Project.Slug != "haus" || e.Subject != doc.Document.ID || e.Actor == "" {
		t.Fatalf("event: %v %+v", r.Header, e)
	}
	// the signature verifies with the secret and the timestamp in the header
	sig := r.Header.Get("Phoedox-Signature")
	var unix int64
	for _, part := range strings.Split(sig, ",") {
		if v, ok := strings.CutPrefix(part, "t="); ok {
			unix = mustInt(t, v)
		}
	}
	if Sign(h.Secret, time.Unix(unix, 0), bodies[0]) != sig {
		t.Fatalf("signature does not verify: %s", sig)
	}

	// failures are logged and retried by the queue
	fail = true
	_, _ = s.DeleteDocument(ctx, owner, p.Slug, doc.Document.Slug)
	ds, _ := s.WebhookDeliveries(ctx, owner, p.Slug, h.ID)
	if len(ds) != 2 || ds[0].Status != "failed" || ds[0].Response != 500 || ds[1].Status != "ok" {
		t.Fatalf("log: %+v", ds)
	}
	if err := s.deliver(ctx, ds[0].ID); err == nil {
		t.Fatal("failed delivery must return an error for the retry")
	}
	fail = false
	if err := s.deliver(ctx, ds[0].ID); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if x, _ := s.TestWebhook(ctx, owner, p.Slug, h.ID); x.Status != "ok" || x.EventType != "ping" {
		t.Fatalf("test: %+v", x)
	}
	// switching off stops deliveries
	off := false
	if _, err := s.UpdateWebhook(ctx, owner, p.Slug, h.ID, WebhookInput{Active: &off}); err != nil {
		t.Fatal(err)
	}
	n := len(got)
	_, _ = s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "markdown", Title: "Zwei", Markdown: "x"})
	if len(got) != n {
		t.Fatal("inactive hook got a delivery")
	}
	// private networks are blocked unless allowed
	s.RequestPolicy = requests.Policy{}
	on := true
	_, _ = s.UpdateWebhook(ctx, owner, p.Slug, h.ID, WebhookInput{Active: &on})
	if x, _ := s.TestWebhook(ctx, owner, p.Slug, h.ID); x.Status != "failed" || !strings.Contains(x.Error, "not allowed") {
		t.Fatalf("SSRF: %+v", x)
	}
}

func mustInt(t *testing.T, s string) int64 {
	t.Helper()
	var n int64
	for _, c := range s {
		n = n*10 + int64(c-'0')
	}
	return n
}
