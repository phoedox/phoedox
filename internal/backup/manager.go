package backup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/phoedox/phoedox/internal/blob"
	"github.com/phoedox/phoedox/internal/store"
)

// Info describes a stored backup.
type Info struct {
	Name      string    `json:"name"`
	Size      int64     `json:"size"`
	CreatedAt time.Time `json:"createdAt"`
}

// Target stores backup files.
type Target interface {
	Put(ctx context.Context, name, path string) error
	List(ctx context.Context) ([]Info, error)
	Open(ctx context.Context, name string) (io.ReadCloser, int64, error)
	Delete(ctx context.Context, name string) error
	String() string
}

// ErrNotFound is returned for unknown backups.
var ErrNotFound = errors.New("backup: not found")

// Local keeps backups in a directory.
type Local struct{ Dir string }

func (l Local) String() string { return "local" }

func (l Local) Put(_ context.Context, name, path string) error {
	if err := os.MkdirAll(l.Dir, 0o700); err != nil {
		return err
	}
	in, err := os.Open(path)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp, err := os.CreateTemp(l.Dir, ".tmp-*")
	if err != nil {
		return err
	}
	if _, err := io.Copy(tmp, in); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(l.Dir, name))
}

func (l Local) List(context.Context) ([]Info, error) {
	es, err := os.ReadDir(l.Dir)
	if errors.Is(err, os.ErrNotExist) {
		return []Info{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []Info{}
	for _, e := range es {
		if !IsName(e.Name()) {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, Info{Name: e.Name(), Size: fi.Size(), CreatedAt: fi.ModTime().UTC()})
	}
	return out, nil
}

func (l Local) Open(_ context.Context, name string) (io.ReadCloser, int64, error) {
	if !IsName(name) {
		return nil, 0, ErrNotFound
	}
	f, err := os.Open(filepath.Join(l.Dir, name))
	if errors.Is(err, os.ErrNotExist) {
		return nil, 0, ErrNotFound
	}
	if err != nil {
		return nil, 0, err
	}
	fi, _ := f.Stat()
	return f, fi.Size(), nil
}

func (l Local) Delete(_ context.Context, name string) error {
	if !IsName(name) {
		return ErrNotFound
	}
	err := os.Remove(filepath.Join(l.Dir, name))
	if errors.Is(err, os.ErrNotExist) {
		return ErrNotFound
	}
	return err
}

// S3 keeps backups in a bucket under a prefix.
type S3 struct {
	c      *minio.Client
	bucket string
	prefix string
}

// NewS3 uses the same bucket settings as the file storage.
func NewS3(cfg blob.S3Config) (*S3, error) {
	c, err := minio.New(cfg.Endpoint, &minio.Options{Creds: credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""), Secure: !cfg.Insecure, Region: cfg.Region})
	if err != nil {
		return nil, err
	}
	p := cfg.Prefix
	if p != "" && !strings.HasSuffix(p, "/") {
		p += "/"
	}
	return &S3{c: c, bucket: cfg.Bucket, prefix: p + "backups/"}, nil
}

func (s *S3) String() string { return "s3" }

func (s *S3) Put(ctx context.Context, name, path string) error {
	_, err := s.c.FPutObject(ctx, s.bucket, s.prefix+name, path, minio.PutObjectOptions{ContentType: "application/gzip"})
	return err
}

func (s *S3) List(ctx context.Context) ([]Info, error) {
	out := []Info{}
	for o := range s.c.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{Prefix: s.prefix}) {
		if o.Err != nil {
			return nil, o.Err
		}
		name := strings.TrimPrefix(o.Key, s.prefix)
		if IsName(name) {
			out = append(out, Info{Name: name, Size: o.Size, CreatedAt: o.LastModified.UTC()})
		}
	}
	return out, nil
}

func (s *S3) Open(ctx context.Context, name string) (io.ReadCloser, int64, error) {
	if !IsName(name) {
		return nil, 0, ErrNotFound
	}
	o, err := s.c.GetObject(ctx, s.bucket, s.prefix+name, minio.GetObjectOptions{})
	if err != nil {
		return nil, 0, err
	}
	st, err := o.Stat()
	if err != nil {
		o.Close()
		return nil, 0, ErrNotFound
	}
	return o, st.Size, nil
}

func (s *S3) Delete(ctx context.Context, name string) error {
	if !IsName(name) {
		return ErrNotFound
	}
	return s.c.RemoveObject(ctx, s.bucket, s.prefix+name, minio.RemoveObjectOptions{})
}

// Manager makes, keeps and restores backups.
type Manager struct {
	DB        *store.DB
	Blobs     blob.Store
	Target    Target
	TmpDir    string
	App       string
	WithFiles bool
	KeyID     string        // fingerprint of the master key (see Manifest.KeyID)
	Keep      int           // newest backups kept (0 = all)
	Every     time.Duration // automatic backups (0 = off)
	// AfterRestore runs after a restore (live editing reset, search index).
	AfterRestore func(ctx context.Context)

	mu      sync.Mutex
	running bool
	lastErr string
}

// Status of the manager for the admin page.
type Status struct {
	Backups   []Info    `json:"backups"`
	Target    string    `json:"target"`
	WithFiles bool      `json:"withFiles"`
	Keep      int       `json:"keep"`
	EveryH    int       `json:"everyHours"`
	Running   bool      `json:"running"`
	LastError string    `json:"lastError,omitempty"`
	Next      time.Time `json:"next"`
}

// Status lists the backups, newest first.
func (m *Manager) Status(ctx context.Context) (Status, error) {
	bs, err := m.Target.List(ctx)
	if err != nil {
		return Status{}, err
	}
	sort.Slice(bs, func(i, j int) bool { return bs[i].Name > bs[j].Name })
	m.mu.Lock()
	defer m.mu.Unlock()
	st := Status{Backups: bs, Target: m.Target.String(), WithFiles: m.WithFiles, Keep: m.Keep, EveryH: int(m.Every.Hours()), Running: m.running, LastError: m.lastErr}
	if m.Every > 0 {
		st.Next = time.Now().UTC()
		if len(bs) > 0 {
			st.Next = bs[0].CreatedAt.Add(m.Every)
		}
	}
	return st, nil
}

// ErrBusy is returned while another backup or restore runs.
var ErrBusy = errors.New("backup: already running")

func (m *Manager) begin() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.running {
		return ErrBusy
	}
	m.running = true
	return nil
}

func (m *Manager) end(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.running = false
	m.lastErr = ""
	if err != nil {
		m.lastErr = err.Error()
	}
}

// Create makes a backup now and removes the oldest beyond Keep.
func (m *Manager) Create(ctx context.Context) (info Info, err error) {
	if err := m.begin(); err != nil {
		return Info{}, err
	}
	defer func() { m.end(err) }()
	return m.create(ctx)
}

func (m *Manager) create(ctx context.Context) (Info, error) {
	if err := os.MkdirAll(m.TmpDir, 0o700); err != nil {
		return Info{}, err
	}
	f, err := os.CreateTemp(m.TmpDir, "backup-*")
	if err != nil {
		return Info{}, err
	}
	defer os.Remove(f.Name())
	man, err := Export(ctx, m.DB, m.Blobs, m.WithFiles, m.App, m.KeyID, f)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return Info{}, err
	}
	name := Name(man.CreatedAt)
	if err := m.Target.Put(ctx, name, f.Name()); err != nil {
		return Info{}, err
	}
	fi, _ := os.Stat(f.Name())
	m.prune(ctx)
	return Info{Name: name, Size: fi.Size(), CreatedAt: man.CreatedAt}, nil
}

func (m *Manager) prune(ctx context.Context) {
	if m.Keep <= 0 {
		return
	}
	bs, err := m.Target.List(ctx)
	if err != nil {
		return
	}
	sort.Slice(bs, func(i, j int) bool { return bs[i].Name > bs[j].Name })
	for _, b := range bs[min(len(bs), m.Keep):] {
		_ = m.Target.Delete(ctx, b.Name)
	}
}

// Restore replaces all data with a stored backup. A backup of the current
// state is made first.
func (m *Manager) Restore(ctx context.Context, name string) (man Manifest, err error) {
	if err := m.begin(); err != nil {
		return Manifest{}, err
	}
	defer func() { m.end(err) }()
	path, err := m.download(ctx, name)
	if err != nil {
		return Manifest{}, err
	}
	defer os.Remove(path)
	if _, err := ReadManifest(path); err != nil {
		return Manifest{}, err
	}
	if _, err := m.create(ctx); err != nil {
		return Manifest{}, fmt.Errorf("backup before restore: %w", err)
	}
	man, err = Restore(ctx, m.DB, m.Blobs, m.TmpDir, path)
	if err == nil && m.AfterRestore != nil {
		m.AfterRestore(ctx)
	}
	return man, err
}

func (m *Manager) download(ctx context.Context, name string) (string, error) {
	rc, _, err := m.Target.Open(ctx, name)
	if err != nil {
		return "", err
	}
	defer rc.Close()
	if err := os.MkdirAll(m.TmpDir, 0o700); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(m.TmpDir, "restore-*")
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(f, rc); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), f.Close()
}

// Upload stores a backup file from elsewhere (e.g. another instance) after
// checking its manifest.
func (m *Manager) Upload(ctx context.Context, r io.Reader) (Info, error) {
	if err := os.MkdirAll(m.TmpDir, 0o700); err != nil {
		return Info{}, err
	}
	f, err := os.CreateTemp(m.TmpDir, "upload-*")
	if err != nil {
		return Info{}, err
	}
	defer os.Remove(f.Name())
	size, err := io.Copy(f, r)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return Info{}, err
	}
	man, err := ReadManifest(f.Name())
	if err != nil {
		return Info{}, err
	}
	name := Name(man.CreatedAt)
	if err := m.Target.Put(ctx, name, f.Name()); err != nil {
		return Info{}, err
	}
	return Info{Name: name, Size: size, CreatedAt: man.CreatedAt}, nil
}

// Run makes automatic backups every Every until ctx ends.
func (m *Manager) Run(ctx context.Context, log func(msg string, args ...any)) {
	if m.Every <= 0 {
		return
	}
	t := time.NewTicker(10 * time.Minute)
	defer t.Stop()
	for {
		if st, err := m.Status(ctx); err == nil && !st.Running && !time.Now().Before(st.Next) {
			if info, err := m.Create(ctx); err != nil {
				log("automatic backup failed", "err", err)
			} else {
				log("automatic backup made", "name", info.Name, "size", info.Size)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
