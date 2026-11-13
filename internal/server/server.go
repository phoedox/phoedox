// Package server wires the HTTP routes of a Phoedox instance.
package server

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/phoedox/phoedox/internal/config"
)

// Version is set at build time via -ldflags.
var Version = "dev"

// New returns the root handler. web holds the built frontend (may be empty in dev).
func New(cfg config.Config, web fs.FS) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.RealIP, middleware.Recoverer, middleware.Timeout(60*time.Second))

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": Version})
	})

	r.Route("/api/v1", func(api chi.Router) {
		api.Get("/instance", func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusOK, map[string]any{
				"version":     Version,
				"domain":      cfg.Domain,
				"multiTenant": cfg.MultiTenant,
			})
		})
	})

	// MCP (Streamable HTTP) will be mounted at /mcp, realtime (Yjs) at /rt.

	if web != nil {
		r.Handle("/*", spa(web))
	}
	return r
}

// spa serves the built frontend and falls back to index.html for client routes.
func spa(web fs.FS) http.Handler {
	files := http.FileServer(http.FS(web))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if p != "/" {
			if _, err := fs.Stat(web, p[1:]); err != nil {
				r.URL.Path = "/"
			}
		}
		files.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
