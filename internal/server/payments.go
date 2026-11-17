package server

import (
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/phoedox/phoedox/internal/core"
)

// Payments (ADR-0079): settings, checkouts, the customer portal and the
// provider's webhook.

func (a *api) adminPayments(w http.ResponseWriter, r *http.Request) {
	v, err := a.core.AdminPayments(r.Context(), actorOf(r))
	respond(w, v, err)
}

func (a *api) setAdminPayments(w http.ResponseWriter, r *http.Request) {
	var in core.PaymentSettingsInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	v, err := a.core.SetAdminPayments(r.Context(), actorOf(r), in)
	respond(w, v, err)
}

func (a *api) checkPayments(w http.ResponseWriter, r *http.Request) {
	v, err := a.core.CheckPayments(r.Context(), actorOf(r))
	respond(w, v, err)
}

func (a *api) testPurchase(w http.ResponseWriter, r *http.Request) {
	v, err := a.core.TestPurchase(r.Context(), actorOf(r))
	respond(w, v, err)
}

func (a *api) adminPurchases(w http.ResponseWriter, r *http.Request) {
	v, err := a.core.AdminPurchases(r.Context(), actorOf(r))
	respond(w, v, err)
}

func (a *api) refundPurchase(w http.ResponseWriter, r *http.Request) {
	respond(w, nil, a.core.RefundPurchase(r.Context(), actorOf(r), chi.URLParam(r, "id")))
}

func (a *api) myPurchases(w http.ResponseWriter, r *http.Request) {
	v, err := a.core.MyPurchases(r.Context(), actorOf(r))
	respond(w, v, err)
}

type portalIn struct {
	ReturnPath string `json:"returnPath"`
}

func (a *api) paymentPortal(w http.ResponseWriter, r *http.Request) {
	var in portalIn
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	u, err := a.core.PaymentPortal(r.Context(), actorOf(r), in.ReturnPath)
	respond(w, map[string]string{"url": u}, err)
}

// payInfo serves the /pay page (Paddle's checkout on our domain).
func (a *api) payInfo(w http.ResponseWriter, r *http.Request) {
	v, err := a.core.PaymentPayInfo(r.Context(), chi.URLParam(r, "ref"))
	respond(w, v, err)
}

// paymentWebhook reads the raw body: the signature covers its exact bytes.
func (a *api) paymentWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "unreadable", http.StatusBadRequest)
		return
	}
	err = a.core.HandlePaymentWebhook(r.Context(), chi.URLParam(r, "provider"), r.Header, body)
	switch {
	case err == nil:
		w.WriteHeader(http.StatusOK)
	case errors.Is(err, core.ErrNotFound):
		http.NotFound(w, r)
	default:
		writeErr(w, err)
	}
}
