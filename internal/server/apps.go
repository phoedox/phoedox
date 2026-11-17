package server

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/phoedox/phoedox/internal/config"
	"github.com/phoedox/phoedox/internal/core"
	"github.com/phoedox/phoedox/internal/usercontent"
)

// Apps (apps-definition): the isolated frame, the libraries apps import and
// the bridge the page around an app calls for it.

// requestOrigin is scheme://host of a request (frames live on the host the
// page came from).
func requestOrigin(cfg config.Config, r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || (cfg.BehindProxy && r.Header.Get("X-Forwarded-Proto") == "https") || strings.HasPrefix(cfg.PublicURL, "https://") {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

// appFrame serves an app's files by its sealed address. The response is
// sandboxed (opaque origin: no cookies, no storage of Phoedox, no access to
// the page around it) and may load only its own files and the libraries.
func appFrame(svc *core.Service, cfg config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tok := chi.URLParam(r, "tok")
		name := chi.URLParam(r, "*")
		origin := requestOrigin(cfg, r)
		f, err := svc.AppFrameFile(r.Context(), tok, name, origin, "")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		own := origin + "/app-frame/" + tok + "/"
		lib := origin + "/applib/"
		h := w.Header()
		h.Set("Content-Type", f.Type)
		h.Set("Content-Security-Policy", "sandbox allow-scripts allow-forms allow-modals allow-popups allow-downloads; default-src 'none'; "+
			"script-src 'unsafe-inline' 'unsafe-eval' "+own+" "+lib+"; style-src 'unsafe-inline' "+own+" "+lib+"; img-src data: blob: "+own+"; "+
			"font-src data: "+lib+" "+origin+"/fonts/; media-src data: blob: "+own+"; connect-src "+own+"; worker-src blob:; "+
			"frame-ancestors "+origin+"; form-action 'none'; base-uri 'none'")
		h.Set("Access-Control-Allow-Origin", "*")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Cache-Control", "no-store")
		_, _ = w.Write(f.Body)
	}
}

// appLibs serves the version-pinned libraries apps import (CORS, because
// frames have an opaque origin).
func appLibs(web fs.FS) http.Handler {
	files := http.FileServer(http.FS(web))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := fs.Stat(web, strings.TrimPrefix(r.URL.Path, "/")); err != nil {
			http.NotFound(w, r)
			return
		}
		h := w.Header()
		h.Set("Access-Control-Allow-Origin", "*")
		h.Set("Cross-Origin-Resource-Policy", "cross-origin")
		h.Set("Cache-Control", "public, max-age=86400")
		files.ServeHTTP(w, r)
	})
}

func (a *api) openApp(w http.ResponseWriter, r *http.Request) {
	v, err := a.core.OpenApp(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "doc"))
	respond(w, v, err)
}

func (a *api) previewApp(w http.ResponseWriter, r *http.Request) {
	var in core.App
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	v, err := a.core.PreviewApp(r.Context(), actorOf(r), chi.URLParam(r, "p"), r.URL.Query().Get("doc"), in)
	respond(w, v, err)
}

func (a *api) grantApp(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Level int `json:"level"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	lvl, err := a.core.GrantApp(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "doc"), in.Level)
	respond(w, map[string]int{"granted": lvl}, err)
}

func (a *api) readApp(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Source string `json:"source"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	v, err := a.core.ReadAppSource(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "doc"), in.Source)
	respond(w, v, err)
}

func personal(r *http.Request) bool { return r.URL.Query().Get("personal") == "1" }

func (a *api) appStorageList(w http.ResponseWriter, r *http.Request) {
	v, err := a.core.AppStorageList(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "doc"), personal(r))
	respond(w, v, err)
}

func (a *api) appStorageGet(w http.ResponseWriter, r *http.Request) {
	v, err := a.core.AppStorageGet(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "doc"), chi.URLParam(r, "key"), personal(r))
	respond(w, v, err)
}

func (a *api) appStorageSet(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Value json.RawMessage `json:"value"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	if len(in.Value) == 0 {
		in.Value = json.RawMessage("null")
	}
	respond(w, nil, a.core.AppStorageSet(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "doc"), chi.URLParam(r, "key"), in.Value, personal(r)))
}

func (a *api) appStorageDelete(w http.ResponseWriter, r *http.Request) {
	respond(w, nil, a.core.AppStorageDelete(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "doc"), chi.URLParam(r, "key"), personal(r)))
}

func (a *api) appConsole(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Logs []core.AppLog `json:"logs"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	respond(w, nil, a.core.LogApp(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "doc"), in.Logs))
}

// UserContentHandler serves apps on <app>.<user content domain>/f/<tok>/…:
// the app has its own origin there (browser storage works, nothing of
// Phoedox is reachable), the instance's address embeds it.
func UserContentHandler(svc *core.Service, cfg config.Config) http.Handler {
	r := chi.NewRouter()
	r.Get("/f/{tok}/*", func(w http.ResponseWriter, r *http.Request) {
		instance := strings.TrimSuffix(cfg.PublicURL, "/")
		if instance == "" {
			http.NotFound(w, r)
			return
		}
		tok, name := chi.URLParam(r, "tok"), chi.URLParam(r, "*")
		f, err := svc.AppFrameFile(r.Context(), tok, name, instance, r.Header.Get("X-Phoedox-Host-Label"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		lib := instance + "/applib/"
		h := w.Header()
		h.Set("Content-Type", f.Type)
		h.Set("Content-Security-Policy", "sandbox allow-scripts allow-same-origin allow-forms allow-modals allow-popups allow-downloads; default-src 'none'; "+
			"script-src 'self' 'unsafe-inline' 'unsafe-eval' "+lib+"; style-src 'self' 'unsafe-inline' "+lib+"; img-src 'self' data: blob:; "+
			"font-src 'self' data: "+lib+" "+instance+"/fonts/; media-src 'self' data: blob:; connect-src 'self'; worker-src 'self' blob:; "+
			"frame-ancestors "+instance+"; form-action 'none'; base-uri 'none'")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Cache-Control", "no-store")
		_, _ = w.Write(f.Body)
	})
	return r
}

func (a *api) userContent(w http.ResponseWriter, r *http.Request) {
	v, err := a.core.UserContentSettings(r.Context(), actorOf(r))
	respond(w, v, err)
}

func (a *api) saveUserContent(w http.ResponseWriter, r *http.Request) {
	var in usercontent.Config
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	v, err := a.core.SaveUserContent(r.Context(), actorOf(r), in)
	respond(w, v, err)
}

func (a *api) appAI(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Prompt string `json:"prompt"`
		JSON   bool   `json:"json"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	v, err := a.core.AskAppAI(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "doc"), in.Prompt, in.JSON)
	respond(w, v, err)
}
