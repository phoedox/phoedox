package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/phoedox/phoedox/internal/auth"
	"github.com/phoedox/phoedox/internal/core"
)

// Personal data export and account deletion (ADR-0055).

func (a *api) myExport(w http.ResponseWriter, r *http.Request) {
	st, err := a.core.MyExport(r.Context(), actorOf(r))
	respond(w, st, err)
}

func (a *api) requestExport(w http.ResponseWriter, r *http.Request) {
	e, err := a.core.RequestExport(r.Context(), actorOf(r))
	respondStatus(w, http.StatusAccepted, e, err)
}

func (a *api) downloadExport(w http.ResponseWriter, r *http.Request) {
	e, rc, err := a.core.ExportDownload(r.Context(), actorOf(r), chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rc.Close()
	h := w.Header()
	h.Set("Content-Type", "application/zip")
	h.Set("Content-Disposition", `attachment; filename="phoedox-data-`+e.CreatedAt.UTC().Format("2006-01-02")+`.zip"`)
	h.Set("Cache-Control", "private, no-store")
	http.ServeContent(w, r, "", e.CreatedAt.Time, rc)
}

// deletionInfo is what the account page shows before deleting.
type deletionInfo struct {
	core.DeletionPlan
	auth.DeletionNeeds
}

func (a *api) accountDeletion(w http.ResponseWriter, r *http.Request) {
	plan, err := a.core.AccountDeletion(r.Context(), actorOf(r))
	if err != nil {
		writeErr(w, err)
		return
	}
	needs, err := a.auth.DeletionNeeds(r.Context(), actorOf(r).User.ID)
	respond(w, deletionInfo{plan, needs}, err)
}

func (a *api) deleteAccount(w http.ResponseWriter, r *http.Request) {
	var in auth.DeleteInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	actor := actorOf(r)
	if err := a.auth.ConfirmDeletion(r.Context(), actor, auth.SessionID(r), in); err != nil {
		writeErr(w, err)
		return
	}
	if _, err := a.core.DeleteAccount(r.Context(), actor, actor.User.ID); err != nil {
		writeErr(w, err)
		return
	}
	http.SetCookie(w, a.auth.Logout(r))
	w.WriteHeader(http.StatusNoContent)
}
