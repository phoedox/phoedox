package server

import (
	"net/http"
	"net/url"

	"github.com/go-chi/chi/v5"

	"github.com/phoedox/phoedox/internal/store"
)

// webauthnBody limits the size of WebAuthn responses read by the library.
func webauthnBody(w http.ResponseWriter, r *http.Request) *http.Request {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	return r
}

func (a *api) session(w http.ResponseWriter, c *http.Cookie, u store.User, err error) {
	if err != nil {
		writeErr(w, err)
		return
	}
	http.SetCookie(w, c)
	writeJSON(w, http.StatusOK, u)
}

type codeIn struct {
	Challenge string `json:"challenge"`
	Code      string `json:"code"`
}

func (a *api) loginTOTP(w http.ResponseWriter, r *http.Request) {
	if a.limited(w, r) {
		return
	}
	var in codeIn
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	c, u, err := a.auth.LoginTOTP(r.Context(), in.Challenge, in.Code)
	a.session(w, c, u, err)
}

func (a *api) loginRecovery(w http.ResponseWriter, r *http.Request) {
	if a.limited(w, r) {
		return
	}
	var in codeIn
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	c, u, err := a.auth.LoginRecovery(r.Context(), in.Challenge, in.Code)
	a.session(w, c, u, err)
}

type ceremony struct {
	Options any    `json:"options"`
	Token   string `json:"token"`
}

func (a *api) loginPasskeyBegin(w http.ResponseWriter, r *http.Request) {
	var in struct{ Challenge string }
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	opts, token, err := a.auth.BeginPasskeySecondFactor(r.Context(), in.Challenge, a.baseURL(r))
	respond(w, ceremony{opts, token}, err)
}

func (a *api) loginPasskeyFinish(w http.ResponseWriter, r *http.Request) {
	if a.limited(w, r) {
		return
	}
	c, u, err := a.auth.FinishPasskeySecondFactor(r.Context(), r.URL.Query().Get("token"), a.baseURL(r), webauthnBody(w, r))
	a.session(w, c, u, err)
}

func (a *api) passkeyBegin(w http.ResponseWriter, r *http.Request) {
	if a.limited(w, r) {
		return
	}
	opts, token, err := a.auth.BeginPasskeyLogin(r.Context(), a.baseURL(r))
	respond(w, ceremony{opts, token}, err)
}

func (a *api) passkeyFinish(w http.ResponseWriter, r *http.Request) {
	c, u, err := a.auth.FinishPasskeyLogin(r.Context(), r.URL.Query().Get("token"), a.baseURL(r), webauthnBody(w, r))
	a.session(w, c, u, err)
}

func (a *api) security(w http.ResponseWriter, r *http.Request) {
	s, err := a.auth.Security(r.Context(), actorOf(r))
	respond(w, s, err)
}

func (a *api) totpSetup(w http.ResponseWriter, r *http.Request) {
	issuer := "Phoedox"
	if u, err := url.Parse(a.baseURL(r)); err == nil && u.Hostname() != "" {
		issuer = "Phoedox (" + u.Hostname() + ")"
	}
	s, err := a.auth.SetupTOTP(r.Context(), actorOf(r), issuer)
	respond(w, s, err)
}

type codesOut struct {
	RecoveryCodes []string `json:"recoveryCodes"`
}

func (a *api) totpConfirm(w http.ResponseWriter, r *http.Request) {
	var in struct{ Code string }
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	codes, err := a.auth.ConfirmTOTP(r.Context(), actorOf(r), in.Code)
	respond(w, codesOut{codes}, err)
}

type confirmIn struct {
	Password string `json:"password"`
	Code     string `json:"code"`
}

func (a *api) totpDisable(w http.ResponseWriter, r *http.Request) {
	var in confirmIn
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	if err := a.auth.DisableTOTP(r.Context(), actorOf(r), in.Password, in.Code); err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) recoveryCodes(w http.ResponseWriter, r *http.Request) {
	var in confirmIn
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	codes, err := a.auth.RegenerateRecoveryCodes(r.Context(), actorOf(r), in.Password, in.Code)
	respond(w, codesOut{codes}, err)
}

func (a *api) passkeyRegisterBegin(w http.ResponseWriter, r *http.Request) {
	opts, token, err := a.auth.BeginPasskeyRegistration(r.Context(), actorOf(r), a.baseURL(r))
	respond(w, ceremony{opts, token}, err)
}

func (a *api) passkeyRegisterFinish(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	p, err := a.auth.FinishPasskeyRegistration(r.Context(), actorOf(r), q.Get("token"), q.Get("name"), a.baseURL(r), webauthnBody(w, r))
	respondStatus(w, http.StatusCreated, p, err)
}

func (a *api) passkeyRename(w http.ResponseWriter, r *http.Request) {
	var in struct{ Name string }
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	if err := a.auth.RenamePasskey(r.Context(), actorOf(r), chi.URLParam(r, "id"), in.Name); err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) passkeyDelete(w http.ResponseWriter, r *http.Request) {
	if err := a.auth.DeletePasskey(r.Context(), actorOf(r), chi.URLParam(r, "id")); err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) reset2FA(w http.ResponseWriter, r *http.Request) {
	if err := a.auth.ResetSecondFactors(r.Context(), actorOf(r), chi.URLParam(r, "id")); err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
