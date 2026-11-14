package core

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/phoedox/phoedox/internal/mail"
	"github.com/phoedox/phoedox/internal/store"
)

// TestDrafts covers ADR-0027: editing works on a shared draft; the page
// changes only on publish; cancel discards everything; API/MCP writes go
// into an open draft.
func TestDrafts(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, _ := setup(t)
	var resets, ends []string
	s.OnContentChange = func(_ context.Context, id, by string) { resets = append(resets, id) }
	s.OnDraftEnd = func(_ context.Context, id, reason, by string) { ends = append(ends, reason+":"+by) }
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Haus", AIWriteMode: "direct"})
	_ = s.SetMember(ctx, owner, p.Slug, "reader@x.de", store.RoleReader)
	_ = s.SetMember(ctx, owner, p.Slug, "stranger@x.de", store.RoleEditor)
	editor := Actor{User: store.User{ID: mustUser(t, s, "stranger@x.de"), Name: "Ed"}, Via: "web"}
	res, _ := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "markdown", Title: "Netz", Markdown: "eins"})
	slug := res.Document.Slug
	published := func() store.Document {
		d, err := s.Document(ctx, owner, p.Slug, slug)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}

	// Only editors start drafts; only Markdown pages have them; live editing needs a draft.
	if _, err := s.StartDraft(ctx, reader, p.Slug, slug); !errors.Is(err, ErrForbidden) {
		t.Fatalf("reader: %v", err)
	}
	adr, _ := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "adr", Title: "A", ADR: &ADR{Decision: "d"}})
	if _, err := s.StartDraft(ctx, owner, p.Slug, adr.Document.Slug); invalidKey(err) != "validation.draft_markdown" {
		t.Fatalf("adr: %v", err)
	}
	if _, err := s.LiveDocument(ctx, owner, p.Slug, slug); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("live without draft: %v", err)
	}

	d, err := s.StartDraft(ctx, owner, p.Slug, slug)
	if err != nil || strings.TrimSpace(d.Markdown) != "eins" || d.BaseVersion != 1 || len(d.Participants) != 1 || !d.CanEnd {
		t.Fatalf("start: %v %+v", err, d)
	}
	if _, err := s.LiveDocument(ctx, owner, p.Slug, slug); err != nil {
		t.Fatalf("live with draft: %v", err)
	}
	// A second editor joins the same draft.
	if d, _ = s.StartDraft(ctx, editor, p.Slug, slug); len(d.Participants) != 2 {
		t.Fatalf("join: %+v", d.Participants)
	}
	if _, err := s.SaveDraft(ctx, editor, p.Slug, slug, DraftPatch{Markdown: strp("eins zwei"), Title: strp("Netzwerk")}); err != nil {
		t.Fatal(err)
	}
	if doc := published(); doc.Version != 1 || doc.Title != "Netz" || strings.TrimSpace(markdownOf(doc)) != "eins" {
		t.Fatalf("drafts must not change the page: v%d %q", doc.Version, markdownOf(doc))
	}
	if len(mustSearch(t, s, owner, "zwei")) != 0 {
		t.Fatal("drafts must not be indexed")
	}
	if list, _ := s.ProjectDrafts(ctx, owner, p.Slug); len(list) != 1 || list[0].Slug != slug {
		t.Fatalf("project drafts: %+v", list)
	}
	if list, _ := s.ProjectDrafts(ctx, reader, p.Slug); len(list) != 0 {
		t.Fatal("readers see no drafts")
	}

	// API/MCP writes go into the open draft; the AI counts as participant.
	ai := Actor{User: owner.User, Via: "mcp", TokenID: "t"}
	r, err := s.UpdateDocument(ctx, ai, p.Slug, slug, DocumentPatch{Markdown: strp("eins zwei drei")})
	if err != nil || r.Draft == nil || r.Draft.Markdown != "eins zwei drei\n" || !r.Draft.Participants[2].AI {
		t.Fatalf("mcp write: %v %+v", err, r.Draft)
	}
	if published().Version != 1 || len(resets) != 1 {
		t.Fatalf("mcp write must go into the draft and reload editors: %v", resets)
	}
	// Readers and AI clients cannot publish.
	if _, err := s.PublishDraft(ctx, reader, p.Slug, slug); err == nil {
		t.Fatal("reader publish")
	}
	if _, err := s.PublishDraft(ctx, ai, p.Slug, slug); !errors.Is(err, ErrForbidden) {
		t.Fatalf("AI publish: %v", err)
	}

	// Publish: one version with co-authors, session ends for everyone.
	pr, err := s.PublishDraft(ctx, editor, p.Slug, slug)
	if err != nil || pr.Document.Version != 2 || pr.Document.Title != "Netzwerk" {
		t.Fatalf("publish: %v %+v", err, pr.Document)
	}
	if h, _ := s.VersionHistory(ctx, owner, p.Slug, slug); len(h) != 2 || len(h[0].Coauthors) != 1 {
		t.Fatalf("co-authors: %+v", h[0])
	}
	if len(mustSearch(t, s, owner, "drei")) != 1 {
		t.Fatal("published content must be indexed")
	}
	if len(ends) != 1 || ends[0] != "published:Ed" {
		t.Fatalf("ends: %v", ends)
	}
	// Publishing ends the session; it must not also tell editors to reload
	// (they would reopen a draft).
	if len(resets) != 1 {
		t.Fatalf("publish must not reset the live room: %v", resets)
	}
	if _, err := s.Draft(ctx, owner, p.Slug, slug); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("draft must be gone")
	}
	if _, err := s.SaveDraft(ctx, owner, p.Slug, slug, DraftPatch{Markdown: strp("x")}); invalidKey(err) != "validation.draft_ended" {
		t.Fatalf("saving an ended draft: %v", err)
	}

	// Without a draft, API writes publish directly as before.
	if r, _ := s.UpdateDocument(ctx, ai, p.Slug, slug, DocumentPatch{Markdown: strp("direkt")}); r.Draft != nil || r.Document.Version != 3 {
		t.Fatalf("direct write: %+v", r)
	}

	// Cancel discards the changes of all participants.
	_, _ = s.StartDraft(ctx, owner, p.Slug, slug)
	_, _ = s.SaveDraft(ctx, owner, p.Slug, slug, DraftPatch{Markdown: strp("verworfen")})
	if err := s.CancelDraft(ctx, editor, p.Slug, slug); !errors.Is(err, ErrForbidden) {
		t.Fatalf("non-participant editors may not cancel: %v", err)
	}
	if err := s.CancelDraft(ctx, owner, p.Slug, slug); err != nil {
		t.Fatal(err)
	}
	if doc := published(); doc.Version != 3 || strings.TrimSpace(markdownOf(doc)) != "direkt" || ends[1] != "cancelled:owner@x.de" {
		t.Fatalf("cancel: v%d %q %v", doc.Version, markdownOf(doc), ends)
	}

	// Deleting the page ends an open draft.
	_, _ = s.StartDraft(ctx, editor, p.Slug, slug)
	_, _ = s.DeleteDocument(ctx, owner, p.Slug, slug)
	if ends[len(ends)-1] != "deleted:owner@x.de" {
		t.Fatalf("delete: %v", ends)
	}
}

type fakeMail struct{ sent []mail.Message }

func (f *fakeMail) Enabled() bool { return true }
func (f *fakeMail) Send(_ context.Context, m mail.Message) error {
	f.sent = append(f.sent, m)
	return nil
}

func TestDraftReminders(t *testing.T) {
	ctx := context.Background()
	s, owner, _, _ := setup(t)
	fm := &fakeMail{}
	s.Mail, s.PublicURL, s.DefaultLocale = fm, "https://app.example.org", "de"
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Haus"})
	res, _ := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "markdown", Title: "Netz", Markdown: "eins"})
	_, _ = s.StartDraft(ctx, owner, p.Slug, res.Document.Slug)
	sys := store.System(ctx)
	now := time.Now()
	s.RemindDrafts(sys, now.Add(13*24*time.Hour))
	if len(fm.sent) != 0 {
		t.Fatal("no reminder before 14 days")
	}
	s.RemindDrafts(sys, now.Add(15*24*time.Hour))
	if len(fm.sent) != 1 || !strings.Contains(fm.sent[0].Body, "/p/"+p.Slug+"/d/"+res.Document.Slug) || !strings.Contains(fm.sent[0].Subject, "Netz") {
		t.Fatalf("reminder: %+v", fm.sent)
	}
	s.RemindDrafts(sys, now.Add(16*24*time.Hour))
	if len(fm.sent) != 1 {
		t.Fatal("not again within 14 days")
	}
	s.RemindDrafts(sys, now.Add(30*24*time.Hour))
	if len(fm.sent) != 2 {
		t.Fatal("again after another 14 days")
	}
	if _, err := s.Draft(ctx, owner, p.Slug, res.Document.Slug); err != nil {
		t.Fatal("drafts are never discarded")
	}
}

func mustUser(t *testing.T, s *Service, email string) string {
	t.Helper()
	u, err := s.DB.UserByEmail(context.Background(), email)
	if err != nil {
		t.Fatal(err)
	}
	return u.ID
}
