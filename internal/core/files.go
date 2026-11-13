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
	"github.com/phoedox/phoedox/internal/scan"
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
	p, docID, entID, err := s.uploadTarget(ctx, a, ref, in)
	if err != nil {
		return store.File{}, err
	}
	sp, err := s.spool(r)
	if err != nil {
		return store.File{}, err
	}
	defer os.Remove(sp.path)
	return s.ingest(ctx, a, p, sp, cleanName(in.Name), docID, entID, nil)
}

// NewFileVersion stores new content for an existing file.
func (s *Service) NewFileVersion(ctx context.Context, a Actor, ref, id string, r io.Reader) (store.File, error) {
	f, err := s.fileIn(ctx, a, ref, id, store.RoleEditor)
	if err != nil {
		return f, err
	}
	p, err := s.project(ctx, a, ref, store.RoleEditor)
	if err != nil {
		return f, err
	}
	sp, err := s.spool(r)
	if err != nil {
		return store.File{}, err
	}
	defer os.Remove(sp.path)
	return s.ingest(ctx, a, p, sp, f.Name, "", "", &f)
}

func (s *Service) uploadTarget(ctx context.Context, a Actor, ref string, in FileInput) (store.Project, string, string, error) {
	p, err := s.project(ctx, a, ref, store.RoleEditor)
	if err != nil {
		return p, "", "", err
	}
	if a.Via == "mcp" {
		return p, "", "", ErrForbidden
	}
	if s.Blobs == nil {
		return p, "", "", errNoBlobs
	}
	docID, entID, err := s.attachTargets(ctx, p, in.Document, in.Entity)
	return p, docID, entID, err
}

type spooled struct {
	path, hash string
	size       int64
	head       []byte
}

func (s *Service) maxUpload() int64 {
	if s.MaxUpload <= 0 {
		return 100 << 20
	}
	return s.MaxUpload
}

func errTooLarge(max int64) error {
	return invalid("validation.file_too_large", map[string]any{"max": max >> 20}, "the file is larger than %d MB", max>>20)
}

func (s *Service) spool(r io.Reader) (spooled, error) {
	max := s.maxUpload()
	path, hash, size, head, err := blob.Spool(s.TempDir, r, max)
	if errors.Is(err, blob.ErrTooLarge) {
		return spooled{}, errTooLarge(max)
	}
	return spooled{path, hash, size, head}, err
}

// quotaBytes is the effective storage limit of a project (0 = none): the
// instance limit, lowered by the project's own setting.
func (s *Service) quotaBytes(ctx context.Context, p store.Project) int64 {
	limit := s.instanceQuotaMB(ctx)
	lower := func(mb int) {
		if mb > 0 && (limit == 0 || mb < limit) {
			limit = mb
		}
	}
	if p.OrgID != "" {
		if o, err := s.DB.OrgFor(ctx, p.OrgID, ""); err == nil {
			lower(o.QuotaMB)
		}
	}
	if own, err := s.DB.ProjectQuota(ctx, p.ID); err == nil {
		lower(own)
	}
	return int64(limit) << 20
}

func (s *Service) checkQuota(ctx context.Context, p store.Project, add int64) error {
	limit := s.quotaBytes(ctx, p)
	if limit == 0 {
		return nil
	}
	used, err := s.DB.FileUsage(ctx, p.ID)
	if err != nil {
		return err
	}
	if used+add > limit {
		return invalid("validation.quota_exceeded", map[string]any{"used": used >> 20, "limit": limit >> 20}, "the project's storage is full (%d of %d MB)", used>>20, limit>>20)
	}
	return nil
}

// ingest checks, stores and registers spooled content as a new file or a
// new version of existing, then queues preview and text extraction.
func (s *Service) ingest(ctx context.Context, a Actor, p store.Project, sp spooled, name, docID, entID string, existing *store.File) (store.File, error) {
	if sp.size == 0 {
		return store.File{}, invalid("validation.file_empty", nil, "the file is empty")
	}
	if s.Scanner != nil {
		var inf scan.ErrInfected
		if err := s.Scanner.Scan(ctx, sp.path); errors.As(err, &inf) {
			s.event(ctx, a, "file.infected", p.ID, map[string]string{"name": name, "signature": inf.Signature})
			return store.File{}, invalid("validation.file_infected", map[string]any{"signature": inf.Signature}, "the file contains malware (%s)", inf.Signature)
		} else if err != nil {
			slog.Error("virus scan failed", "err", err)
			return store.File{}, invalid("validation.file_scan_failed", nil, "the file could not be checked for malware")
		}
	}
	if err := s.checkQuota(ctx, p, sp.size); err != nil {
		return store.File{}, err
	}
	if err := s.Blobs.Put(ctx, sp.hash, sp.path, sp.size); err != nil {
		return store.File{}, err
	}
	mimeType := sniffMime(name, sp.head)
	var f store.File
	var err error
	if existing != nil {
		f, err = s.DB.AddFileVersion(ctx, existing.ID, store.FileVersion{SHA256: sp.hash, Mime: mimeType, Size: sp.size, CreatedBy: a.User.ID})
		if err == nil {
			s.event(ctx, a, "file.version", f.ID, map[string]any{"version": f.Version, "size": f.Size})
		}
	} else {
		f, err = s.DB.CreateFile(ctx, store.File{ID: newID(), ProjectID: p.ID, SHA256: sp.hash, Name: name, Mime: mimeType, Size: sp.size,
			DocumentID: docID, EntityID: entID, CreatedBy: a.User.ID})
		if err == nil {
			s.event(ctx, a, "file.upload", f.ID, map[string]any{"name": f.Name, "size": f.Size, "document": docID, "entity": entID})
		}
	}
	if err != nil {
		return f, err
	}
	s.queueProcessing(ctx, f)
	return f, nil
}

// Files lists files of a project, optionally those of one document or entity.
func (s *Service) Files(ctx context.Context, a Actor, ref string, ff FileFilter) ([]store.File, error) {
	p, err := s.project(ctx, a, ref, store.RoleDoc)
	if err != nil {
		return nil, err
	}
	docID, entID, err := s.attachTargets(ctx, p, ff.Document, ff.Entity)
	if err != nil {
		return nil, err
	}
	fs, err := s.DB.ListFiles(ctx, p.ID, store.FileFilter{DocumentID: docID, EntityID: entID})
	if err != nil {
		return nil, err
	}
	allowed, err := s.fileRule(ctx, a, p)
	if err != nil {
		return nil, err
	}
	out := fs[:0]
	for _, f := range fs {
		if allowed(f) {
			out = append(out, f)
		}
	}
	return out, nil
}

// fileRule tells which files of a project the actor may read: files of a
// page follow the page's rights, others need read access to the project.
func (s *Service) fileRule(ctx context.Context, a Actor, p store.Project) (func(store.File) bool, error) {
	see, err := s.docFilter(ctx, a, p)
	if err != nil {
		return nil, err
	}
	docs, err := s.DB.ListDocuments(ctx, p.ID, store.DocFilter{})
	if err != nil {
		return nil, err
	}
	restricted := map[string]bool{}
	live := map[string]bool{}
	for _, d := range docs {
		live[d.ID] = true
		restricted[d.ID] = d.Restricted
	}
	projectReader := rank[p.Role] >= rank[store.RoleReader]
	return func(f store.File) bool {
		if f.DocumentID != "" && live[f.DocumentID] {
			return see(f.DocumentID, restricted[f.DocumentID])
		}
		return projectReader
	}, nil
}

// FileContent is what OpenFile serves.
type FileContent struct {
	File   store.File
	Mime   string
	SHA256 string
	Reader blob.ReadSeekCloser
}

// OpenFile returns a file's content if the actor may read its project:
// the current version, an older one (version > 0) or the preview image.
func (s *Service) OpenFile(ctx context.Context, a Actor, id string, version int, preview bool) (FileContent, error) {
	f, err := s.DB.GetFile(ctx, id)
	if err != nil {
		return FileContent{}, err
	}
	p, err := s.project(ctx, a, f.ProjectID, store.RoleDoc)
	if err != nil {
		return FileContent{}, err
	}
	if allowed, err := s.fileRule(ctx, a, p); err != nil {
		return FileContent{}, err
	} else if !allowed(f) {
		return FileContent{}, ErrNotFound
	}
	if s.Blobs == nil {
		return FileContent{}, errNoBlobs
	}
	out := FileContent{File: f, Mime: f.Mime, SHA256: f.SHA256}
	switch {
	case preview:
		if f.ThumbSHA256 == "" {
			return out, ErrNotFound
		}
		out.SHA256, out.Mime = f.ThumbSHA256, "" // JPEG or PNG, detected when served
	case version > 0 && version != f.Version:
		v, err := s.DB.GetFileVersion(ctx, f.ID, version)
		if err != nil {
			return out, err
		}
		out.SHA256, out.Mime = v.SHA256, v.Mime
	}
	rc, err := s.Blobs.Open(ctx, out.SHA256)
	if errors.Is(err, blob.ErrNotFound) {
		slog.Error("file content missing", "file", f.ID, "sha256", out.SHA256)
		return out, ErrNotFound
	}
	out.Reader = rc
	return out, err
}

// FileVersions lists the versions of a file (readers).
func (s *Service) FileVersions(ctx context.Context, a Actor, ref, id string) ([]store.FileVersion, error) {
	f, err := s.fileIn(ctx, a, ref, id, store.RoleReader)
	if err != nil {
		return nil, err
	}
	return s.DB.FileVersions(ctx, f.ID)
}

// RestoreFileVersion makes an older version the current one (as a new version).
func (s *Service) RestoreFileVersion(ctx context.Context, a Actor, ref, id string, version int) (store.File, error) {
	f, err := s.fileIn(ctx, a, ref, id, store.RoleEditor)
	if err != nil {
		return f, err
	}
	v, err := s.DB.GetFileVersion(ctx, f.ID, version)
	if err != nil {
		return f, err
	}
	p, err := s.project(ctx, a, ref, store.RoleEditor)
	if err != nil {
		return f, err
	}
	if err := s.checkQuota(ctx, p, v.Size); err != nil {
		return f, err
	}
	nf, err := s.DB.AddFileVersion(ctx, f.ID, store.FileVersion{SHA256: v.SHA256, Mime: v.Mime, Size: v.Size, CreatedBy: a.User.ID})
	if err != nil {
		return nf, err
	}
	s.event(ctx, a, "file.version_restore", f.ID, map[string]int{"from": version, "version": nf.Version})
	s.queueProcessing(ctx, nf)
	return nf, nil
}

// Storage is the storage use and limit of a project.
type Storage struct {
	UsedBytes  int64 `json:"usedBytes"`
	LimitBytes int64 `json:"limitBytes"` // effective limit, 0 = none
	ProjectMB  int   `json:"projectMb"`  // the project's own setting (0 = instance limit)
	MaxMB      int   `json:"maxMb"`      // instance limit (0 = none)
}

// ProjectStorage returns the storage use of a project (readers).
func (s *Service) ProjectStorage(ctx context.Context, a Actor, ref string) (Storage, error) {
	p, err := s.project(ctx, a, ref, store.RoleReader)
	if err != nil {
		return Storage{}, err
	}
	used, err := s.DB.FileUsage(ctx, p.ID)
	if err != nil {
		return Storage{}, err
	}
	own, _ := s.DB.ProjectQuota(ctx, p.ID)
	return Storage{UsedBytes: used, LimitBytes: s.quotaBytes(ctx, p), ProjectMB: own, MaxMB: s.instanceQuotaMB(ctx)}, nil
}

// SetProjectQuota sets a project's own storage limit (owners); it may only
// stay below the instance limit.
func (s *Service) SetProjectQuota(ctx context.Context, a Actor, ref string, mb int) (Storage, error) {
	p, err := s.project(ctx, a, ref, store.RoleOwner)
	if err != nil {
		return Storage{}, err
	}
	if a.Via == "mcp" {
		return Storage{}, ErrForbidden
	}
	max := s.instanceQuotaMB(ctx)
	if mb < 0 || (max > 0 && mb > max) {
		return Storage{}, invalid("validation.quota_above_max", map[string]any{"max": max}, "the quota must be between 0 and %d MB", max)
	}
	if err := s.DB.SetProjectQuota(ctx, p.ID, mb); err != nil {
		return Storage{}, err
	}
	s.event(ctx, a, "project.quota", p.ID, map[string]int{"mb": mb})
	return s.ProjectStorage(ctx, a, ref)
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
	s.unindex(ctx, f.ID)
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
	if allowed, err := s.fileRule(ctx, a, p); err != nil {
		return f, err
	} else if !allowed(f) {
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
