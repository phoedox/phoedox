package server

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/phoedox/phoedox/internal/core"
)

func (a *api) listOrgs(w http.ResponseWriter, r *http.Request) {
	os, err := a.core.Orgs(r.Context(), actorOf(r))
	respond(w, os, err)
}

func (a *api) createOrg(w http.ResponseWriter, r *http.Request) {
	var in core.OrgInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	o, err := a.core.CreateOrg(r.Context(), actorOf(r), in)
	respondStatus(w, http.StatusCreated, o, err)
}

func (a *api) getOrg(w http.ResponseWriter, r *http.Request) {
	o, err := a.core.Org(r.Context(), actorOf(r), chi.URLParam(r, "o"))
	respond(w, o, err)
}

func (a *api) updateOrg(w http.ResponseWriter, r *http.Request) {
	var in core.OrgInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	o, err := a.core.UpdateOrg(r.Context(), actorOf(r), chi.URLParam(r, "o"), in)
	respond(w, o, err)
}

func (a *api) setOrgMember(w http.ResponseWriter, r *http.Request) {
	var in struct{ Email, Role string }
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	respond(w, map[string]bool{"ok": true}, a.core.SetOrgMember(r.Context(), actorOf(r), chi.URLParam(r, "o"), in.Email, in.Role))
}

func (a *api) removeOrgMember(w http.ResponseWriter, r *http.Request) {
	respond(w, map[string]bool{"ok": true}, a.core.RemoveOrgMember(r.Context(), actorOf(r), chi.URLParam(r, "o"), chi.URLParam(r, "user")))
}

func (a *api) saveGroup(w http.ResponseWriter, r *http.Request) {
	var in core.GroupInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	g, err := a.core.SaveGroup(r.Context(), actorOf(r), chi.URLParam(r, "o"), chi.URLParam(r, "g"), in)
	respond(w, g, err)
}

func (a *api) deleteGroup(w http.ResponseWriter, r *http.Request) {
	respond(w, map[string]bool{"ok": true}, a.core.DeleteGroup(r.Context(), actorOf(r), chi.URLParam(r, "o"), chi.URLParam(r, "g")))
}

func (a *api) groupMember(on bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Email string }
		if err := decode(r, &in); err != nil {
			writeErr(w, err)
			return
		}
		respond(w, map[string]bool{"ok": true}, a.core.SetGroupMember(r.Context(), actorOf(r), chi.URLParam(r, "o"), chi.URLParam(r, "g"), in.Email, on))
	}
}

func (a *api) removeMember(w http.ResponseWriter, r *http.Request) {
	respond(w, map[string]bool{"ok": true}, a.core.RemoveMember(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "user")))
}

func (a *api) listProjectGroups(w http.ResponseWriter, r *http.Request) {
	gs, err := a.core.ProjectGroups(r.Context(), actorOf(r), chi.URLParam(r, "p"))
	respond(w, gs, err)
}

func (a *api) setProjectGroup(w http.ResponseWriter, r *http.Request) {
	var in struct{ Role string }
	if r.Method != http.MethodDelete {
		if err := decode(r, &in); err != nil {
			writeErr(w, err)
			return
		}
	}
	respond(w, map[string]bool{"ok": true}, a.core.SetProjectGroup(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "g"), in.Role))
}

func (a *api) sharing(w http.ResponseWriter, r *http.Request) {
	s, err := a.core.Sharing(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "d"))
	respond(w, s, err)
}

func (a *api) setRestricted(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Restricted bool `json:"restricted"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	s, err := a.core.SetRestricted(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "d"), in.Restricted)
	respond(w, s, err)
}

func (a *api) shareDocument(w http.ResponseWriter, r *http.Request) {
	var in core.GrantInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	s, err := a.core.ShareDocument(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "d"), in)
	respond(w, s, err)
}

func (a *api) unshareDocument(w http.ResponseWriter, r *http.Request) {
	s, err := a.core.UnshareDocument(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "d"), chi.URLParam(r, "id"))
	respond(w, s, err)
}

func (a *api) listShareLinks(w http.ResponseWriter, r *http.Request) {
	ls, err := a.core.ShareLinks(r.Context(), actorOf(r), chi.URLParam(r, "p"))
	respond(w, ls, err)
}

func (a *api) createShareLink(w http.ResponseWriter, r *http.Request) {
	var in core.ShareLinkInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	l, err := a.core.CreateShareLink(r.Context(), actorOf(r), chi.URLParam(r, "p"), in)
	respondStatus(w, http.StatusCreated, l, err)
}

func (a *api) revokeShareLink(w http.ResponseWriter, r *http.Request) {
	respond(w, map[string]bool{"ok": true}, a.core.RevokeShareLink(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "id")))
}

// Shared views need no account; the token in the URL is the key. Responses
// are not cached by shared caches and not indexed.
func sharedHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Robots-Tag", "noindex")
	w.Header().Set("Referrer-Policy", "no-referrer")
}

func (a *api) sharedView(w http.ResponseWriter, r *http.Request) {
	sharedHeaders(w)
	v, err := a.core.Shared(r.Context(), chi.URLParam(r, "token"))
	respond(w, v, err)
}

func (a *api) sharedDocument(w http.ResponseWriter, r *http.Request) {
	sharedHeaders(w)
	d, err := a.core.SharedDocument(r.Context(), chi.URLParam(r, "token"), chi.URLParam(r, "d"))
	respond(w, d, err)
}

func (a *api) sharedFile(w http.ResponseWriter, r *http.Request) {
	sharedHeaders(w)
	fc, err := a.core.SharedFile(r.Context(), chi.URLParam(r, "token"), chi.URLParam(r, "id"), r.URL.Query().Get("preview") != "")
	if err != nil {
		writeErr(w, err)
		return
	}
	a.writeFile(w, r, fc, 0)
}

// parseUntil reads a guest's end date ("2026-12-31" or RFC 3339); empty = none.
func parseUntil(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse(time.DateOnly, s); err == nil {
		return t.Add(24*time.Hour - time.Second), nil // until the end of that day (UTC)
	}
	return time.Parse(time.RFC3339, s)
}

func (a *api) orgDomain(w http.ResponseWriter, r *http.Request) {
	d, err := a.core.OrgDomain(r.Context(), actorOf(r), chi.URLParam(r, "o"))
	respond(w, map[string]any{"domain": d}, err)
}

func (a *api) setOrgDomain(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Domain string `json:"domain"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	d, err := a.core.SetOrgDomain(r.Context(), actorOf(r), chi.URLParam(r, "o"), in.Domain)
	respond(w, map[string]any{"domain": d}, err)
}

func (a *api) verifyOrgDomain(w http.ResponseWriter, r *http.Request) {
	d, err := a.core.VerifyOrgDomain(r.Context(), actorOf(r), chi.URLParam(r, "o"))
	respond(w, map[string]any{"domain": d}, err)
}
