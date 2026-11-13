package core

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/phoedox/phoedox/internal/blob"
	"github.com/phoedox/phoedox/internal/store"
)

var png = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89")

func TestFiles(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, stranger := setup(t)
	dir := t.TempDir()
	s.Blobs = blob.Local{Dir: filepath.Join(dir, "files")}
	s.TempDir = filepath.Join(dir, "tmp")
	s.MaxUpload = 1 << 20
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Haus"})
	_ = s.SetMember(ctx, owner, p.Slug, reader.User.Email, store.RoleReader)
	doc, _ := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "markdown", Title: "Netz", Markdown: "x"})

	if _, err := s.UploadFile(ctx, reader, p.Slug, FileInput{Name: "a.txt"}, strings.NewReader("x")); !errors.Is(err, ErrForbidden) {
		t.Fatalf("reader upload: %v", err)
	}
	ai := owner
	ai.Via = "mcp"
	if _, err := s.UploadFile(ctx, ai, p.Slug, FileInput{Name: "a.txt"}, strings.NewReader("x")); !errors.Is(err, ErrForbidden) {
		t.Fatalf("AI upload: %v", err)
	}
	if _, err := s.UploadFile(ctx, owner, p.Slug, FileInput{Name: "big.bin"}, bytes.NewReader(make([]byte, 1<<20+1))); invalidKey(err) != "validation.file_too_large" {
		t.Fatalf("too large: %v", err)
	}
	if _, err := s.UploadFile(ctx, owner, p.Slug, FileInput{Name: "x"}, strings.NewReader("")); invalidKey(err) != "validation.file_empty" {
		t.Fatalf("empty: %v", err)
	}

	// the type comes from the content, not from the name; paths are dropped from names
	img, err := s.UploadFile(ctx, owner, p.Slug, FileInput{Name: "../../etc/Plan.txt", Document: doc.Document.Slug}, bytes.NewReader(png))
	if err != nil || img.Mime != "image/png" || img.Name != "Plan.txt" || img.DocumentID != doc.Document.ID {
		t.Fatalf("upload: %v %+v", err, img)
	}
	html, _ := s.UploadFile(ctx, owner, p.Slug, FileInput{Name: "evil.png"}, strings.NewReader("<html><script>alert(1)</script></html>"))
	if html.Mime != "text/html; charset=utf-8" {
		t.Fatalf("html disguised as png: %q", html.Mime)
	}
	notes, _ := s.UploadFile(ctx, owner, p.Slug, FileInput{Name: "notes.md"}, strings.NewReader("# Notizen"))
	if !strings.HasPrefix(notes.Mime, "text/markdown") {
		t.Fatalf("markdown by extension: %q", notes.Mime)
	}
	// same content again: stored once
	again, _ := s.UploadFile(ctx, owner, p.Slug, FileInput{Name: "kopie.png"}, bytes.NewReader(png))
	if again.SHA256 != img.SHA256 {
		t.Fatal("same content must have the same hash")
	}

	if fs, _ := s.Files(ctx, reader, p.Slug, FileFilter{Document: doc.Document.Slug}); len(fs) != 1 || fs[0].ID != img.ID {
		t.Fatalf("document files: %+v", fs)
	}
	_, rc, err := s.OpenFile(ctx, reader, img.ID)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(rc)
	rc.Close()
	if !bytes.Equal(b, png) {
		t.Fatal("content differs")
	}
	if _, _, err := s.OpenFile(ctx, stranger, img.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stranger: %v", err)
	}
	if f, err := s.RenameFile(ctx, owner, p.Slug, img.ID, "Grundriss.png"); err != nil || f.Name != "Grundriss.png" {
		t.Fatalf("rename: %v %+v", err, f)
	}

	// trash: restore, then purge; the content goes only when nobody uses it
	if err := s.DeleteFile(ctx, owner, p.Slug, img.ID); err != nil {
		t.Fatal(err)
	}
	items, _ := s.Trash(ctx, owner, p.Slug)
	if len(items) != 1 || items[0].Kind != "file" || items[0].Title != "Grundriss.png" {
		t.Fatalf("trash: %+v", items)
	}
	if _, err := s.RestoreFromTrash(ctx, owner, p.Slug, img.ID); err != nil {
		t.Fatal(err)
	}
	_ = s.DeleteFile(ctx, owner, p.Slug, img.ID)
	if err := s.PurgeFromTrash(ctx, owner, p.Slug, img.ID); err != nil {
		t.Fatal(err)
	}
	if _, rc, err := s.OpenFile(ctx, owner, again.ID); err != nil {
		t.Fatalf("shared content must stay: %v", err)
	} else {
		rc.Close()
	}
	_ = s.DeleteFile(ctx, owner, p.Slug, again.ID)
	_ = s.PurgeFromTrash(ctx, owner, p.Slug, again.ID)
	if _, err := s.Blobs.Open(ctx, img.SHA256); !errors.Is(err, blob.ErrNotFound) {
		t.Fatalf("unused content must be deleted: %v", err)
	}
}

func TestCleanName(t *testing.T) {
	for in, want := range map[string]string{"": "file", "a\\b\\c.pdf": "c.pdf", "x\n\"y\".txt": "xy.txt", strings.Repeat("a", 300) + ".pdf": strings.Repeat("a", 196) + ".pdf"} {
		if got := cleanName(in); got != want {
			t.Errorf("cleanName(%q) = %q, want %q", in, got, want)
		}
	}
}
