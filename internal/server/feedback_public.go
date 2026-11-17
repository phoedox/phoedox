package server

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/phoedox/phoedox/internal/botcheck"
	"github.com/phoedox/phoedox/internal/core"
)

// Public view of feedback portals: released topics, votes, following and
// comments. Signed-in people act with their account (cookie requests need
// the X-Phoedox header); anonymous visitors pass the bot check for
// comments and following and use a key kept in their browser for votes.

// optionalActor returns the signed-in person, nil for anonymous visitors,
// or ok=false after answering a request that failed the CSRF check.
func (a *api) optionalActor(w http.ResponseWriter, r *http.Request) (*core.Actor, bool) {
	act, viaCookie, err := a.auth.FromRequest(r, "token")
	if err != nil {
		return nil, true
	}
	if viaCookie && r.Method != http.MethodGet && r.Header.Get("X-Phoedox") != "1" {
		writeJSON(w, http.StatusForbidden, errBody("csrf", "Missing X-Phoedox header."))
		return nil, false
	}
	return &act, true
}

func (a *api) publicTopics(w http.ResponseWriter, r *http.Request) {
	act, ok := a.optionalActor(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	v, err := a.core.PublicFeedbackTopics(r.Context(), act, chi.URLParam(r, "handle"), r.Header.Get("X-Phoedox-Voter"), q.Get("sort"), q.Get("q"), q.Get("type"))
	respond(w, v, err)
}

func (a *api) publicTopic(w http.ResponseWriter, r *http.Request) {
	act, ok := a.optionalActor(w, r)
	if !ok {
		return
	}
	v, err := a.core.PublicFeedbackTopic(r.Context(), act, chi.URLParam(r, "handle"), chi.URLParam(r, "id"), r.Header.Get("X-Phoedox-Voter"))
	respond(w, v, err)
}

func (a *api) voteTopic(w http.ResponseWriter, r *http.Request) {
	if !a.feedbackIP.allow(clientIP(r)) {
		writeJSON(w, http.StatusTooManyRequests, errBody("rate_limited", "Too many requests. Please wait a while."))
		return
	}
	act, ok := a.optionalActor(w, r)
	if !ok {
		return
	}
	v, err := a.core.VoteFeedbackTopic(r.Context(), act, chi.URLParam(r, "handle"), chi.URLParam(r, "id"), r.Header.Get("X-Phoedox-Voter"))
	respond(w, v, err)
}

// anonymousChecked runs the bot check for anonymous visitors.
func (a *api) anonymousChecked(w http.ResponseWriter, r *http.Request, act *core.Actor, token string) bool {
	if act == nil && a.core.Bot != nil && a.core.Bot.Enabled(r.Context(), botcheck.PlaceFeedback) {
		return a.botCheck(w, r, botcheck.PlaceFeedback, token)
	}
	return true
}

func (a *api) followTopic(w http.ResponseWriter, r *http.Request) {
	if !a.feedbackIP.allow(clientIP(r)) {
		writeJSON(w, http.StatusTooManyRequests, errBody("rate_limited", "Too many requests. Please wait a while."))
		return
	}
	act, ok := a.optionalActor(w, r)
	if !ok {
		return
	}
	var in struct {
		core.FollowInput
		BotToken string `json:"botToken"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&in); err != nil {
		writeErr(w, &core.InvalidError{Key: "validation.json", Msg: "invalid JSON"})
		return
	}
	if !a.anonymousChecked(w, r, act, in.BotToken) {
		return
	}
	respond(w, map[string]bool{"ok": true}, a.core.FollowFeedbackTopic(r.Context(), act, chi.URLParam(r, "handle"), chi.URLParam(r, "id"), in.FollowInput))
}

func (a *api) unfollowTopic(w http.ResponseWriter, r *http.Request) {
	act, ok := a.optionalActor(w, r)
	if !ok {
		return
	}
	respond(w, map[string]bool{"ok": true}, a.core.UnfollowFeedbackTopic(r.Context(), act, "", chi.URLParam(r, "handle"), chi.URLParam(r, "id")))
}

func (a *api) unfollowByLink(w http.ResponseWriter, r *http.Request) {
	respond(w, map[string]bool{"ok": true}, a.core.UnfollowFeedbackTopic(r.Context(), nil, chi.URLParam(r, "token"), "", ""))
}

func (a *api) commentTopic(w http.ResponseWriter, r *http.Request) {
	if !a.feedbackIP.allow(clientIP(r)) {
		writeJSON(w, http.StatusTooManyRequests, errBody("rate_limited", "Too many requests. Please wait a while."))
		return
	}
	act, ok := a.optionalActor(w, r)
	if !ok {
		return
	}
	var in struct {
		core.TopicCommentInput
		BotToken string `json:"botToken"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&in); err != nil {
		writeErr(w, &core.InvalidError{Key: "validation.json", Msg: "invalid JSON"})
		return
	}
	if !a.anonymousChecked(w, r, act, in.BotToken) {
		return
	}
	v, err := a.core.CommentFeedbackTopic(r.Context(), act, chi.URLParam(r, "handle"), chi.URLParam(r, "id"), in.TopicCommentInput)
	respondStatus(w, http.StatusCreated, v, err)
}

// Moderators: decide on comments, comment as the team.

func (a *api) decideTopicComment(w http.ResponseWriter, r *http.Request) {
	approve := chi.URLParam(r, "action") == "approve"
	if !approve && chi.URLParam(r, "action") != "reject" {
		http.NotFound(w, r)
		return
	}
	v, err := a.core.DecideFeedbackComment(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "id"), chi.URLParam(r, "cid"), approve)
	respond(w, v, err)
}

func (a *api) teamCommentTopic(w http.ResponseWriter, r *http.Request) {
	var in core.TopicCommentInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	v, err := a.core.TeamCommentFeedbackTopic(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "id"), in)
	respond(w, v, err)
}
