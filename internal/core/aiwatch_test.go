package core

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/phoedox/phoedox/internal/store"
)

func TestWaitForChanges(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, stranger := setup(t)
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Haus", AIWriteMode: AIDirect})
	_ = s.SetMember(ctx, owner, p.Slug, reader.User.Email, store.RoleEditor)
	res, err := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: store.DocMarkdown, Title: "Notes", Markdown: "a"})
	if err != nil {
		t.Fatal(err)
	}
	other, _ := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: store.DocMarkdown, Title: "Other", Markdown: "b"})
	ai := Actor{User: owner.User, Via: "mcp", TokenID: "t1"}
	if _, err := s.WaitForChanges(ctx, stranger, p.Slug, nil, 0, 0); err == nil {
		t.Fatal("stranger")
	}
	start, err := s.WaitForChanges(ctx, ai, p.Slug, nil, 0, 0)
	if err != nil || start.Cursor == 0 {
		t.Fatalf("cursor: %+v %v", start, err)
	}
	// The AI's own change is not reported; a person's comment is, also while waiting.
	body := "a b"
	if _, err := s.UpdateDocument(ctx, ai, p.Slug, res.Document.Slug, DocumentPatch{Markdown: &body}); err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(200 * time.Millisecond)
		_, _ = s.AddComment(ctx, reader, p.Slug, other.Document.Slug, CommentInput{Body: "elsewhere"})
		_, _ = s.AddComment(ctx, reader, p.Slug, res.Document.Slug, CommentInput{Body: "Please add the router"})
	}()
	t0 := time.Now()
	got, err := s.WaitForChanges(ctx, ai, p.Slug, []string{res.Document.Slug}, start.Cursor, 5)
	if err != nil || time.Since(t0) > 4*time.Second {
		t.Fatalf("wait: %v %v", err, time.Since(t0))
	}
	if len(got.Changes) != 1 || got.Changes[0].Kind != "comment.create" || got.Changes[0].Comment != "Please add the router" ||
		got.Changes[0].Document != res.Document.Slug || got.Changes[0].By != reader.User.Name || got.Cursor <= start.Cursor {
		t.Fatalf("changes: %+v", got)
	}
	// Nothing new: times out quickly with the same cursor.
	again, err := s.WaitForChanges(ctx, ai, p.Slug, []string{res.Document.Slug}, got.Cursor, 1)
	if err != nil || len(again.Changes) != 0 || !again.TimedOut {
		t.Fatalf("again: %+v %v", again, err)
	}
	// Without documents: the person's edit of any page.
	body2 := "c"
	_, _ = s.UpdateDocument(ctx, reader, p.Slug, other.Document.Slug, DocumentPatch{Markdown: &body2})
	all, _ := s.WaitForChanges(ctx, ai, p.Slug, nil, again.Cursor, 0)
	if len(all.Changes) == 0 || all.Changes[len(all.Changes)-1].Document != other.Document.Slug {
		t.Fatalf("all: %+v", all)
	}
}

func TestAIAnswersAppComments(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, _ := setup(t)
	f := &fakeAI{text: `{"reply":"Done, the title is red now.","edits":[{"file":"index.html","old":"<h1>","new":"<h1 style=\"color:red\">"}]}`}
	s.AI = f
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Haus", AIWriteMode: AIDirect})
	_ = s.SetMember(ctx, owner, p.Slug, reader.User.Email, store.RoleReader)
	res, err := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: store.DocApp, Title: "Poll", App: &App{Files: map[string]string{"index.html": "<h1>Poll</h1>"}}})
	if err != nil {
		t.Fatal(err)
	}
	doc := res.Document
	// Without @AI nothing happens.
	if _, err := s.AddComment(ctx, owner, p.Slug, doc.Slug, CommentInput{Body: "nice"}); err != nil {
		t.Fatal(err)
	}
	c, err := s.AddComment(ctx, owner, p.Slug, doc.Slug, CommentInput{Body: "@AI make the title red"})
	if err != nil {
		t.Fatal(err)
	}
	var reply store.Comment
	for i := 0; i < 50 && reply.ID == ""; i++ {
		time.Sleep(50 * time.Millisecond)
		th, _ := s.Comments(ctx, owner, p.Slug, doc.Slug)
		for _, x := range th {
			if x.ID == c.ID && len(x.Replies) > 0 {
				reply = x.Replies[0]
			}
		}
	}
	if !strings.Contains(reply.Body, "red now") || !strings.HasPrefix(reply.Actor, "mcp:") {
		t.Fatalf("reply: %+v", reply)
	}
	if f.calls != 1 {
		t.Fatalf("calls: %d", f.calls)
	}
	cur, _ := s.DB.GetDocument(ctx, p.ID, doc.ID)
	if !strings.Contains(appOf(cur).Files["index.html"], `color:red`) {
		t.Fatalf("not edited: %s", appOf(cur).Files["index.html"])
	}
	// The AI's own reply does not ask the AI again, and readers' asks are answered without edits applying beyond their rights.
	time.Sleep(100 * time.Millisecond)
	if f.calls != 1 {
		t.Fatalf("loop: %d", f.calls)
	}
}
