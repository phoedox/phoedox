package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/phoedox/phoedox/internal/core"
)

func (a *api) listComments(w http.ResponseWriter, r *http.Request) {
	list, err := a.core.Comments(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "d"))
	respond(w, list, err)
}

func (a *api) addComment(w http.ResponseWriter, r *http.Request) {
	var in core.CommentInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	c, err := a.core.AddComment(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "d"), in)
	respondStatus(w, http.StatusCreated, c, err)
}

func (a *api) updateComment(w http.ResponseWriter, r *http.Request) {
	var in core.CommentPatch
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	c, err := a.core.UpdateComment(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "id"), in)
	respond(w, c, err)
}

func (a *api) deleteComment(w http.ResponseWriter, r *http.Request) {
	respond(w, nil, a.core.DeleteComment(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "id")))
}

func (a *api) openComments(w http.ResponseWriter, r *http.Request) {
	m, err := a.core.OpenComments(r.Context(), actorOf(r), chi.URLParam(r, "p"))
	respond(w, m, err)
}
