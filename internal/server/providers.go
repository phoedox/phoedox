package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/phoedox/phoedox/internal/core"
)

// Service providers (ADR-0073).

func (a *api) orgProviders(w http.ResponseWriter, r *http.Request) {
	v, err := a.core.OrgProviderLinks(r.Context(), actorOf(r), chi.URLParam(r, "o"))
	respond(w, v, err)
}

func (a *api) grantProvider(w http.ResponseWriter, r *http.Request) {
	var in core.ProviderInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	v, err := a.core.GrantProvider(r.Context(), actorOf(r), chi.URLParam(r, "o"), in)
	respond(w, v, err)
}

func (a *api) revokeProvider(w http.ResponseWriter, r *http.Request) {
	respond(w, map[string]bool{"ok": true}, a.core.RevokeProvider(r.Context(), actorOf(r), chi.URLParam(r, "o"), chi.URLParam(r, "id")))
}

func (a *api) customerOverview(w http.ResponseWriter, r *http.Request) {
	v, err := a.core.ProviderOverview(r.Context(), actorOf(r), chi.URLParam(r, "o"))
	respond(w, v, err)
}

func (a *api) customerTimeCSV(w http.ResponseWriter, r *http.Request) {
	month := r.URL.Query().Get("month")
	b, err := a.core.ProviderTimeCSV(r.Context(), actorOf(r), chi.URLParam(r, "o"), month)
	if err != nil {
		writeErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="time-customers-`+month+`.csv"`)
	_, _ = w.Write(b)
}
