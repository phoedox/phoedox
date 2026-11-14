package core

import (
	"context"
	"log/slog"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/phoedox/phoedox/internal/mail"
	"github.com/phoedox/phoedox/internal/store"
	"github.com/phoedox/phoedox/resources"
)

// Comments: threads on a page or on one block of it (anchor = a quote of
// the block's text). Everyone who can read a page may comment; mentioned
// people and the people in a thread get a mail. AI clients comment like
// they read: not in read-only projects or with read-only tokens.
const (
	commentMaxBody   = 10000
	commentMaxAnchor = 300
)

// CommentInput adds a comment; Parent answers a thread.
type CommentInput struct {
	Body     string   `json:"body"`
	Anchor   string   `json:"anchor,omitempty"`
	Parent   string   `json:"parent,omitempty"`
	Mentions []string `json:"mentions,omitempty"` // user ids or e-mails of people who can read the page
}

// CommentPatch edits the text or switches the done state of a thread.
type CommentPatch struct {
	Body     *string  `json:"body,omitempty"`
	Mentions []string `json:"mentions,omitempty"`
	Resolved *bool    `json:"resolved,omitempty"`
}

// Thread is the first comment with its replies.
type Thread struct {
	store.Comment
	Replies []store.Comment `json:"replies"`
}

// Comments lists the threads of a page, open ones first.
func (s *Service) Comments(ctx context.Context, a Actor, ref, docRef string) ([]Thread, error) {
	_, doc, err := s.docAccess(ctx, a, ref, docRef, store.RoleReader)
	if err != nil {
		return nil, err
	}
	all, err := s.DB.DocumentComments(ctx, doc.ID)
	if err != nil {
		return nil, err
	}
	out := []Thread{}
	index := map[string]int{}
	for _, c := range all {
		if c.ParentID == "" {
			index[c.ID] = len(out)
			out = append(out, Thread{Comment: c, Replies: []store.Comment{}})
		}
	}
	for _, c := range all {
		if i, ok := index[c.ParentID]; ok && c.ParentID != "" {
			out[i].Replies = append(out[i].Replies, c)
		}
	}
	slices.SortStableFunc(out, func(x, y Thread) int {
		return boolInt(!x.ResolvedAt.IsZero()) - boolInt(!y.ResolvedAt.IsZero())
	})
	return out, nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// OpenComments counts the open threads per page id of a project.
func (s *Service) OpenComments(ctx context.Context, a Actor, ref string) (map[string]int, error) {
	p, err := s.project(ctx, a, ref, store.RoleDoc)
	if err != nil {
		return nil, err
	}
	return s.DB.OpenCommentCounts(ctx, p.ID)
}

func cleanBody(body string) (string, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return "", invalid("validation.comment_empty", nil, "the comment is empty")
	}
	if utf8.RuneCountInString(body) > commentMaxBody {
		return "", invalid("validation.too_long", map[string]any{"field": "body", "max": commentMaxBody}, "the comment is too long")
	}
	return body, nil
}

// AddComment starts a thread or answers one.
func (s *Service) AddComment(ctx context.Context, a Actor, ref, docRef string, in CommentInput) (store.Comment, error) {
	p, doc, err := s.docAccess(ctx, a, ref, docRef, store.RoleReader)
	if err != nil {
		return store.Comment{}, err
	}
	if s.readOnly(a, p) {
		return store.Comment{}, ErrForbidden
	}
	body, err := cleanBody(in.Body)
	if err != nil {
		return store.Comment{}, err
	}
	c := store.Comment{ID: newID(), ProjectID: p.ID, DocumentID: doc.ID, Body: body, AuthorID: a.User.ID, AuthorName: a.User.Name, Actor: a.String()}
	var thread []store.Comment
	if in.Parent != "" {
		root, err := s.DB.GetComment(ctx, p.ID, in.Parent)
		if err != nil || root.DocumentID != doc.ID {
			return store.Comment{}, ErrNotFound
		}
		if root.ParentID != "" {
			root, err = s.DB.GetComment(ctx, p.ID, root.ParentID)
			if err != nil {
				return store.Comment{}, ErrNotFound
			}
		}
		c.ParentID = root.ID
		all, _ := s.DB.DocumentComments(ctx, doc.ID)
		for _, x := range all {
			if x.ID == root.ID || x.ParentID == root.ID {
				thread = append(thread, x)
			}
		}
	} else {
		c.Anchor = strings.Join(strings.Fields(in.Anchor), " ")
		if utf8.RuneCountInString(c.Anchor) > commentMaxAnchor {
			c.Anchor = string([]rune(c.Anchor)[:commentMaxAnchor])
		}
	}
	c.Mentions = s.mentionable(ctx, p, doc, in.Mentions)
	if c, err = s.DB.AddComment(ctx, c); err != nil {
		return store.Comment{}, err
	}
	s.event(ctx, a, "comment.create", c.ID, map[string]any{"document": doc.ID, "parent": c.ParentID, "mentions": c.Mentions})
	s.notifyComment(ctx, a, p, doc, c, thread)
	return c, nil
}

// mentionable keeps the people (ids or e-mails) who can read the page.
func (s *Service) mentionable(ctx context.Context, p store.Project, doc store.Document, refs []string) []string {
	out := []string{}
	for _, ref := range refs {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			continue
		}
		u, err := s.DB.UserByID(ctx, ref)
		if err != nil {
			if u, err = s.DB.UserByEmail(ctx, ref); err != nil {
				continue
			}
		}
		if u.Disabled || slices.Contains(out, u.ID) || !s.canRead(ctx, u, p, doc) {
			continue
		}
		out = append(out, u.ID)
	}
	return out
}

// canRead tells whether a person can read a page.
func (s *Service) canRead(ctx context.Context, u store.User, p store.Project, doc store.Document) bool {
	pu, err := s.DB.ProjectFor(ctx, p.ID, u.ID)
	if err != nil {
		return false
	}
	if u.IsAdmin {
		return true
	}
	if pu.Role == "" {
		return false
	}
	role, err := s.docRole(ctx, Actor{User: u, Via: "web"}, pu, doc)
	return err == nil && role != ""
}

// notifyComment mails mentioned people and the people in the thread.
func (s *Service) notifyComment(ctx context.Context, a Actor, p store.Project, doc store.Document, c store.Comment, thread []store.Comment) {
	if s.Mail == nil || !s.Mail.Enabled() || s.PublicURL == "" {
		return
	}
	type target struct {
		id        string
		mentioned bool
	}
	var to []target
	seen := map[string]bool{a.User.ID: true}
	for _, id := range c.Mentions {
		if !seen[id] {
			seen[id] = true
			to = append(to, target{id, true})
		}
	}
	for _, x := range thread {
		if !seen[x.AuthorID] {
			seen[x.AuthorID] = true
			to = append(to, target{x.AuthorID, false})
		}
	}
	excerpt := c.Body
	if utf8.RuneCountInString(excerpt) > 600 {
		excerpt = string([]rune(excerpt)[:600]) + " …"
	}
	for _, t := range to {
		u, err := s.DB.UserByID(ctx, t.id)
		if err != nil || u.Disabled || !s.canRead(ctx, u, p, doc) {
			continue
		}
		locale := u.Locale
		if locale == "" {
			locale = s.DefaultLocale
		}
		subject, body, err := resources.Mail("comment", locale, map[string]any{
			"Name": u.Name, "Author": a.User.Name, "Title": doc.Title, "Project": p.Name, "Mentioned": t.mentioned,
			"Reply": c.ParentID != "", "Body": excerpt, "Link": s.PublicURL + "/p/" + p.Slug + "/d/" + doc.Slug + "#comment-" + c.ID,
		})
		if err == nil {
			err = s.Mail.Send(ctx, mail.Message{To: u.Email, Subject: subject, Body: body})
		}
		if err != nil {
			slog.Warn("comment mail failed", "user", u.ID, "err", err)
		}
	}
}

// commentAccess loads a comment the actor may see, with project and page.
func (s *Service) commentAccess(ctx context.Context, a Actor, ref, id string) (store.Project, store.Document, store.Comment, error) {
	p, err := s.project(ctx, a, ref, store.RoleDoc)
	if err != nil {
		return p, store.Document{}, store.Comment{}, err
	}
	c, err := s.DB.GetComment(ctx, p.ID, id)
	if err != nil {
		return p, store.Document{}, c, err
	}
	p, doc, err := s.docAccess(ctx, a, ref, c.DocumentID, store.RoleReader)
	if err != nil {
		return p, doc, c, err
	}
	if s.readOnly(a, p) {
		return p, doc, c, ErrForbidden
	}
	return p, doc, c, nil
}

// UpdateComment edits a comment (its author) or marks a thread done
// (its author or editors of the page).
func (s *Service) UpdateComment(ctx context.Context, a Actor, ref, id string, in CommentPatch) (store.Comment, error) {
	p, doc, c, err := s.commentAccess(ctx, a, ref, id)
	if err != nil {
		return c, err
	}
	if in.Body != nil {
		if c.AuthorID != a.User.ID {
			return c, ErrForbidden
		}
		body, err := cleanBody(*in.Body)
		if err != nil {
			return c, err
		}
		c.Body, c.Mentions = body, s.mentionable(ctx, p, doc, in.Mentions)
		if err := s.DB.EditComment(ctx, c.ID, c.Body, c.Mentions); err != nil {
			return c, err
		}
		s.event(ctx, a, "comment.update", c.ID, map[string]any{"document": doc.ID})
	}
	if in.Resolved != nil {
		if c.ParentID != "" {
			return c, invalid("validation.comment_reply_resolve", nil, "only threads can be marked as done")
		}
		role, _ := s.docRole(ctx, a, p, doc)
		if c.AuthorID != a.User.ID && rank[role] < rank[store.RoleEditor] {
			return c, ErrForbidden
		}
		by := ""
		if *in.Resolved {
			by = a.User.ID
		}
		if err := s.DB.ResolveComment(ctx, c.ID, by); err != nil {
			return c, err
		}
		kind := "comment.reopen"
		if *in.Resolved {
			kind = "comment.resolve"
		}
		s.event(ctx, a, kind, c.ID, map[string]any{"document": doc.ID})
	}
	return s.DB.GetComment(ctx, p.ID, c.ID)
}

// DeleteComment removes a comment (its author or project owners); the
// first comment takes its thread with it.
func (s *Service) DeleteComment(ctx context.Context, a Actor, ref, id string) error {
	p, doc, c, err := s.commentAccess(ctx, a, ref, id)
	if err != nil {
		return err
	}
	if c.AuthorID != a.User.ID && p.Role != store.RoleOwner {
		return ErrForbidden
	}
	if err := s.DB.DeleteComment(ctx, c.ID); err != nil {
		return err
	}
	s.event(ctx, a, "comment.delete", c.ID, map[string]any{"document": doc.ID})
	return nil
}
