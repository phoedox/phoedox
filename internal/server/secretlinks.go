package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/phoedox/phoedox/internal/core"
)

func (a *api) createSecretLink(w http.ResponseWriter, r *http.Request) {
	var in core.SecretLinkInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	l, err := a.core.CreateSecretLink(r.Context(), actorOf(r), in)
	respondStatus(w, http.StatusCreated, l, err)
}

func (a *api) openSecretLink(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	l, err := a.core.OpenSecretLink(r.Context(), chi.URLParam(r, "id"))
	respond(w, map[string]any{"ciphertext": l.Ciphertext, "passphrase": l.Passphrase}, err)
}

func (a *api) verifyAudit(w http.ResponseWriter, r *http.Request) {
	rep, err := a.core.VerifyAudit(r.Context(), actorOf(r))
	respond(w, rep, err)
}
