package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/phoedox/phoedox/internal/core"
)

func (a *api) snapshots(w http.ResponseWriter, r *http.Request) {
	v, err := a.core.Snapshots(r.Context(), actorOf(r), chi.URLParam(r, "p"))
	respond(w, v, err)
}

func (a *api) createSnapshot(w http.ResponseWriter, r *http.Request) {
	var in core.SnapshotInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	v, err := a.core.CreateSnapshot(r.Context(), actorOf(r), chi.URLParam(r, "p"), in)
	respondStatus(w, http.StatusCreated, v, err)
}

func (a *api) snapshot(w http.ResponseWriter, r *http.Request) {
	v, err := a.core.Snapshot(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "name"))
	respond(w, v, err)
}

func (a *api) deleteSnapshot(w http.ResponseWriter, r *http.Request) {
	respond(w, nil, a.core.DeleteSnapshot(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "name")))
}

func (a *api) compareSnapshots(w http.ResponseWriter, r *http.Request) {
	v, err := a.core.CompareSnapshots(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "name"), r.URL.Query().Get("to"))
	respond(w, v, err)
}

func (a *api) glossary(w http.ResponseWriter, r *http.Request) {
	v, err := a.core.Glossary(r.Context(), actorOf(r), chi.URLParam(r, "p"))
	respond(w, v, err)
}

func (a *api) setGlossary(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Entries []core.GlossaryEntry `json:"entries"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	v, err := a.core.SetGlossary(r.Context(), actorOf(r), chi.URLParam(r, "p"), in.Entries)
	respond(w, v, err)
}

func (a *api) glossaryCheck(w http.ResponseWriter, r *http.Request) {
	v, err := a.core.GlossaryCheck(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "d"), r.URL.Query().Get("lang"))
	respond(w, v, err)
}

func (a *api) repoDBModel(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	sql, err := a.core.RepoDBModel(r.Context(), actorOf(r), chi.URLParam(r, "p"), q.Get("repo"), q.Get("path"))
	respond(w, map[string]string{"sql": sql}, err)
}
