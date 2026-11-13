package server

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/phoedox/phoedox/internal/core"
)

func versionParam(r *http.Request) (int, error) {
	n, err := strconv.Atoi(chi.URLParam(r, "n"))
	if err != nil || n < 1 {
		return 0, core.Invalid("validation.version", nil, "version must be a positive number")
	}
	return n, nil
}

func (a *api) getVersion(w http.ResponseWriter, r *http.Request) {
	n, err := versionParam(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	v, err := a.core.DocumentVersion(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "d"), n)
	respond(w, v, err)
}

func (a *api) restoreVersion(w http.ResponseWriter, r *http.Request) {
	n, err := versionParam(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	res, err := a.core.RestoreVersion(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "d"), n)
	respond(w, res, err)
}

func (a *api) listTrash(w http.ResponseWriter, r *http.Request) {
	items, err := a.core.Trash(r.Context(), actorOf(r), chi.URLParam(r, "p"))
	respond(w, map[string]any{"items": items, "days": a.cfg.TrashDays}, err)
}

func (a *api) restoreTrash(w http.ResponseWriter, r *http.Request) {
	it, err := a.core.RestoreFromTrash(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "id"))
	respond(w, it, err)
}

func (a *api) purgeTrash(w http.ResponseWriter, r *http.Request) {
	if err := a.core.PurgeFromTrash(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "id")); err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) listFavorites(w http.ResponseWriter, r *http.Request) {
	fs, err := a.core.Favorites(r.Context(), actorOf(r), chi.URLParam(r, "p"))
	respond(w, fs, err)
}

// ?scope=project for project favorites, otherwise personal ones.
func personalScope(r *http.Request) bool { return r.URL.Query().Get("scope") != "project" }

func (a *api) setFavorite(on bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := a.core.SetFavorite(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "d"), personalScope(r), on); err != nil {
			writeErr(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func (a *api) reorderFavorites(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Documents []string `json:"documents"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	if err := a.core.ReorderFavorites(r.Context(), actorOf(r), chi.URLParam(r, "p"), personalScope(r), in.Documents); err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
