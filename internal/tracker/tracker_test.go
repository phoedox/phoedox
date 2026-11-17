package tracker

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type call struct {
	Method, Path, Auth string
	Body               map[string]any
}

func fake(t *testing.T, answer func(c call) (int, any)) (*httptest.Server, *[]call) {
	t.Helper()
	calls := &[]call{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := call{Method: r.Method, Path: r.URL.EscapedPath(), Auth: r.Header.Get("Authorization") + r.Header.Get("PRIVATE-TOKEN")}
		if r.URL.RawQuery != "" {
			c.Path += "?" + r.URL.RawQuery
		}
		_ = json.NewDecoder(r.Body).Decode(&c.Body)
		*calls = append(*calls, c)
		code, out := answer(c)
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(out)
	}))
	t.Cleanup(srv.Close)
	return srv, calls
}

func TestNewValidates(t *testing.T) {
	for _, bad := range []Config{
		{Kind: GitHub, URL: "https://github.com/onlyowner"},
		{Kind: GitHub, URL: "https://github.com/a/b/c"},
		{Kind: GitHub, URL: "ftp://github.com/a/b"},
		{Kind: GitLab, URL: "https://gitlab.com/x"},
		{Kind: Jira, URL: "https://x.atlassian.net", Project: "lower"},
		{Kind: "trac", URL: "https://example.com/a/b"},
	} {
		if _, err := New(bad, nil); err == nil {
			t.Fatalf("accepted %+v", bad)
		}
	}
	c, err := New(Config{Kind: GitLab, URL: "https://gitlab.example/group/sub/game.git"}, nil)
	if err != nil || c.api != "https://gitlab.example/api/v4" || c.repo != "group%2Fsub%2Fgame" || c.Name() != "group/sub/game" {
		t.Fatalf("gitlab: %v %+v", err, c)
	}
	c, err = New(Config{Kind: Gitea, URL: "https://git.example/forge/owner/game"}, nil)
	if err != nil || c.api != "https://git.example/forge/api/v1" || c.Web() != "https://git.example/forge/owner/game" {
		t.Fatalf("gitea: %v %+v", err, c)
	}
}

func TestGitHub(t *testing.T) {
	srv, calls := fake(t, func(c call) (int, any) {
		switch {
		case c.Method == "POST" && c.Path == "/repos/acme/game/issues":
			return 201, map[string]any{"number": 42, "html_url": "https://github.com/acme/game/issues/42"}
		case c.Method == "GET" && c.Path == "/repos/acme/game/issues/42":
			return 200, map[string]any{"state": "closed"}
		}
		return 201, map[string]any{}
	})
	c, err := New(Config{Kind: GitHub, URL: "https://github.com/acme/game", Token: "tok", Labels: []string{"feedback"}, APIURL: srv.URL}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	is, err := c.Create(ctx, " Crash on start ", "body")
	if err != nil || is.Ref != "42" || is.URL != "https://github.com/acme/game/issues/42" {
		t.Fatalf("create: %v %+v", err, is)
	}
	if err := c.Comment(ctx, "42", "+2 new reports"); err != nil {
		t.Fatal(err)
	}
	if closed, err := c.Closed(ctx, "42"); err != nil || !closed {
		t.Fatalf("closed: %v %v", closed, err)
	}
	if err := c.Comment(ctx, "../x", "no"); err == nil {
		t.Fatal("bad ref accepted")
	}
	cs := *calls
	if cs[0].Auth != "Bearer tok" || cs[0].Body["title"] != "Crash on start" || cs[0].Body["labels"] == nil {
		t.Fatalf("create call: %+v", cs[0])
	}
	if cs[1].Path != "/repos/acme/game/issues/42/comments" || cs[1].Body["body"] != "+2 new reports" {
		t.Fatalf("comment call: %+v", cs[1])
	}
}

func TestGitLab(t *testing.T) {
	srv, calls := fake(t, func(c call) (int, any) {
		switch {
		case c.Method == "POST" && strings.HasSuffix(c.Path, "/issues"):
			return 201, map[string]any{"iid": 7, "web_url": "https://gitlab.example/g/game/-/issues/7"}
		case c.Method == "GET":
			return 200, map[string]any{"state": "opened"}
		}
		return 201, map[string]any{}
	})
	c, _ := New(Config{Kind: GitLab, URL: "https://gitlab.example/g/game", Token: "glpat", Labels: []string{"a", "b"}, APIURL: srv.URL}, nil)
	ctx := context.Background()
	is, err := c.Create(ctx, "T", "B")
	if err != nil || is.Ref != "7" {
		t.Fatalf("create: %v %+v", err, is)
	}
	if closed, err := c.Closed(ctx, "7"); err != nil || closed {
		t.Fatalf("open issue: %v %v", closed, err)
	}
	_ = c.Comment(ctx, "7", "note")
	cs := *calls
	if cs[0].Path != "/projects/g%2Fgame/issues" || cs[0].Auth != "glpat" || cs[0].Body["labels"] != "a,b" || cs[0].Body["description"] != "B" {
		t.Fatalf("create call: %+v", cs[0])
	}
	if cs[2].Path != "/projects/g%2Fgame/issues/7/notes" {
		t.Fatalf("note call: %+v", cs[2])
	}
}

func TestJira(t *testing.T) {
	srv, calls := fake(t, func(c call) (int, any) {
		switch {
		case c.Method == "POST" && c.Path == "/issue":
			return 201, map[string]any{"key": "GAME-12"}
		case c.Method == "GET":
			return 200, map[string]any{"fields": map[string]any{"status": map[string]any{"statusCategory": map[string]any{"key": "done"}}}}
		}
		return 201, map[string]any{}
	})
	c, err := New(Config{Kind: Jira, URL: "https://acme.atlassian.net", Project: "GAME", User: "me@example.org", Token: "t", Labels: []string{"from feedback"}, APIURL: srv.URL}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	is, err := c.Create(ctx, "Crash", "Steps")
	if err != nil || is.Ref != "GAME-12" || is.URL != "https://acme.atlassian.net/browse/GAME-12" {
		t.Fatalf("create: %v %+v", err, is)
	}
	if closed, _ := c.Closed(ctx, "GAME-12"); !closed {
		t.Fatal("done status should count as closed")
	}
	cs := *calls
	f := cs[0].Body["fields"].(map[string]any)
	if !strings.HasPrefix(cs[0].Auth, "Basic ") || f["issuetype"].(map[string]any)["name"] != "Bug" || f["labels"].([]any)[0] != "from-feedback" {
		t.Fatalf("create call: %+v", cs[0])
	}
	if cs[1].Path != "/issue/GAME-12?fields=status" {
		t.Fatalf("status call: %+v", cs[1])
	}
	// A failing tracker answers with an HTTPError.
	bad, _ := fake(t, func(call) (int, any) { return 401, map[string]any{} })
	c2, _ := New(Config{Kind: Jira, URL: "https://acme.atlassian.net", Project: "GAME", APIURL: bad.URL}, nil)
	var he *HTTPError
	if err := c2.Check(ctx); err == nil || !asHTTP(err, &he) || he.Status != 401 {
		t.Fatalf("check: %v", err)
	}
}

func asHTTP(err error, target **HTTPError) bool {
	h, ok := err.(*HTTPError)
	if ok {
		*target = h
	}
	return ok
}
