package core

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/phoedox/phoedox/internal/mail"
	"github.com/phoedox/phoedox/internal/store"
	"github.com/phoedox/phoedox/resources"
)

// Drafts (ADR-0027): editing a Markdown page happens in a draft shared by
// everyone who edits it. Readers, search, MCP reads and webhooks see the
// published content until someone publishes the draft.

// DraftReminder is how long a draft may stay unchanged before its
// participants are reminded (and again after each further period).
const DraftReminder = 14 * 24 * time.Hour

// Participant is someone who worked on a draft.
type Participant struct {
	Actor string `json:"actor"`
	Name  string `json:"name"`
	AI    bool   `json:"ai,omitempty"` // an AI client (MCP)
}

// DraftInfo is a draft as clients see it.
type DraftInfo struct {
	store.Draft
	Slug         string        `json:"slug,omitempty"`
	Participants []Participant `json:"participants"`
	StartedBy    Participant   `json:"startedBy"`
	UpdatedBy    Participant   `json:"updatedBy"`
	// Outdated: the published page changed after the draft started (e.g. a
	// restored version); publishing replaces that change.
	Outdated bool `json:"outdated"`
	// CanEnd tells whether the caller may publish or cancel.
	CanEnd bool `json:"canEnd"`
}

// DraftPatch changes a draft.
type DraftPatch struct {
	Markdown *string `json:"markdown,omitempty"`
	Title    *string `json:"title,omitempty"`
	Category *string `json:"category,omitempty"`
}

// errDraftOnly: drafts exist only for Markdown pages.
func errDraftOnly() error {
	return invalid("validation.draft_markdown", nil, "only Markdown pages have drafts")
}

func (s *Service) participants(ctx context.Context, actors []string) []Participant {
	ids := make([]string, 0, len(actors))
	for _, a := range actors {
		if id := store.ActorUserID(a); id != "" {
			ids = append(ids, id)
		}
	}
	names, _ := s.DB.UserNames(ctx, ids)
	out := make([]Participant, 0, len(actors))
	for _, a := range actors {
		p := Participant{Actor: a, Name: names[store.ActorUserID(a)], AI: strings.HasPrefix(a, "mcp:")}
		if p.Name == "" {
			p.Name = a
		}
		out = append(out, p)
	}
	return out
}

func (s *Service) draftInfo(ctx context.Context, a Actor, p store.Project, doc store.Document, d store.Draft) DraftInfo {
	info := DraftInfo{Draft: d, Slug: doc.Slug, Participants: s.participants(ctx, d.Participants), Outdated: doc.Version != d.BaseVersion}
	if ps := s.participants(ctx, []string{d.StartedBy, d.UpdatedBy}); len(ps) == 2 {
		info.StartedBy, info.UpdatedBy = ps[0], ps[1]
	}
	info.CanEnd = s.mayEndDraft(a, p, d)
	return info
}

// mayEndDraft: participants with editor rights and project owners may
// publish or cancel a draft.
func (s *Service) mayEndDraft(a Actor, p store.Project, d store.Draft) bool {
	if p.Role == store.RoleOwner {
		return true
	}
	if rank[p.Role] < rank[store.RoleEditor] {
		return false
	}
	return slices.ContainsFunc(d.Participants, func(x string) bool { return store.ActorUserID(x) == a.User.ID })
}

func markdownOf(doc store.Document) string {
	var c struct {
		Markdown string `json:"markdown"`
	}
	_ = json.Unmarshal(doc.Content, &c)
	return c.Markdown
}

// Draft returns the draft of a page (editors only).
func (s *Service) Draft(ctx context.Context, a Actor, ref, docRef string) (DraftInfo, error) {
	p, doc, err := s.docAccess(ctx, a, ref, docRef, store.RoleEditor)
	if err != nil {
		return DraftInfo{}, err
	}
	d, err := s.DB.GetDraft(ctx, doc.ID)
	if err != nil {
		return DraftInfo{}, err
	}
	return s.draftInfo(ctx, a, p, doc, d), nil
}

// ProjectDrafts lists the open drafts of a project (editors; others get none).
func (s *Service) ProjectDrafts(ctx context.Context, a Actor, ref string) ([]DraftInfo, error) {
	p, err := s.project(ctx, a, ref, store.RoleDoc)
	if err != nil {
		return nil, err
	}
	out := []DraftInfo{}
	if rank[p.Role] < rank[store.RoleEditor] {
		return out, nil
	}
	list, err := s.DB.ProjectDrafts(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	allowed, err := s.docFilter(ctx, a, p)
	if err != nil {
		return nil, err
	}
	for _, d := range list {
		doc, err := s.DB.GetDocument(ctx, p.ID, d.DocumentID)
		if err != nil || !allowed(doc.ID, doc.Restricted) {
			continue
		}
		out = append(out, s.draftInfo(ctx, a, p, doc, d.Draft))
	}
	return out, nil
}

// StartDraft opens (or joins) the draft of a page for editing.
func (s *Service) StartDraft(ctx context.Context, a Actor, ref, docRef string) (DraftInfo, error) {
	p, doc, err := s.docAccess(ctx, a, ref, docRef, store.RoleEditor)
	if err != nil {
		return DraftInfo{}, err
	}
	if doc.Type != store.DocMarkdown {
		return DraftInfo{}, errDraftOnly()
	}
	d, err := s.DB.GetDraft(ctx, doc.ID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		now := store.Now()
		d = store.Draft{DocumentID: doc.ID, BaseVersion: doc.Version, Title: doc.Title, Category: doc.Category, Markdown: markdownOf(doc),
			StartedBy: a.String(), StartedAt: now, UpdatedBy: a.String(), UpdatedAt: now}
		d.AddParticipant(a.String())
		if err := s.DB.SaveDraft(ctx, d); err != nil {
			return DraftInfo{}, err
		}
		s.event(ctx, a, "document.draft_start", doc.ID, map[string]int{"base": doc.Version})
	case err != nil:
		return DraftInfo{}, err
	default:
		if !slices.Contains(d.Participants, a.String()) {
			d.AddParticipant(a.String())
			if err := s.DB.SaveDraft(ctx, d); err != nil {
				return DraftInfo{}, err
			}
		}
	}
	return s.draftInfo(ctx, a, p, doc, d), nil
}

// SaveDraft stores changes of the live editor (or a form) in the draft.
func (s *Service) SaveDraft(ctx context.Context, a Actor, ref, docRef string, patch DraftPatch) (DraftInfo, error) {
	p, doc, err := s.docAccess(ctx, a, ref, docRef, store.RoleEditor)
	if err != nil {
		return DraftInfo{}, err
	}
	if doc.Type != store.DocMarkdown {
		return DraftInfo{}, errDraftOnly()
	}
	d, err := s.DB.GetDraft(ctx, doc.ID)
	if errors.Is(err, store.ErrNotFound) {
		// Ended meanwhile (published or cancelled): do not silently reopen.
		return DraftInfo{}, invalid("validation.draft_ended", nil, "the draft was published or cancelled")
	}
	if err != nil {
		return DraftInfo{}, err
	}
	if err := applyDraftPatch(&d, patch); err != nil {
		return DraftInfo{}, err
	}
	d.AddParticipant(a.String())
	d.UpdatedBy, d.UpdatedAt = a.String(), store.Now()
	if err := s.DB.SaveDraft(ctx, d); err != nil {
		return DraftInfo{}, err
	}
	return s.draftInfo(ctx, a, p, doc, d), nil
}

func applyDraftPatch(d *store.Draft, patch DraftPatch) error {
	if patch.Title != nil {
		t := strings.TrimSpace(*patch.Title)
		if t == "" {
			return invalid("validation.title_required", nil, "title must not be empty")
		}
		d.Title = t
	}
	if patch.Category != nil {
		d.Category = strings.TrimSpace(*patch.Category)
	}
	if patch.Markdown != nil {
		if _, _, err := content(store.DocMarkdown, *patch.Markdown, nil, nil); err != nil {
			return err
		}
		d.Markdown = strings.TrimRight(*patch.Markdown, "\n") + "\n"
	}
	return nil
}

// draftWrite puts a change made outside the live editor (API, MCP, accepted
// suggestion, restored version) into an open draft. It reports whether a
// draft was open. Live editors reload the draft.
func (s *Service) draftWrite(ctx context.Context, a Actor, p store.Project, cur store.Document, patch DocumentPatch) (DocResult, bool, error) {
	if cur.Type != store.DocMarkdown || (patch.Markdown == nil && patch.Title == nil && patch.Category == nil) {
		return DocResult{}, false, nil
	}
	d, err := s.DB.GetDraft(ctx, cur.ID)
	if errors.Is(err, store.ErrNotFound) {
		return DocResult{}, false, nil
	}
	if err != nil {
		return DocResult{}, true, err
	}
	if err := applyDraftPatch(&d, DraftPatch{Markdown: patch.Markdown, Title: patch.Title, Category: patch.Category}); err != nil {
		return DocResult{}, true, err
	}
	d.AddParticipant(a.String())
	d.UpdatedBy, d.UpdatedAt = a.String(), store.Now()
	if err := s.DB.SaveDraft(ctx, d); err != nil {
		return DocResult{}, true, err
	}
	// Slug and position are not part of the draft: they change right away.
	if patch.Slug != nil || patch.Position != nil {
		if _, err := s.publishPatch(ctx, a, p, cur, DocumentPatch{Slug: patch.Slug, Position: patch.Position}); err != nil {
			return DocResult{}, true, err
		}
	}
	s.contentChanged(ctx, a, cur.ID)
	s.event(ctx, a, "document.draft_write", cur.ID, nil)
	info := s.draftInfo(ctx, a, p, cur, d)
	return DocResult{Applied: true, Document: &cur, Draft: &info}, true, nil
}

// PublishDraft makes the draft the published page and ends the session for
// everyone (one version, all participants as co-authors).
func (s *Service) PublishDraft(ctx context.Context, a Actor, ref, docRef string) (DocResult, error) {
	p, doc, err := s.docAccess(ctx, a, ref, docRef, store.RoleEditor)
	if err != nil {
		return DocResult{}, err
	}
	if a.Via == "mcp" || a.Plugin != "" {
		return DocResult{}, ErrForbidden // publishing stays with people (ADR-0027)
	}
	d, err := s.DB.GetDraft(ctx, doc.ID)
	if err != nil {
		return DocResult{}, err
	}
	if !s.mayEndDraft(a, p, d) {
		return DocResult{}, ErrForbidden
	}
	res := DocResult{Applied: true, Document: &doc}
	changed := d.Markdown != markdownOf(doc) || d.Title != doc.Title || d.Category != doc.Category
	if changed {
		res, err = s.publishPatch(ctx, a, p, doc, DocumentPatch{Markdown: &d.Markdown, Title: &d.Title, Category: &d.Category})
		if err != nil {
			return res, err
		}
		var co []string
		for _, x := range d.Participants {
			if id := store.ActorUserID(x); id != "" && id != a.User.ID && !slices.Contains(co, id) {
				co = append(co, id)
			}
		}
		if len(co) > 0 {
			_ = s.DB.SetCoauthors(ctx, doc.ID, res.Document.Version, co)
		}
	}
	if err := s.DB.DeleteDraft(ctx, doc.ID); err != nil {
		return res, err
	}
	s.event(ctx, a, "document.draft_publish", doc.ID, map[string]any{"changed": changed, "participants": d.Participants})
	s.draftEnded(ctx, a, doc.ID, "published")
	return res, nil
}

// CancelDraft discards the draft of all participants and ends the session.
func (s *Service) CancelDraft(ctx context.Context, a Actor, ref, docRef string) error {
	p, doc, err := s.docAccess(ctx, a, ref, docRef, store.RoleEditor)
	if err != nil {
		return err
	}
	if a.Via == "mcp" || a.Plugin != "" {
		return ErrForbidden
	}
	d, err := s.DB.GetDraft(ctx, doc.ID)
	if err != nil {
		return err
	}
	if !s.mayEndDraft(a, p, d) {
		return ErrForbidden
	}
	if err := s.DB.DeleteDraft(ctx, doc.ID); err != nil {
		return err
	}
	s.event(ctx, a, "document.draft_cancel", doc.ID, map[string]any{"participants": d.Participants})
	s.draftEnded(ctx, a, doc.ID, "cancelled")
	return nil
}

func (s *Service) draftEnded(ctx context.Context, a Actor, docID, reason string) {
	if s.OnDraftEnd != nil {
		s.OnDraftEnd(ctx, docID, reason, a.User.Name)
	}
}

// RemindDrafts reminds the participants of drafts unchanged for
// DraftReminder (System context, all tenants). Drafts are never discarded.
func (s *Service) RemindDrafts(ctx context.Context, now time.Time) {
	before := store.Time{Time: now.Add(-DraftReminder)}
	due, err := s.DB.DueDrafts(ctx, before)
	if err != nil {
		slog.Warn("draft reminders", "err", err)
		return
	}
	for _, d := range due {
		tctx := store.WithTenant(ctx, d.TenantID)
		doc, err := s.DB.GetDocument(tctx, d.ProjectID, d.DocumentID)
		if err != nil {
			continue
		}
		p, err := s.DB.ProjectFor(tctx, d.ProjectID, "")
		if err != nil {
			continue
		}
		sent := map[string]bool{}
		for _, actor := range d.Participants {
			id := store.ActorUserID(actor)
			if id == "" || sent[id] {
				continue
			}
			sent[id] = true
			u, err := s.DB.UserByID(tctx, id)
			if err != nil || u.Disabled {
				continue
			}
			s.mailDraftReminder(tctx, u, p, doc, d.Draft, now)
		}
		_ = s.DB.MarkDraftReminded(tctx, d.DocumentID, store.Time{Time: now})
		_ = s.DB.AddEvent(tctx, "system", "document.draft_reminder", d.DocumentID, map[string]int{"days": int(now.Sub(d.UpdatedAt.Time).Hours() / 24)})
	}
}

func (s *Service) mailDraftReminder(ctx context.Context, u store.User, p store.Project, doc store.Document, d store.Draft, now time.Time) {
	if s.Mail == nil || !s.Mail.Enabled() || s.PublicURL == "" {
		return
	}
	locale := u.Locale
	if locale == "" {
		locale = s.DefaultLocale
	}
	subject, body, err := resources.Mail("draft", locale, map[string]any{
		"Name": u.Name, "Title": d.Title, "Project": p.Name, "Days": int(now.Sub(d.UpdatedAt.Time).Hours() / 24),
		"Link": s.PublicURL + "/p/" + p.Slug + "/d/" + doc.Slug,
	})
	if err == nil {
		err = s.Mail.Send(ctx, mail.Message{To: u.Email, Subject: subject, Body: body})
	}
	if err != nil {
		slog.Warn("draft reminder mail failed", "user", u.ID, "err", err)
	}
}
