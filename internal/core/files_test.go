package core

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/phoedox/phoedox/internal/blob"
	"github.com/phoedox/phoedox/internal/preview"
	"github.com/phoedox/phoedox/internal/scan"
	"github.com/phoedox/phoedox/internal/store"
)

var pngBytes = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89")

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
	img, err := s.UploadFile(ctx, owner, p.Slug, FileInput{Name: "../../etc/Plan.txt", Document: doc.Document.Slug}, bytes.NewReader(pngBytes))
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
	again, _ := s.UploadFile(ctx, owner, p.Slug, FileInput{Name: "kopie.png"}, bytes.NewReader(pngBytes))
	if again.SHA256 != img.SHA256 {
		t.Fatal("same content must have the same hash")
	}

	if fs, _ := s.Files(ctx, reader, p.Slug, FileFilter{Document: doc.Document.Slug}); len(fs) != 1 || fs[0].ID != img.ID {
		t.Fatalf("document files: %+v", fs)
	}
	fc, err := s.OpenFile(ctx, reader, img.ID, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(fc.Reader)
	fc.Reader.Close()
	if !bytes.Equal(b, pngBytes) {
		t.Fatal("content differs")
	}
	if _, err := s.OpenFile(ctx, stranger, img.ID, 0, false); !errors.Is(err, ErrNotFound) {
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
	if fc, err := s.OpenFile(ctx, owner, again.ID, 0, false); err != nil {
		t.Fatalf("shared content must stay: %v", err)
	} else {
		fc.Reader.Close()
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

type fakeScanner struct{}

func (fakeScanner) Scan(_ context.Context, path string) error {
	b, _ := os.ReadFile(path)
	if bytes.Contains(b, []byte("EICAR")) {
		return scan.ErrInfected{Signature: "Eicar-Test"}
	}
	if bytes.Contains(b, []byte("SCANFAIL")) {
		return errors.New("clamd down")
	}
	return nil
}

func TestFileVersionsQuotaScanAndText(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, _ := setup(t)
	dir := t.TempDir()
	s.Blobs = blob.Local{Dir: filepath.Join(dir, "files")}
	s.TempDir = filepath.Join(dir, "tmp")
	s.Preview = preview.Detect(false, "")
	s.Scanner = fakeScanner{}
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Haus"})
	_ = s.SetMember(ctx, owner, p.Slug, reader.User.Email, store.RoleReader)

	// virus scan
	if _, err := s.UploadFile(ctx, owner, p.Slug, FileInput{Name: "x.txt"}, strings.NewReader("X5O EICAR test")); invalidKey(err) != "validation.file_infected" {
		t.Fatalf("infected: %v", err)
	}
	if _, err := s.UploadFile(ctx, owner, p.Slug, FileInput{Name: "x.txt"}, strings.NewReader("SCANFAIL")); invalidKey(err) != "validation.file_scan_failed" {
		t.Fatalf("scan failure must reject: %v", err)
	}

	// text files are indexed for search at once (no queue in tests)
	f, err := s.UploadFile(ctx, owner, p.Slug, FileInput{Name: "notiz.txt"}, strings.NewReader("Der Zählerschrank steht im Keller."))
	if err != nil {
		t.Fatal(err)
	}
	f, _ = s.DB.GetFile(ctx, f.ID)
	if f.TextStatus != "done" || f.Version != 1 {
		t.Fatalf("processed: %+v", f)
	}
	if hits := mustSearch(t, s, reader, "zählerschrank"); len(hits) != 1 || hits[0].Type != "file" {
		t.Fatalf("file search: %+v", hits)
	}

	// images get a preview
	img, _ := s.UploadFile(ctx, owner, p.Slug, FileInput{Name: "p.png"}, bytes.NewReader(pngImage(t)))
	if fc, err := s.OpenFile(ctx, reader, img.ID, 0, true); err != nil {
		t.Fatalf("preview: %v", err)
	} else {
		fc.Reader.Close()
	}

	// versions: new content, old one stays readable and restorable
	v2, err := s.NewFileVersion(ctx, owner, p.Slug, f.ID, strings.NewReader("Der Zählerschrank ist jetzt im Flur."))
	if err != nil || v2.Version != 2 {
		t.Fatalf("v2: %v %+v", err, v2)
	}
	if hits := mustSearch(t, s, reader, "flur"); len(hits) != 1 {
		t.Fatalf("new version indexed: %+v", hits)
	}
	fc, _ := s.OpenFile(ctx, reader, f.ID, 1, false)
	old, _ := io.ReadAll(fc.Reader)
	fc.Reader.Close()
	if !strings.Contains(string(old), "Keller") {
		t.Fatalf("old version: %q", old)
	}
	if _, err := s.NewFileVersion(ctx, reader, p.Slug, f.ID, strings.NewReader("x")); !errors.Is(err, ErrForbidden) {
		t.Fatalf("reader new version: %v", err)
	}
	v3, err := s.RestoreFileVersion(ctx, owner, p.Slug, f.ID, 1)
	if err != nil || v3.Version != 3 || v3.SHA256 != f.SHA256 {
		t.Fatalf("restore: %v %+v", err, v3)
	}
	if vs, _ := s.FileVersions(ctx, reader, p.Slug, f.ID); len(vs) != 3 || vs[0].Version != 3 {
		t.Fatalf("versions: %+v", vs)
	}

	// quota: instance limit, project limit only below it
	_, _ = s.UpdateSettings(ctx, Actor{User: store.User{ID: "adm", IsAdmin: true}, Via: "web"}, SettingsPatch{QuotaProjectMB: new(1)})
	if _, err := s.SetProjectQuota(ctx, owner, p.Slug, 5); invalidKey(err) != "validation.quota_above_max" {
		t.Fatalf("project above instance: %v", err)
	}
	if st, err := s.SetProjectQuota(ctx, owner, p.Slug, 1); err != nil || st.LimitBytes != 1<<20 || st.UsedBytes == 0 {
		t.Fatalf("storage: %v %+v", err, st)
	}
	if _, err := s.UploadFile(ctx, owner, p.Slug, FileInput{Name: "big.bin"}, bytes.NewReader(make([]byte, 1<<20))); invalidKey(err) != "validation.quota_exceeded" {
		t.Fatalf("quota: %v", err)
	}

	// purge frees all versions and the preview
	_ = s.DeleteFile(ctx, owner, p.Slug, img.ID)
	thumb := func() string { x, _ := s.DB.GetFileAny(ctx, img.ID); return x.ThumbSHA256 }()
	if err := s.PurgeFromTrash(ctx, owner, p.Slug, img.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Blobs.Open(ctx, thumb); !errors.Is(err, blob.ErrNotFound) {
		t.Fatalf("preview must be deleted: %v", err)
	}
}

func TestResumableUpload(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, _ := setup(t)
	dir := t.TempDir()
	s.Blobs = blob.Local{Dir: filepath.Join(dir, "files")}
	s.TempDir = filepath.Join(dir, "tmp")
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Haus"})
	_ = s.SetMember(ctx, owner, p.Slug, reader.User.Email, store.RoleReader)
	data := []byte(strings.Repeat("phoedox-", 1000))

	if _, err := s.StartUpload(ctx, reader, p.Slug, int64(len(data)), UploadMeta{Name: "a.txt"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("reader: %v", err)
	}
	u, err := s.StartUpload(ctx, owner, p.Slug, int64(len(data)), UploadMeta{Name: "Handbuch.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Upload(ctx, reader, u.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("someone else's upload: %v", err)
	}
	// first chunk, then a wrong offset, then the rest
	u2, f, err := s.AppendUpload(ctx, owner, u.ID, 0, bytes.NewReader(data[:3000]))
	if err != nil || f != nil || u2.Received != 3000 {
		t.Fatalf("chunk 1: %v %+v", err, u2)
	}
	if _, _, err := s.AppendUpload(ctx, owner, u.ID, 100, bytes.NewReader(data[100:200])); !errors.Is(err, ErrConflict) {
		t.Fatalf("wrong offset: %v", err)
	}
	_, f, err = s.AppendUpload(ctx, owner, u.ID, 3000, bytes.NewReader(data[3000:]))
	if err != nil || f == nil || f.Size != int64(len(data)) || f.Name != "Handbuch.txt" {
		t.Fatalf("finish: %v %+v", err, f)
	}
	fc, _ := s.OpenFile(ctx, owner, f.ID, 0, false)
	got, _ := io.ReadAll(fc.Reader)
	fc.Reader.Close()
	if !bytes.Equal(got, data) {
		t.Fatal("content differs")
	}
	if _, err := s.Upload(ctx, owner, u.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("finished upload must be gone: %v", err)
	}
	// a new version through a resumable upload
	u, _ = s.StartUpload(ctx, owner, p.Slug, 3, UploadMeta{File: f.ID})
	if _, nf, err := s.AppendUpload(ctx, owner, u.ID, 0, strings.NewReader("neu")); err != nil || nf.Version != 2 || nf.Name != "Handbuch.txt" {
		t.Fatalf("version upload: %v %+v", err, nf)
	}
}

func pngImage(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 64, 32))
	for i := range img.Pix {
		img.Pix[i] = 200
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
