// Command phoedox runs a Phoedox instance: API, MCP and the web app.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/phoedox/phoedox/internal/app"
	"github.com/phoedox/phoedox/internal/mcp"
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

	handler := server.New(server.Deps{
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

	<-ctx.Done()
	shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdown)
}
