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

// exportProject streams the project as a ZIP (owners; read-only tokens work).
func (a *api) exportProject(w http.ResponseWriter, r *http.Request) {
	var buf bytes.Buffer
	if err := a.core.ExportProject(r.Context(), actorOf(r), chi.URLParam(r, "p"), &buf); err != nil {
		writeErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+chi.URLParam(r, "p")+"-"+time.Now().UTC().Format("20060102")+`.phoedox.zip"`)
	_, _ = w.Write(buf.Bytes())
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
