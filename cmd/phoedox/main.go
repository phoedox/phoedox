// Command phoedox runs a Phoedox instance: API, MCP and the web app.
package main

import (
	"context"
	"errors"
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

	hub := realtime.NewHub(a.DB)
	a.Core.OnContentChange = hub.Reset
	handler := server.New(server.Deps{
		Hub:    hub,
		Config: a.Config,
		Core:   a.Core,
		Auth:   a.Auth,
		MCP:    mcp.Handler(a.Core, a.Auth, server.Version, server.BaseURL(a.Config)),
		Web:    web.Dist(),
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
