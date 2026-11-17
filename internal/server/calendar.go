package server

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/phoedox/phoedox/internal/core"
)

// Calendar (ADR-0089): the project's dates and external calendars.

func splitList(v string) []string {
	var out []string
	for _, x := range strings.Split(v, ",") {
		if x = strings.TrimSpace(x); x != "" {
			out = append(out, x)
		}
	}
	return out
}

// calendar answers GET /projects/{p}/calendar?from&to&doc&sources&kinds&calendars.
func (a *api) calendar(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	from, to, err := core.CalendarRange(q.Get("from"), q.Get("to"), time.Now())
	if err != nil {
		writeErr(w, err)
		return
	}
	ctx, act, p := r.Context(), actorOf(r), chi.URLParam(r, "p")
	spec := &core.CalendarSpec{}
	if d := q.Get("doc"); d != "" {
		if spec, err = a.core.CalendarDocSpec(ctx, act, p, d); err != nil {
			writeErr(w, err)
			return
		}
	}
	if v := q.Get("sources"); v != "" {
		spec.Sources = splitList(v)
	}
	if v := q.Get("kinds"); v != "" {
		spec.Kinds = splitList(v)
	}
	if v := q.Get("calendars"); v != "" {
		spec.Calendars = splitList(v)
	}
	v, err := a.core.Calendar(ctx, act, p, from, to, spec)
	respond(w, v, err)
}

func (a *api) externalCalendars(w http.ResponseWriter, r *http.Request) {
	v, err := a.core.ExternalCalendars(r.Context(), actorOf(r), chi.URLParam(r, "p"))
	respond(w, v, err)
}

func (a *api) saveExternalCalendar(w http.ResponseWriter, r *http.Request) {
	var in core.CalendarInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	id := chi.URLParam(r, "id")
	v, err := a.core.SaveExternalCalendar(r.Context(), actorOf(r), chi.URLParam(r, "p"), id, in)
	if id == "" {
		respondStatus(w, http.StatusCreated, v, err)
		return
	}
	respond(w, v, err)
}

func (a *api) deleteExternalCalendar(w http.ResponseWriter, r *http.Request) {
	err := a.core.DeleteExternalCalendar(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "id"))
	respond(w, map[string]bool{"deleted": err == nil}, err)
}

func (a *api) syncExternalCalendar(w http.ResponseWriter, r *http.Request) {
	v, err := a.core.SyncExternalCalendar(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "id"))
	respond(w, v, err)
}

type calendarFeedIn struct {
	On bool `json:"on"`
}

func (a *api) setCalendarFeed(w http.ResponseWriter, r *http.Request) {
	var in calendarFeedIn
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	v, err := a.core.SetCalendarFeed(r.Context(), actorOf(r), chi.URLParam(r, "p"), in.On)
	respond(w, v, err)
}

// calendarFeed serves /cal/{token}.ics: the secret feed of a project's
// dates for phones and other calendars.
func calendarFeed(c *core.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		serveCalendarFeed(c, w, r)
	}
}

func serveCalendarFeed(c *core.Service, w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSuffix(chi.URLParam(r, "token"), ".ics")
	name, data, err := c.CalendarFeed(r.Context(), token)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Cache-Control", "private, max-age=300")
	w.Header().Set("X-Robots-Tag", "noindex")
	w.Header().Set("Content-Disposition", `inline; filename="`+safeFileName(name)+`.ics"`)
	_, _ = w.Write(data)
}

func safeFileName(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r < 128 && (r == '-' || r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			b.WriteRune(r)
		} else if r == ' ' {
			b.WriteByte('-')
		}
	}
	if b.Len() == 0 {
		return "calendar"
	}
	return b.String()
}
