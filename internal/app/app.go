// Package app wires configuration, storage and services together. It is
// shared by the server (cmd/phoedox) and the CLI (cmd/phx).
package app

import (
	"context"
	"fmt"
	"github.com/phoedox/phoedox/internal/backup"
	"github.com/phoedox/phoedox/internal/blob"
	"github.com/phoedox/phoedox/internal/jobs"
	"github.com/phoedox/phoedox/internal/preview"
	"github.com/phoedox/phoedox/internal/scan"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/phoedox/phoedox/internal/auth"
	"github.com/phoedox/phoedox/internal/config"
	"github.com/phoedox/phoedox/internal/core"
	"github.com/phoedox/phoedox/internal/mail"
	"github.com/phoedox/phoedox/internal/plugins"
	"github.com/phoedox/phoedox/internal/requests"
	"github.com/phoedox/phoedox/internal/secret"
	"github.com/phoedox/phoedox/internal/store"
	"github.com/phoedox/phoedox/internal/storeclient"
)

// App holds the opened services.
type App struct {
	Config config.Config
	DB     *store.DB
	Core   *core.Service
	Auth   *auth.Auth
	Jobs   *jobs.Queue
	// Backups is nil in the hosted mode (the database is backed up as a whole there).
	Backups *backup.Manager
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
	// The database may start after us (Kubernetes, Docker Compose): wait up to two minutes.
	var db *store.DB
	for i := 0; ; i++ {
		db, err = store.Open(ctx, dialect, cfg.DBURL, opts...)
		if err == nil || i >= 60 || !strings.Contains(err.Error(), "store: connect") {
			break
		}
		if i == 0 {
			slog.Info("waiting for the database", "err", err)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
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
	svc.Mail, svc.DefaultLocale = sender, cfg.DefaultLocale
	svc.HashPassword, svc.VerifyPassword = auth.HashPassword, auth.VerifyPassword
	svc.PublicURL = cfg.PublicURL
	if svc.PublicURL == "" && cfg.Domain != "" {
		svc.PublicURL = "https://" + cfg.Domain
	}
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
	svc.AgentReleases = cfg.AgentReleases
	svc.AgentReleaseDir = filepath.Join(cfg.DataDir, "agent-releases")
	// Uploads, plugin packages and file processing write here; S3 setups have no local files dir.
	if err := os.MkdirAll(svc.TempDir, 0o700); err != nil {
		db.Close()
		return nil, fmt.Errorf("temp dir: %w", err)
	}
	svc.MaxUpload = int64(cfg.MaxUploadMB) << 20
	svc.Preview = preview.Detect(cfg.OCR, cfg.OCRLangs)
	if cfg.ClamAV != "" {
		svc.Scanner = scan.Clamd{Addr: cfg.ClamAV}
	}
	if cfg.Plugins {
		keys, err := plugins.ParseKeys(plugins.StoreKeys + "," + cfg.PluginKeys)
		if err != nil {
			db.Close()
			return nil, fmt.Errorf("PHOEDOX_PLUGIN_KEYS: %w", err)
		}
		rt := plugins.NewRuntime(slog.Default(), 8)
		rt.Code = core.PluginErrorCode
		svc.Plugins = core.PluginConfig{Runtime: rt, Keys: keys, AllowUnsigned: cfg.PluginsUnsigned, Version: "dev"}
		if cfg.StoreURL != "" {
			svc.Plugins.Store = storeclient.New(cfg.StoreURL)
		}
	}
	queue := &jobs.Queue{DB: db, Workers: cfg.Workers}
	svc.RegisterJobs(queue)
	if err := svc.EnsureSearchIndex(store.System(ctx)); err != nil {
		slog.Warn("building the search index failed", "err", err)
	}
	app := &App{Config: cfg, DB: db, Core: svc, Auth: a, Jobs: queue}
	if !cfg.MultiTenant {
		var target backup.Target = backup.Local{Dir: cfg.BackupDir}
		if cfg.BackupTarget == "s3" {
			if cfg.S3.Endpoint == "" {
				db.Close()
				return nil, fmt.Errorf("PHOEDOX_BACKUP_TARGET=s3 needs PHOEDOX_S3_*")
			}
			t, err := backup.NewS3(blob.S3Config(cfg.S3))
			if err != nil {
				db.Close()
				return nil, err
			}
			target = t
		}
		app.Backups = &backup.Manager{DB: db, Blobs: svc.Blobs, Target: target, TmpDir: filepath.Join(cfg.DataDir, "tmp"),
			WithFiles: cfg.BackupFiles, KeyID: box.KeyID(), Keep: cfg.BackupKeep, Every: time.Duration(cfg.BackupHours) * time.Hour,
			AfterRestore: func(ctx context.Context) {
				if _, err := svc.Reindex(store.System(ctx)); err != nil {
					slog.Warn("rebuilding the search index after a restore failed", "err", err)
				}
			}}
	}
	return app, nil
}
