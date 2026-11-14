package server

import (
	"net/http"
	"strconv"
)

func (a *api) recordView(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Project string `json:"project"`
		Kind    string `json:"kind"`
		Ref     string `json:"ref"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	respond(w, map[string]bool{"ok": true}, a.core.RecordView(r.Context(), actorOf(r), in.Project, in.Kind, in.Ref))
}

func (a *api) recent(w http.ResponseWriter, r *http.Request) {
	n, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	list, err := a.core.Recent(r.Context(), actorOf(r), n)
	respond(w, list, err)
}

func (a *api) clearRecent(w http.ResponseWriter, r *http.Request) {
	respond(w, map[string]bool{"ok": true}, a.core.ClearRecent(r.Context(), actorOf(r), r.URL.Query().Get("ref")))
}

func (a *api) shortcuts(w http.ResponseWriter, r *http.Request) {
	m, err := a.core.Shortcuts(r.Context(), actorOf(r))
	respond(w, m, err)
}

func (a *api) saveShortcuts(w http.ResponseWriter, r *http.Request) {
	var in map[string]string
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	m, err := a.core.SaveShortcuts(r.Context(), actorOf(r), in)
	respond(w, m, err)
}
