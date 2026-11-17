package core

import (
	"context"
	"encoding/json"
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

func TestFeedbackForwarding(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, _ := setup(t)
	s.PublicURL = "https://docs.example"
	s.Box, _ = secret.New(make([]byte, 32))
	var mu sync.Mutex
	var bodies []map[string]any
	closed := false
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		p := strings.TrimPrefix(r.URL.Path, "/api/v1")
		var in map[string]any
		_ = json.NewDecoder(r.Body).Decode(&in)
		if r.Header.Get("Authorization") != "token tok-9" {
			w.WriteHeader(401)
			return
		}
		switch {
		case r.Method == "GET" && p == "/repos/acme/game":
			_, _ = w.Write([]byte(`{}`))
		case r.Method == "POST" && p == "/repos/acme/game/issues":
			bodies = append(bodies, in)
			w.WriteHeader(201)
			_, _ = w.Write([]byte(`{"number":5,"html_url":"https://forge/acme/game/issues/5"}`))
		case r.Method == "POST" && p == "/repos/acme/game/issues/5/comments":
			bodies = append(bodies, in)
			w.WriteHeader(201)
			_, _ = w.Write([]byte(`{}`))
		case r.Method == "GET" && p == "/repos/acme/game/issues/5":
			state := "open"
			if closed {
				state = "closed"
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"state": state})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	s.RequestPolicy = requests.Policy{AllowPrivate: true, RootCAs: srv.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs}

	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Game"})
	_ = s.SetMember(ctx, owner, p.Slug, reader.User.Email, store.RoleReader)
	if err := s.SetVariable(ctx, owner, p.Slug, "tracker_token", "tok-9", true); err != nil {
		t.Fatal(err)
	}
	settings := DefaultPortalSettings()
	settings.Anonymous = true
	if _, err := s.SaveFeedbackPortal(ctx, owner, p.Slug, "", PortalInput{Handle: "game", Name: "Game", Settings: &settings}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SubmitFeedback(ctx, nil, "game", ReportInput{Type: "bug", Title: "Crash in level 2", Body: "It crashes. password=hunter2", Email: "me@example.org", Context: map[string]string{"build": "0.9.1"}}, nil, Intake{Via: "widget"}); err != nil {
		t.Fatal(err)
	}
	topics, _ := s.FeedbackTopics(ctx, owner, p.Slug, TopicQuery{})
	topic := topics.Topics[0]

	in := TrackerInput{Kind: "gitea", URL: srv.URL + "/acme/game", TokenVar: "tracker_token", Labels: []string{"feedback"}}
	if _, err := s.SaveFeedbackTracker(ctx, reader, p.Slug, "", in); err == nil {
		t.Fatal("reader added a tracker")
	}
	if _, err := s.SaveFeedbackTracker(ctx, owner, p.Slug, "", TrackerInput{Kind: "trac", URL: in.URL, TokenVar: "tracker_token"}); invalidKey(err) != "validation.tracker_kind" {
		t.Fatalf("kind: %v", err)
	}
	if _, err := s.SaveFeedbackTracker(ctx, owner, p.Slug, "", TrackerInput{Kind: "gitea", URL: in.URL, TokenVar: "nope"}); invalidKey(err) != "validation.git_token_var" {
		t.Fatalf("secret: %v", err)
	}
	if _, err := s.SaveFeedbackTracker(ctx, owner, p.Slug, "", TrackerInput{Kind: "gitea", URL: srv.URL + "/acme/missing", TokenVar: "tracker_token"}); invalidKey(err) != "validation.tracker_not_found" {
		t.Fatalf("missing repo: %v", err)
	}
	tr, err := s.SaveFeedbackTracker(ctx, owner, p.Slug, "", in)
	if err != nil || tr.Name != "acme/game" {
		t.Fatalf("tracker: %+v %v", tr, err)
	}

	d, err := s.ForwardFeedbackTopic(ctx, owner, p.Slug, topic.ID, "acme/game")
	if err != nil || len(d.Forwards) != 1 || d.Forwards[0].Ref != "5" || d.Forwards[0].ReportsSent != 1 || len(d.Trackers) != 1 {
		t.Fatalf("forward: %v %+v", err, d.Forwards)
	}
	body := bodies[0]["body"].(string)
	if bodies[0]["title"] != "Crash in level 2" || !strings.Contains(body, "Build 0.9.1") || strings.Contains(body, "hunter2") || strings.Contains(body, "me@example.org") ||
		!strings.Contains(body, "https://docs.example/p/"+p.Slug+"/feedback?topic="+topic.ID) {
		t.Fatalf("issue: %+v", bodies[0])
	}
	if _, err := s.ForwardFeedbackTopic(ctx, owner, p.Slug, topic.ID, tr.ID); invalidKey(err) != "validation.tracker_forwarded" {
		t.Fatalf("twice: %v", err)
	}

	// A second report about the same thing: +1 on the issue.
	if _, err := s.SubmitFeedback(ctx, nil, "game", ReportInput{Type: "bug", Title: "Game closes in level two", Body: "Same here"}, nil, Intake{Via: "form"}); err != nil {
		t.Fatal(err)
	}
	topics, _ = s.FeedbackTopics(ctx, owner, p.Slug, TopicQuery{})
	for _, other := range topics.Topics {
		if other.ID != topic.ID {
			if _, err := s.MergeFeedbackTopics(ctx, owner, p.Slug, topic.ID, other.ID); err != nil {
				t.Fatal(err)
			}
		}
	}
	now := time.Now()
	s.RunFeedbackForwards(ctx, now)
	if len(bodies) != 2 || !strings.Contains(bodies[1]["body"].(string), "+1 new report") || !strings.Contains(bodies[1]["body"].(string), "Game closes in level two") {
		t.Fatalf("update: %+v", bodies)
	}
	d, _ = s.FeedbackTopic(ctx, owner, p.Slug, topic.ID)
	if d.Forwards[0].ReportsSent != 2 || d.Forwards[0].Closed || d.State == "done" {
		t.Fatalf("after sync: %+v %s", d.Forwards[0], d.State)
	}
	// Checked a moment ago: nothing new is sent.
	s.RunFeedbackForwards(ctx, now.Add(time.Minute))
	if len(bodies) != 2 {
		t.Fatal("synced again too early")
	}
	// The issue is closed: the topic is done.
	mu.Lock()
	closed = true
	mu.Unlock()
	s.RunFeedbackForwards(ctx, now.Add(31*time.Minute))
	d, _ = s.FeedbackTopic(ctx, owner, p.Slug, topic.ID)
	if !d.Forwards[0].Closed || d.State != "done" {
		t.Fatalf("closed issue: %+v %s", d.Forwards[0], d.State)
	}
	// Removing the tracker keeps the topic.
	if err := s.DeleteFeedbackTracker(ctx, owner, p.Slug, tr.ID); err != nil {
		t.Fatal(err)
	}
	if d, err := s.FeedbackTopic(ctx, owner, p.Slug, topic.ID); err != nil || len(d.Forwards) != 0 {
		t.Fatalf("after delete: %v %+v", err, d.Forwards)
	}
}
