package server

import (
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"path"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/phoedox/phoedox/internal/core"
	"github.com/phoedox/phoedox/internal/plugins"
)

func (a *api) listPlugins(w http.ResponseWriter, r *http.Request) {
	list, err := a.core.InstalledPlugins(r.Context(), actorOf(r))
	respond(w, map[string]any{"plugins": list, "store": a.core.Plugins.Store != nil, "enabled": a.core.Plugins.Runtime != nil,
		"unsigned": a.core.Plugins.AllowUnsigned}, err)
}

// uploadPlugin installs a package sent as the request body. The optional
// Phoedox-Signature header ("keyId:base64") carries its signature.
func (a *api) uploadPlugin(w http.ResponseWriter, r *http.Request) {
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, plugins.MaxPackage))
	if err != nil {
		writeErr(w, core.Invalid("validation.plugin_package", map[string]any{"reason": "size"}, "the package is larger than %d MB", plugins.MaxPackage>>20))
		return
	}
	var sig *plugins.Signature
	if h := r.Header.Get("Phoedox-Signature"); h != "" {
		id, s, _ := strings.Cut(h, ":")
		sig = &plugins.Signature{KeyID: id, Sig: s}
	}
	info, err := a.core.InstallPlugin(r.Context(), actorOf(r), data, sig, "upload")
	respondStatus(w, http.StatusCreated, info, err)
}

func (a *api) pluginCatalog(w http.ResponseWriter, r *http.Request) {
	list, err := a.core.PluginCatalog(r.Context(), actorOf(r))
	respond(w, list, err)
}

func (a *api) installFromStore(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID      string `json:"id"`
		Version string `json:"version"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	info, err := a.core.InstallFromStore(r.Context(), actorOf(r), in.ID, in.Version)
	respondStatus(w, http.StatusCreated, info, err)
}

func (a *api) patchPlugin(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Enabled bool `json:"enabled"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	info, err := a.core.SetPluginEnabled(r.Context(), actorOf(r), chi.URLParam(r, "id"), in.Enabled)
	respond(w, info, err)
}

func (a *api) deletePlugin(w http.ResponseWriter, r *http.Request) {
	respond(w, map[string]bool{"ok": true}, a.core.UninstallPlugin(r.Context(), actorOf(r), chi.URLParam(r, "id")))
}

func (a *api) pluginLogs(w http.ResponseWriter, r *http.Request) {
	logs, err := a.core.PluginLogs(r.Context(), actorOf(r), chi.URLParam(r, "id"))
	respond(w, logs, err)
}

func (a *api) pluginLicense(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<10))
	if err != nil {
		writeErr(w, err)
		return
	}
	info, err := a.core.SetPluginLicense(r.Context(), actorOf(r), chi.URLParam(r, "id"), json.RawMessage(raw))
	respond(w, info, err)
}

func (a *api) redeemLicense(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Code string `json:"code"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	info, err := a.core.RedeemPluginLicense(r.Context(), actorOf(r), chi.URLParam(r, "id"), in.Code)
	respond(w, info, err)
}

func (a *api) projectPlugins(w http.ResponseWriter, r *http.Request) {
	list, err := a.core.ProjectPlugins(r.Context(), actorOf(r), chi.URLParam(r, "p"))
	respond(w, list, err)
}

func (a *api) setProjectPlugin(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Enabled bool `json:"enabled"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	err := a.core.SetProjectPlugin(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "id"), in.Enabled)
	respond(w, map[string]bool{"ok": true}, err)
}

func (a *api) setPluginSecrets(w http.ResponseWriter, r *http.Request) {
	var in map[string]string
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	err := a.core.SetPluginSecrets(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "id"), in)
	respond(w, map[string]bool{"ok": true}, err)
}

func (a *api) pluginTool(w http.ResponseWriter, r *http.Request) {
	args, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		writeErr(w, err)
		return
	}
	if len(strings.TrimSpace(string(args))) == 0 {
		args = []byte("{}")
	}
	if !json.Valid(args) {
		writeErr(w, core.Invalid("validation.json", nil, "the body is not JSON"))
		return
	}
	out, err := a.core.PluginTool(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "id"), chi.URLParam(r, "tool"), args)
	respond(w, out, err)
}

// pluginFile serves frontend files of a plugin. The version is part of the
// path, so files can be cached for good.
func (a *api) pluginFile(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "*")
	b, err := a.core.PluginFile(r.Context(), actorOf(r), chi.URLParam(r, "id"), chi.URLParam(r, "v"), name)
	if err != nil {
		writeErr(w, err)
		return
	}
	ct := mime.TypeByExtension(path.Ext(name))
	switch path.Ext(name) {
	case ".js", ".mjs":
		ct = "text/javascript; charset=utf-8"
	case ".json":
		ct = "application/json"
	case ".css":
		ct = "text/css; charset=utf-8"
	case ".svg":
		ct = "image/svg+xml"
	}
	if ct == "" || strings.HasPrefix(ct, "text/html") {
		ct = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	_, _ = w.Write(b)
}
