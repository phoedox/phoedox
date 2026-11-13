package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/phoedox/phoedox/internal/realtime"
)

// live upgrades to the live editing session of a Markdown document.
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

func (a *api) liveSave(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Markdown string `json:"markdown"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	doc, err := a.core.LiveSave(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "d"), in.Markdown)
	respond(w, map[string]any{"version": doc.Version, "updatedAt": doc.UpdatedAt}, err)
}
