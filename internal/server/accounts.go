package server

import (
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/url"

	"github.com/go-chi/chi/v5"

	"github.com/phoedox/phoedox/internal/auth"
	"github.com/phoedox/phoedox/internal/config"
	"github.com/phoedox/phoedox/internal/core"
)

// requireHeader blocks cross-site requests to public endpoints: browsers
// only send the custom header after a successful CORS preflight.
func requireHeader(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Header.Get("X-Phoedox") != "1" {
			writeJSON(w, http.StatusForbidden, errBody("csrf", "Missing X-Phoedox header."))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// trustedBase is the configured public URL. Self-service emails only use
// this one – a URL derived from the Host header could be forged to send a
// reset link to an attacker's domain.
func (a *api) trustedBase() string {
	if a.cfg.PublicURL != "" {
		return a.cfg.PublicURL
	}
	if a.cfg.Domain != "" {
		return "https://" + a.cfg.Domain
	}
	return ""
}

// baseURL is the trusted base or the request's origin.
func (a *api) baseURL(r *http.Request) string { return BaseURL(a.cfg)(r) }

// BaseURL returns a function giving the public origin of a request: the
// configured public URL or domain, else scheme and host of the request.
func BaseURL(cfg config.Config) func(*http.Request) string {
	return func(r *http.Request) string {
		if cfg.PublicURL != "" {
			return cfg.PublicURL
		}
		if cfg.Domain != "" {
			return "https://" + cfg.Domain
		}
		scheme := "http"
		if r.TLS != nil || (cfg.BehindProxy && r.Header.Get("X-Forwarded-Proto") == "https") {
			scheme = "https"
		}
		return scheme + "://" + r.Host
	}
}

func (a *api) resetAvailable() bool {
	return a.auth.Mail != nil && a.auth.Mail.Enabled() && a.trustedBase() != ""
}

func (a *api) limited(w http.ResponseWriter, r *http.Request) bool {
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	if a.limiter.allow(ip) {
		return false
	}
	writeJSON(w, http.StatusTooManyRequests, errBody("rate_limited", "Too many attempts. Please wait a few minutes."))
	return true
}

func (a *api) resetRequest(w http.ResponseWriter, r *http.Request) {
	if a.limited(w, r) {
		return
	}
	var in struct{ Email string }
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	if a.resetAvailable() {
		if err := a.auth.RequestReset(r.Context(), in.Email, a.trustedBase()); err != nil {
			writeErr(w, err)
			return
		}
	}
	// Same answer whether or not the address exists.
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) linkInfo(w http.ResponseWriter, r *http.Request) {
	if a.limited(w, r) {
		return
	}
	info, err := a.auth.Link(r.Context(), chi.URLParam(r, "token"))
	respond(w, info, err)
}

func (a *api) completeLink(w http.ResponseWriter, r *http.Request) {
	if a.limited(w, r) {
		return
	}
	var in struct{ Password string }
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	c, u, err := a.auth.CompleteLink(r.Context(), chi.URLParam(r, "token"), in.Password)
	if err != nil {
		writeErr(w, err)
		return
	}
	http.SetCookie(w, c)
	writeJSON(w, http.StatusOK, u)
}

func (a *api) updateProfile(w http.ResponseWriter, r *http.Request) {
	var in auth.ProfilePatch
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	u, err := a.auth.UpdateProfile(r.Context(), actorOf(r), in)
	respond(w, u, err)
}

func (a *api) changePassword(w http.ResponseWriter, r *http.Request) {
	var in struct{ Current, New string }
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	if err := a.auth.ChangePassword(r.Context(), actorOf(r), in.Current, in.New, auth.SessionID(r)); err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) listUsers(w http.ResponseWriter, r *http.Request) {
	us, err := a.auth.Users(r.Context(), actorOf(r))
	respond(w, us, err)
}

func (a *api) inviteUser(w http.ResponseWriter, r *http.Request) {
	var in auth.InviteInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	res, err := a.auth.Invite(r.Context(), actorOf(r), in, a.baseURL(r))
	respondStatus(w, http.StatusCreated, res, err)
}

func (a *api) updateUser(w http.ResponseWriter, r *http.Request) {
	var in auth.UserPatch
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	u, err := a.auth.UpdateUser(r.Context(), actorOf(r), chi.URLParam(r, "id"), in)
	respond(w, u, err)
}

func (a *api) userLink(w http.ResponseWriter, r *http.Request) {
	res, err := a.auth.NewAccountLink(r.Context(), actorOf(r), chi.URLParam(r, "id"), a.baseURL(r))
	respond(w, res, err)
}

// oidcStart sends the browser to the identity provider.
func (a *api) oidcStart(w http.ResponseWriter, r *http.Request) {
	u, c, err := a.auth.OIDCStart(r.Context(), a.baseURL(r), r.URL.Query().Get("return"))
	if err != nil {
		http.Redirect(w, r, "/login?error="+url.QueryEscape(errKey(err)), http.StatusFound)
		return
	}
	http.SetCookie(w, c)
	http.Redirect(w, r, u, http.StatusFound)
}

// oidcCallback finishes the sign-in and continues in the web app.
func (a *api) oidcCallback(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: auth.OIDCCookie, Path: "/api/v1/auth/oidc", MaxAge: -1, HttpOnly: true, Secure: a.cfg.SecureCookies})
	c, ret, err := a.auth.OIDCCallback(r.Context(), a.baseURL(r), r)
	if err != nil {
		slog.Info("oidc sign-in failed", "err", err)
		http.Redirect(w, r, "/login?error="+url.QueryEscape(errKey(err))+"&return="+url.QueryEscape(ret), http.StatusFound)
		return
	}
	http.SetCookie(w, c)
	http.Redirect(w, r, auth.SafeReturn(ret), http.StatusFound)
}

// errKey is the translation key of an error for redirects to the web app.
func errKey(err error) string {
	var inv *core.InvalidError
	if errors.As(err, &inv) {
		return inv.Key
	}
	return "validation.oidc_failed"
}
