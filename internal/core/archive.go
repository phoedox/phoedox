package core

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"github.com/phoedox/phoedox/internal/store"
)

// Archiving vendor documentation: the target of a link page (a manual, a
// data sheet) is downloaded once and kept as a project file, so it stays
// available when the vendor takes it offline.

const maxArchive = 100 << 20

// ArchiveLink downloads the target of a link page and attaches it (editors).
func (s *Service) ArchiveLink(ctx context.Context, a Actor, ref, docRef string) (DocResult, error) {
	p, doc, err := s.docAccess(ctx, a, ref, docRef, store.RoleEditor)
	if err != nil {
		return DocResult{}, err
	}
	if a.Via == "mcp" && p.AIWriteMode != "direct" {
		return DocResult{}, ErrForbidden
	}
	if doc.Type != store.DocLink {
		return DocResult{}, invalid("validation.archive_link", nil, "only link pages can be archived")
	}
	var link Link
	_ = json.Unmarshal(doc.Content, &link)
	f, err := s.downloadArchive(ctx, a, p, doc.ID, link.URL)
	if err != nil {
		return DocResult{}, invalid("validation.archive_failed", map[string]any{"reason": err.Error()}, "archive: %v", err)
	}
	link.Archive = &LinkArchive{File: f.ID, At: store.Now(), Mime: f.Mime}
	res, err := s.publishPatch(ctx, a, p, doc, DocumentPatch{Link: &link})
	if err == nil {
		s.event(ctx, a, "document.archive", doc.ID, map[string]any{"url": link.URL, "file": f.ID})
	}
	return res, err
}

// ArchiveAllLinks archives every link page of a project that has no copy yet
// (owners); it returns how many were archived and which failed.
func (s *Service) ArchiveAllLinks(ctx context.Context, a Actor, ref string) (map[string]any, error) {
	p, err := s.project(ctx, a, ref, store.RoleOwner)
	if err != nil {
		return nil, err
	}
	docs, err := s.DB.ListDocuments(ctx, p.ID, store.DocFilter{Type: store.DocLink})
	if err != nil {
		return nil, err
	}
	done, failed := 0, []string{}
	for _, d := range docs {
		full, err := s.DB.GetDocument(ctx, p.ID, d.ID)
		if err != nil {
			continue
		}
		var link Link
		_ = json.Unmarshal(full.Content, &link)
		if link.Archive != nil {
			continue
		}
		if _, err := s.ArchiveLink(ctx, a, ref, d.ID); err != nil {
			failed = append(failed, d.Slug)
			continue
		}
		done++
	}
	return map[string]any{"archived": done, "failed": failed}, nil
}

func (s *Service) downloadArchive(ctx context.Context, a Actor, p store.Project, docID, raw string) (store.File, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return store.File{}, fmt.Errorf("not an http(s) address")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return store.File{}, err
	}
	req.Header.Set("User-Agent", "Phoedox archive")
	res, err := s.RequestPolicy.Client().Do(req)
	if err != nil {
		return store.File{}, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return store.File{}, fmt.Errorf("the address answered %s", res.Status)
	}
	if res.ContentLength > maxArchive {
		return store.File{}, fmt.Errorf("larger than %d MB", maxArchive>>20)
	}
	sp, err := s.spool(io.LimitReader(res.Body, maxArchive))
	if err != nil {
		return store.File{}, err
	}
	defer os.Remove(sp.path)
	name := path.Base(u.Path)
	if name == "" || name == "/" || name == "." {
		name = u.Host
	}
	// Name by content type when the address has no extension (e.g. HTML pages).
	if path.Ext(name) == "" {
		ct, _, _ := mime.ParseMediaType(res.Header.Get("Content-Type"))
		if exts, _ := mime.ExtensionsByType(ct); len(exts) > 0 {
			name += exts[len(exts)-1]
		}
	}
	return s.ingest(ctx, a, p, sp, cleanName(strings.TrimSpace(name)), docID, "", nil)
}
