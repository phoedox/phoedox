package server

import (
	"bytes"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/phoedox/phoedox/internal/core"
)

func (a *api) pageSkill(w http.ResponseWriter, r *http.Request) {
	sk, err := a.core.PageSkill(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "d"))
	respond(w, map[string]any{"skill": sk}, err)
}

func (a *api) setPageSkill(w http.ResponseWriter, r *http.Request) {
	var in core.SkillInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	sk, err := a.core.SetPageSkill(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "d"), in)
	respond(w, map[string]any{"skill": sk}, err)
}

func (a *api) removePageSkill(w http.ResponseWriter, r *http.Request) {
	if err := a.core.RemovePageSkill(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "d")); err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) listSkills(w http.ResponseWriter, r *http.Request) {
	res, err := a.core.Skills(r.Context(), actorOf(r), chi.URLParam(r, "p"))
	respond(w, res, err)
}

// mySkills lists the skills of all projects of the signed-in person.
func (a *api) mySkills(w http.ResponseWriter, r *http.Request) {
	res, err := a.core.Skills(r.Context(), actorOf(r), "")
	respond(w, res, err)
}

func (a *api) getSkill(w http.ResponseWriter, r *http.Request) {
	res, err := a.core.Skill(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "name"))
	respond(w, res, err)
}

// downloadSkills sends one skill (/skills/{name}/download) or all skills of
// a project (/skills.zip) as a ZIP of SKILL.md folders.
func (a *api) downloadSkills(w http.ResponseWriter, r *http.Request) {
	var names []string
	file := chi.URLParam(r, "p") + "-skills"
	if n := chi.URLParam(r, "name"); n != "" {
		names, file = []string{n}, n
	}
	var buf bytes.Buffer
	if err := a.core.WriteSkills(r.Context(), actorOf(r), chi.URLParam(r, "p"), names, &buf); err != nil {
		writeErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+file+`.zip"`)
	_, _ = w.Write(buf.Bytes())
}
