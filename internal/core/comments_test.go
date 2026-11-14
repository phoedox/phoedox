package core

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/phoedox/phoedox/internal/mail"
	"github.com/phoedox/phoedox/internal/store"
)

func TestComments(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, stranger := setup(t)
	mails := &mail.Capture{}
	s.Mail, s.PublicURL = mails, "https://phx.test"
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Haus", AIWriteMode: AIRead})
	_ = s.SetMember(ctx, owner, p.Slug, "reader@x.de", store.RoleReader)
	doc, _ := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "markdown", Title: "Netz", Markdown: "# Netz\n\nDer Router steht im Flur.\n"})
	slug := doc.Document.Slug

	if _, err := s.AddComment(ctx, stranger, p.Slug, slug, CommentInput{Body: "hallo"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stranger: %v", err)
	}
	if _, err := s.AddComment(ctx, reader, p.Slug, slug, CommentInput{Body: "  "}); invalidKey(err) != "validation.comment_empty" {
		t.Fatalf("empty: %v", err)
	}
	if _, err := s.AddComment(ctx, Actor{User: owner.User, Via: "mcp"}, p.Slug, slug, CommentInput{Body: "KI"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("AI in a read-only project: %v", err)
	}
	// a reader comments on a block and mentions the owner; the stranger cannot be mentioned
	root, err := s.AddComment(ctx, reader, p.Slug, slug, CommentInput{Body: "Steht der wirklich da? @owner", Anchor: "  Der Router\n steht im Flur. ",
		Mentions: []string{owner.User.ID, "stranger@x.de"}})
	if err != nil || root.Anchor != "Der Router steht im Flur." || len(root.Mentions) != 1 || root.Mentions[0] != owner.User.ID {
		t.Fatalf("add: %v %+v", err, root)
	}
	if m, ok := mails.Last(); !ok || m.To != "owner@x.de" || !strings.Contains(m.Body, "Steht der wirklich da?") || !strings.Contains(m.Body, "#comment-"+root.ID) {
		t.Fatalf("mention mail: %+v", m)
	}
	// the owner answers: the thread starter is told
	reply, err := s.AddComment(ctx, owner, p.Slug, slug, CommentInput{Body: "Ja, neben der Garderobe.", Parent: root.ID})
	if err != nil || reply.ParentID != root.ID || reply.Anchor != "" {
		t.Fatalf("reply: %v %+v", err, reply)
	}
	if m, _ := mails.Last(); m.To != "reader@x.de" || len(mails.Sent) != 2 {
		t.Fatalf("reply mail: %+v (%d)", m, len(mails.Sent))
	}
	// an answer to a reply lands in the same thread
	r2, _ := s.AddComment(ctx, reader, p.Slug, slug, CommentInput{Body: "Danke!", Parent: reply.ID})
	if r2.ParentID != root.ID {
		t.Fatalf("nested reply: %+v", r2)
	}
	threads, err := s.Comments(ctx, reader, p.Slug, slug)
	if err != nil || len(threads) != 1 || len(threads[0].Replies) != 2 || threads[0].AuthorName != "reader@x.de" {
		t.Fatalf("threads: %v %+v", err, threads)
	}
	open, _ := s.OpenComments(ctx, reader, p.Slug)
	if open[doc.Document.ID] != 1 {
		t.Fatalf("open: %v", open)
	}

	// only authors edit; thread author or editors resolve; replies cannot be resolved
	if _, err := s.UpdateComment(ctx, owner, p.Slug, root.ID, CommentPatch{Body: strp("x")}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("edit by other: %v", err)
	}
	if c, err := s.UpdateComment(ctx, reader, p.Slug, root.ID, CommentPatch{Body: strp("Steht er wirklich da?")}); err != nil || c.Body != "Steht er wirklich da?" {
		t.Fatalf("edit: %v %+v", err, c)
	}
	yes := true
	if _, err := s.UpdateComment(ctx, owner, p.Slug, reply.ID, CommentPatch{Resolved: &yes}); invalidKey(err) != "validation.comment_reply_resolve" {
		t.Fatalf("resolve reply: %v", err)
	}
	if c, err := s.UpdateComment(ctx, owner, p.Slug, root.ID, CommentPatch{Resolved: &yes}); err != nil || c.ResolvedAt.IsZero() || c.ResolvedBy != owner.User.ID {
		t.Fatalf("resolve: %v %+v", err, c)
	}
	if open, _ := s.OpenComments(ctx, reader, p.Slug); open[doc.Document.ID] != 0 {
		t.Fatalf("open after resolve: %v", open)
	}

	// deleting: the author or owners; the first comment takes the thread
	if err := s.DeleteComment(ctx, reader, p.Slug, reply.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("reader deletes other's reply: %v", err)
	}
	if err := s.DeleteComment(ctx, owner, p.Slug, root.ID); err != nil {
		t.Fatal(err)
	}
	if threads, _ := s.Comments(ctx, reader, p.Slug, slug); len(threads) != 0 {
		t.Fatalf("thread not deleted: %+v", threads)
	}
	evs, _ := s.DB.Events(ctx, store.EventFilter{ProjectID: p.ID, Kind: "comment."})
	if len(evs) < 6 {
		t.Fatalf("events: %d", len(evs))
	}
}
