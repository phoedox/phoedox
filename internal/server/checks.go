package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/phoedox/phoedox/internal/core"
)

func (a *api) listChecks(w http.ResponseWriter, r *http.Request) {
	list, err := a.core.Checks(r.Context(), actorOf(r), chi.URLParam(r, "p"), r.URL.Query().Get("document"))
	respond(w, list, err)
}

func (a *api) createCheck(w http.ResponseWriter, r *http.Request) {
	var in core.CheckInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	c, err := a.core.CreateCheck(r.Context(), actorOf(r), chi.URLParam(r, "p"), in)
	respondStatus(w, http.StatusCreated, c, err)
}

func (a *api) updateCheck(w http.ResponseWriter, r *http.Request) {
	var in core.CheckInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	c, err := a.core.UpdateCheck(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "id"), in)
	respond(w, c, err)
}

func (a *api) deleteCheck(w http.ResponseWriter, r *http.Request) {
	respond(w, nil, a.core.DeleteCheck(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "id")))
}

func (a *api) runCheck(w http.ResponseWriter, r *http.Request) {
	if !a.runLimiter.allow(actorOf(r).User.ID) {
		writeJSON(w, http.StatusTooManyRequests, errBody("rate_limited", "Too many requests. Please wait a minute."))
		return
	}
	c, err := a.core.RunCheckNow(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "id"))
	respond(w, c, err)
}
