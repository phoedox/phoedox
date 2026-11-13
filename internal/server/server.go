// Package server wires the HTTP routes of a Phoedox instance.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/phoedox/phoedox/internal/backup"
	"github.com/phoedox/phoedox/internal/metrics"
	"github.com/phoedox/phoedox/internal/publish"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
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
	// Metrics counts requests; /metrics is served if Config.MetricsToken is set.
	Metrics *metrics.Metrics
	// Backups for system admins; nil disables the backup routes.
	Backups *backup.Manager
	// Publish serves publications (/pub/<slug> and own domains); nil disables them.
	Publish *publish.Site
	// Website serves the product website on its domains; nil disables it.
	Website *publish.Website
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
	if d.Metrics != nil {
		r.Use(d.Metrics.Middleware)
		if d.Config.MetricsToken != "" {
			r.Handle("/metrics", d.Metrics.Handler(d.Config.MetricsToken))
		}
	}
	if d.Website != nil {
		r.Use(d.Website.Middleware)
	}
	if d.Publish != nil {
		// Own domains of publications come before tenant lookup by host.
		r.Use(publicationByHost(d.Publish, d.Config.Domain))
	}
	if d.Config.MultiTenant && d.Core != nil {
		r.Use(tenantByHost(d.Core.DB))
	}

	// Liveness: the process answers. Readiness: the database answers too
	// (Kubernetes sends traffic only then).
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": Version})
	})
	r.Get("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if d.Core != nil {
			ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
			defer cancel()
			if err := d.Core.DB.SQL.PingContext(ctx); err != nil {
				writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "database unavailable"})
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready", "version": Version})
	})

	if d.Core != nil && d.Auth != nil {
		api := &api{core: d.Core, auth: d.Auth, cfg: d.Config, limiter: newLimiter(10, 5*time.Minute), regLimiter: newLimiter(30, time.Hour), runLimiter: newLimiter(30, time.Minute)}
		r.Route("/api/v1", api.routes)
		api.root = r
		api.backups = d.Backups
		api.oauthRoutes(r)
		if d.Hub != nil {
			r.With(api.requireActor).Get("/rt/doc/{p}/{d}", api.live(d.Hub))
		}
	}
	if d.MCP != nil {
		r.Handle("/mcp", d.MCP)
	}
	if d.Publish != nil && d.Core != nil {
		servePub := func(w http.ResponseWriter, r *http.Request) {
			slug := chi.URLParam(r, "slug")
			p, err := d.Core.DB.GetPublication(r.Context(), slug)
			if err != nil || p.Slug != slug {
				http.NotFound(w, r)
				return
			}
			d.Publish.Serve(w, r, p, "/pub/"+p.Slug)
		}
		r.Get("/pub/{slug}", servePub)
		r.Handle("/pub/{slug}/*", http.HandlerFunc(servePub))
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

// tenantByHost puts the tenant serving the request's host into the context
// (hosted mode). Unknown hosts get 404 before anything else runs.
// publicationByHost serves publications on their own domains (ADR-0006).
// Lookups are cached briefly; the app's own domain is never a publication.
func publicationByHost(site *publish.Site, appDomain string) func(http.Handler) http.Handler {
	type entry struct {
		p   store.Publication
		ok  bool
		exp time.Time
	}
	var mu sync.Mutex
	cache := map[string]entry{}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			host := strings.ToLower(r.Host)
			if h, _, err := net.SplitHostPort(host); err == nil {
				host = h
			}
			if host == "" || host == strings.ToLower(appDomain) || host == "localhost" || net.ParseIP(host) != nil ||
				r.URL.Path == "/healthz" || r.URL.Path == "/readyz" || r.URL.Path == "/metrics" {
				next.ServeHTTP(w, r)
				return
			}
			mu.Lock()
			e, hit := cache[host]
			mu.Unlock()
			if !hit || time.Now().After(e.exp) {
				p, err := site.Core.DB.PublicationByDomain(r.Context(), host)
				e = entry{p: p, ok: err == nil, exp: time.Now().Add(30 * time.Second)}
				mu.Lock()
				if len(cache) > 1000 {
					cache = map[string]entry{}
				}
				cache[host] = e
				mu.Unlock()
			}
			if !e.ok {
				next.ServeHTTP(w, r)
				return
			}
			site.Serve(w, r, e.p, "")
		})
	}
}

func tenantByHost(db *store.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" || r.URL.Path == "/metrics" {
				next.ServeHTTP(w, r)
				return
			}
			host := r.Host
			if h, _, err := net.SplitHostPort(host); err == nil {
				host = h
			}
			tenant, err := db.TenantByDomain(r.Context(), host)
			if err != nil {
				writeJSON(w, http.StatusNotFound, errBody("unknown_domain", "No Phoedox instance at this address."))
				return
			}
			next.ServeHTTP(w, r.WithContext(store.WithTenant(r.Context(), tenant)))
		})
	}
}
