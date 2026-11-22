package server

import (
	"encoding/json"
	"io"
	"net/http"
)

// Instance license (ADR-0092): system admins see its state, upload the
// signed file from the store or remove it.

func (a *api) adminLicense(w http.ResponseWriter, r *http.Request) {
	v, err := a.core.LicenseForAdmin(r.Context(), actorOf(r))
	respond(w, v, err)
}

func (a *api) setLicense(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 128<<10))
	if err != nil {
		writeErr(w, err)
		return
	}
	v, err := a.core.SetLicense(r.Context(), actorOf(r), json.RawMessage(raw))
	respond(w, v, err)
}

func (a *api) removeLicense(w http.ResponseWriter, r *http.Request) {
	v, err := a.core.SetLicense(r.Context(), actorOf(r), nil)
	respond(w, v, err)
}
