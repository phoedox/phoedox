package server

import (
	"bytes"
	"github.com/phoedox/phoedox/internal/publish"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/phoedox/phoedox/internal/core"
)

// exportProject streams the project as a ZIP (owners; read-only tokens
// work); ?format=git sends a Git repository with the history instead.
func (a *api) exportProject(w http.ResponseWriter, r *http.Request) {
	var buf bytes.Buffer
	format := r.URL.Query().Get("format")
	var err error
	if format == "git" {
		err = a.core.ExportProjectGit(r.Context(), actorOf(r), chi.URLParam(r, "p"), &buf)
	} else {
		err = a.core.ExportProject(r.Context(), actorOf(r), chi.URLParam(r, "p"), &buf)
	}
	if err != nil {
		writeErr(w, err)
		return
	}
	sendExport(w, chi.URLParam(r, "p"), format, buf.Bytes())
}

func sendExport(w http.ResponseWriter, slug, format string, data []byte) {
	name := slug + "-" + time.Now().UTC().Format("20060102") + ".phoedox.zip"
	if format == "git" {
		name = slug + "-" + time.Now().UTC().Format("20060102") + ".git.zip"
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	_, _ = w.Write(data)
}

// exportLink makes a 15-minute download link (for AI clients and scripts).
func (a *api) exportLink(w http.ResponseWriter, r *http.Request) {
	v, err := a.core.ProjectExportLink(r.Context(), actorOf(r), chi.URLParam(r, "p"), r.URL.Query().Get("format"))
	respond(w, v, err)
}

// openExportLink serves the export behind a download link.
func (a *api) openExportLink(w http.ResponseWriter, r *http.Request) {
	var buf bytes.Buffer
	slug, format, err := a.core.OpenExportLink(r.Context(), chi.URLParam(r, "token"), &buf)
	if err != nil {
		writeErr(w, err)
		return
	}
	sendExport(w, slug, format, buf.Bytes())
}

// offlineCopy sends the emergency copy of a project: static HTML pages in a
// ZIP that open without a server. It is built in a temporary file because
// attachments can be large.
func (a *api) offlineCopy(w http.ResponseWriter, r *http.Request) {
	d, err := a.core.Offline(r.Context(), actorOf(r), chi.URLParam(r, "p"))
	if err != nil {
		writeErr(w, err)
		return
	}
	tmp, err := os.CreateTemp("", "phx-offline-*.zip")
	if err != nil {
		writeErr(w, err)
		return
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	locale := actorOf(r).User.Locale
	if locale == "" {
		locale = a.cfg.DefaultLocale
	}
	if err := publish.WriteOffline(r.Context(), tmp, d, a.core.OfflineFile, locale, time.Now()); err != nil {
		writeErr(w, err)
		return
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		writeErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+d.Project.Slug+"-offline-"+time.Now().UTC().Format("20060102")+`.zip"`)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = io.Copy(w, tmp)
}

// importProject creates a project from an export ZIP (request body).
func (a *api) importProject(w http.ResponseWriter, r *http.Request) {
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 2<<30))
	if err != nil {
		writeErr(w, &core.InvalidError{Key: "validation.import_zip", Msg: "upload failed: " + err.Error()})
		return
	}
	res, err := a.core.ImportProject(r.Context(), actorOf(r), data)
	respondStatus(w, http.StatusCreated, res, err)
}

func (a *api) remoteProjects(w http.ResponseWriter, r *http.Request) {
	var in core.RemoteSource
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	list, err := a.core.RemoteProjects(r.Context(), actorOf(r), in)
	respond(w, list, err)
}

func (a *api) importRemote(w http.ResponseWriter, r *http.Request) {
	var in core.RemoteSource
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	res, err := a.core.ImportRemote(r.Context(), actorOf(r), in)
	respondStatus(w, http.StatusCreated, res, err)
}

func (a *api) projectGit(w http.ResponseWriter, r *http.Request) {
	g, err := a.core.ProjectGit(r.Context(), actorOf(r), chi.URLParam(r, "p"))
	respond(w, g, err)
}

func (a *api) saveProjectGit(w http.ResponseWriter, r *http.Request) {
	var in core.GitInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	g, err := a.core.SaveProjectGit(r.Context(), actorOf(r), chi.URLParam(r, "p"), in)
	respond(w, g, err)
}

func (a *api) syncProjectGit(w http.ResponseWriter, r *http.Request) {
	g, err := a.core.SyncProjectGit(r.Context(), actorOf(r), chi.URLParam(r, "p"))
	respond(w, g, err)
}

// answer gives a short spoken answer to a question (voice assistants).
func (a *api) answer(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Q    string `json:"q"`
		Lang string `json:"lang"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	if in.Lang == "" {
		in.Lang = actorOf(r).User.Locale
	}
	if in.Lang == "" {
		in.Lang = a.cfg.DefaultLocale
	}
	res, err := a.core.Answer(r.Context(), actorOf(r), chi.URLParam(r, "p"), in.Q, in.Lang)
	respond(w, res, err)
}
