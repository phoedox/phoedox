package core

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/phoedox/phoedox/internal/blob"
	"github.com/phoedox/phoedox/internal/secret"
)

func TestExportGitAndLink(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, _ := setup(t)
	dir := t.TempDir()
	s.Blobs, s.TempDir = blob.Local{Dir: filepath.Join(dir, "files")}, filepath.Join(dir, "tmp")
	_ = os.MkdirAll(s.TempDir, 0o700)
	s.Box, _ = secret.New(make([]byte, 32))
	s.PublicURL = "https://docs.example"
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Repo docs"})
	_, _ = s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "markdown", Title: "Deploy", Category: "Ops", Markdown: "v1"})
	v2 := "v2"
	if _, err := s.UpdateDocument(ctx, owner, p.Slug, "deploy", DocumentPatch{Markdown: &v2}); err != nil {
		t.Fatal(err)
	}
	_, _ = s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "adr", Title: "Use Go", ADR: &ADR{Status: "accepted", Decision: "Go."}})

	if err := s.ExportProjectGit(ctx, reader, p.Slug, &bytes.Buffer{}); err == nil {
		t.Fatal("only owners export")
	}
	var buf bytes.Buffer
	if err := s.ExportProjectGit(ctx, owner, p.Slug, &buf); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	for _, f := range zr.File {
		rc, _ := f.Open()
		var b bytes.Buffer
		_, _ = b.ReadFrom(rc)
		rc.Close()
		full := filepath.Join(out, filepath.FromSlash(f.Name))
		_ = os.MkdirAll(filepath.Dir(full), 0o755)
		_ = os.WriteFile(full, b.Bytes(), 0o644)
	}
	repo, err := git.PlainOpen(filepath.Join(out, p.Slug))
	if err != nil {
		t.Fatal(err)
	}
	iter, _ := repo.Log(&git.LogOptions{})
	var msgs []string
	_ = iter.ForEach(func(c *object.Commit) error { msgs = append(msgs, c.Message); return nil })
	// 3 versions + the full export.
	if len(msgs) != 4 || !strings.HasPrefix(msgs[0], "Phoedox export") || msgs[3] != "Create Deploy" {
		t.Fatalf("history: %q", msgs)
	}
	page, _ := os.ReadFile(filepath.Join(out, p.Slug, "docs", "Ops", "deploy.md"))
	if !strings.Contains(string(page), "v2") {
		t.Fatalf("page: %s", page)
	}
	if _, err := os.Stat(filepath.Join(out, p.Slug, "documents.json")); err != nil {
		t.Fatal("full export missing")
	}

	// Download links for AI clients.
	ai := Actor{User: owner.User, Via: "mcp", TokenID: "t"}
	if _, err := s.ProjectExportLink(ctx, reader, p.Slug, "zip"); err == nil {
		t.Fatal("only owners")
	}
	l, err := s.ProjectExportLink(ctx, ai, p.Slug, "git")
	if err != nil || !strings.HasPrefix(l.URL, "https://docs.example/api/v1/exports/") {
		t.Fatalf("link: %v %+v", err, l)
	}
	tok := strings.TrimPrefix(l.URL, "https://docs.example/api/v1/exports/")
	var dl bytes.Buffer
	slug, format, err := s.OpenExportLink(ctx, tok, &dl)
	if err != nil || slug != p.Slug || format != "git" || dl.Len() == 0 {
		t.Fatalf("open: %v %s %s %d", err, slug, format, dl.Len())
	}
	if _, _, err := s.OpenExportLink(ctx, tok+"x", &bytes.Buffer{}); err == nil {
		t.Fatal("tampered link")
	}
}
