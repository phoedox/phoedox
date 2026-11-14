package core

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/phoedox/phoedox/internal/blob"
	"github.com/phoedox/phoedox/internal/requests"
	"github.com/phoedox/phoedox/internal/store"
)

func TestExportImportProject(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, _ := setup(t)
	dir := t.TempDir()
	s.Blobs, s.TempDir = blob.Local{Dir: filepath.Join(dir, "files")}, filepath.Join(dir, "tmp")
	_ = os.MkdirAll(s.TempDir, 0o700)
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Haus", AIWriteMode: "direct"})
	en, de := "en", "de"
	_, _ = s.UpdateProject(ctx, owner, p.Slug, ProjectInput{Name: p.Name, Short: p.Short, PrimaryColor: p.PrimaryColor, AIWriteMode: p.AIWriteMode, MainLocale: &en, Locales: &[]string{de}})
	_ = s.SetMember(ctx, owner, p.Slug, "reader@x.de", store.RoleEditor)
	if _, err := s.UpsertEntity(ctx, owner, p.ID, EntityInput{Kind: "router", Name: "Router", Attrs: map[string]any{"ip": "192.168.1.1"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpsertEntity(ctx, owner, p.ID, EntityInput{Kind: "server", Name: "NAS"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Link(ctx, owner, p.ID, EdgeInput{From: "nas", Rel: "wired_to", To: "router"}); err != nil {
		t.Fatal(err)
	}
	page, _ := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "markdown", Title: "Network", Category: "Ops/Net", Markdown: "The @router is in the hall."})
	md := "Der @router steht im Flur."
	if _, err := s.SaveTranslation(ctx, owner, p.Slug, page.Document.Slug, "de", TranslationInput{Title: "Netzwerk", Markdown: &md}); err != nil {
		t.Fatal(err)
	}
	_, _ = s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "adr", Title: "Use VLANs", ADR: &ADR{Status: "accepted", Decision: "Three VLANs."}})
	board, _ := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "board", Title: "Tasks", Board: &Board{}, Parent: page.Document.Slug})
	if _, err := s.CreateTask(ctx, owner, p.Slug, board.Document.Slug, TaskInput{Title: "Label cables", Assignee: "reader@x.de", Labels: []string{"net"}}); err != nil {
		t.Fatal(err)
	}
	_, _ = s.CreateTask(ctx, owner, p.Slug, board.Document.Slug, TaskInput{Title: "Ask Claude", Assignee: "Claude", Column: "done"})
	if _, err := s.UploadFile(ctx, owner, p.Slug, FileInput{Name: "manual.txt", Entity: "nas"}, strings.NewReader("hello")); err != nil {
		t.Fatal(err)
	}

	if err := s.ExportProject(ctx, reader, p.Slug, io.Discard); err == nil {
		t.Fatal("editor exported")
	}
	var buf bytes.Buffer
	if err := s.ExportProject(ctx, owner, p.Slug, &buf); err != nil {
		t.Fatal(err)
	}
	z, _ := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	names := map[string]bool{}
	for _, f := range z.File {
		names[f.Name] = true
	}
	for _, want := range []string{"phoedox.json", "documents.json", "entities.json", "edges.json", "tasks.json", "files.json", "docs/Ops/Net/network.md", "docs/Ops/Net/network.de.md"} {
		if !names[want] {
			t.Fatalf("missing %s in %v", want, names)
		}
	}

	res, err := s.ImportProject(ctx, owner, buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Problems) != 0 || res.Pages != 3 || res.Entries != 2 || res.Tasks != 2 || res.Files != 1 {
		t.Fatalf("import: %+v", res)
	}
	q := res.Project
	if q.Slug == p.Slug || q.Name != "Haus" {
		t.Fatalf("project: %+v", q)
	}
	q, _ = s.DB.ProjectFor(ctx, q.ID, owner.User.ID)
	if q.MainLocale != "en" || len(q.Locales) != 1 {
		t.Fatalf("languages: %+v", q)
	}
	d, err := s.LocalizedDocument(ctx, owner, q.Slug, "network", "de")
	if err != nil || d.Title != "Netzwerk" || d.Category != "Ops/Net" {
		t.Fatalf("translation: %v %+v", err, d)
	}
	if adr, err := s.DB.GetDecision(ctx, q.ID, 1); err != nil || adr.Title != "Use VLANs" {
		t.Fatalf("decision: %v %+v", err, adr)
	}
	b, _ := s.DB.GetDocument(ctx, q.ID, "tasks")
	if b.ParentID == "" {
		t.Fatal("board lost its page")
	}
	tasks, _ := s.DB.ListTasks(ctx, q.ID, store.TaskFilter{})
	var assigned, text string
	for _, x := range tasks {
		if x.Title == "Label cables" {
			assigned = x.AssigneeText + x.AssigneeID
		}
		if x.Title == "Ask Claude" {
			text = x.AssigneeText
		}
	}
	// reader is not a member of the new project: the name stays as text
	if assigned != "reader" || text != "Claude" {
		t.Fatalf("assignees: %q %q", assigned, text)
	}
	g, _ := s.ProjectGraph(ctx, owner, q.Slug)
	if len(g.Edges) != 1 {
		t.Fatalf("edges: %+v", g.Edges)
	}
	mentions, _ := s.DB.DocumentsMentioning(ctx, g.Entities[1].ID)
	_ = mentions
	files, _ := s.Files(ctx, owner, q.Slug, FileFilter{})
	if len(files) != 1 || files[0].EntityID == "" {
		t.Fatalf("files: %+v", files)
	}
	if _, err := s.ImportProject(ctx, owner, []byte("nope")); invalidKey(err) != "validation.import_zip" {
		t.Fatalf("bad zip: %v", err)
	}
}

func TestImportRemote(t *testing.T) {
	ctx := context.Background()
	s, owner, _, _ := setup(t)
	dir := t.TempDir()
	s.Blobs, s.TempDir = blob.Local{Dir: filepath.Join(dir, "files")}, filepath.Join(dir, "tmp")
	_ = os.MkdirAll(s.TempDir, 0o700)
	s.RequestPolicy = requests.Policy{AllowPrivate: true}
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Altes Projekt"})
	_, _ = s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "markdown", Title: "Start", Markdown: "Hallo"})
	var export bytes.Buffer
	if err := s.ExportProject(ctx, owner, p.Slug, &export); err != nil {
		t.Fatal(err)
	}
	old := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer phx_old" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/v1/projects":
			_, _ = w.Write([]byte(`[{"slug":"altes-projekt","name":"Altes Projekt","role":"owner"},{"slug":"fremd","name":"Fremd","role":"reader"}]`))
		case "/api/v1/projects/altes-projekt/export":
			_, _ = w.Write(export.Bytes())
		default:
			http.NotFound(w, r)
		}
	}))
	defer old.Close()

	if _, err := s.RemoteProjects(ctx, owner, RemoteSource{URL: old.URL, Token: "wrong"}); invalidKey(err) != "validation.remote_token" {
		t.Fatalf("wrong token: %v", err)
	}
	list, err := s.RemoteProjects(ctx, owner, RemoteSource{URL: old.URL, Token: "phx_old"})
	if err != nil || len(list) != 1 || list[0].Slug != "altes-projekt" {
		t.Fatalf("list (owned only): %v %+v", err, list)
	}
	res, err := s.ImportRemote(ctx, owner, RemoteSource{URL: old.URL, Token: "phx_old", Project: "altes-projekt"})
	if err != nil || res.Pages != 1 || res.Project.Slug != "altes-projekt-2" {
		t.Fatalf("move: %v %+v", err, res)
	}
	if _, err := s.ImportRemote(ctx, Actor{User: owner.User, Via: "mcp"}, RemoteSource{URL: old.URL, Token: "phx_old", Project: "altes-projekt"}); err == nil {
		t.Fatal("AI client moved a project")
	}
}
