// Package app wires configuration, storage and services together. It is
// shared by the server (cmd/phoedox) and the CLI (cmd/phx).
package app

import (
	"context"
	"fmt"
	"github.com/phoedox/phoedox/internal/blob"
	"github.com/phoedox/phoedox/internal/jobs"
	"github.com/phoedox/phoedox/internal/preview"
	"github.com/phoedox/phoedox/internal/scan"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/phoedox/phoedox/internal/auth"
	"github.com/phoedox/phoedox/internal/config"
	"github.com/phoedox/phoedox/internal/core"
	"github.com/phoedox/phoedox/internal/mail"
	"github.com/phoedox/phoedox/internal/requests"
	"github.com/phoedox/phoedox/internal/secret"
	"github.com/phoedox/phoedox/internal/store"
)

// App holds the opened services.
type App struct {
	Config config.Config
	DB     *store.DB
	Core   *core.Service
	Auth   *auth.Auth
	Jobs   *jobs.Queue
}

// Open loads the configuration, connects to the database and runs migrations.
func Open(ctx context.Context) (*App, error) {
	cfg, err := config.FromEnv()
	if err != nil {
		return nil, err
	}
	dialect := store.SQLite
	if cfg.DBDriver == config.DriverPostgres {
		dialect = store.Postgres
	} else if err := os.MkdirAll(filepath.Dir(cfg.DBURL), 0o750); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	var opts []store.Option
	if cfg.MultiTenant {
		opts = append(opts, store.WithMultiTenant())
	}
	db, err := store.Open(ctx, dialect, cfg.DBURL, opts...)
	if err != nil {
		return nil, err
	}
	if cfg.MultiTenant {
		// Row-level security is the second line of defence; a superuser
		// would silently skip it.
		if bypass, err := db.RLSBypassed(ctx); err != nil || bypass {
			db.Close()
			return nil, fmt.Errorf("PHOEDOX_MULTI_TENANT: the database user must not be a superuser or have BYPASSRLS (row-level security would be skipped)")
		}
	}
	var sender mail.Sender = mail.Disabled{}
	if cfg.SMTP.Host != "" {
		sender = &mail.SMTP{Host: cfg.SMTP.Host, Port: cfg.SMTP.Port, User: cfg.SMTP.User, Password: cfg.SMTP.Password,
			From: cfg.SMTP.From, TLS: mail.TLSMode(cfg.SMTP.TLS)}
	}
	box, err := secret.Load(cfg.MasterKey, filepath.Join(cfg.DataDir, "master.key"))
	if err != nil {
		db.Close()
		return nil, err
	}
	a := &auth.Auth{DB: db, SecureCookies: cfg.SecureCookies, Mail: sender, DefaultLocale: cfg.DefaultLocale, Box: box}
	if cfg.OIDC.Enabled() {
		a.OIDC = &auth.OIDC{Config: cfg.OIDC}
	}
	svc := core.New(db)
	svc.Box = box
	svc.RequestPolicy = requests.Policy{AllowPrivate: cfg.RequestsPrivate}
	svc.Blobs = blob.Local{Dir: filepath.Join(cfg.DataDir, "files")}
	if cfg.S3.Endpoint != "" {
		s3, err := blob.NewS3(blob.S3Config(cfg.S3))
		if err != nil {
			db.Close()
			return nil, err
		}
		svc.Blobs = s3
	}
	svc.TempDir = filepath.Join(cfg.DataDir, "tmp")
	svc.MaxUpload = int64(cfg.MaxUploadMB) << 20
	svc.Preview = preview.Detect(cfg.OCR, cfg.OCRLangs)
	if cfg.ClamAV != "" {
		svc.Scanner = scan.Clamd{Addr: cfg.ClamAV}
	}
	queue := &jobs.Queue{DB: db, Workers: cfg.Workers}
	svc.RegisterJobs(queue)
	if err := svc.EnsureSearchIndex(store.System(ctx)); err != nil {
		slog.Warn("building the search index failed", "err", err)
	}
	return &App{Config: cfg, DB: db, Core: svc, Auth: a, Jobs: queue}, nil
}
