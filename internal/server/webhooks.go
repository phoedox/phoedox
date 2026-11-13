package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/phoedox/phoedox/internal/core"
)

func (a *api) listWebhooks(w http.ResponseWriter, r *http.Request) {
	hs, err := a.core.Webhooks(r.Context(), actorOf(r), chi.URLParam(r, "p"))
	respond(w, map[string]any{"webhooks": hs, "events": core.WebhookEventTypes()}, err)
}

func (a *api) createWebhook(w http.ResponseWriter, r *http.Request) {
	var in core.WebhookInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	h, err := a.core.CreateWebhook(r.Context(), actorOf(r), chi.URLParam(r, "p"), in)
	respondStatus(w, http.StatusCreated, h, err)
}

func (a *api) updateWebhook(w http.ResponseWriter, r *http.Request) {
	var in core.WebhookInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	h, err := a.core.UpdateWebhook(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "id"), in)
	respond(w, h, err)
}

func (a *api) deleteWebhook(w http.ResponseWriter, r *http.Request) {
	respond(w, map[string]bool{"ok": true}, a.core.DeleteWebhook(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "id")))
}

func (a *api) webhookDeliveries(w http.ResponseWriter, r *http.Request) {
	ds, err := a.core.WebhookDeliveries(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "id"))
	respond(w, ds, err)
}

func (a *api) testWebhook(w http.ResponseWriter, r *http.Request) {
	x, err := a.core.TestWebhook(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "id"))
	respond(w, x, err)
}
