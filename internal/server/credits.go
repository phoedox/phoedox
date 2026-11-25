package server

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/phoedox/phoedox/internal/core"
)

// AI credits (ADR-0080).

func (a *api) myCredits(w http.ResponseWriter, r *http.Request) {
	v, err := a.core.MyCredits(r.Context(), actorOf(r))
	respond(w, v, err)
}

func (a *api) orgCredits(w http.ResponseWriter, r *http.Request) {
	v, err := a.core.OrgCredits(r.Context(), actorOf(r), chi.URLParam(r, "o"))
	respond(w, v, err)
}

type buyCreditsIn struct {
	Offer string `json:"offer"`
}

func (a *api) buyCredits(w http.ResponseWriter, r *http.Request) {
	var in buyCreditsIn
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	v, err := a.core.BuyCredits(r.Context(), actorOf(r), chi.URLParam(r, "o"), in.Offer)
	respond(w, v, err)
}

func (a *api) adminCredits(w http.ResponseWriter, r *http.Request) {
	v, err := a.core.AdminCredits(r.Context(), actorOf(r))
	respond(w, v, err)
}

func (a *api) setCreditOffers(w http.ResponseWriter, r *http.Request) {
	var in []core.CreditOffer
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	v, err := a.core.SetCreditOffers(r.Context(), actorOf(r), in)
	respond(w, v, err)
}

func (a *api) grantCredits(w http.ResponseWriter, r *http.Request) {
	var in core.CreditGrantInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	v, err := a.core.GrantCredits(r.Context(), actorOf(r), in)
	respond(w, v, err)
}

// Semantic search settings (ADR-0081).

func (a *api) adminEmbeddings(w http.ResponseWriter, r *http.Request) {
	v, err := a.core.AdminEmbeddings(r.Context(), actorOf(r))
	respond(w, v, err)
}

func (a *api) setAdminEmbeddings(w http.ResponseWriter, r *http.Request) {
	var in core.EmbeddingSettings
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	v, err := a.core.SetAdminEmbeddings(r.Context(), actorOf(r), in)
	respond(w, v, err)
}

func (a *api) adminRenderer(w http.ResponseWriter, r *http.Request) {
	v, err := a.core.AdminRenderer(r.Context(), actorOf(r))
	respond(w, v, err)
}

func (a *api) setAdminRenderer(w http.ResponseWriter, r *http.Request) {
	var in core.RendererSettings
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	v, err := a.core.SetAdminRenderer(r.Context(), actorOf(r), in)
	respond(w, v, err)
}

func (a *api) rendererUses(w http.ResponseWriter, r *http.Request) {
	respond(w, a.core.RendererUses(r.Context()), nil)
}

// printPDF returns pages as PDF, printed by the server-side browser
// (?doc=slug or ?category=name, ?lang).
func (a *api) printPDF(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	pdf, name, err := a.core.PrintPDF(r.Context(), actorOf(r), chi.URLParam(r, "p"), q.Get("doc"), q.Get("category"), q.Get("lang"))
	if errors.Is(err, core.ErrRendererOff) {
		writeJSON(w, http.StatusConflict, errBody("renderer_off", "The server-side browser is off for PDF export."))
		return
	}
	if err != nil {
		writeErr(w, err)
		return
	}
	file := strings.Trim(fileNameRe.ReplaceAllString(name, "-"), "-")
	if file == "" {
		file = "phoedox"
	}
	h := w.Header()
	h.Set("Content-Type", "application/pdf")
	h.Set("Content-Disposition", `attachment; filename="`+file+`.pdf"`)
	h.Set("Cache-Control", "no-store")
	_, _ = w.Write(pdf)
}
