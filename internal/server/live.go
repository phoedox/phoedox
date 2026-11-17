package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/phoedox/phoedox/internal/core"
	"github.com/phoedox/phoedox/internal/realtime"
)

// live upgrades to the live editing session (the draft) of a Markdown page.
func (a *api) live(hub *realtime.Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorOf(r)
		doc, readOnly, err := a.core.LiveSession(r.Context(), actor, chi.URLParam(r, "p"), chi.URLParam(r, "d"))
		if err != nil {
			writeErr(w, err)
			return
		}
		hub.Serve(w, r, doc.ID, actor.User.Name, readOnly)
	}
}

func (a *api) projectDrafts(w http.ResponseWriter, r *http.Request) {
	list, err := a.core.ProjectDrafts(r.Context(), actorOf(r), chi.URLParam(r, "p"))
	respond(w, list, err)
}

func (a *api) getDraft(w http.ResponseWriter, r *http.Request) {
	d, err := a.core.Draft(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "d"))
	respond(w, d, err)
}

func (a *api) startDraft(w http.ResponseWriter, r *http.Request) {
	d, err := a.core.StartDraft(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "d"))
	respond(w, d, err)
}

func (a *api) saveDraft(w http.ResponseWriter, r *http.Request) {
	var in core.DraftPatch
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	d, err := a.core.SaveDraft(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "d"), in)
	respond(w, d, err)
}

func (a *api) publishDraft(w http.ResponseWriter, r *http.Request) {
	res, err := a.core.PublishDraft(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "d"))
	respond(w, res, err)
}

func (a *api) cancelDraft(w http.ResponseWriter, r *http.Request) {
	respond(w, map[string]bool{"ok": true}, a.core.CancelDraft(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "d")))
}

// saveWhiteboard stores the board from the live editor (published at once, ADR-0068).
func (a *api) saveWhiteboard(w http.ResponseWriter, r *http.Request) {
	var in core.Whiteboard
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	doc, err := a.core.SaveWhiteboard(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "d"), &in)
	respond(w, map[string]any{"version": doc.Version, "updatedAt": doc.UpdatedAt}, err)
}

// whiteboardOps applies element operations (add, update, delete) to a
// board – the app draws with these; open editors reload the board.
func (a *api) whiteboardOps(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Ops []core.WBOp `json:"ops"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	v, err := a.core.EditWhiteboard(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "d"), in.Ops)
	respond(w, v, err)
}

// embedPreview fetches title, description and site of an address for a
// whiteboard embed.
func (a *api) embedPreview(w http.ResponseWriter, r *http.Request) {
	pv, kind, err := a.core.EmbedPreview(r.Context(), actorOf(r), chi.URLParam(r, "p"), r.URL.Query().Get("url"))
	respond(w, map[string]any{"kind": kind, "preview": pv}, err)
}

func (a *api) embedPolicy(w http.ResponseWriter, r *http.Request) {
	v, err := a.core.ProjectEmbedPolicy(r.Context(), actorOf(r), chi.URLParam(r, "p"))
	respond(w, v, err)
}

type codeStatusIn struct {
	URLs []string `json:"urls"`
}

func (a *api) whiteboardCodeStatus(w http.ResponseWriter, r *http.Request) {
	var in codeStatusIn
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	v, err := a.core.WhiteboardCodeStatus(r.Context(), actorOf(r), chi.URLParam(r, "p"), in.URLs)
	respond(w, v, err)
}

type mermaidIn struct {
	Source   string `json:"source"`
	Document string `json:"document,omitempty"`
	Title    string `json:"title,omitempty"`
	Category string `json:"category,omitempty"`
}

func (a *api) whiteboardMermaid(w http.ResponseWriter, r *http.Request) {
	var in mermaidIn
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	v, err := a.core.DrawMermaid(r.Context(), actorOf(r), chi.URLParam(r, "p"), in.Document, in.Title, in.Category, in.Source)
	respond(w, v, err)
}

type neighbourhoodIn struct {
	Entity string `json:"entity"`
}

func (a *api) whiteboardNeighbourhood(w http.ResponseWriter, r *http.Request) {
	var in neighbourhoodIn
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	v, err := a.core.DrawNeighbourhood(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "d"), in.Entity)
	respond(w, v, err)
}
