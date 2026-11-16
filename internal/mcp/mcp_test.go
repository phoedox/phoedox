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
	"github.com/phoedox/phoedox/resources"
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
	svc.PublicURL = "https://docs.example.org"
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
	// The Phoedox skill (resources/skills) explains every tool.
	skill := resources.SkillText()
	for _, tl := range tools.Tools {
		if !strings.Contains(skill, "`"+tl.Name+"`") {
			t.Errorf("tool %s is not explained in resources/skills/phoedox", tl.Name)
		}
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
	if !strings.Contains(out, `"url": "https://docs.example.org/p/phoedox/d/01`) { // addresses use the id
		t.Fatalf("decision without url: %s", out)
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
	// Open questions: a proposed decision with options, no decision text yet.
	out = call("create_decision", map[string]any{"project": "phoedox", "title": "Datenbank", "status": "proposed", "context": "Welche DB?",
		"options": []map[string]any{{"title": "PostgreSQL", "description": "+ RLS"}, {"title": "SQLite"}}})
	if !strings.Contains(out, `"applied": true`) || !strings.Contains(out, `"id": "o2"`) {
		t.Fatalf("decision with options: %s", out)
	}
	call("update_decision", map[string]any{"project": "phoedox", "document": "datenbank", "context": "Welche DB für Self-Hosting?"})
	if doc := call("get_document", map[string]any{"project": "phoedox", "document": "datenbank"}); !strings.Contains(doc, "SQLite") {
		t.Fatalf("options lost on update: %s", doc)
	}
	if out := call("list_decisions", map[string]any{"project": "phoedox"}); !strings.Contains(out, `"options": 2`) {
		t.Fatalf("list without options: %s", out)
	}
	call("create_document", map[string]any{"project": "phoedox", "title": "Architektur", "category": "Technik", "markdown": "Der @go-server …"})
	call("upsert_entity", map[string]any{"project": "phoedox", "kind": "service", "name": "Go-Server", "attrs": map[string]any{"lang": "go"}})
	bulk := call("upsert_entities", map[string]any{"project": "phoedox", "entities": []map[string]any{
		{"kind": "service", "name": "Worker"}, {"kind": "service", "name": "Cron"}, {"kind": "", "name": "Kaputt"}}})
	if !strings.Contains(bulk, `"created": 2`) || !strings.Contains(bulk, `"row": 2`) {
		t.Fatalf("upsert_entities: %s", bulk)
	}
	call("create_link", map[string]any{"project": "phoedox", "title": "Riverpod", "url": "https://riverpod.dev"})
	ov := call("get_project", map[string]any{"project": "phoedox"})
	if !strings.Contains(ov, `"Decisions"`) || !strings.Contains(ov, `"Technik"`) || !strings.Contains(ov, `"service": 3`) {
		t.Fatalf("overview: %s", ov)
	}
	if sum := call("get_project", map[string]any{"project": "phoedox", "documents": "summary"}); !strings.Contains(sum, `"documentCountsByCategory"`) || strings.Contains(sum, `"documentsByCategory"`) {
		t.Fatalf("overview summary: %s", sum)
	}

	// Many decisions at once, with own numbers and translations (multilingual project).
	call("update_project_settings", map[string]any{"project": "phoedox", "main_locale": "en", "locales": []string{"de"}})
	many := call("create_decisions", map[string]any{"project": "phoedox", "decisions": []map[string]any{
		{"title": "Use Lit", "ext_ref": "D-1", "status": "accepted", "decision": "Lit for the web",
			"translations": map[string]any{"de": map[string]any{"title": "Lit verwenden", "decision": "Lit fürs Web", "original": true}}},
		{"title": "Use Lit 4", "ext_ref": "D-2", "status": "accepted", "decision": "Lit 4", "supersedes": "D-1", "supersedes_partly": true, "supersedes_scope": "the version"},
		{"title": "Broken", "ext_ref": "ADR-0099", "decision": "x"},
	}})
	if !strings.Contains(many, `"created": 2`) || !strings.Contains(many, `"failed": 1`) || !strings.Contains(many, `"de": "saved"`) ||
		!strings.Contains(many, `"extRef": "D-2"`) || !strings.Contains(many, `"slug": "use-lit-4"`) || !strings.Contains(many, "/p/phoedox/d/01") {
		t.Fatalf("create_decisions: %s", many)
	}
	if d := call("get_document", map[string]any{"project": "phoedox", "document": "D-1", "lang": "de"}); !strings.Contains(d, "Lit fürs Web") || !strings.Contains(d, `"original": true`) || strings.Contains(d, `"machine": true`) {
		t.Fatalf("original translation: %s", d)
	}
	one := call("create_decision", map[string]any{"project": "phoedox", "title": "Bun", "decision": "Bun", "status": "accepted",
		"translations": map[string]any{"de": map[string]any{"title": "Bun", "decision": "Bun nutzen"}, "fr": map[string]any{"title": "Bun", "decision": "Bun"}}})
	if !strings.Contains(one, `"de": "saved"`) || !strings.Contains(one, `"fr": "error:`) {
		t.Fatalf("create_decision with translations: %s", one)
	}

	// links and boards can be changed through MCP
	if l := call("update_document", map[string]any{"project": "phoedox", "document": "riverpod", "description": "State management", "verbose": true}); !strings.Contains(l, "State management") || !strings.Contains(l, "riverpod.dev") {
		t.Fatalf("update link: %s", l)
	}

	// Long pages: edits, short answers, block checks, order, translations
	// that keep their text, batches (feedback of an AI client).
	page := call("create_document", map[string]any{"project": "phoedox", "title": "Long", "category": "Technik",
		"markdown": "Alpha beta gamma.\n\n```mermaid\nsequenceDiagram\n  participant Off\n```\n"})
	if !strings.Contains(page, `"blockProblems"`) || !strings.Contains(page, "Mermaid keyword") || strings.Contains(page, "Alpha beta") {
		t.Fatalf("create_document brief with block problems: %s", page)
	}
	if e := call("update_document", map[string]any{"project": "phoedox", "document": "long", "edits": []map[string]any{{"old": "beta", "new": "delta"}, {"old": "participant Off", "new": "participant Remote"}}}); !strings.Contains(e, `"version": 2`) || strings.Contains(e, "blockProblems") {
		t.Fatalf("edits: %s", e)
	}
	if d := call("get_document", map[string]any{"project": "phoedox", "document": "long"}); !strings.Contains(d, "Alpha delta gamma") || !strings.Contains(d, "participant Remote") {
		t.Fatalf("edited text: %s", d)
	}
	if res, _ := session.CallTool(ctx, &sdk.CallToolParams{Name: "update_document", Arguments: map[string]any{"project": "phoedox", "document": "long", "edits": []map[string]any{{"old": "a", "new": "b"}}}}); !res.IsError || !strings.Contains(res.Content[0].(*sdk.TextContent).Text, "occurs") {
		t.Fatalf("ambiguous edit accepted")
	}
	call("translate_document", map[string]any{"project": "phoedox", "document": "long", "lang": "de", "title": "Lang", "markdown": "Alpha Delta Gamma (de)."})
	// Only the title: the text stays (it was deleted before).
	if tr := call("translate_document", map[string]any{"project": "phoedox", "document": "long", "lang": "de", "title": "Lang (de)"}); !strings.Contains(tr, `"outdated": false`) || strings.Contains(tr, "Gamma") {
		t.Fatalf("title-only translation: %s", tr)
	}
	if d := call("get_document", map[string]any{"project": "phoedox", "document": "long", "lang": "de"}); !strings.Contains(d, "Alpha Delta Gamma (de).") || !strings.Contains(d, "Lang (de)") {
		t.Fatalf("translation text lost: %s", d)
	}
	// A new title in the main language, then only the translated title: current again.
	b := call("batch_update", map[string]any{"project": "phoedox", "operations": []map[string]any{
		{"action": "update", "document": "long", "title": "Long page"},
		{"action": "translate", "document": "long", "lang": "de", "title": "Lange Seite"},
	}})
	if !strings.Contains(b, `"applied": 2`) || !strings.Contains(b, `"outdated": false`) {
		t.Fatalf("batch: %s", b)
	}
	if bad, _ := session.CallTool(ctx, &sdk.CallToolParams{Name: "batch_update", Arguments: map[string]any{"project": "phoedox", "operations": []map[string]any{
		{"action": "update", "document": "long", "title": "Nope"},
		{"action": "update", "document": "missing-page", "title": "x"},
	}}}); !bad.IsError || !strings.Contains(bad.Content[0].(*sdk.TextContent).Text, "nothing was written") {
		t.Fatalf("invalid batch accepted: %v", bad.Content[0].(*sdk.TextContent).Text)
	}
	if d := call("get_document", map[string]any{"project": "phoedox", "document": "long"}); !strings.Contains(d, `"title": "Long page"`) {
		t.Fatalf("invalid batch wrote something: %s", d)
	}
	// Text change in the main language makes the translation stale.
	call("update_document", map[string]any{"project": "phoedox", "document": "long", "edits": []map[string]any{{"old": "gamma", "new": "epsilon"}}})
	if l := call("list_documents", map[string]any{"project": "phoedox", "translation": "stale"}); !strings.Contains(l, `"slug": "long"`) || !strings.Contains(l, `"de": "outdated"`) {
		t.Fatalf("stale translations: %s", l)
	}
	call("translate_document", map[string]any{"project": "phoedox", "document": "long", "lang": "de", "edits": []map[string]any{{"old": "Gamma", "new": "Epsilon"}}})
	if l := call("list_documents", map[string]any{"project": "phoedox", "translation": "outdated"}); strings.Contains(l, `"slug": "long"`) {
		t.Fatalf("translation still stale: %s", l)
	}
	// Order: a page right after another one.
	call("create_document", map[string]any{"project": "phoedox", "title": "Short", "markdown": "x", "after": "architektur"})
	if l := call("list_documents", map[string]any{"project": "phoedox", "category": "Technik"}); strings.Index(l, `"architektur"`) > strings.Index(l, `"short"`) || strings.Index(l, `"short"`) > strings.Index(l, `"long"`) {
		t.Fatalf("after: %s", l)
	}
	if f := call("find_in_documents", map[string]any{"project": "phoedox", "find": "delta", "documents": []string{"long"}}); !strings.Contains(f, `"total": 1`) || !strings.Contains(f, `"before": "Alpha "`) {
		t.Fatalf("find_in_documents: %s", f)
	}
	if r := call("replace_in_documents", map[string]any{"project": "phoedox", "find": "delta", "replace": "zeta", "documents": []string{"long"}}); !strings.Contains(r, `"replacement": "zeta"`) || strings.Contains(r, `"result"`) {
		t.Fatalf("replace preview: %s", r)
	}
	if r := call("replace_in_documents", map[string]any{"project": "phoedox", "find": "delta", "replace": "zeta", "documents": []string{"long"}, "apply": true}); !strings.Contains(r, `"replaced": 1`) || !strings.Contains(r, `"result": "applied"`) {
		t.Fatalf("replace apply: %s", r)
	}
	if v := call("validate_blocks", map[string]any{"project": "phoedox", "markdown": "```mermaid\nflowhart TD\n```\n```dbmodel\n-- tables: ghosts\n```"}); !strings.Contains(v, "unknown diagram type") || !strings.Contains(v, `ghosts\" is not defined`) {
		t.Fatalf("validate_blocks: %s", v)
	}
	call("create_board", map[string]any{"project": "phoedox", "title": "Plan"})
	card := call("create_task", map[string]any{"project": "phoedox", "board": "plan", "title": "Erste Karte", "documents": []string{"ADR-0001", "go-version"}})
	if !strings.Contains(card, `"url": "https://docs.example.org/p/phoedox/d/plan?task=`) || strings.Count(card, `"slug": "go-version"`) != 1 ||
		!strings.Contains(card, `"url": "https://docs.example.org/p/phoedox/d/01`) {
		t.Fatalf("card with documents: %s", card)
	}
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

	if dm := call("get_data_model", map[string]any{"project": "phoedox"}); !strings.Contains(dm, `"$id": "phoedox:kind/router"`) || !strings.Contains(dm, `"service": 3`) && !strings.Contains(dm, `"count": 3`) {
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
