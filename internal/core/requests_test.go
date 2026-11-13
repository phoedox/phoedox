package core

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/phoedox/phoedox/internal/requests"
	"github.com/phoedox/phoedox/internal/secret"
	"github.com/phoedox/phoedox/internal/store"
)

func TestEnvironmentAndRequests(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, _ := setup(t)
	box, _ := secret.Load("", filepath.Join(t.TempDir(), "k"))
	s.Box = box
	s.RequestPolicy = requests.Policy{AllowPrivate: true}
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Haus", AIWriteMode: "direct"})
	_ = s.SetMember(ctx, owner, p.Slug, "reader@x.de", store.RoleReader)
	_ = s.SetMember(ctx, owner, p.Slug, "stranger@x.de", store.RoleEditor)
	editor := Actor{User: store.User{ID: mustUser(t, s, "stranger@x.de"), Name: "Ed"}, Via: "web"}

	var gotAuth, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotMethod = r.Header.Get("Authorization"), r.Method
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"state":-55,"token":"` + strings.TrimPrefix(gotAuth, "Bearer ") + `"}`))
	}))
	defer srv.Close()

	// environment rights: editors set variables, only owners secrets
	if err := s.SetVariable(ctx, editor, p.Slug, "base", srv.URL, false); err != nil {
		t.Fatal(err)
	}
	if err := s.SetVariable(ctx, editor, p.Slug, "token", "sehr-geheim-42", true); !errors.Is(err, ErrForbidden) {
		t.Fatalf("editor secret: %v", err)
	}
	if err := s.SetVariable(ctx, owner, p.Slug, "token", "sehr-geheim-42", true); err != nil {
		t.Fatal(err)
	}
	if err := s.SetVariable(ctx, editor, p.Slug, "token", "überschrieben", false); !errors.Is(err, ErrForbidden) {
		t.Fatalf("editor must not replace a secret: %v", err)
	}
	if err := s.SetVariable(ctx, owner, p.Slug, "1bad", "x", false); invalidKey(err) != "validation.env_name" {
		t.Fatalf("name: %v", err)
	}
	if err := s.SetVariable(ctx, Actor{User: owner.User, Via: "mcp"}, p.Slug, "x", "y", false); !errors.Is(err, ErrForbidden) {
		t.Fatalf("AI must not change the environment: %v", err)
	}
	vars, _ := s.Environment(ctx, reader, p.Slug)
	if len(vars) != 2 || vars[1].Name != "token" || !vars[1].Secret || vars[1].Value != "" || vars[0].Value != srv.URL {
		t.Fatalf("environment: %+v", vars)
	}
	raw, _ := s.DB.Env(ctx, p.ID)
	if strings.Contains(raw[1].Value, "sehr-geheim") {
		t.Fatal("secret stored in plain text")
	}

	md := "# API\n\n```request\nGET {{base}}/status\nAuthorization: Bearer {{token}}\n\nexpect status == 200\nexpect $.state >= -70\n```\n\n```request\nPOST {{base}}/reboot\n```\n\n```request\nGET {{base}}/{{unknown}}\n```\n"
	doc, _ := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "markdown", Title: "API", Markdown: md})
	slug := doc.Document.Slug

	res, err := s.RunRequest(ctx, reader, p.Slug, slug, 0)
	if err != nil || res.Status != 200 || len(res.Checks) != 2 || !res.Checks[0].OK || !res.Checks[1].OK {
		t.Fatalf("run: %v %+v", err, res)
	}
	if gotAuth != "Bearer sehr-geheim-42" {
		t.Fatalf("secret not filled in: %q", gotAuth)
	}
	if strings.Contains(res.Body, "sehr-geheim-42") || !strings.Contains(res.Body, "••••") {
		t.Fatalf("secret not masked: %s", res.Body)
	}
	// changing requests: editors only, never AI clients
	if _, err := s.RunRequest(ctx, reader, p.Slug, slug, 1); !errors.Is(err, ErrForbidden) {
		t.Fatalf("reader POST: %v", err)
	}
	if _, err := s.RunRequest(ctx, Actor{User: owner.User, Via: "mcp"}, p.Slug, slug, 1); !errors.Is(err, ErrForbidden) {
		t.Fatalf("AI POST: %v", err)
	}
	if r, err := s.RunRequest(ctx, editor, p.Slug, slug, 1); err != nil || gotMethod != "POST" || r.Status != 200 {
		t.Fatalf("editor POST: %v %+v", err, r)
	}
	if _, err := s.RunRequest(ctx, reader, p.Slug, slug, 2); invalidKey(err) != "validation.request_variable" {
		t.Fatalf("unknown variable: %v", err)
	}
	if _, err := s.RunRequest(ctx, reader, p.Slug, slug, 3); invalidKey(err) != "validation.request_missing" {
		t.Fatalf("missing block: %v", err)
	}
	if err := s.DeleteVariable(ctx, editor, p.Slug, "token"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("editor deletes secret: %v", err)
	}
	if err := s.DeleteVariable(ctx, owner, p.Slug, "token"); err != nil {
		t.Fatal(err)
	}
}
