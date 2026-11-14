package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/phoedox/phoedox/internal/core"
)

type noteIn struct {
	Note string `json:"note"`
}

type approvalIn struct {
	Enabled bool `json:"enabled"`
}

func (a *api) getApproval(w http.ResponseWriter, r *http.Request) {
	on, err := a.core.Approval(r.Context(), actorOf(r), chi.URLParam(r, "p"))
	respond(w, approvalIn{Enabled: on}, err)
}

func (a *api) setApproval(w http.ResponseWriter, r *http.Request) {
	var in approvalIn
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	respond(w, in, a.core.SetApproval(r.Context(), actorOf(r), chi.URLParam(r, "p"), in.Enabled))
}

func (a *api) requestReview(w http.ResponseWriter, r *http.Request) {
	var in noteIn
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	res, err := a.core.RequestReview(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "d"), in.Note)
	respond(w, res, err)
}

func (a *api) rejectReview(w http.ResponseWriter, r *http.Request) {
	var in noteIn
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	res, err := a.core.RejectReview(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "d"), in.Note)
	respond(w, res, err)
}

func (a *api) pageRules(w http.ResponseWriter, r *http.Request) {
	res, err := a.core.PageRules(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "d"))
	respond(w, res, err)
}

func (a *api) setPageRules(w http.ResponseWriter, r *http.Request) {
	var in core.PageRulesInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	res, err := a.core.SetPageRules(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "d"), in)
	respond(w, res, err)
}

func (a *api) markReviewed(w http.ResponseWriter, r *http.Request) {
	res, err := a.core.MarkReviewed(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "d"))
	respond(w, res, err)
}

func (a *api) confirmRead(w http.ResponseWriter, r *http.Request) {
	res, err := a.core.ConfirmRead(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "d"))
	respond(w, res, err)
}

func (a *api) listMaintenance(w http.ResponseWriter, r *http.Request) {
	res, err := a.core.MaintenancePlans(r.Context(), actorOf(r), chi.URLParam(r, "p"), r.URL.Query().Get("entity"))
	respond(w, res, err)
}

func (a *api) saveMaintenance(w http.ResponseWriter, r *http.Request) {
	var in core.MaintenanceInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	res, err := a.core.SaveMaintenancePlan(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "id"), in)
	respond(w, res, err)
}

func (a *api) deleteMaintenance(w http.ResponseWriter, r *http.Request) {
	respond(w, map[string]bool{"ok": true}, a.core.DeleteMaintenancePlan(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "id")))
}
