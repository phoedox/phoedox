package core

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/phoedox/phoedox/internal/store"
	"github.com/phoedox/phoedox/resources"
)

// AI clients and what people do (Jan, 2026-11-17, like artifacts):
// wait_for_changes tells an AI session when people change or comment on
// pages and apps it works on, and a comment that asks the AI (@AI) on an
// app is answered by the background AI, which may change the app as asked.

// ChangeItem is one change people made.
type ChangeItem struct {
	Cursor   int64  `json:"cursor"`
	Kind     string `json:"kind"` // e.g. document.update, comment.create, whiteboard.comment, app.grant
	Document string `json:"document,omitempty"`
	Title    string `json:"title,omitempty"`
	Type     string `json:"type,omitempty"`
	Comment  string `json:"comment,omitempty"` // the comment's text (comments)
	By       string `json:"by"`
	At       string `json:"at"`
}

// Changes is the answer of WaitForChanges: the changes after the cursor
// and the cursor to pass next time.
type Changes struct {
	Cursor   int64        `json:"cursor"`
	Changes  []ChangeItem `json:"changes"`
	TimedOut bool         `json:"timedOut,omitempty"`
}

var watchKinds = []string{"document.", "comment.", "whiteboard.comment", "decision.", "task.", "file.upload", "app."}

// WaitForChanges returns what people (not the caller's own AI session)
// changed or commented in a project after cursor – in the given documents
// only, if any. Without a cursor it returns the current one. With wait > 0
// it waits up to that many seconds (at most 50) for the first change.
func (s *Service) WaitForChanges(ctx context.Context, a Actor, ref string, docs []string, cursor int64, wait int) (Changes, error) {
	p, err := s.project(ctx, a, ref, store.RoleReader)
	if err != nil {
		return Changes{}, err
	}
	ids := map[string]bool{}
	for _, d := range docs {
		doc, err := s.DB.GetDocument(ctx, p.ID, d)
		if err != nil {
			return Changes{}, err
		}
		ids[doc.ID] = true
	}
	if cursor <= 0 {
		last, err := s.DB.Events(ctx, store.EventFilter{ProjectID: p.ID, Limit: 1})
		if err != nil {
			return Changes{}, err
		}
		out := Changes{Changes: []ChangeItem{}}
		if len(last) > 0 {
			out.Cursor = last[0].ID
		}
		return out, nil
	}
	wait = min(max(wait, 0), 50)
	var ch <-chan ProjectChange
	if wait > 0 {
		c, stop := s.projects.subscribe(p.ID)
		defer stop()
		ch = c
	}
	deadline := time.After(time.Duration(wait) * time.Second)
	for {
		out, err := s.changesAfter(ctx, a, p, ids, cursor)
		if err != nil || len(out.Changes) > 0 || wait == 0 {
			return out, err
		}
		cursor = out.Cursor
		select {
		case <-ctx.Done():
			return out, nil
		case <-deadline:
			out.TimedOut = true
			return out, nil
		case <-ch:
			// Something changed: look again (it may be the caller's own change).
		}
	}
}

func (s *Service) changesAfter(ctx context.Context, a Actor, p store.Project, ids map[string]bool, cursor int64) (Changes, error) {
	out := Changes{Cursor: cursor, Changes: []ChangeItem{}}
	evs, err := s.DB.Events(ctx, store.EventFilter{ProjectID: p.ID, After: cursor, Limit: 200})
	if err != nil {
		return out, err
	}
	own := a.String()
	docs := map[string]store.Document{}
	names := map[string]string{}
	for _, e := range evs {
		out.Cursor = e.ID
		if e.Actor == own || strings.HasPrefix(e.Actor, own+"~") || !slices.ContainsFunc(watchKinds, func(k string) bool { return strings.HasPrefix(e.Kind, k) }) {
			continue
		}
		var payload map[string]any
		_ = json.Unmarshal([]byte(e.Payload), &payload)
		docID := e.Subject
		if v, ok := payload["document"].(string); ok && v != "" {
			docID = v
		}
		if v, ok := payload["app"].(string); ok && v != "" {
			docID = v
		}
		if len(ids) > 0 && !ids[docID] {
			continue
		}
		it := ChangeItem{Cursor: e.ID, Kind: e.Kind, At: e.CreatedAt.UTC().Format(time.RFC3339)}
		doc, ok := docs[docID]
		if !ok {
			if _, d, err := s.docAccess(ctx, a, p.Slug, docID, store.RoleReader); err == nil {
				doc = d
			}
			docs[docID] = doc
		}
		if doc.ID == "" && strings.HasPrefix(e.Kind, "comment.") {
			continue // a page the caller cannot read
		}
		if doc.ID != "" {
			it.Document, it.Title, it.Type = doc.Slug, doc.Title, string(doc.Type)
		}
		if strings.HasPrefix(e.Kind, "comment.") && e.Kind != "comment.delete" {
			if c, err := s.DB.GetComment(ctx, p.ID, e.Subject); err == nil {
				it.Comment = excerpt(c.Body, 2000)
			}
		}
		uid := actorUser(e.Actor)
		if _, ok := names[uid]; !ok && uid != "" {
			n, _ := s.DB.UserNames(ctx, []string{uid})
			names[uid] = n[uid]
		}
		it.By = names[uid]
		if strings.HasPrefix(e.Actor, "mcp:") {
			it.By += " (AI)"
		}
		out.Changes = append(out.Changes, it)
	}
	return out, nil
}

// actorUser is the user id in an actor string (user:<id>, mcp:<id>:<token> …).
func actorUser(actor string) string {
	actor, _, _ = strings.Cut(actor, "~")
	parts := strings.Split(actor, ":")
	if len(parts) >= 2 {
		return parts[1]
	}
	return ""
}

// --- comments that ask the AI -----------------------------------------------------

var askAIRe = regexp.MustCompile(`(?i)(^|[\s(])@(ai|ki|claude)\b`)

// appCommentAI is what the background AI answers to a comment on an app.
type appCommentAI struct {
	Reply string            `json:"reply"`
	Edits []AppEdit         `json:"edits,omitempty"`
	Files map[string]string `json:"files,omitempty"`
}

// askAIAboutApp answers a comment on an app that mentions the AI: a reply in
// the thread and, if asked, a change of the app – written as the commenter's
// AI, so the project's AI write mode applies (direct or a suggestion).
func (s *Service) askAIAboutApp(ctx context.Context, a Actor, p store.Project, doc store.Document, c store.Comment) {
	if doc.Type != store.DocApp || !askAIRe.MatchString(c.Body) || a.Via == "mcp" || p.AIWriteMode == AIOff {
		return
	}
	thread, _ := s.DB.DocumentComments(ctx, doc.ID)
	root := c.ParentID
	if root == "" {
		root = c.ID
	}
	var conv strings.Builder
	for _, x := range thread {
		if x.ID == root || x.ParentID == root {
			fmt.Fprintf(&conv, "%s: %s\n\n", x.AuthorName, x.Body)
		}
	}
	app := appOf(doc)
	files, _ := json.MarshalIndent(app.Files, "", "  ")
	logs, _ := json.Marshal(appLogs(doc.ID))
	prompt := fmt.Sprintf("App %q (level %d).\n\nFiles:\n%s\n\nRecent console errors:\n%s\n\nComment thread (the last comment asks you):\n%s",
		doc.Title, app.Level, files, logs, conv.String())
	text, err := s.BackgroundAsk(ctx, p, "app-comment:"+doc.ID, []string{resources.Prompt("app-comment")}, prompt, 8000)
	if err != nil {
		return
	}
	var ans appCommentAI
	t := strings.TrimSpace(text)
	t = strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(t, "```json"), "```"), "```")
	if json.Unmarshal([]byte(strings.TrimSpace(t)), &ans) != nil {
		ans = appCommentAI{Reply: strings.TrimSpace(text)}
	}
	ai := Actor{User: a.User, Via: "mcp", TokenID: "comment-ai"}
	reply := strings.TrimSpace(ans.Reply)
	if len(ans.Edits) > 0 || len(ans.Files) > 0 {
		var files map[string]*string
		if len(ans.Files) > 0 {
			files = map[string]*string{}
			for k, v := range ans.Files {
				files[k] = &v
			}
		}
		res, err := s.EditApp(ctx, ai, p.Slug, doc.ID, files, ans.Edits, nil)
		switch {
		case err != nil:
			reply += "\n\n" + resources.Prompt("app-comment-failed") + " " + err.Error()
		case res.Suggestion != nil:
			reply += "\n\n" + resources.Prompt("app-comment-suggested")
		}
	}
	if reply == "" {
		return
	}
	_, _ = s.AddComment(ctx, ai, p.Slug, doc.ID, CommentInput{Body: excerpt(reply, 10_000), Parent: root})
}
