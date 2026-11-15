package server

import (
	"net/http"
	"strings"

	"github.com/phoedox/phoedox/internal/core"
)

// "Sign in with Phoedox" for our own services (ADR-0058).

func (a *api) identityRequest(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	c, err := a.auth.IdentityRequest(q.Get("client_id"), q.Get("redirect_uri"))
	if err != nil {
		writeErr(w, err)
		return
	}
	if c.App {
		writeJSON(w, http.StatusOK, map[string]any{"clientId": c.ID, "name": c.Name, "app": true})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"clientId": c.ID, "name": c.Name, "host": hostOnly(c.Redirect)})
}

func hostOnly(u string) string {
	u = strings.TrimPrefix(strings.TrimPrefix(u, "https://"), "http://")
	host, _, _ := strings.Cut(u, "/")
	return host
}

func (a *api) identityApprove(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ClientID      string `json:"client_id"`
		RedirectURI   string `json:"redirect_uri"`
		State         string `json:"state"`
		CodeChallenge string `json:"code_challenge"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	redirect, err := a.auth.ApproveIdentity(r.Context(), actorOf(r), in.ClientID, in.RedirectURI, in.State, in.CodeChallenge)
	respond(w, map[string]string{"redirect": redirect}, err)
}

func (a *api) identityToken(w http.ResponseWriter, r *http.Request) {
	if !a.regLimiter.allow("identity:" + clientIP(r)) {
		writeJSON(w, http.StatusTooManyRequests, errBody("rate_limited", "Too many attempts. Please wait a few minutes."))
		return
	}
	var in struct {
		ClientID     string `json:"client_id"`
		RedirectURI  string `json:"redirect_uri"`
		Code         string `json:"code"`
		CodeVerifier string `json:"code_verifier"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	id, err := a.auth.RedeemIdentity(in.ClientID, in.RedirectURI, in.Code, in.CodeVerifier)
	if err != nil {
		writeErr(w, err)
		return
	}
	// The person may have been disabled or deleted since the code was issued.
	if u, err := a.auth.DB.UserByID(r.Context(), id.Subject); err != nil || u.Disabled || u.Deleted {
		writeErr(w, core.ErrForbidden)
		return
	}
	writeJSON(w, http.StatusOK, id)
}

// appToken finishes the sign-in of the mobile app (ADR-0065): code and
// PKCE verifier become a personal API token for the device.
func (a *api) appToken(w http.ResponseWriter, r *http.Request) {
	if !a.regLimiter.allow("identity:" + clientIP(r)) {
		writeJSON(w, http.StatusTooManyRequests, errBody("rate_limited", "Too many attempts. Please wait a few minutes."))
		return
	}
	var in struct {
		Code         string `json:"code"`
		CodeVerifier string `json:"code_verifier"`
		Device       string `json:"device"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	token, id, err := a.auth.RedeemApp(r.Context(), in.Code, in.CodeVerifier, in.Device)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"token": token, "name": id.Name, "email": id.Email})
}
