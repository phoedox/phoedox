package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/phoedox/phoedox/internal/store"
)

// UploadMeta is what a resumable upload becomes once complete.
type UploadMeta struct {
	Name     string `json:"name"`
	Document string `json:"document,omitempty"`
	Entity   string `json:"entity,omitempty"`
	File     string `json:"file,omitempty"` // new version of this file
}

// StartUpload begins a resumable (tus) upload of length bytes (editors).
func (s *Service) StartUpload(ctx context.Context, a Actor, ref string, length int64, meta UploadMeta) (store.Upload, error) {
	p, _, _, err := s.uploadTarget(ctx, a, ref, FileInput{Name: meta.Name, Document: meta.Document, Entity: meta.Entity})
	if err != nil {
		return store.Upload{}, err
	}
	if meta.File != "" {
		if _, err := s.fileIn(ctx, a, ref, meta.File, store.RoleEditor); err != nil {
			return store.Upload{}, err
		}
	}
	if length <= 0 {
		return store.Upload{}, invalid("validation.file_empty", nil, "the file is empty")
	}
	if max := s.maxUpload(); length > max {
		return store.Upload{}, errTooLarge(max)
	}
	if err := s.checkQuota(ctx, p, length); err != nil {
		return store.Upload{}, err
	}
	b, _ := json.Marshal(meta)
	exp := store.Now()
	exp.Time = exp.Add(uploadTTL)
	u := store.Upload{ID: newID(), ProjectID: p.ID, UserID: a.User.ID, Length: length, Meta: b, ExpiresAt: exp}
	if err := os.MkdirAll(filepath.Dir(s.uploadPath(u.ID)), 0o700); err != nil {
		return u, err
	}
	if err := os.WriteFile(s.uploadPath(u.ID), nil, 0o600); err != nil {
		return u, err
	}
	return u, s.DB.CreateUpload(ctx, u)
}

// Upload returns the state of the actor's own upload.
func (s *Service) Upload(ctx context.Context, a Actor, id string) (store.Upload, error) {
	u, err := s.DB.GetUpload(ctx, id)
	if err != nil {
		return u, err
	}
	if u.UserID != a.User.ID {
		return store.Upload{}, ErrNotFound
	}
	return u, nil
}

// AppendUpload writes the next chunk at offset (which must match what the
// server has). When the last byte arrives the file is checked and stored;
// the result then holds the file.
func (s *Service) AppendUpload(ctx context.Context, a Actor, id string, offset int64, r io.Reader) (store.Upload, *store.File, error) {
	u, err := s.Upload(ctx, a, id)
	if err != nil {
		return u, nil, err
	}
	if offset != u.Received {
		return u, nil, ErrConflict
	}
	f, err := os.OpenFile(s.uploadPath(u.ID), os.O_WRONLY, 0o600)
	if err != nil {
		return u, nil, err
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		f.Close()
		return u, nil, err
	}
	// Keep what arrived even if the connection breaks mid-chunk (tus).
	n, copyErr := io.Copy(f, io.LimitReader(r, u.Length-offset))
	if err := f.Close(); err != nil {
		return u, nil, err
	}
	if err := s.DB.SetUploadReceived(ctx, u.ID, offset, offset+n); err != nil {
		return u, nil, err
	}
	u.Received = offset + n
	if copyErr != nil || u.Received < u.Length {
		return u, nil, copyErr
	}
	file, err := s.finishUpload(ctx, a, u)
	if err != nil {
		// A rejected file (malware, quota) cannot be fixed by resuming.
		_ = os.Remove(s.uploadPath(u.ID))
		_ = s.DB.DeleteUpload(ctx, u.ID)
		return u, nil, err
	}
	return u, &file, nil
}

func (s *Service) finishUpload(ctx context.Context, a Actor, u store.Upload) (store.File, error) {
	var meta UploadMeta
	if err := json.Unmarshal(u.Meta, &meta); err != nil {
		return store.File{}, err
	}
	path := s.uploadPath(u.ID)
	sp, err := hashFile(path)
	if err != nil {
		return store.File{}, err
	}
	p, err := s.project(ctx, a, u.ProjectID, store.RoleEditor)
	if err != nil {
		return store.File{}, err
	}
	var file store.File
	if meta.File != "" {
		cur, err := s.fileIn(ctx, a, p.ID, meta.File, store.RoleEditor)
		if err != nil {
			return file, err
		}
		file, err = s.ingest(ctx, a, p, sp, cur.Name, "", "", &cur)
		if err != nil {
			return file, err
		}
	} else {
		docID, entID, err := s.attachTargets(ctx, p, meta.Document, meta.Entity)
		if err != nil {
			return file, err
		}
		if file, err = s.ingest(ctx, a, p, sp, cleanName(meta.Name), docID, entID, nil); err != nil {
			return file, err
		}
	}
	_ = os.Remove(path)
	_ = s.DB.DeleteUpload(ctx, u.ID)
	return file, nil
}

// CancelUpload discards an unfinished upload.
func (s *Service) CancelUpload(ctx context.Context, a Actor, id string) error {
	u, err := s.Upload(ctx, a, id)
	if err != nil {
		return err
	}
	_ = os.Remove(s.uploadPath(u.ID))
	return s.DB.DeleteUpload(ctx, u.ID)
}

func hashFile(path string) (spooled, error) {
	f, err := os.Open(path)
	if err != nil {
		return spooled{}, err
	}
	defer f.Close()
	h := sha256.New()
	head := make([]byte, 512)
	n, err := io.ReadFull(f, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return spooled{}, err
	}
	head = head[:n]
	h.Write(head)
	rest, err := io.Copy(h, f)
	if err != nil {
		return spooled{}, err
	}
	return spooled{path: path, hash: hex.EncodeToString(h.Sum(nil)), size: int64(n) + rest, head: head}, nil
}
