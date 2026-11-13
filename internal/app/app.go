// Package app wires configuration, storage and services together. It is
// shared by the server (cmd/phoedox) and the CLI (cmd/phx).
package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/phoedox/phoedox/internal/auth"
	"github.com/phoedox/phoedox/internal/config"
	"github.com/phoedox/phoedox/internal/core"
	"github.com/phoedox/phoedox/internal/store"
)

// App holds the opened services.
type App struct {
	Config config.Config
	DB     *store.DB
	Core   *core.Service
	Auth   *auth.Auth
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
	db, err := store.Open(ctx, dialect, cfg.DBURL)
	if err != nil {
		return nil, err
	}
	return &App{Config: cfg, DB: db, Core: core.New(db), Auth: &auth.Auth{DB: db, SecureCookies: cfg.SecureCookies}}, nil
}
