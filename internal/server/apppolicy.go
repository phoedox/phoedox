package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/phoedox/phoedox/internal/core"
)

// App switches of the instance and organizations.

func (a *api) instanceAppPolicy(w http.ResponseWriter, r *http.Request) {
	v, err := a.core.InstanceAppPolicy(r.Context(), actorOf(r))
	respond(w, v, err)
}

func (a *api) setInstanceAppPolicy(w http.ResponseWriter, r *http.Request) {
	var in core.AppPolicy
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	v, err := a.core.SetInstanceAppPolicy(r.Context(), actorOf(r), in)
	respond(w, v, err)
}

func (a *api) orgAppPolicy(w http.ResponseWriter, r *http.Request) {
	v, err := a.core.GetOrgAppPolicy(r.Context(), actorOf(r), chi.URLParam(r, "o"))
	respond(w, v, err)
}

func (a *api) setOrgAppPolicy(w http.ResponseWriter, r *http.Request) {
	var in core.AppPolicy
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	v, err := a.core.SetOrgAppPolicy(r.Context(), actorOf(r), chi.URLParam(r, "o"), in)
	respond(w, v, err)
}
