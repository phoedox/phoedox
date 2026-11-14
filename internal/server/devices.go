package server

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/phoedox/phoedox/internal/core"
)

func (a *api) device(w http.ResponseWriter, r *http.Request) {
	res, err := a.core.Device(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "e"))
	respond(w, res, err)
}

func (a *api) setDevice(w http.ResponseWriter, r *http.Request) {
	var in struct {
		AIControl string `json:"aiControl"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	res, err := a.core.SetDeviceAI(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "e"), in.AIControl)
	respond(w, res, err)
}

func (a *api) deviceLog(w http.ResponseWriter, r *http.Request) {
	res, err := a.core.DeviceLog(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "e"))
	respond(w, res, err)
}

func (a *api) controlDevice(w http.ResponseWriter, r *http.Request) {
	var in core.ControlInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	out, err := a.core.ControlDevice(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "e"), in)
	respond(w, json.RawMessage(out), err)
}

func (a *api) grantControl(w http.ResponseWriter, r *http.Request) {
	var in core.DeviceGrantInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	res, err := a.core.GrantControl(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "e"), in)
	respond(w, res, err)
}

func (a *api) revokeControl(w http.ResponseWriter, r *http.Request) {
	res, err := a.core.RevokeControl(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "e"), chi.URLParam(r, "id"))
	respond(w, res, err)
}

func (a *api) sharedDevices(w http.ResponseWriter, r *http.Request) {
	res, err := a.core.SharedDevices(r.Context(), actorOf(r))
	respond(w, res, err)
}

func (a *api) controlShared(w http.ResponseWriter, r *http.Request) {
	var in core.ControlInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	out, err := a.core.ControlShared(r.Context(), actorOf(r), chi.URLParam(r, "grant"), in)
	respond(w, json.RawMessage(out), err)
}
