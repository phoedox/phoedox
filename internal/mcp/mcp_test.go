package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	phxauth "github.com/phoedox/phoedox/internal/auth"
	"github.com/phoedox/phoedox/internal/core"
	"github.com/phoedox/phoedox/internal/store"
)

type bearer struct {
	token string
	next  http.RoundTripper
}

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return b.next.RoundTrip(r)
}

func TestMCPTools(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, store.SQLite, filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	a := &phxauth.Auth{DB: db}
	u, _ := a.CreateUser(ctx, "admin@coph.dev", "Admin", "sehr-geheimes-passwort", false)
	_, token, _ := a.CreateToken(ctx, u.ID, "Claude")
	svc := core.New(db)
	if _, err := svc.CreateProject(ctx, core.Actor{User: u, Via: "web"}, core.ProjectInput{Name: "Phoedox", AIWriteMode: "direct"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateProject(ctx, core.Actor{User: u, Via: "web"}, core.ProjectInput{Name: "Wohnung"}); err != nil { // suggest mode
		t.Fatal(err)
	}

	srv := httptest.NewServer(Handler(svc, a, "test", nil))
	defer srv.Close()

	// without token: rejected
	if resp, err := http.Post(srv.URL, "application/json", strings.NewReader(`{}`)); err != nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("missing token must be 401, got %v %v", resp.StatusCode, err)
	}

	client := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "1"}, nil)
	session, err := client.Connect(ctx, &sdk.StreamableClientTransport{Endpoint: srv.URL, HTTPClient: &http.Client{Transport: bearer{token, http.DefaultTransport}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	tools, err := session.ListTools(ctx, nil)
	if err != nil || len(tools.Tools) < 15 {
		t.Fatalf("tools: %v %d", err, len(tools.Tools))
	}

	call := func(name string, args map[string]any) string {
		t.Helper()
		res, err := session.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		text := res.Content[0].(*sdk.TextContent).Text
		if res.IsError {
			t.Fatalf("%s returned error: %s", name, text)
		}
		return text
	}

	out := call("create_decision", map[string]any{"project": "phoedox", "title": "Go-Version", "status": "accepted",
		"context": "Neue Go-Versionen bringen Sicherheits-Fixes.", "decision": "Immer die aktuelle stabile Go-Version verwenden."})
	if !strings.Contains(out, `"applied": true`) {
		t.Fatalf("decision not applied: %s", out)
	}
	call("update_decision", map[string]any{"project": "phoedox", "document": "go-version", "consequences": "Dockerfile und CI mitziehen."})
	doc := call("get_document", map[string]any{"project": "phoedox", "document": "go-version"})
	if !strings.Contains(doc, "Dockerfile und CI mitziehen.") || !strings.Contains(doc, "Immer die aktuelle stabile") || !strings.Contains(doc, `"version": 2`) {
		t.Fatalf("decision update lost fields: %s", doc)
	}
	if !strings.Contains(doc, `"ref": "ADR-0001"`) {
		t.Fatalf("decision ref missing: %s", doc)
	}
	call("create_decision", map[string]any{"project": "phoedox", "title": "Go 1.27", "status": "accepted",
		"decision": "go 1.27.0 mit toolchain-Pin.", "supersedes": "ADR-0001"})
	old := call("get_document", map[string]any{"project": "phoedox", "document": "adr-1"})
	if !strings.Contains(old, `"status": "superseded"`) || !strings.Contains(old, `"supersededBy": "go-1-27"`) {
		t.Fatalf("older decision not superseded: %s", old)
	}
	if log := call("list_decisions", map[string]any{"project": "phoedox", "status": "accepted"}); !strings.Contains(log, `"ref": "ADR-0002"`) || strings.Contains(log, "ADR-0001") {
		t.Fatalf("list_decisions: %s", log)
	}
	call("create_document", map[string]any{"project": "phoedox", "title": "Architektur", "category": "Technik", "markdown": "Der @go-server …"})
	call("upsert_entity", map[string]any{"project": "phoedox", "kind": "service", "name": "Go-Server", "attrs": map[string]any{"lang": "go"}})
	call("create_link", map[string]any{"project": "phoedox", "title": "Riverpod", "url": "https://riverpod.dev"})
	ov := call("get_project", map[string]any{"project": "phoedox"})
	if !strings.Contains(ov, `"Entscheidungen"`) || !strings.Contains(ov, `"Technik"`) || !strings.Contains(ov, `"service": 1`) {
		t.Fatalf("overview: %s", ov)
	}
	if s := call("search", map[string]any{"query": "stabile go"}); !strings.Contains(s, "go-version") {
		t.Fatalf("search: %s", s)
	}

	// suggest mode: AI writes become suggestions
	out = call("create_document", map[string]any{"project": "wohnung", "title": "Netz", "markdown": "Router"})
	if !strings.Contains(out, `"applied": false`) || !strings.Contains(out, `"status": "pending"`) {
		t.Fatalf("suggest mode: %s", out)
	}

	// errors are tool errors, not protocol errors
	res, err := session.CallTool(ctx, &sdk.CallToolParams{Name: "get_document", Arguments: map[string]any{"project": "phoedox", "document": "gibt-es-nicht"}})
	if err != nil || !res.IsError {
		t.Fatalf("missing document must be a tool error: %v %+v", err, res)
	}
}
