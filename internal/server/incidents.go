package server

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/phoedox/phoedox/internal/core"
)

func (a *api) listIncidents(w http.ResponseWriter, r *http.Request) {
	list, err := a.core.Incidents(r.Context(), actorOf(r), chi.URLParam(r, "p"))
	respond(w, list, err)
}

func (a *api) createIncident(w http.ResponseWriter, r *http.Request) {
	var in core.IncidentInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	inc, err := a.core.CreateIncident(r.Context(), actorOf(r), chi.URLParam(r, "p"), in)
	respondStatus(w, http.StatusCreated, inc, err)
}

func (a *api) updateIncident(w http.ResponseWriter, r *http.Request) {
	var in core.IncidentInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	inc, err := a.core.UpdateIncident(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "id"), in)
	respond(w, inc, err)
}

func (a *api) deleteIncident(w http.ResponseWriter, r *http.Request) {
	respond(w, map[string]bool{"ok": true}, a.core.DeleteIncident(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "id")))
}

func (a *api) addIncidentUpdate(w http.ResponseWriter, r *http.Request) {
	var in core.IncidentUpdateInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	inc, err := a.core.AddIncidentUpdate(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "id"), in)
	respond(w, inc, err)
}

func (a *api) projectStatusPage(w http.ResponseWriter, r *http.Request) {
	s, err := a.core.ProjectStatusPage(r.Context(), actorOf(r), chi.URLParam(r, "p"))
	respond(w, s, err)
}

func (a *api) setProjectStatusPage(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Enabled bool `json:"enabled"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	s, err := a.core.SetProjectStatusPage(r.Context(), actorOf(r), chi.URLParam(r, "p"), in.Enabled)
	respond(w, s, err)
}

// publicStatus returns a public status page as JSON (for widgets and tools).
func (a *api) publicStatus(w http.ResponseWriter, r *http.Request) {
	page, _, err := a.core.StatusByToken(r.Context(), chi.URLParam(r, "token"), time.Now())
	w.Header().Set("Cache-Control", "public, max-age=30")
	respond(w, page, err)
}
