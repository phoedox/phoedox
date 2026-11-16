package core

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/phoedox/phoedox/internal/requests"
	"github.com/phoedox/phoedox/internal/secret"
	"github.com/phoedox/phoedox/internal/store"
)

func TestParseRepoURL(t *testing.T) {
	for in, want := range map[string][3]string{
		"https://github.com/phoedox/phoedox":     {"github", "https://api.github.com", "phoedox/phoedox"},
		"https://github.com/phoedox/phoedox.git": {"github", "https://api.github.com", "phoedox/phoedox"},
		"https://git.example.com/team/svc":       {"gitea", "https://git.example.com/api/v1", "team/svc"},
		"https://example.com/forgejo/team/svc/":  {"gitea", "https://example.com/forgejo/api/v1", "team/svc"},
	} {
		r, err := parseRepoURL(in)
		if err != nil || r.Provider != want[0] || r.APIURL != want[1] || r.Owner+"/"+r.Name != want[2] {
			t.Errorf("%s: %+v %v", in, r, err)
		}
	}
	for _, bad := range []string{"http://github.com/a/b", "https://github.com/a", "https://user:pw@git.example.com/a/b", "github.com/a/b", "https://github.com/x/a/b"} {
		if _, err := parseRepoURL(bad); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
}

func TestTaskCodeLinks(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, _ := setup(t)
	s.Box, _ = secret.New(make([]byte, 32))
	const sha = "a1b2c3d4e5f60718293a4b5c6d7e8f9012345678"
	commitAt := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	auths := map[string]string{}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auths[r.URL.Path] = r.Header.Get("Authorization")
		p := strings.TrimPrefix(r.URL.Path, "/api/v1")
		commit := map[string]any{"sha": sha, "html_url": "https://forge/c/" + sha, "commit": map[string]any{"message": "Fix login\n\nDetails", "committer": map[string]any{"date": commitAt}}}
		rel := func(tag string, at time.Time) map[string]any {
			return map[string]any{"tag_name": tag, "name": "Release " + tag, "html_url": "https://forge/r/" + tag, "published_at": at}
		}
		var out any
		switch {
		case p == "/repos/acme/app" || p == "/repos/team/svc":
			out = map[string]any{"full_name": "x"}
		case p == "/repos/acme/app/commits":
			out = []any{commit, map[string]any{"sha": "ffff000011112222", "html_url": "x", "commit": map[string]any{"message": "Other", "committer": map[string]any{"date": commitAt}}}}
		case p == "/repos/acme/app/commits/"+sha, p == "/repos/team/svc/git/commits/"+sha:
			out = commit
		case p == "/repos/acme/app/releases", p == "/repos/team/svc/releases":
			out = []any{rel("v1.1.0", commitAt.Add(48*time.Hour)), rel("v1.0.0", commitAt.Add(-48*time.Hour)), map[string]any{"tag_name": "v2", "draft": true}}
		case p == "/repos/acme/app/releases/tags/v1.0.0":
			out = rel("v1.0.0", commitAt.Add(-48*time.Hour))
		case p == "/repos/acme/app/pulls/7":
			out = map[string]any{"number": 7, "title": "Login fix", "html_url": "https://forge/p/7", "merged_at": commitAt}
		case p == "/repos/acme/app/compare/"+sha+"...v1.1.0":
			out = map[string]any{"status": "ahead"}
		default:
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(out)
	}))
	defer srv.Close()
	pool := srv.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs
	s.RequestPolicy = requests.Policy{AllowPrivate: true, RootCAs: pool}

	p, err := s.CreateProject(ctx, owner, ProjectInput{Name: "Code"})
	if err != nil {
		t.Fatal(err)
	}
	_ = s.SetMember(ctx, owner, p.Slug, reader.User.Email, store.RoleReader)
	if err := s.SetVariable(ctx, owner, p.Slug, "forge_token", "tok-123", true); err != nil {
		t.Fatal(err)
	}

	// Gitea/Forgejo through the public form; GitHub with its API pointed at the test server.
	if _, err := s.AddCodeRepo(ctx, reader, p.Slug, CodeRepoInput{URL: srv.URL + "/team/svc"}); err == nil {
		t.Fatal("reader connected a repository")
	}
	if _, err := s.AddCodeRepo(ctx, Actor{User: owner.User, Via: "mcp"}, p.Slug, CodeRepoInput{URL: srv.URL + "/team/svc"}); err == nil {
		t.Fatal("AI client connected a repository")
	}
	if _, err := s.AddCodeRepo(ctx, owner, p.Slug, CodeRepoInput{URL: srv.URL + "/team/svc", TokenVar: "nope"}); invalidKey(err) != "validation.git_token_var" {
		t.Fatalf("unknown secret: %v", err)
	}
	if _, err := s.AddCodeRepo(ctx, owner, p.Slug, CodeRepoInput{URL: srv.URL + "/team/missing"}); invalidKey(err) != "validation.code_repo_unreachable" {
		t.Fatalf("missing repo: %v", err)
	}
	gitea, err := s.AddCodeRepo(ctx, owner, p.Slug, CodeRepoInput{URL: srv.URL + "/team/svc", TokenVar: "forge_token"})
	if err != nil || gitea.Provider != "gitea" || auths["/api/v1/repos/team/svc"] != "token tok-123" {
		t.Fatalf("gitea: %+v %v %v", gitea, err, auths)
	}
	gh := store.CodeRepo{ID: newID(), ProjectID: p.ID, Provider: "github", WebURL: "https://github.com/acme/app", APIURL: srv.URL, Owner: "acme", Name: "app", TokenVar: "forge_token", CreatedBy: owner.User.ID}
	if err := s.DB.AddCodeRepo(ctx, gh); err != nil {
		t.Fatal(err)
	}

	board, err := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: store.DocBoard, Title: "Dev", Board: &Board{}})
	if err != nil {
		t.Fatal(err)
	}
	task, err := s.CreateTask(ctx, owner, p.Slug, board.Document.Slug, TaskInput{Title: "Login bug"})
	if err != nil {
		t.Fatal(err)
	}

	// Searching commits: words of the message, or a SHA.
	if items, err := s.CodeCommits(ctx, owner, p.Slug, "acme/app", "login"); err != nil || len(items) != 1 || items[0].Title != "Fix login" {
		t.Fatalf("search: %+v %v", items, err)
	}
	if auths["/repos/acme/app/commits"] != "Bearer tok-123" {
		t.Fatalf("github auth: %v", auths)
	}
	if _, err := s.CodeCommits(ctx, reader, p.Slug, "acme/app", ""); err == nil {
		t.Fatal("reader searched commits")
	}

	// Linking needs edit rights and a valid reference.
	if _, err := s.LinkTaskCode(ctx, reader, p.Slug, task.ID, CodeLinkInput{Repo: "acme/app", Kind: "commit", Ref: sha}); err == nil {
		t.Fatal("reader linked a commit")
	}
	if _, err := s.LinkTaskCode(ctx, owner, p.Slug, task.ID, CodeLinkInput{Repo: "acme/app", Kind: "commit", Ref: "xyz"}); invalidKey(err) != "validation.code_ref" {
		t.Fatalf("bad sha: %v", err)
	}
	if _, err := s.LinkTaskCode(ctx, owner, p.Slug, task.ID, CodeLinkInput{Repo: "acme/app", Kind: "commit", Ref: "deadbeef"}); invalidKey(err) != "validation.code_not_found" {
		t.Fatalf("unknown sha: %v", err)
	}
	links, err := s.LinkTaskCode(ctx, owner, p.Slug, task.ID, CodeLinkInput{Repo: gh.ID, Kind: "commit", Ref: sha})
	if err != nil || len(links) != 1 || links[0].Title != "Fix login" || links[0].Repo != "acme/app" {
		t.Fatalf("link: %+v %v", links, err)
	}
	if links, err = s.LinkTaskCode(ctx, owner, p.Slug, task.ID, CodeLinkInput{Repo: "acme/app", Kind: "pull", Ref: "#7"}); err != nil || len(links) != 2 {
		t.Fatalf("pull: %+v %v", links, err)
	}

	// The first release that contains the commit is linked by itself.
	s.RunCodeReleases(ctx, time.Now())
	links, _ = s.TaskCode(ctx, reader, p.Slug, task.ID)
	var rel *store.TaskCodeLink
	for i, l := range links {
		if l.Kind == "release" {
			rel = &links[i]
		}
	}
	if rel == nil || rel.Ref != "v1.1.0" || !rel.Auto || rel.Title != "Release v1.1.0" {
		t.Fatalf("auto release: %+v", links)
	}
	// Tasks carry their links in lists.
	ts, _ := s.Tasks(ctx, reader, p.Slug, board.Document.Slug, "", "", false)
	if len(ts) != 1 || len(ts[0].CodeLinks) != 3 {
		t.Fatalf("task list: %+v", ts)
	}
	// A release can also be linked by hand; links can be removed.
	if links, err = s.LinkTaskCode(ctx, owner, p.Slug, task.ID, CodeLinkInput{Repo: "acme/app", Kind: "release", Ref: "v1.0.0"}); err != nil || len(links) != 4 {
		t.Fatalf("manual release: %+v %v", links, err)
	}
	if err := s.UnlinkTaskCode(ctx, owner, p.Slug, task.ID, links[0].ID); err != nil {
		t.Fatal(err)
	}
	// Disconnecting a repository removes its links.
	if err := s.DeleteCodeRepo(ctx, owner, p.Slug, gh.ID); err != nil {
		t.Fatal(err)
	}
	if links, _ = s.TaskCode(ctx, owner, p.Slug, task.ID); len(links) != 0 {
		t.Fatalf("links after disconnect: %+v", links)
	}
}

func TestUpSQL(t *testing.T) {
	in := "-- +goose Up\nCREATE TABLE a (id int);\n-- +goose Down\nDROP TABLE a;\n"
	if got := upSQL(in); strings.Contains(got, "DROP") || !strings.Contains(got, "CREATE TABLE a") {
		t.Fatalf("goose: %q", got)
	}
	if got := upSQL("-- migrate:up\ncreate table b (x int);\n-- migrate:down\ndrop table b;"); strings.Contains(got, "drop") {
		t.Fatalf("dbmate: %q", got)
	}
}

func TestDBModelFromRepoIsNotAProblem(t *testing.T) {
	if ps := checkDBModel("from acme/app db/migrations\n", map[string][]string{}); len(ps) != 0 {
		t.Fatalf("from line: %+v", ps)
	}
	if ps := checkDBModel("hello\n", map[string][]string{}); len(ps) != 1 {
		t.Fatalf("plain text still is: %+v", ps)
	}
}
