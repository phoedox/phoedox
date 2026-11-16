package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/phoedox/phoedox/internal/aichat"
	"github.com/phoedox/phoedox/internal/core"
)

// AI chat (ADR-0066). Only signed-in people in the web app chat; tokens
// and AI clients cannot spend the budget.

func (a *api) chatActor(w http.ResponseWriter, r *http.Request) (core.Actor, bool) {
	act := actorOf(r)
	if act.Via != "web" || a.chat == nil {
		writeErr(w, core.ErrForbidden)
		return act, false
	}
	return act, true
}

func (a *api) chatStatus(w http.ResponseWriter, r *http.Request) {
	act, ok := a.chatActor(w, r)
	if !ok {
		return
	}
	st, err := a.chat.Status(r.Context(), act, chi.URLParam(r, "p"))
	respond(w, st, err)
}

func (a *api) listChats(w http.ResponseWriter, r *http.Request) {
	act, ok := a.chatActor(w, r)
	if !ok {
		return
	}
	cs, err := a.chat.Chats(r.Context(), act, chi.URLParam(r, "p"))
	respond(w, cs, err)
}

func (a *api) createChat(w http.ResponseWriter, r *http.Request) {
	act, ok := a.chatActor(w, r)
	if !ok {
		return
	}
	var in struct {
		Document string `json:"document"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	c, err := a.chat.Create(r.Context(), act, chi.URLParam(r, "p"), in.Document)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

func (a *api) getChat(w http.ResponseWriter, r *http.Request) {
	act, ok := a.chatActor(w, r)
	if !ok {
		return
	}
	v, err := a.chat.Get(r.Context(), act, chi.URLParam(r, "p"), chi.URLParam(r, "c"))
	respond(w, v, err)
}

func (a *api) deleteChat(w http.ResponseWriter, r *http.Request) {
	act, ok := a.chatActor(w, r)
	if !ok {
		return
	}
	if err := a.chat.Delete(r.Context(), act, chi.URLParam(r, "p"), chi.URLParam(r, "c")); err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// sendChat streams the answer as server-sent events: text pieces, tool
// calls, proposals and notices, then "done"; an "error" event carries a
// translated error key.
func (a *api) sendChat(w http.ResponseWriter, r *http.Request) {
	act, ok := a.chatActor(w, r)
	if !ok {
		return
	}
	var in aichat.Input
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	send := func(name string, v any) {
		b, _ := json.Marshal(v)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, b)
		fl.Flush()
	}
	err := a.chat.Send(r.Context(), act, chi.URLParam(r, "p"), chi.URLParam(r, "c"), in, func(e aichat.Event) { send(e.Type, e) })
	if err != nil && r.Context().Err() == nil {
		var inv *core.InvalidError
		body := errBody("internal", "Internal error.")
		switch {
		case asInvalid(err, &inv):
			body = apiError{Error: inv.Msg, Code: "invalid", Key: inv.Key, Params: inv.Params}
		case err == core.ErrForbidden:
			body = errBody("forbidden", "You do not have permission for this.")
		}
		send("error", body)
	}
}

func (a *api) decideChatProposal(w http.ResponseWriter, r *http.Request) {
	act, ok := a.chatActor(w, r)
	if !ok {
		return
	}
	var in struct {
		Accept bool  `json:"accept"`
		Cards  []int `json:"cards"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	ts, err := a.chat.Decide(r.Context(), act, chi.URLParam(r, "p"), chi.URLParam(r, "c"), chi.URLParam(r, "tool"), in.Accept, in.Cards)
	respond(w, ts, err)
}

func (a *api) orgAIChat(w http.ResponseWriter, r *http.Request) {
	v, err := a.core.OrgAIChatInfo(r.Context(), actorOf(r), chi.URLParam(r, "o"))
	respond(w, v, err)
}

func asInvalid(err error, target **core.InvalidError) bool { return errors.As(err, target) }

func (a *api) chatModels(w http.ResponseWriter, r *http.Request) {
	act, ok := a.chatActor(w, r)
	if !ok {
		return
	}
	ms, err := a.core.ProjectChatModels(r.Context(), act, chi.URLParam(r, "p"))
	respond(w, ms, err)
}

// Model providers of the instance (/admin/ai/…) or an organization
// (/orgs/{o}/ai/…; ADR-0067).

func (a *api) listAIProviders(w http.ResponseWriter, r *http.Request) {
	ps, err := a.core.AIProviders(r.Context(), actorOf(r), chi.URLParam(r, "o"))
	respond(w, ps, err)
}

func (a *api) saveAIProvider(w http.ResponseWriter, r *http.Request) {
	var in core.AIProviderInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	p, err := a.core.SaveAIProvider(r.Context(), actorOf(r), chi.URLParam(r, "o"), chi.URLParam(r, "id"), in)
	respond(w, p, err)
}

func (a *api) deleteAIProvider(w http.ResponseWriter, r *http.Request) {
	if err := a.core.DeleteAIProvider(r.Context(), actorOf(r), chi.URLParam(r, "o"), chi.URLParam(r, "id")); err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) discoverAIModels(w http.ResponseWriter, r *http.Request) {
	ms, err := a.core.DiscoverAIModels(r.Context(), actorOf(r), chi.URLParam(r, "o"), chi.URLParam(r, "id"))
	respond(w, ms, err)
}

func (a *api) projectUsage(w http.ResponseWriter, r *http.Request) {
	v, err := a.core.ProjectUsage(r.Context(), actorOf(r), chi.URLParam(r, "p"))
	respond(w, v, err)
}

func (a *api) orgUsage(w http.ResponseWriter, r *http.Request) {
	v, err := a.core.OrgUsage(r.Context(), actorOf(r), chi.URLParam(r, "o"))
	respond(w, v, err)
}

func (a *api) instanceUsage(w http.ResponseWriter, r *http.Request) {
	v, err := a.core.InstanceUsage(r.Context(), actorOf(r))
	respond(w, v, err)
}

func (a *api) myUsage(w http.ResponseWriter, r *http.Request) {
	v, err := a.core.MyUsage(r.Context(), actorOf(r))
	respond(w, v, err)
}

func (a *api) orgPrices(w http.ResponseWriter, r *http.Request) {
	v, err := a.core.OrgPriceHistory(r.Context(), actorOf(r), chi.URLParam(r, "o"))
	respond(w, v, err)
}

func (a *api) setOrgPrice(w http.ResponseWriter, r *http.Request) {
	var in core.OrgPriceInput
	if err := decode(r, &in); err != nil {
		respond(w, nil, err)
		return
	}
	v, err := a.core.SetOrgPrice(r.Context(), actorOf(r), chi.URLParam(r, "o"), in)
	respond(w, v, err)
}

func (a *api) previewOrgPrice(w http.ResponseWriter, r *http.Request) {
	var in core.OrgPriceInput
	if err := decode(r, &in); err != nil {
		respond(w, nil, err)
		return
	}
	v, err := a.core.PreviewOrgPrice(r.Context(), actorOf(r), chi.URLParam(r, "o"), in)
	respond(w, v, err)
}

func (a *api) deleteOrgPrice(w http.ResponseWriter, r *http.Request) {
	v, err := a.core.DeleteOrgPrice(r.Context(), actorOf(r), chi.URLParam(r, "o"), chi.URLParam(r, "id"))
	respond(w, v, err)
}
