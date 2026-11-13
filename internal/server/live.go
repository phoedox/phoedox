package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/phoedox/phoedox/internal/core"
	"github.com/phoedox/phoedox/internal/realtime"
)

// live upgrades to the live editing session (the draft) of a Markdown page.
func (a *api) live(hub *realtime.Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorOf(r)
		doc, err := a.core.LiveDocument(r.Context(), actor, chi.URLParam(r, "p"), chi.URLParam(r, "d"))
		if err != nil {
			writeErr(w, err)
			return
		}
		hub.Serve(w, r, doc.ID, actor.User.Name)
	}
}

func (a *api) projectDrafts(w http.ResponseWriter, r *http.Request) {
	list, err := a.core.ProjectDrafts(r.Context(), actorOf(r), chi.URLParam(r, "p"))
	respond(w, list, err)
}

func (a *api) getDraft(w http.ResponseWriter, r *http.Request) {
	d, err := a.core.Draft(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "d"))
	respond(w, d, err)
}

func (a *api) startDraft(w http.ResponseWriter, r *http.Request) {
	d, err := a.core.StartDraft(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "d"))
	respond(w, d, err)
}

func (a *api) saveDraft(w http.ResponseWriter, r *http.Request) {
	var in core.DraftPatch
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	d, err := a.core.SaveDraft(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "d"), in)
	respond(w, d, err)
}

func (a *api) publishDraft(w http.ResponseWriter, r *http.Request) {
	res, err := a.core.PublishDraft(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "d"))
	respond(w, res, err)
}

func (a *api) cancelDraft(w http.ResponseWriter, r *http.Request) {
	respond(w, map[string]bool{"ok": true}, a.core.CancelDraft(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "d")))
}
