package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/phoedox/phoedox/internal/core"
)

// Data processing agreement: texts (system admins) and acceptances
// (organization admins), with the PDF of an acceptance.

func (a *api) orgDPA(w http.ResponseWriter, r *http.Request) {
	v, err := a.core.OrgDPA(r.Context(), actorOf(r), chi.URLParam(r, "o"), r.URL.Query().Get("lang"))
	respond(w, v, err)
}

func (a *api) acceptDPA(w http.ResponseWriter, r *http.Request) {
	var in core.DPAAcceptInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	v, err := a.core.AcceptDPA(r.Context(), actorOf(r), chi.URLParam(r, "o"), in)
	respond(w, v, err)
}

func (a *api) dpaPDF(w http.ResponseWriter, r *http.Request) {
	pdf, name, err := a.core.DPAPDF(r.Context(), actorOf(r), chi.URLParam(r, "o"), chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(pdf)
}

func (a *api) adminDPA(w http.ResponseWriter, r *http.Request) {
	v, err := a.core.AdminDPA(r.Context(), actorOf(r))
	respond(w, v, err)
}

func (a *api) publishDPA(w http.ResponseWriter, r *http.Request) {
	var in core.DPAVersionInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	v, err := a.core.PublishDPA(r.Context(), actorOf(r), in)
	respond(w, v, err)
}
