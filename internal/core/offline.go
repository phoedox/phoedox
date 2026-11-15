package core

import (
	"context"
	"io"

	"github.com/phoedox/phoedox/internal/store"
)

// The offline emergency copy: a project as static HTML pages in a ZIP that
// opens without a server, e.g. from a USB stick or a NAS share when the
// instance itself is down. It holds exactly what the person may read.

// OfflineData is the content of an offline copy.
type OfflineData struct {
	Project  store.Project
	Docs     []store.Document
	Tasks    map[string][]store.Task // per board document id
	Entities []store.Entity
	Edges    []store.Edge
	Files    []store.File
	Model    Model
}

// MaxOfflineFiles limits the size of the attachments in an offline copy.
const MaxOfflineFiles = 500 << 20

// Offline collects the content of a project for an offline copy (people
// signed in on the web or with a token; not AI clients).
func (s *Service) Offline(ctx context.Context, a Actor, ref string) (OfflineData, error) {
	var d OfflineData
	if a.Via == "mcp" {
		return d, ErrForbidden
	}
	p, err := s.project(ctx, a, ref, store.RoleReader)
	if err != nil {
		return d, err
	}
	d.Project = p
	if d.Docs, err = s.Documents(ctx, a, ref, store.DocFilter{WithContent: true}); err != nil {
		return d, err
	}
	d.Tasks = map[string][]store.Task{}
	for _, doc := range d.Docs {
		if doc.Type == store.DocBoard {
			ts, err := s.Tasks(ctx, a, ref, doc.ID, "", "", false)
			if err != nil {
				return d, err
			}
			d.Tasks[doc.ID] = ts
		}
	}
	if d.Entities, err = s.Entities(ctx, a, ref, ""); err != nil {
		return d, err
	}
	if d.Edges, err = s.DB.EdgesOfProject(ctx, p.ID); err != nil {
		return d, err
	}
	if s.Blobs != nil {
		files, err := s.Files(ctx, a, ref, FileFilter{})
		if err != nil {
			return d, err
		}
		var total int64
		for _, f := range files {
			if total+f.Size > MaxOfflineFiles {
				continue
			}
			total += f.Size
			d.Files = append(d.Files, f)
		}
	}
	if d.Model, err = s.DataModel(ctx, a, ref); err != nil {
		return d, err
	}
	s.event(ctx, a, "project.offline_copy", p.ID, map[string]any{"documents": len(d.Docs), "entities": len(d.Entities), "files": len(d.Files)})
	return d, nil
}

// OpenFile opens the content of a file of an offline copy.
func (s *Service) OfflineFile(ctx context.Context, f store.File) (io.ReadCloser, error) {
	if s.Blobs == nil {
		return nil, errNoBlobs
	}
	return s.Blobs.Open(ctx, f.SHA256)
}
