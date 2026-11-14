package core

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/phoedox/phoedox/internal/blob"
	"github.com/phoedox/phoedox/internal/mail"
	"github.com/phoedox/phoedox/internal/store"
)

func TestPersonalExport(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, _ := setup(t)
	dir := t.TempDir()
	s.Blobs, s.TempDir = blob.Local{Dir: filepath.Join(dir, "files")}, filepath.Join(dir, "tmp")
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Haus"})
	_ = s.SetMember(ctx, owner, p.Slug, reader.User.Email, store.RoleEditor)
	doc, _ := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "markdown", Title: "Netz", Markdown: "x"})
	if _, err := s.AddComment(ctx, reader, p.Slug, doc.Document.Slug, CommentInput{Body: "Router ist alt"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UploadFile(ctx, reader, p.Slug, FileInput{Name: "plan.txt"}, strings.NewReader("Grundriss")); err != nil {
		t.Fatal(err)
	}

	// Without mail delivery there is no export (the link goes out by mail).
	if _, err := s.RequestExport(ctx, reader); err == nil {
		t.Fatal("export without mail")
	}
	mails := &mail.Capture{}
	s.Mail, s.PublicURL = mails, "https://phx.test"
	if _, err := s.RequestExport(ctx, Actor{User: reader.User, Via: "mcp"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("AI clients must not export: %v", err)
	}
	e, err := s.RequestExport(ctx, reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RequestExport(ctx, reader); err == nil {
		t.Fatal("second export within 24 h")
	}
	if m, ok := mails.Last(); !ok || m.To != reader.User.Email || !strings.Contains(m.Body, "https://phx.test/settings/account") {
		t.Fatalf("mail: %+v", m)
	}
	if _, _, err := s.ExportDownload(ctx, owner, e.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("someone else's export: %v", err)
	}
	_, rc, err := s.ExportDownload(ctx, reader, e.ID)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(rc)
	rc.Close()
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for _, f := range z.File {
		r, _ := f.Open()
		b, _ := io.ReadAll(r)
		r.Close()
		files[f.Name] = string(b)
	}
	if !strings.Contains(files["account/profile.json"], reader.User.Email) || !strings.Contains(files["activity/comments.json"], "Router ist alt") ||
		!strings.Contains(files["memberships/projects.json"], `"editor"`) || files["README.txt"] == "" {
		t.Fatalf("export content: %v", keys(files))
	}
	found := false
	for name, body := range files {
		if strings.HasPrefix(name, "files/haus/") && strings.HasSuffix(name, "plan.txt") && body == "Grundriss" {
			found = true
		}
		if strings.Contains(body, "password_hash") || strings.Contains(body, "token_hash") || strings.Contains(body, "secret_enc") {
			t.Fatalf("%s contains a secret column", name)
		}
	}
	if !found {
		t.Fatalf("uploaded file missing: %v", keys(files))
	}

	// After 7 days the export and its file are gone.
	s.PruneExports(ctx, time.Now().Add(8*24*time.Hour))
	if _, _, err := s.ExportDownload(ctx, reader, e.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired export: %v", err)
	}
}

func keys(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestDeleteAccount(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, stranger := setup(t)
	mails := &mail.Capture{}
	s.Mail, s.PublicURL, s.DefaultLocale, s.BackupDays = mails, "https://phx.test", "de", 14
	shared, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Gemeinsam"})
	_ = s.SetMember(ctx, owner, shared.Slug, reader.User.Email, store.RoleEditor)
	doc, _ := s.CreateDocument(ctx, owner, shared.Slug, DocumentInput{Type: "markdown", Title: "Netz", Markdown: "x"})
	c, _ := s.AddComment(ctx, owner, shared.Slug, doc.Document.Slug, CommentInput{Body: "Hallo"})
	solo, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Privat"})

	// The only owner of a project with other members cannot leave.
	plan, err := s.AccountDeletion(ctx, owner)
	if err != nil || len(plan.Blockers) != 1 || plan.Blockers[0].Slug != shared.Slug || len(plan.Deletes) != 1 || plan.Deletes[0].Slug != solo.Slug {
		t.Fatalf("plan: %+v %v", plan, err)
	}
	if _, err := s.DeleteAccount(ctx, owner, owner.User.ID); err == nil {
		t.Fatal("deleted while sole owner")
	}
	// With a second owner the project stays and moves to them.
	_ = s.SetMember(ctx, owner, shared.Slug, stranger.User.Email, store.RoleOwner)
	plan, err = s.DeleteAccount(ctx, owner, owner.User.ID)
	if err != nil {
		t.Fatalf("delete: %v %+v", err, plan)
	}

	u, _ := s.DB.UserByID(ctx, owner.User.ID)
	if !u.Deleted || u.Name != "Gelöschte Person" || strings.Contains(u.Email, "owner@") || u.HasPassword {
		t.Fatalf("anonymised: %+v", u)
	}
	if _, err := s.Project(ctx, stranger, solo.Slug); !errors.Is(err, ErrNotFound) {
		t.Fatalf("own project without members must be gone: %v", err)
	}
	p, err := s.Project(ctx, stranger, shared.Slug)
	if err != nil || p.OwnerID != stranger.User.ID {
		t.Fatalf("shared project kept and moved: %+v %v", p, err)
	}
	cs, err := s.Comments(ctx, reader, shared.Slug, doc.Document.Slug)
	if err != nil || len(cs) != 1 || cs[0].ID != c.ID {
		t.Fatalf("comment kept: %+v %v", cs, err)
	}
	if m, ok := mails.Last(); !ok || m.To != "owner@x.de" || !strings.Contains(m.Body, "14 Tage") {
		t.Fatalf("confirmation mail: %+v", m)
	}
	if _, err := s.DeleteAccount(ctx, owner, owner.User.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete: %v", err)
	}
	// The address can be used for a new account.
	if _, err := s.DB.CreateUser(ctx, store.User{ID: newID(), Email: "owner@x.de", Name: "Neu"}); err != nil {
		t.Fatalf("address free again: %v", err)
	}
}
