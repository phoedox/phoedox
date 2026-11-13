package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/phoedox/phoedox/internal/blob"
	"github.com/phoedox/phoedox/internal/jobs"
	"github.com/phoedox/phoedox/internal/preview"
	"github.com/phoedox/phoedox/internal/store"
)

// JobFileProcess makes the preview and extracts text of a file version.
const JobFileProcess = "file.process"

type fileJob struct {
	File    string `json:"file"`
	Version int    `json:"version"`
}

// RegisterJobs connects the service's background work to the queue.
func (s *Service) RegisterJobs(q *jobs.Queue) {
	s.Jobs = q
	s.registerWebhookJobs(q)
	s.registerPluginJobs()
	q.Handle(JobFileProcess, func(ctx context.Context, payload json.RawMessage) error {
		var j fileJob
		if err := json.Unmarshal(payload, &j); err != nil {
			return jobs.Permanent(err)
		}
		return s.processFile(ctx, j.File, j.Version)
	})
}

func (s *Service) queueProcessing(ctx context.Context, f store.File) {
	if s.Jobs == nil {
		if err := s.processFile(ctx, f.ID, f.Version); err != nil {
			slog.Warn("processing a file failed", "file", f.ID, "err", err)
		}
		return
	}
	if err := s.Jobs.Add(ctx, JobFileProcess, fileJob{File: f.ID, Version: f.Version}); err != nil {
		slog.Warn("queueing file processing failed", "file", f.ID, "err", err)
	}
}

// processFile makes the preview image and indexes the text of one version.
// It is idempotent and skips files that changed or went to the trash.
func (s *Service) processFile(ctx context.Context, id string, version int) error {
	f, err := s.DB.GetFile(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if f.Version != version || s.Blobs == nil {
		return nil
	}
	tmp, err := s.blobToTemp(ctx, f.SHA256)
	if err != nil {
		return err
	}
	defer os.Remove(tmp)

	thumb := ""
	if img, _, err := s.Preview.Thumbnail(ctx, tmp, f.Mime); err == nil {
		if h, err := s.storeBytes(ctx, img); err == nil {
			thumb = h
		} else {
			return err
		}
	} else if !errors.Is(err, preview.ErrUnsupported) {
		slog.Info("no preview", "file", f.ID, "err", err)
	}

	status := "done"
	text, err := s.Preview.Text(ctx, tmp, f.Mime)
	switch {
	case errors.Is(err, preview.ErrUnsupported):
		status = "none"
	case err != nil:
		slog.Info("text extraction failed", "file", f.ID, "err", err)
		status = "failed"
	case text == "":
		status = "none"
	}
	if err := s.DB.IndexPut(ctx, store.IndexEntry{RefID: f.ID, Kind: "file", ProjectID: f.ProjectID, Slug: f.ID, Subtype: f.Mime, Title: f.Name, Body: text}); err != nil {
		return err
	}
	return s.DB.SetFileProcessed(ctx, f.ID, f.Version, thumb, status)
}

func (s *Service) blobToTemp(ctx context.Context, hash string) (string, error) {
	rc, err := s.Blobs.Open(ctx, hash)
	if err != nil {
		return "", err
	}
	defer rc.Close()
	if err := os.MkdirAll(s.TempDir, 0o700); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(s.TempDir, "work-*")
	if err != nil {
		return "", err
	}
	defer tmp.Close()
	if _, err := io.Copy(tmp, rc); err != nil {
		os.Remove(tmp.Name())
		return "", err
	}
	return tmp.Name(), nil
}

func (s *Service) storeBytes(ctx context.Context, b []byte) (string, error) {
	path, hash, size, _, err := blob.Spool(s.TempDir, bytes.NewReader(b), int64(len(b))+1)
	if err != nil {
		return "", err
	}
	defer os.Remove(path)
	return hash, s.Blobs.Put(ctx, hash, path, size)
}

// ReprocessFiles queues preview and text extraction for all files (after
// installing OCR or when rebuilding the search index).
func (s *Service) ReprocessFiles(ctx context.Context) (int, error) {
	fs, err := s.DB.AllFiles(ctx)
	if err != nil {
		return 0, err
	}
	for _, f := range fs {
		s.queueProcessing(ctx, f)
	}
	return len(fs), nil
}

// CleanupUploads removes expired resumable uploads.
func (s *Service) CleanupUploads(ctx context.Context) {
	ids, err := s.DB.ExpiredUploads(ctx)
	if err != nil {
		return
	}
	for _, id := range ids {
		_ = os.Remove(s.uploadPath(id))
		_ = s.DB.DeleteUpload(ctx, id)
	}
}

func (s *Service) uploadPath(id string) string { return filepath.Join(s.TempDir, "tus", id) }

var uploadTTL = 24 * time.Hour
