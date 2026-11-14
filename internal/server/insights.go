package server

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
)

func (a *api) gaps(w http.ResponseWriter, r *http.Request) {
	days, _ := strconv.Atoi(r.URL.Query().Get("staleDays"))
	rep, err := a.core.DocumentationGaps(r.Context(), actorOf(r), chi.URLParam(r, "p"), days)
	respond(w, rep, err)
}

func (a *api) changes(w http.ResponseWriter, r *http.Request) {
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	if days <= 0 || days > 90 {
		days = 7
	}
	sum, err := a.core.Changes(r.Context(), actorOf(r), chi.URLParam(r, "p"), time.Now().AddDate(0, 0, -days))
	respond(w, sum, err)
}

func (a *api) archiveLink(w http.ResponseWriter, r *http.Request) {
	res, err := a.core.ArchiveLink(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "d"))
	respond(w, res, err)
}

func (a *api) archiveAllLinks(w http.ResponseWriter, r *http.Request) {
	res, err := a.core.ArchiveAllLinks(r.Context(), actorOf(r), chi.URLParam(r, "p"))
	respond(w, res, err)
}
