// Package server wires the HTTP routes of a Phoedox instance.
package server

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/phoedox/phoedox/internal/auth"
	"github.com/phoedox/phoedox/internal/config"
	"github.com/phoedox/phoedox/internal/core"
	"github.com/phoedox/phoedox/internal/realtime"
	"github.com/phoedox/phoedox/internal/store"
)

// Version is set at build time via -ldflags.
var Version = "dev"

// Deps are the services the HTTP layer needs. All fields except Config may be
// nil in tests; the related routes are then not mounted.
type Deps struct {
	Config config.Config
	Core   *core.Service
	Auth   *auth.Auth
	MCP    http.Handler
	Web    fs.FS
	// Hub serves live editing (/rt); nil disables it.
	Hub *realtime.Hub
}

// New returns the root handler.
func New(d Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	if d.Config.BehindProxy {
		// Forwarded headers can be spoofed unless a trusted proxy overwrites them.
		r.Use(middleware.RealIP)
	}
	r.Use(middleware.Recoverer)

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": Version})
	})

	if d.Core != nil && d.Auth != nil {
		api := &api{core: d.Core, auth: d.Auth, cfg: d.Config, limiter: newLimiter(10, 5*time.Minute), regLimiter: newLimiter(30, time.Hour), runLimiter: newLimiter(30, time.Minute)}
		r.Route("/api/v1", api.routes)
		api.oauthRoutes(r)
		if d.Hub != nil {
			r.With(api.requireActor).Get("/rt/doc/{p}/{d}", api.live(d.Hub))
		}
	}
	if d.MCP != nil {
		r.Handle("/mcp", d.MCP)
	}
	if d.Web != nil {
		r.Handle("/*", spa(d.Web))
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

// apiError is the JSON error body. Clients translate Key (with Params);
// Error is an English fallback and must not be shown to users directly.
type apiError struct {
	Error  string         `json:"error"`
	Code   string         `json:"code"`
	Key    string         `json:"key"`
	Params map[string]any `json:"params,omitempty"`
}

func errBody(code, msg string) apiError {
	return apiError{Error: msg, Code: code, Key: "error." + code}
}

func writeErr(w http.ResponseWriter, err error) {
	var inv *core.InvalidError
	switch {
	case errors.As(err, &inv):
		writeJSON(w, http.StatusBadRequest, apiError{Error: inv.Msg, Code: "invalid", Key: inv.Key, Params: inv.Params})
	case errors.Is(err, core.ErrForbidden):
		writeJSON(w, http.StatusForbidden, errBody("forbidden", "You do not have permission for this."))
	case errors.Is(err, store.ErrNotFound):
		writeJSON(w, http.StatusNotFound, errBody("not_found", "Not found."))
	case errors.Is(err, store.ErrConflict):
		writeJSON(w, http.StatusConflict, errBody("conflict", "This already exists or was changed in the meantime."))
	case errors.Is(err, auth.ErrBadLogin):
		writeJSON(w, http.StatusUnauthorized, errBody("bad_login", err.Error()))
	default:
		slog.Error("request failed", "err", err)
		writeJSON(w, http.StatusInternalServerError, errBody("internal", "Internal error."))
	}
}
