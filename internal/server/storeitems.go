package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/phoedox/phoedox/internal/core"
)

// Themes and app templates from the store.

func (a *api) storeItems(w http.ResponseWriter, r *http.Request) {
	v, err := a.core.StoreCatalog(r.Context(), actorOf(r), r.URL.Query().Get("type"), r.URL.Query().Get("project"))
	respond(w, v, err)
}

func (a *api) storeItemVersions(w http.ResponseWriter, r *http.Request) {
	v, err := a.core.StoreVersions(r.Context(), actorOf(r), chi.URLParam(r, "id"))
	respond(w, v, err)
}

func (a *api) installStoreTheme(w http.ResponseWriter, r *http.Request) {
	var in core.StoreThemeInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	v, err := a.core.InstallStoreTheme(r.Context(), actorOf(r), in)
	respond(w, v, err)
}

func (a *api) installStoreApp(w http.ResponseWriter, r *http.Request) {
	var in core.StoreAppInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	v, err := a.core.InstallStoreApp(r.Context(), actorOf(r), chi.URLParam(r, "p"), in)
	respond(w, v, err)
}

func (a *api) requestStoreItem(w http.ResponseWriter, r *http.Request) {
	var in core.StoreRequestInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	v, err := a.core.RequestStoreItem(r.Context(), actorOf(r), chi.URLParam(r, "p"), in)
	respond(w, v, err)
}

func (a *api) myStoreRequests(w http.ResponseWriter, r *http.Request) {
	v, err := a.core.MyStoreRequests(r.Context(), actorOf(r), chi.URLParam(r, "p"))
	respond(w, v, err)
}

func (a *api) orgStoreAccess(w http.ResponseWriter, r *http.Request) {
	v, err := a.core.OrgStoreAccess(r.Context(), actorOf(r), chi.URLParam(r, "o"))
	respond(w, v, err)
}

func (a *api) setOrgStoreAccess(w http.ResponseWriter, r *http.Request) {
	var in core.StoreAccess
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	v, err := a.core.SetOrgStoreAccess(r.Context(), actorOf(r), chi.URLParam(r, "o"), in)
	respond(w, v, err)
}

func (a *api) orgStoreRequests(w http.ResponseWriter, r *http.Request) {
	v, err := a.core.StoreRequests(r.Context(), actorOf(r), chi.URLParam(r, "o"))
	respond(w, v, err)
}

func (a *api) decideStoreRequest(w http.ResponseWriter, r *http.Request) {
	var in core.StoreDecision
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	v, err := a.core.DecideStoreRequest(r.Context(), actorOf(r), chi.URLParam(r, "o"), chi.URLParam(r, "id"), in)
	respond(w, v, err)
}
