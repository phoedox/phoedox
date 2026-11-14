package server

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

func (a *api) listEnv(w http.ResponseWriter, r *http.Request) {
	vars, err := a.core.Environment(r.Context(), actorOf(r), chi.URLParam(r, "p"))
	respond(w, vars, err)
}

func (a *api) setEnv(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Value  string `json:"value"`
		Secret bool   `json:"secret"`
		// VaultRef keeps the value in the instance's vault instead.
		VaultRef string `json:"vaultRef"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	var err error
	if in.VaultRef != "" {
		err = a.core.SetVaultVariable(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "name"), in.VaultRef)
	} else {
		err = a.core.SetVariable(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "name"), in.Value, in.Secret)
	}
	if err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) deleteEnv(w http.ResponseWriter, r *http.Request) {
	if err := a.core.DeleteVariable(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "name")); err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) runRequest(w http.ResponseWriter, r *http.Request) {
	if !a.runLimiter.allow(actorOf(r).User.ID) {
		writeJSON(w, http.StatusTooManyRequests, errBody("rate_limited", "Too many requests. Please wait a minute."))
		return
	}
	n, err := strconv.Atoi(chi.URLParam(r, "n"))
	if err != nil {
		writeErr(w, err)
		return
	}
	res, err := a.core.RunRequest(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "d"), n)
	respond(w, res, err)
}
