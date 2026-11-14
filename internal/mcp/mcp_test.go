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
	_, token, _ := a.CreateToken(ctx, u.ID, "Claude", "write", nil)
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
	if !strings.Contains(ov, `"Decisions"`) || !strings.Contains(ov, `"Technik"`) || !strings.Contains(ov, `"service": 1`) {
		t.Fatalf("overview: %s", ov)
	}
	// links and boards can be changed through MCP
	if l := call("update_document", map[string]any{"project": "phoedox", "document": "riverpod", "description": "State management"}); !strings.Contains(l, "State management") || !strings.Contains(l, "riverpod.dev") {
		t.Fatalf("update link: %s", l)
	}
	call("create_board", map[string]any{"project": "phoedox", "title": "Plan"})
	call("create_task", map[string]any{"project": "phoedox", "board": "plan", "title": "Erste Karte"})
	if _, err := session.CallTool(ctx, &sdk.CallToolParams{Name: "update_board", Arguments: map[string]any{"project": "phoedox", "board": "plan",
		"columns": []map[string]any{{"id": "doing", "name": "Doing"}, {"id": "done", "name": "Done", "done": true}}}}); err != nil {
		t.Fatal(err)
	} else if b := call("get_document", map[string]any{"project": "phoedox", "document": "plan"}); strings.Contains(b, "Doing") {
		t.Fatalf("removed a column with tasks: %s", b)
	}
	if b := call("update_board", map[string]any{"project": "phoedox", "board": "plan", "title": "Roadmap", "description": "Next steps",
		"columns": []map[string]any{{"id": "backlog", "name": "Backlog"}, {"id": "todo", "name": "To do"}, {"id": "doing", "name": "In progress"}, {"id": "done", "name": "Done", "done": true}}}); !strings.Contains(b, "Backlog") || !strings.Contains(b, "Roadmap") {
		t.Fatalf("update board: %s", b)
	}
	if s := call("search", map[string]any{"query": "stabile go"}); !strings.Contains(s, "go-version") {
		t.Fatalf("search: %s", s)
	}

	if dm := call("get_data_model", map[string]any{"project": "phoedox"}); !strings.Contains(dm, `"$id": "phoedox:kind/router"`) || !strings.Contains(dm, `"service": 1`) && !strings.Contains(dm, `"count": 1`) {
		t.Fatalf("data model: %s", dm)
	}
	call("define_kind", map[string]any{"project": "phoedox", "id": "queue", "name": "Queue", "icon": "Workflow",
		"attrs": []map[string]any{{"name": "broker", "type": "select", "options": []string{"nats", "rabbitmq"}}}})
	call("upsert_entity", map[string]any{"project": "phoedox", "kind": "queue", "name": "Events", "attrs": map[string]any{"broker": "nats"}})
	res0, _ := session.CallTool(ctx, &sdk.CallToolParams{Name: "link_entities", Arguments: map[string]any{"project": "phoedox", "from": "events", "rel": "mounted_in", "to": "go-server"}})
	if !res0.IsError || !strings.Contains(res0.Content[0].(*sdk.TextContent).Text, "allowed: related_to, depends_on") {
		t.Fatalf("relation rules: %+v", res0)
	}

	// Context packs and resources.
	call("link_entities", map[string]any{"project": "phoedox", "from": "events", "rel": "depends_on", "to": "go-server"})
	if ent := call("get_entity", map[string]any{"project": "phoedox", "entity": "go-server"}); !strings.Contains(ent, "# Go-Server (service, @go-server)") ||
		!strings.Contains(ent, "← depends_on from Events") || !strings.Contains(ent, "Values: lang: go") {
		t.Fatalf("context pack: %s", ent)
	}
	if list, err := session.ListResourceTemplates(ctx, nil); err != nil || len(list.ResourceTemplates) != 2 {
		t.Fatalf("templates: %v %+v", err, list)
	}
	rr, err := session.ReadResource(ctx, &sdk.ReadResourceParams{URI: "phoedox://phoedox/doc/go-version"})
	if err != nil || !strings.Contains(rr.Contents[0].Text, "## Decision") || !strings.Contains(rr.Contents[0].Text, "Dockerfile und CI") {
		t.Fatalf("doc resource: %v %+v", err, rr)
	}
	rr, err = session.ReadResource(ctx, &sdk.ReadResourceParams{URI: "phoedox://phoedox/entity/go-server"})
	if err != nil || !strings.Contains(rr.Contents[0].Text, "@go-server") {
		t.Fatalf("entity resource: %v %+v", err, rr)
	}
	if rr, err = session.ReadResource(ctx, &sdk.ReadResourceParams{URI: "phoedox://projects"}); err != nil || !strings.Contains(rr.Contents[0].Text, `"phoedox"`) {
		t.Fatalf("projects resource: %v %+v", err, rr)
	}
	if _, err := session.ReadResource(ctx, &sdk.ReadResourceParams{URI: "phoedox://phoedox/doc/gibt-es-nicht"}); err == nil {
		t.Fatal("missing resource")
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
