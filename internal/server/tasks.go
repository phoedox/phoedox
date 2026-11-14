package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

func (a *api) myTasks(w http.ResponseWriter, r *http.Request) {
	list, err := a.core.MyTasks(r.Context(), actorOf(r))
	respond(w, list, err)
}

// boardEvents streams changes of a board as server-sent events, so open
// boards reload at once instead of waiting for their poll.
func (a *api) boardEvents(w http.ResponseWriter, r *http.Request) {
	ch, stop, err := a.core.WatchBoard(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "d"))
	if err != nil {
		writeErr(w, err)
		return
	}
	defer stop()
	streamEvents(w, r, ch)
}

// projectLive streams changes of a project (pages, decisions, entries,
// files …) as server-sent events, so open lists update live.
func (a *api) projectLive(w http.ResponseWriter, r *http.Request) {
	ch, stop, err := a.core.WatchProject(r.Context(), actorOf(r), chi.URLParam(r, "p"))
	if err != nil {
		writeErr(w, err)
		return
	}
	defer stop()
	streamEvents(w, r, ch)
}

// streamEvents writes each value of ch as an SSE "change" event until the
// client goes away; a comment every 25 s keeps proxies from closing it.
func streamEvents[T any](w http.ResponseWriter, r *http.Request, ch <-chan T) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no") // proxies must not hold events back
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "retry: 5000\n\n")
	fl.Flush()
	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
		case c := <-ch:
			b, _ := json.Marshal(c)
			fmt.Fprintf(w, "event: change\ndata: %s\n\n", b)
		}
		fl.Flush()
	}
}
