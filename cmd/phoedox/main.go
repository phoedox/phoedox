// Command phoedox runs a Phoedox instance: API, MCP and the web app.
package main

import (
	"context"
	"errors"
	"github.com/phoedox/phoedox/internal/metrics"
	"github.com/phoedox/phoedox/internal/store"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/phoedox/phoedox/internal/app"
	"github.com/phoedox/phoedox/internal/mcp"
	"github.com/phoedox/phoedox/internal/realtime"
	"github.com/phoedox/phoedox/internal/server"
	"github.com/phoedox/phoedox/web"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(log)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	a, err := app.Open(ctx)
	if err != nil {
		log.Error("startup failed", "err", err)
		os.Exit(2)
	}
	defer a.DB.Close()
	if n, _ := a.DB.CountUsers(ctx); n == 0 {
		log.Warn("no users yet – create the first admin with: phx admin create-user --admin --email you@example.org --name 'Your Name'")
	}

	a.Core.Plugins.Version = server.Version
	hub := realtime.NewHub(a.DB)
	a.Core.OnContentChange = hub.Reset
	m := metrics.New(metrics.Sources{DB: a.DB.SQL, Version: server.Version, LiveEditors: hub.TotalEditors,
		JobCounts: func(ctx context.Context) (map[string]int, error) { return a.DB.JobCounts(store.System(ctx)) }})
	if a.Backups != nil {
		a.Backups.App = server.Version
		after := a.Backups.AfterRestore
		a.Backups.AfterRestore = func(ctx context.Context) {
			hub.CloseAll()
			after(ctx)
		}
		go a.Backups.Run(ctx, log.Info)
	}
	handler := server.New(server.Deps{
		Backups: a.Backups,
		Metrics: m,
		Hub:     hub,
		Config:  a.Config,
		Core:    a.Core,
		Auth:    a.Auth,
		MCP:     mcp.Handler(a.Core, a.Auth, server.Version, server.BaseURL(a.Config)),
		Web:     web.Dist(),
	})
	srv := &http.Server{Addr: a.Config.Listen, Handler: handler, ReadHeaderTimeout: 10 * time.Second}

	go func() {
		log.Info("phoedox listening", "addr", a.Config.Listen, "db", a.Config.DBDriver, "version", server.Version)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("server stopped", "err", err)
			os.Exit(1)
		}
	}()

	if a.Config.Workers > 0 {
		go a.Jobs.Run(ctx)
	}
	log.Info("file tools", "tools", a.Core.Preview.Available(), "virus_scan", a.Core.Scanner != nil)

	if a.Core.Plugins.Runtime != nil {
		defer a.Core.Plugins.Runtime.Close(context.Background())
		go runPlugins(ctx, a, log)
	}

	// Housekeeping: empty the trash, drop stale uploads and old jobs now and every six hours.
	go func() {
		t := time.NewTicker(6 * time.Hour)
		defer t.Stop()
		for {
			sys := store.System(ctx) // housekeeping covers all tenants
			a.Core.PurgeExpiredTrash(sys, a.Config.TrashDays)
			a.Core.CleanupUploads(sys)
			_, _ = a.DB.PruneJobs(sys, store.Time{Time: time.Now().Add(-7 * 24 * time.Hour)})
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()

	<-ctx.Done()
	shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdown)
}

// runPlugins wakes scheduled plugins every ten seconds and asks the store
// for updates and recalls every six hours (for every tenant).
func runPlugins(ctx context.Context, a *app.App, log *slog.Logger) {
	tick := time.NewTicker(10 * time.Second)
	defer tick.Stop()
	var synced time.Time
	for {
		sys := store.System(ctx)
		a.Core.RunPluginSchedules(sys, time.Now())
		if time.Since(synced) > 6*time.Hour {
			synced = time.Now()
			tenants, err := a.DB.Tenants(sys)
			if err != nil {
				log.Warn("plugin store sync", "err", err)
			}
			for _, t := range tenants {
				if err := a.Core.SyncPluginStore(store.WithTenant(ctx, t.ID)); err != nil {
					log.Warn("plugin store sync", "tenant", t.ID, "err", err)
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
