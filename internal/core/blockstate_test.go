package core

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/phoedox/phoedox/internal/store"
)

func TestBlockState(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, stranger := setup(t)
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Haus"})
	_ = s.SetMember(ctx, owner, p.Slug, reader.User.Email, store.RoleReader)
	res, err := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: store.DocMarkdown, Title: "Setup", Markdown: "> [!steps id=s1] Setup\n> 1. a\n> 2. b"})
	if err != nil {
		t.Fatal(err)
	}
	doc := res.Document.Slug
	// Each reader keeps their own ticks.
	if _, err := s.SetBlockState(ctx, reader, p.Slug, doc, "s1", json.RawMessage(`{"done":[0]}`)); err != nil {
		t.Fatal(err)
	}
	v, _ := s.BlockState(ctx, owner, p.Slug, doc, "s1")
	if string(v.Mine) != "null" {
		t.Fatalf("owner sees the reader's ticks: %s", v.Mine)
	}
	v, _ = s.BlockState(ctx, reader, p.Slug, doc, "s1")
	if string(v.Mine) != `{"done":[0]}` {
		t.Fatalf("mine: %s", v.Mine)
	}
	// Polls count everyone's choices, without saying who.
	_, _ = s.SetBlockState(ctx, owner, p.Slug, doc, "poll1", json.RawMessage(`{"choices":["Pizza"]}`))
	v, err = s.SetBlockState(ctx, reader, p.Slug, doc, "poll1", json.RawMessage(`{"choices":["Pizza","Pasta","Pizza"]}`))
	if err != nil || v.Voters != 2 || v.Votes["Pizza"] != 2 || v.Votes["Pasta"] != 1 || sortedVotes(v.Votes)[0] != "Pizza" {
		t.Fatalf("votes: %+v %v", v, err)
	}
	// Taking a vote back.
	v, _ = s.SetBlockState(ctx, reader, p.Slug, doc, "poll1", nil)
	if v.Voters != 1 || string(v.Mine) != "null" {
		t.Fatalf("after removing: %+v", v)
	}
	if _, err := s.SetBlockState(ctx, reader, p.Slug, doc, "bad key!", json.RawMessage(`{}`)); invalidKey(err) != "validation.block_key" {
		t.Fatalf("key: %v", err)
	}
	if _, err := s.SetBlockState(ctx, reader, p.Slug, doc, "s1", json.RawMessage(`[1,2]`)); invalidKey(err) != "validation.block_state" {
		t.Fatalf("not an object: %v", err)
	}
	if _, err := s.BlockState(ctx, stranger, p.Slug, doc, "s1"); err == nil {
		t.Fatal("stranger")
	}
}
