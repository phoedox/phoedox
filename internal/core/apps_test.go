package core

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/phoedox/phoedox/internal/store"
)

func TestAppBuildAndFrame(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, stranger := setup(t)
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Haus"})
	_ = s.SetMember(ctx, owner, p.Slug, reader.User.Email, store.RoleReader)

	// Invalid file names and a missing entry are refused.
	if _, err := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: store.DocApp, Title: "x", App: &App{Files: map[string]string{"../evil.js": "x"}}}); invalidKey(err) != "validation.app_file_name" {
		t.Fatalf("file name: %v", err)
	}
	if _, err := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: store.DocApp, Title: "x", App: &App{Files: map[string]string{"style.css": "x"}}}); invalidKey(err) != "validation.app_entry" {
		t.Fatalf("entry: %v", err)
	}
	src := "import { useState } from \"react\";\nexport default function App() { const [n] = useState<number>(1); return <b>{n}</b>; }\n"
	res, err := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: store.DocApp, Title: "Counter", App: &App{Files: map[string]string{"app.tsx": src, "style.css": "b{color:red}", "broken.ts": "let x: = 1"}}})
	if err != nil {
		t.Fatal(err)
	}
	doc := res.Document
	var app App
	_ = json.Unmarshal(doc.Content, &app)
	if app.Build == nil || !strings.Contains(app.Build.Files["app.tsx"], "react/jsx-runtime") || len(app.Build.Errors) != 1 || app.Build.Errors[0].File != "broken.ts" {
		t.Fatalf("build: %+v", app.Build)
	}
	// Readers open it; strangers do not.
	if _, err := s.OpenApp(ctx, stranger, p.Slug, doc.Slug); err == nil {
		t.Fatal("strangers must not open apps")
	}
	fr, err := s.OpenApp(ctx, reader, p.Slug, doc.Slug)
	if err != nil || !strings.HasPrefix(fr.URL, "/app-frame/") || fr.Granted != -1 || fr.CanEdit {
		t.Fatalf("open: %v %+v", err, fr)
	}
	tok := strings.TrimSuffix(strings.TrimPrefix(fr.URL, "/app-frame/"), "/")
	page, err := s.AppFrameFile(ctx, tok, "", "https://phx.test", "")
	if err != nil || !strings.Contains(string(page.Body), `"react":"https://phx.test/applib/react.js"`) || !strings.Contains(string(page.Body), "window.phoedox") ||
		!strings.Contains(string(page.Body), `<link rel="stylesheet" href="style.css">`) || !strings.Contains(string(page.Body), `src="app.tsx"`) {
		t.Fatalf("page: %v %s", err, page.Body)
	}
	js, err := s.AppFrameFile(ctx, tok, "app.tsx", "https://phx.test", "")
	if err != nil || !strings.HasPrefix(js.Type, "text/javascript") || strings.Contains(string(js.Body), "useState<number>") {
		t.Fatalf("compiled: %v %s", err, js.Body)
	}
	if b, _ := s.AppFrameFile(ctx, tok, "broken.ts", "", ""); !strings.Contains(string(b.Body), "throw new Error") {
		t.Fatalf("broken file: %s", b.Body)
	}
	if _, err := s.AppFrameFile(ctx, tok+"x", "", "", ""); err == nil {
		t.Fatal("a changed token must not open")
	}
	if _, err := s.AppFrameFile(ctx, tok, "missing.js", "", ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing file: %v", err)
	}
	// On the user content domain the token only works on its app's subdomain.
	if _, err := s.AppFrameFile(ctx, tok, "", "", "01otherapp"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other subdomain: %v", err)
	}
	if _, err := s.AppFrameFile(ctx, tok, "", "", strings.ToLower(doc.ID)); err != nil {
		t.Fatalf("own subdomain: %v", err)
	}
	// Preview of unsaved files.
	pv, err := s.PreviewApp(ctx, owner, p.Slug, "", App{Files: map[string]string{"index.html": "<html><head><title>x</title></head><body>hi</body></html>"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.PreviewApp(ctx, reader, p.Slug, "", App{Files: map[string]string{"index.html": "x"}}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("readers must not preview: %v", err)
	}
	ptok := strings.TrimSuffix(strings.TrimPrefix(pv.URL, "/app-frame/"), "/")
	if b, err := s.AppFrameFile(ctx, ptok, "index.html", "https://phx.test", ""); err != nil || !strings.Contains(string(b.Body), "<head><script type=\"importmap\">") {
		t.Fatalf("preview: %v %s", err, b.Body)
	}
}

func TestAppBridge(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, _ := setup(t)
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Haus"})
	_ = s.SetMember(ctx, owner, p.Slug, reader.User.Email, store.RoleReader)
	board, _ := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "board", Title: "Chores"})
	_, _ = s.CreateTask(ctx, owner, p.Slug, board.Document.Slug, TaskInput{Title: "Bins out"})
	res, err := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: store.DocApp, Title: "Checklist", App: &App{
		Files: map[string]string{"index.html": "<p>x</p>"}, Level: 2, StorageMB: 1,
		Sources: []AppSource{{ID: "chores", Kind: "board", Ref: board.Document.Slug}}}})
	if err != nil {
		t.Fatal(err)
	}
	doc := res.Document.Slug
	// Nothing before the person allows it.
	if _, err := s.ReadAppSource(ctx, reader, p.Slug, doc, "chores"); invalidKey(err) != "validation.app_not_allowed" {
		t.Fatalf("read before grant: %v", err)
	}
	// Grants are capped at what the app asks for.
	if lvl, err := s.GrantApp(ctx, reader, p.Slug, doc, 3); err != nil || lvl != 2 {
		t.Fatalf("grant: %v %d", err, lvl)
	}
	got, err := s.ReadAppSource(ctx, reader, p.Slug, doc, "chores")
	if tasks, ok := got.([]store.Task); err != nil || !ok || len(tasks) != 1 {
		t.Fatalf("read: %v %#v", err, got)
	}
	if _, err := s.ReadAppSource(ctx, reader, p.Slug, doc, "secrets"); invalidKey(err) != "validation.app_source_unknown" {
		t.Fatalf("unknown source: %v", err)
	}
	// Shared and personal storage.
	if err := s.AppStorageSet(ctx, reader, p.Slug, doc, "list", json.RawMessage(`["milk"]`), false); err != nil {
		t.Fatal(err)
	}
	if err := s.AppStorageSet(ctx, reader, p.Slug, doc, "mine", json.RawMessage(`{"done":true}`), true); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.AppStorageGet(ctx, reader, p.Slug, doc, "list", false); string(v) != `["milk"]` {
		t.Fatalf("shared value: %s", v)
	}
	if v, _ := s.AppStorageGet(ctx, reader, p.Slug, doc, "mine", false); string(v) != "null" {
		t.Fatalf("personal values are not shared: %s", v)
	}
	if keys, _ := s.AppStorageList(ctx, reader, p.Slug, doc, true); len(keys) != 1 || keys[0] != "mine" {
		t.Fatalf("personal keys: %v", keys)
	}
	if err := s.AppStorageSet(ctx, reader, p.Slug, doc, "bad key!", json.RawMessage(`1`), false); invalidKey(err) != "validation.app_key" {
		t.Fatalf("key: %v", err)
	}
	if err := s.AppStorageSet(ctx, reader, p.Slug, doc, "x", json.RawMessage(`{nope`), false); invalidKey(err) != "validation.app_value" {
		t.Fatalf("value: %v", err)
	}
	big := json.RawMessage(`"` + strings.Repeat("a", 600_000) + `"`)
	_ = s.AppStorageSet(ctx, reader, p.Slug, doc, "big1", big, false)
	if err := s.AppStorageSet(ctx, reader, p.Slug, doc, "big2", big, false); invalidKey(err) != "validation.app_storage_full" {
		t.Fatalf("limit: %v", err)
	}
	if err := s.AppStorageDelete(ctx, reader, p.Slug, doc, "big1", false); err != nil {
		t.Fatal(err)
	}
	// Console messages reach get_app; the AI edits files.
	_ = s.LogApp(ctx, reader, p.Slug, doc, []AppLog{{Level: "error", Text: "boom", File: "https://x/app-frame/tok/app.js", Line: 3}})
	v, err := s.GetApp(ctx, owner, p.Slug, doc)
	if err != nil || len(v.Console) != 1 || v.Console[0].File != "app.js" || v.App.Level != 2 {
		t.Fatalf("get_app: %v %+v", err, v)
	}
	if _, err := s.EditApp(ctx, owner, p.Slug, doc, nil, []AppEdit{{File: "index.html", Old: "nothing", New: "y"}}, nil); invalidKey(err) != "validation.edit_not_found" {
		t.Fatalf("edit not found: %v", err)
	}
	css := "p{color:green}"
	r, err := s.EditApp(ctx, owner, p.Slug, doc, map[string]*string{"style.css": &css}, []AppEdit{{File: "index.html", Old: "<p>x</p>", New: "<p>y</p>"}}, nil)
	if err != nil || !r.Applied {
		t.Fatalf("edit: %v %+v", err, r)
	}
	if _, err := s.EditApp(ctx, reader, p.Slug, doc, nil, nil, &App{Level: 3}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("readers must not edit: %v", err)
	}
	after, _ := s.GetApp(ctx, owner, p.Slug, doc)
	if after.App.Files["index.html"] != "<p>y</p>" || after.App.Files["style.css"] != css {
		t.Fatalf("after edit: %+v", after.App.Files)
	}
}
