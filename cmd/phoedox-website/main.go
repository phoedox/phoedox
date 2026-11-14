// Command phoedox-website serves the product website (phoedox.com) as its
// own service, separate from the app (ADR-0054).
//
// Environment:
//
//	WEBSITE_LISTEN  address to listen on (default :8080)
//	WEBSITE_DIR     operator files: impressum.md, datenschutz.md, contact.txt
//	                (default /etc/phoedox-website; missing files hide their links)
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

	site "github.com/phoedox/phoedox/internal/website"
	"github.com/phoedox/phoedox/website"
)

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	web := website.Dist()
	if web == nil {
		log.Error("website not built: run `bun run build` in website/ before go build")
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	addr := getenv("WEBSITE_LISTEN", ":8080")
	srv := &http.Server{Addr: addr, Handler: &site.Website{Dir: getenv("WEBSITE_DIR", "/etc/phoedox-website"), Web: web}, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	log.Info("website listening", "addr", addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("server failed", "err", err)
		os.Exit(1)
	}
}
