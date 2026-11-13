package core

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path"
	"strings"
	"unicode"

	"github.com/phoedox/phoedox/internal/blob"
	"github.com/phoedox/phoedox/internal/store"
)

// FileInput describes an upload: its name and where to attach it.
type FileInput struct {
	Name     string `json:"name"`
	Document string `json:"document,omitempty"` // slug or id
	Entity   string `json:"entity,omitempty"`   // slug or id
}

// FileFilter selects files by document or entity (slug or id).
type FileFilter struct {
	Document string
	Entity   string
}

var errNoBlobs = errors.New("core: no file storage configured")

// cleanName keeps a safe display name: no paths, no control characters.
func cleanName(name string) string {
	name = path.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == '"' {
			return -1
		}
		return r
	}, strings.TrimSpace(name))
	if name == "" || name == "." || name == "/" {
		name = "file"
	}
	if len(name) > 200 {
		ext := path.Ext(name)
		if len(ext) > 20 {
			ext = ""
		}
		name = strings.ToValidUTF8(name[:200-len(ext)], "") + ext
	}
	return name
}

// sniffMime trusts the content first and the file name only for generic
// results; the client's Content-Type is never used.
func sniffMime(name string, head []byte) string {
	m := http.DetectContentType(head)
	if m == "application/octet-stream" || strings.HasPrefix(m, "text/plain") || strings.HasPrefix(m, "text/xml") {
		if byExt := mime.TypeByExtension(strings.ToLower(path.Ext(name))); byExt != "" {
			// A text file named .html or .svg stays text unless it really looks like markup.
			if !strings.HasPrefix(m, "text/plain") || !strings.Contains(byExt, "html") {
				return byExt
			}
		}
	}
	return m
}

// attachTargets resolves document and entity references inside the project.
func (s *Service) attachTargets(ctx context.Context, p store.Project, doc, ent string) (string, string, error) {
	var docID, entID string
	if doc != "" {
		d, err := s.DB.GetDocument(ctx, p.ID, doc)
		if err != nil {
			return "", "", err
		}
		docID = d.ID
	}
	if ent != "" {
		e, err := s.DB.GetEntity(ctx, p.ID, ent)
		if err != nil {
			return "", "", err
		}
		entID = e.ID
	}
	return docID, entID, nil
}

// UploadFile stores a file in a project (editors). The content is read from r
// up to MaxUpload bytes and stored once per hash.
func (s *Service) UploadFile(ctx context.Context, a Actor, ref string, in FileInput, r io.Reader) (store.File, error) {
	p, err := s.project(ctx, a, ref, store.RoleEditor)
	if err != nil {
		return store.File{}, err
	}
	if a.Via == "mcp" {
		return store.File{}, ErrForbidden
	}
	if s.Blobs == nil {
		return store.File{}, errNoBlobs
	}
	docID, entID, err := s.attachTargets(ctx, p, in.Document, in.Entity)
	if err != nil {
		return store.File{}, err
	}
	max := s.MaxUpload
	if max <= 0 {
		max = 100 << 20
	}
	tmp, hash, size, head, err := blob.Spool(s.TempDir, r, max)
	if errors.Is(err, blob.ErrTooLarge) {
		return store.File{}, invalid("validation.file_too_large", map[string]any{"max": max >> 20}, "the file is larger than %d MB", max>>20)
	}
	if err != nil {
		return store.File{}, err
	}
	defer os.Remove(tmp)
	if size == 0 {
		return store.File{}, invalid("validation.file_empty", nil, "the file is empty")
	}
	if err := s.Blobs.Put(ctx, hash, tmp, size); err != nil {
		return store.File{}, err
	}
	name := cleanName(in.Name)
	f, err := s.DB.CreateFile(ctx, store.File{ID: newID(), ProjectID: p.ID, SHA256: hash, Name: name, Mime: sniffMime(name, head), Size: size,
		DocumentID: docID, EntityID: entID, CreatedBy: a.User.ID})
	if err != nil {
		return f, err
	}
	s.event(ctx, a, "file.upload", f.ID, map[string]any{"name": f.Name, "size": f.Size, "document": docID, "entity": entID})
	return f, nil
}

// Files lists files of a project, optionally those of one document or entity.
func (s *Service) Files(ctx context.Context, a Actor, ref string, ff FileFilter) ([]store.File, error) {
	p, err := s.project(ctx, a, ref, store.RoleReader)
	if err != nil {
		return nil, err
	}
	docID, entID, err := s.attachTargets(ctx, p, ff.Document, ff.Entity)
	if err != nil {
		return nil, err
	}
	return s.DB.ListFiles(ctx, p.ID, store.FileFilter{DocumentID: docID, EntityID: entID})
}

// OpenFile returns a file and its content if the actor may read its project.
func (s *Service) OpenFile(ctx context.Context, a Actor, id string) (store.File, blob.ReadSeekCloser, error) {
	f, err := s.DB.GetFile(ctx, id)
	if err != nil {
		return f, nil, err
	}
	if _, err := s.project(ctx, a, f.ProjectID, store.RoleReader); err != nil {
		return f, nil, err
	}
	if s.Blobs == nil {
		return f, nil, errNoBlobs
	}
	rc, err := s.Blobs.Open(ctx, f.SHA256)
	if errors.Is(err, blob.ErrNotFound) {
		slog.Error("file content missing", "file", f.ID, "sha256", f.SHA256)
		return f, nil, ErrNotFound
	}
	return f, rc, err
}

// RenameFile changes the display name of a file (editors).
func (s *Service) RenameFile(ctx context.Context, a Actor, ref, id, name string) (store.File, error) {
	f, err := s.fileIn(ctx, a, ref, id, store.RoleEditor)
	if err != nil {
		return f, err
	}
	f.Name = cleanName(name)
	if err := s.DB.RenameFile(ctx, f.ID, f.Name); err != nil {
		return f, err
	}
	s.event(ctx, a, "file.rename", f.ID, map[string]string{"name": f.Name})
	return f, nil
}

// DeleteFile moves a file to the trash (editors).
func (s *Service) DeleteFile(ctx context.Context, a Actor, ref, id string) error {
	f, err := s.fileIn(ctx, a, ref, id, store.RoleEditor)
	if err != nil {
		return err
	}
	if err := s.DB.DeleteFile(ctx, f.ID); err != nil {
		return err
	}
	s.event(ctx, a, "file.delete", f.ID, nil)
	return nil
}

func (s *Service) fileIn(ctx context.Context, a Actor, ref, id, need string) (store.File, error) {
	p, err := s.project(ctx, a, ref, need)
	if err != nil {
		return store.File{}, err
	}
	if a.Via == "mcp" && need != store.RoleReader {
		return store.File{}, ErrForbidden
	}
	f, err := s.DB.GetFile(ctx, id)
	if err != nil || f.ProjectID != p.ID {
		return store.File{}, ErrNotFound
	}
	return f, nil
}

// freeBlobs deletes contents no file row refers to any more.
func (s *Service) freeBlobs(ctx context.Context, hashes []string) {
	if s.Blobs == nil {
		return
	}
	for _, h := range hashes {
		used, err := s.DB.HashInUse(ctx, h)
		if err != nil || used {
			continue
		}
		if err := s.Blobs.Delete(ctx, h); err != nil {
			slog.Warn("deleting file content failed", "sha256", h, "err", err)
		}
	}
}
