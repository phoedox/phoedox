package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/phoedox/phoedox/internal/core"
)

// Automation rules (ADR-0060).

func (a *api) listRules(w http.ResponseWriter, r *http.Request) {
	rules, err := a.core.Rules(r.Context(), actorOf(r), chi.URLParam(r, "p"))
	respond(w, rules, err)
}

func (a *api) saveRule(w http.ResponseWriter, r *http.Request) {
	var in core.RuleInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	rule, err := a.core.SaveRule(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "id"), in)
	respond(w, rule, err)
}

func (a *api) deleteRule(w http.ResponseWriter, r *http.Request) {
	if err := a.core.DeleteRule(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "id")); err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) testRule(w http.ResponseWriter, r *http.Request) {
	err := a.core.TestRule(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "id"))
	respond(w, map[string]bool{"ok": err == nil}, err)
}
