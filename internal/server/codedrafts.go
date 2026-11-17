package server

import (
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/phoedox/phoedox/internal/core"
)

// Documentation drafts tied to pull requests.

func (a *api) linkDraftCode(w http.ResponseWriter, r *http.Request) {
	var in core.DraftCodeInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	v, err := a.core.LinkDraftCode(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "d"), in)
	respond(w, v, err)
}

func (a *api) unlinkDraftCode(w http.ResponseWriter, r *http.Request) {
	v, err := a.core.UnlinkDraftCode(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "d"))
	respond(w, v, err)
}

func (a *api) pullRequestDocs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	in := core.DraftCodeInput{Repo: q.Get("repo"), Branch: q.Get("branch")}
	in.PR, _ = strconv.Atoi(q.Get("pr"))
	if ref := q.Get("ref"); ref != "" {
		if p, ok := core.ParsePullRequest(ref); ok {
			in = p
		}
	}
	v, err := a.core.PullRequestDocs(r.Context(), actorOf(r), chi.URLParam(r, "p"), in)
	respond(w, v, err)
}

func (a *api) codeRepoWebhook(w http.ResponseWriter, r *http.Request) {
	v, err := a.core.CodeRepoWebhook(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "id"), r.Method == http.MethodPost)
	respond(w, v, err)
}

// codeHook takes pull request events of GitHub and Gitea.
func (a *api) codeHook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 5<<20))
	if err != nil {
		writeErr(w, err)
		return
	}
	event := r.Header.Get("X-GitHub-Event")
	if event == "" {
		event = r.Header.Get("X-Gitea-Event")
	}
	sig := r.Header.Get("X-Hub-Signature-256")
	if sig == "" {
		sig = r.Header.Get("X-Gitea-Signature")
	}
	err = a.core.HandleCodeWebhook(r.Context(), chi.URLParam(r, "id"), event, sig, body)
	if errors.Is(err, core.ErrNotFound) || errors.Is(err, core.ErrForbidden) {
		// Same answer for unknown repositories and wrong signatures.
		writeJSON(w, http.StatusUnauthorized, errBody("unauthorized", "Signature does not match."))
		return
	}
	respond(w, map[string]bool{"ok": true}, err)
}
