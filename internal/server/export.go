package server

import (
	"bytes"
	"io"
	"net/http"
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
