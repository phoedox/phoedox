// Package blob stores file contents addressed by their SHA-256 hash, on the
// local disk or in an S3-compatible bucket. Metadata (names, owners,
// projects) lives in the database; the same content is stored once.
package blob

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
)

// ErrNotFound is returned for unknown hashes.
var ErrNotFound = errors.New("blob: not found")

// ReadSeekCloser is what Open returns; seeking enables range requests.
type ReadSeekCloser interface {
	io.ReadSeeker
	io.Closer
}

// Store keeps blobs by hash.
type Store interface {
	// Put stores the content of a local file under its hash (no-op if present).
	Put(ctx context.Context, hash, path string, size int64) error
	Open(ctx context.Context, hash string) (ReadSeekCloser, error)
	Delete(ctx context.Context, hash string) error
}

var hashRe = regexp.MustCompile(`^[0-9a-f]{64}$`)

func checkHash(hash string) error {
	if !hashRe.MatchString(hash) {
		return fmt.Errorf("blob: invalid hash %q", hash)
	}
	return nil
}

// key spreads blobs over folders: ab/cd/abcd….
func key(hash string) string { return hash[:2] + "/" + hash[2:4] + "/" + hash }

// Spool copies r into a temporary file in dir while hashing it. At most
// limit bytes are read; ErrTooLarge if r is longer. The caller removes the file.
func Spool(dir string, r io.Reader, limit int64) (path, hash string, size int64, head []byte, err error) {
	if err = os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	f, err := os.CreateTemp(dir, "upload-*")
	if err != nil {
		return
	}
	path = f.Name()
	defer func() {
		f.Close()
		if err != nil {
			os.Remove(path)
			path = ""
		}
	}()
	h := sha256.New()
	hb := &headBuf{max: 512}
	size, err = io.Copy(io.MultiWriter(f, h, hb), io.LimitReader(r, limit+1))
	if err != nil {
		return
	}
	if size > limit {
		err = ErrTooLarge
		return
	}
	return path, hex.EncodeToString(h.Sum(nil)), size, hb.b, f.Sync()
}

// ErrTooLarge is returned by Spool for content over the limit.
var ErrTooLarge = errors.New("blob: content too large")

type headBuf struct {
	b   []byte
	max int
}

func (h *headBuf) Write(p []byte) (int, error) {
	if n := h.max - len(h.b); n > 0 {
		h.b = append(h.b, p[:min(n, len(p))]...)
	}
	return len(p), nil
}

// Local stores blobs below a directory.
type Local struct{ Dir string }

func (l Local) path(hash string) string { return filepath.Join(l.Dir, filepath.FromSlash(key(hash))) }

func (l Local) Put(_ context.Context, hash, src string, _ int64) error {
	if err := checkHash(hash); err != nil {
		return err
	}
	dst := l.path(hash)
	if _, err := os.Stat(dst); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	// Copy (not rename): the spool file may live on another file system.
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".tmp-*")
	if err != nil {
		return err
	}
	if _, err := io.Copy(tmp, in); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), dst)
}

func (l Local) Open(_ context.Context, hash string) (ReadSeekCloser, error) {
	if err := checkHash(hash); err != nil {
		return nil, err
	}
	f, err := os.Open(l.path(hash))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	return f, err
}

func (l Local) Delete(_ context.Context, hash string) error {
	if err := checkHash(hash); err != nil {
		return err
	}
	err := os.Remove(l.path(hash))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
