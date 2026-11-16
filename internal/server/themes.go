package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/phoedox/phoedox/internal/core"
)

// UI themes: the person's choice, the themes of people, projects,
// organizations and the instance, and the theme editor's check.

func (a *api) themes(w http.ResponseWriter, r *http.Request) {
	v, err := a.core.Themes(r.Context(), actorOf(r), r.URL.Query().Get("project"))
	respond(w, v, err)
}

func (a *api) createTheme(w http.ResponseWriter, r *http.Request) {
	var in core.ThemeInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	v, err := a.core.CreateTheme(r.Context(), actorOf(r), in)
	respondStatus(w, http.StatusCreated, v, err)
}

func (a *api) getTheme(w http.ResponseWriter, r *http.Request) {
	v, err := a.core.GetTheme(r.Context(), actorOf(r), chi.URLParam(r, "id"))
	respond(w, v, err)
}

func (a *api) updateTheme(w http.ResponseWriter, r *http.Request) {
	var in core.ThemeInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	v, err := a.core.UpdateTheme(r.Context(), actorOf(r), chi.URLParam(r, "id"), in)
	respond(w, v, err)
}

func (a *api) deleteTheme(w http.ResponseWriter, r *http.Request) {
	respond(w, nil, a.core.DeleteTheme(r.Context(), actorOf(r), chi.URLParam(r, "id")))
}

func (a *api) releaseTheme(w http.ResponseWriter, r *http.Request) {
	v, err := a.core.ReleaseThemeCSS(r.Context(), actorOf(r), chi.URLParam(r, "id"))
	respond(w, v, err)
}

func (a *api) checkTheme(w http.ResponseWriter, r *http.Request) {
	var in core.ThemeInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, a.core.CheckTheme(r.Context(), actorOf(r), in))
}

func (a *api) setMyTheme(w http.ResponseWriter, r *http.Request) {
	var in core.MyThemeInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	v, err := a.core.SetMyTheme(r.Context(), actorOf(r), in)
	respond(w, v, err)
}

func (a *api) setProjectTheme(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ThemeID string `json:"themeId"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	respond(w, nil, a.core.SetProjectTheme(r.Context(), actorOf(r), chi.URLParam(r, "p"), in.ThemeID))
}

func (a *api) orgTheme(w http.ResponseWriter, r *http.Request) {
	v, err := a.core.OrgThemeOf(r.Context(), actorOf(r), chi.URLParam(r, "o"))
	respond(w, v, err)
}

func (a *api) setOrgTheme(w http.ResponseWriter, r *http.Request) {
	var in core.OrgThemeInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	v, err := a.core.SetOrgTheme(r.Context(), actorOf(r), chi.URLParam(r, "o"), in)
	respond(w, v, err)
}

func (a *api) setInstanceTheme(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ThemeID string `json:"themeId"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	respond(w, nil, a.core.SetInstanceTheme(r.Context(), actorOf(r), in.ThemeID))
}

func (a *api) autoLinks(w http.ResponseWriter, r *http.Request) {
	v, err := a.core.ProjectAutoLinks(r.Context(), actorOf(r), chi.URLParam(r, "p"))
	respond(w, v, err)
}

func (a *api) setAutoLinks(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Rules []core.AutoLinkRule `json:"rules"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	v, err := a.core.SetAutoLinkRules(r.Context(), actorOf(r), chi.URLParam(r, "p"), in.Rules)
	respond(w, map[string]any{"rules": v}, err)
}
