package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/phoedox/phoedox/internal/auth"
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

func (a *api) entityMeta(w http.ResponseWriter, r *http.Request) {
	res, err := a.core.EntityMeta(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "e"))
	respond(w, res, err)
}

func (a *api) setEntityMeta(w http.ResponseWriter, r *http.Request) {
	var in core.EntityMetaInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	res, err := a.core.SetEntityMeta(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "e"), in)
	respond(w, res, err)
}

func (a *api) entityReviewed(w http.ResponseWriter, r *http.Request) {
	res, err := a.core.MarkEntityReviewed(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "e"))
	respond(w, res, err)
}

func (a *api) setKindRules(w http.ResponseWriter, r *http.Request) {
	var in core.KindRules
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	res, err := a.core.SetKindRules(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "k"), in)
	respond(w, res, err)
}

func (a *api) locations(w http.ResponseWriter, r *http.Request) {
	res, err := a.core.Locations(r.Context(), actorOf(r), chi.URLParam(r, "p"))
	respond(w, res, err)
}

func (a *api) runQuery(w http.ResponseWriter, r *http.Request) {
	var in core.GraphQuery
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	res, err := a.core.RunQuery(r.Context(), actorOf(r), chi.URLParam(r, "p"), in)
	respond(w, res, err)
}

func (a *api) listQueries(w http.ResponseWriter, r *http.Request) {
	res, err := a.core.SavedQueries(r.Context(), actorOf(r), chi.URLParam(r, "p"))
	respond(w, res, err)
}

func (a *api) saveQuery(w http.ResponseWriter, r *http.Request) {
	var in core.SaveQueryInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	res, err := a.core.SaveQuery(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "q"), in)
	respond(w, res, err)
}

func (a *api) deleteQuery(w http.ResponseWriter, r *http.Request) {
	respond(w, map[string]bool{"ok": true}, a.core.DeleteQuery(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "q")))
}

func (a *api) runSavedQuery(w http.ResponseWriter, r *http.Request) {
	res, err := a.core.RunSavedQuery(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "q"))
	respond(w, res, err)
}

func (a *api) bulkEntities(w http.ResponseWriter, r *http.Request) {
	var in core.BulkInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	res, err := a.core.Bulk(r.Context(), actorOf(r), chi.URLParam(r, "p"), in)
	respond(w, res, err)
}

func (a *api) household(w http.ResponseWriter, r *http.Request) {
	res, err := a.core.Household(r.Context(), actorOf(r), chi.URLParam(r, "p"))
	respond(w, res, err)
}

type powerPriceIn struct {
	Price float64 `json:"price"`
}

func (a *api) setPowerPrice(w http.ResponseWriter, r *http.Request) {
	var in powerPriceIn
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	respond(w, in, a.core.SetPowerPrice(r.Context(), actorOf(r), chi.URLParam(r, "p"), in.Price))
}

type qrIn struct {
	Text string `json:"text"`
}

type qrOut struct {
	SVG string `json:"svg"`
}

// qr renders a QR code (e.g. a guest Wi-Fi card). The text is neither
// stored nor logged.
func (a *api) qr(w http.ResponseWriter, r *http.Request) {
	var in qrIn
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	if in.Text == "" || len(in.Text) > 1000 {
		writeErr(w, core.Invalid("validation.qr_text", nil, "text must have 1 to 1000 characters"))
		return
	}
	svg, err := auth.QRSVG(in.Text)
	w.Header().Set("Cache-Control", "no-store")
	respond(w, qrOut{SVG: svg}, err)
}
