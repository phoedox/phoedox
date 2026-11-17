package core

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
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

func TestParsePullRequest(t *testing.T) {
	for in, want := range map[string]DraftCodeInput{
		"12":                                    {PR: 12},
		"#7":                                    {PR: 7},
		"acme/app#3":                            {Repo: "acme/app", PR: 3},
		"acme/app@feature/docs":                 {Repo: "acme/app", Branch: "feature/docs"},
		"@main":                                 {Branch: "main"},
		"https://github.com/acme/app/pull/42":   {Repo: "acme/app", PR: 42},
		"https://git.example.com/t/svc/pulls/5": {Repo: "t/svc", PR: 5},
	} {
		got, ok := ParsePullRequest(in)
		if !ok || got != want {
			t.Errorf("%s: %+v %v", in, got, ok)
		}
	}
	for _, bad := range []string{"", "abc", "acme/app", "#0", "https://github.com/acme/app"} {
		if _, ok := ParsePullRequest(bad); ok {
			t.Errorf("accepted %q", bad)
		}
	}
	if a, r, d := lineDiff("a\nb\nc\n", "a\nB\nc\nd\n", 100); a != 2 || r != 1 || !strings.Contains(d, "-b\n+B\n") {
		t.Fatalf("diff: %d %d %q", a, r, d)
	}
}

func TestPullRequestDrafts(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, _ := setup(t)
	s.Box, _ = secret.New(make([]byte, 32))
	s.PublicURL = "https://docs.example"
	var mu sync.Mutex
	comments := map[string]string{} // method path → body
	prState := map[string]any{"state": "open", "merged": false}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.URL.Path == "/repos/acme/app/pulls/12":
			_ = json.NewEncoder(w).Encode(prState)
		case r.URL.Path == "/repos/acme/app/issues/12/comments" && r.Method == http.MethodGet:
			out := []any{}
			if b := comments["POST /repos/acme/app/issues/12/comments"]; b != "" {
				out = append(out, map[string]any{"id": 99, "body": b})
			}
			_ = json.NewEncoder(w).Encode(out)
		case r.Method == http.MethodPost || r.Method == http.MethodPatch:
			b, _ := io.ReadAll(r.Body)
			var x struct {
				Body string `json:"body"`
			}
			_ = json.Unmarshal(b, &x)
			comments[r.Method+" "+r.URL.Path] = x.Body
			w.WriteHeader(201)
			_, _ = w.Write([]byte("{}"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	s.RequestPolicy = requests.Policy{AllowPrivate: true, RootCAs: srv.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs}

	p, err := s.CreateProject(ctx, owner, ProjectInput{Name: "Code docs"})
	if err != nil {
		t.Fatal(err)
	}
	_ = s.SetMember(ctx, owner, p.Slug, reader.User.Email, store.RoleReader)
	if err := s.SetVariable(ctx, owner, p.Slug, "forge_token", "tok", true); err != nil {
		t.Fatal(err)
	}
	repo := store.CodeRepo{ID: newID(), ProjectID: p.ID, Provider: "github", WebURL: "https://github.com/acme/app", APIURL: srv.URL, Owner: "acme", Name: "app", TokenVar: "forge_token", CreatedBy: owner.User.ID}
	if err := s.DB.AddCodeRepo(ctx, repo); err != nil {
		t.Fatal(err)
	}
	page, err := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: store.DocMarkdown, Title: "Deploy", Markdown: "Run make deploy.\n"})
	if err != nil {
		t.Fatal(err)
	}
	adr, err := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: store.DocADR, Title: "Use Postgres", ADR: &ADR{Status: "proposed", Context: "We need a DB.", Decision: "Maybe Postgres."}})
	if err != nil {
		t.Fatal(err)
	}
	ai := Actor{User: owner.User, Via: "mcp", TokenID: "t1"}

	// The AI ties its doc changes to pull request 12.
	if _, err := s.LinkDraftCode(ctx, reader, p.Slug, page.Document.Slug, DraftCodeInput{PR: 12}); err == nil {
		t.Fatal("readers cannot open drafts")
	}
	if _, err := s.LinkDraftCode(ctx, ai, p.Slug, page.Document.Slug, DraftCodeInput{PR: 12}); err != nil {
		t.Fatal(err)
	}
	md := "Run make deploy-v2.\n"
	if _, err := s.UpdateDocument(ctx, owner, p.Slug, page.Document.Slug, DocumentPatch{Markdown: &md}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LinkDraftCode(ctx, ai, p.Slug, adr.Document.Slug, DraftCodeInput{Repo: "acme/app", PR: 12}); err != nil {
		t.Fatal(err)
	}
	next := ADR{Status: "accepted", Context: "We need a DB.", Decision: "Postgres 16."}
	if _, err := s.UpdateDocument(ctx, owner, p.Slug, adr.Document.Slug, DocumentPatch{ADR: &next}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LinkDraftCode(ctx, ai, p.Slug, page.Document.Slug, DraftCodeInput{PR: 13}); invalidKey(err) != "validation.draft_other_pr" {
		t.Fatalf("other pr: %v", err)
	}
	// Published stays as it was; the PR shows both changes.
	if d, _ := s.Document(ctx, owner, p.Slug, page.Document.Slug); markdownOf(d) != "Run make deploy.\n" {
		t.Fatalf("published early: %q", markdownOf(d))
	}
	ch, err := s.PullRequestDocs(ctx, owner, p.Slug, DraftCodeInput{PR: 12})
	if err != nil || len(ch.Changes) != 2 || ch.URL != "https://github.com/acme/app/pull/12" {
		t.Fatalf("changes: %v %+v", err, ch)
	}
	for _, c := range ch.Changes {
		if c.Added == 0 || !strings.Contains(c.Diff, "+") {
			t.Fatalf("diff: %+v", c)
		}
	}
	mu.Lock()
	body := comments["POST /repos/acme/app/issues/12/comments"]
	mu.Unlock()
	if !strings.Contains(body, "Docs changes") || !strings.Contains(body, "https://docs.example/p/"+p.Slug+"/d/"+page.Document.Slug) {
		t.Fatalf("comment: %q", body)
	}

	// Webhook: secret, signature, merged → published.
	if _, err := s.CodeRepoWebhook(ctx, reader, p.Slug, repo.ID, false); err == nil {
		t.Fatal("readers must not see the secret")
	}
	hook, err := s.CodeRepoWebhook(ctx, owner, p.Slug, "acme/app", false)
	if err != nil || hook.Secret == "" || !strings.HasSuffix(hook.URL, "/api/v1/hooks/code/"+repo.ID) {
		t.Fatalf("hook: %v %+v", err, hook)
	}
	if again, _ := s.CodeRepoWebhook(ctx, owner, p.Slug, repo.ID, false); again.Secret != hook.Secret {
		t.Fatal("secret must stay until rotated")
	}
	ev := []byte(`{"action":"closed","number":12,"pull_request":{"number":12,"merged":true,"head":{"ref":"docs"}}}`)
	sign := func(b []byte) string {
		m := hmac.New(sha256.New, []byte(hook.Secret))
		m.Write(b)
		return "sha256=" + hex.EncodeToString(m.Sum(nil))
	}
	if err := s.HandleCodeWebhook(ctx, repo.ID, "pull_request", "sha256=00", ev); err != ErrForbidden {
		t.Fatalf("bad signature: %v", err)
	}
	if err := s.HandleCodeWebhook(ctx, repo.ID, "pull_request", sign(ev), ev); err != nil {
		t.Fatal(err)
	}
	if d, _ := s.Document(ctx, owner, p.Slug, page.Document.Slug); markdownOf(d) != md {
		t.Fatalf("not published: %q", markdownOf(d))
	}
	d, _ := s.Document(ctx, owner, p.Slug, adr.Document.Slug)
	var got ADR
	_ = json.Unmarshal(d.Content, &got)
	if got.Decision != "Postgres 16." || got.Status != "accepted" {
		t.Fatalf("decision: %+v", got)
	}
	if _, err := s.DB.GetDraft(ctx, page.Document.ID); err == nil {
		t.Fatal("draft must be gone")
	}

	// A branch draft, then a closed (not merged) pull request found by polling.
	if _, err := s.LinkDraftCode(ctx, owner, p.Slug, page.Document.Slug, DraftCodeInput{Branch: "try"}); err != nil {
		t.Fatal(err)
	}
	other := "Something else.\n"
	if _, err := s.UpdateDocument(ctx, owner, p.Slug, page.Document.Slug, DocumentPatch{Markdown: &other}); err != nil {
		t.Fatal(err)
	}
	open := []byte(`{"action":"opened","number":12,"pull_request":{"number":12,"head":{"ref":"try"}}}`)
	if err := s.HandleCodeWebhook(ctx, repo.ID, "pull_request", sign(open), open); err != nil {
		t.Fatal(err)
	}
	if dr, _ := s.DB.GetDraft(ctx, page.Document.ID); dr.CodePR != 12 {
		t.Fatalf("branch draft learns its pull request: %+v", dr)
	}
	mu.Lock()
	prState = map[string]any{"state": "closed", "merged": false}
	mu.Unlock()
	s.RunCodeReleases(ctx, time.Now())
	if _, err := s.DB.GetDraft(ctx, page.Document.ID); err == nil {
		t.Fatal("closed pull request must discard the draft")
	}
	if d, _ := s.Document(ctx, owner, p.Slug, page.Document.Slug); markdownOf(d) != md {
		t.Fatalf("discarded draft changed the page: %q", markdownOf(d))
	}

	// With the approval workflow a merge does not publish: the draft waits
	// for the owners and leaves the pull request.
	if err := s.SetApproval(ctx, owner, p.Slug, true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LinkDraftCode(ctx, owner, p.Slug, page.Document.Slug, DraftCodeInput{PR: 13}); err != nil {
		t.Fatal(err)
	}
	third := "Third version.\n"
	if _, err := s.UpdateDocument(ctx, owner, p.Slug, page.Document.Slug, DocumentPatch{Markdown: &third}); err != nil {
		t.Fatal(err)
	}
	merged13 := []byte(`{"action":"closed","number":13,"pull_request":{"number":13,"merged":true,"head":{"ref":"x"}}}`)
	if err := s.HandleCodeWebhook(ctx, repo.ID, "pull_request", sign(merged13), merged13); err != nil {
		t.Fatal(err)
	}
	dr, err := s.DB.GetDraft(ctx, page.Document.ID)
	if err != nil || dr.ReviewBy == "" || dr.CodePR != 0 || !strings.HasPrefix(dr.ReviewNote, "Merged: ") || !strings.HasSuffix(dr.ReviewNote, "/13") || dr.Markdown != third {
		t.Fatalf("ready for approval: %v %+v", err, dr)
	}
	if d, _ := s.Document(ctx, owner, p.Slug, page.Document.Slug); markdownOf(d) != md {
		t.Fatalf("merge published in an approval project: %q", markdownOf(d))
	}
	if info, err := s.Draft(ctx, owner, p.Slug, page.Document.Slug); err != nil || info.ReviewBy == nil || info.ReviewBy.Name != "acme/app#13" {
		t.Fatalf("review by the pull request: %v %+v", err, info.ReviewBy)
	}
}
