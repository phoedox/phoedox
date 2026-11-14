package core

import (
	"context"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/phoedox/phoedox/internal/store"
)

// Change summaries: what changed in a project since a point in time, built
// from the audit log (no AI). Sent weekly as a notification that people can
// mute ("project.summary").

// PageChange is a page that changed.
type PageChange struct {
	Slug    string   `json:"slug"`
	Title   string   `json:"title"`
	Created bool     `json:"created,omitempty"`
	Deleted bool     `json:"deleted,omitempty"`
	Edits   int      `json:"edits"`
	Authors []string `json:"authors"`
}

// EntryChange is an entry that was added, changed or removed.
type EntryChange struct {
	Slug    string `json:"slug"`
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Added   bool   `json:"added,omitempty"`
	Removed bool   `json:"removed,omitempty"`
}

// ChangeSummary is what changed in a project.
type ChangeSummary struct {
	Since     store.Time    `json:"since"`
	Pages     []PageChange  `json:"pages"`
	Entries   []EntryChange `json:"entries"`
	TasksDone []string      `json:"tasksDone"`
	Comments  int           `json:"comments"`
	People    []string      `json:"people"`
}

// Empty tells whether nothing changed.
func (c ChangeSummary) Empty() bool {
	return len(c.Pages) == 0 && len(c.Entries) == 0 && len(c.TasksDone) == 0 && c.Comments == 0
}

// Changes summarizes a project's changes since a time (readers).
func (s *Service) Changes(ctx context.Context, a Actor, ref string, since time.Time) (ChangeSummary, error) {
	p, err := s.project(ctx, a, ref, store.RoleReader)
	if err != nil {
		return ChangeSummary{}, err
	}
	allowed, err := s.docFilter(ctx, a, p)
	if err != nil {
		return ChangeSummary{}, err
	}
	return s.changes(ctx, p, since, allowed)
}

func (s *Service) changes(ctx context.Context, p store.Project, since time.Time, allowed func(string, bool) bool) (ChangeSummary, error) {
	out := ChangeSummary{Since: store.Time{Time: since}, Pages: []PageChange{}, Entries: []EntryChange{}, TasksDone: []string{}, People: []string{}}
	// Read the log backwards in pages, at most a few thousand events.
	var events []store.Event
	var before int64
	for i := 0; i < 20; i++ {
		page, err := s.DB.Events(ctx, store.EventFilter{ProjectID: p.ID, Since: store.Time{Time: since}, Before: before, Limit: 200})
		if err != nil {
			return out, err
		}
		events = append(events, page...)
		if len(page) < 200 {
			break
		}
		before = page[len(page)-1].ID
	}
	pages := map[string]*PageChange{}
	entries := map[string]*EntryChange{}
	actors := map[string]bool{}
	for i := len(events) - 1; i >= 0; i-- { // oldest first
		e := events[i]
		uid := store.ActorUserID(e.Actor)
		switch {
		case strings.HasPrefix(e.Kind, "document."):
			if e.Kind != "document.create" && e.Kind != "document.update" && e.Kind != "document.delete" && e.Kind != "document.draft_publish" {
				continue
			}
			pc := pages[e.Subject]
			if pc == nil {
				pc = &PageChange{Authors: []string{}}
				pages[e.Subject] = pc
			}
			switch e.Kind {
			case "document.create":
				pc.Created = true
			case "document.delete":
				pc.Deleted = true
			default:
				pc.Edits++
			}
			if uid != "" && !slices.Contains(pc.Authors, uid) {
				pc.Authors = append(pc.Authors, uid)
			}
		case e.Kind == "entity.upsert" || e.Kind == "entity.delete":
			ec := entries[e.Subject]
			if ec == nil {
				ec = &EntryChange{}
				entries[e.Subject] = ec
			}
			if e.Kind == "entity.delete" {
				ec.Removed = true
			}
		case e.Kind == "comment.create":
			out.Comments++
		default:
			continue
		}
		if uid != "" {
			actors[uid] = true
		}
	}
	ids := []string{}
	for id := range actors {
		ids = append(ids, id)
	}
	names, _ := s.DB.UserNames(ctx, ids)
	for id, pc := range pages {
		d, err := s.DB.GetDocument(ctx, p.ID, id)
		if err != nil && !pc.Deleted {
			continue
		}
		if err == nil {
			if !allowed(d.ID, d.Restricted) {
				continue
			}
			pc.Slug, pc.Title = d.Slug, d.Title
		}
		if pc.Title == "" {
			continue // deleted before we could name it
		}
		for i, uid := range pc.Authors {
			if n := names[uid]; n != "" {
				pc.Authors[i] = n
			}
		}
		out.Pages = append(out.Pages, *pc)
	}
	for id, ec := range entries {
		e, err := s.DB.GetEntity(ctx, p.ID, id)
		if err != nil {
			continue
		}
		ec.Slug, ec.Name, ec.Kind = e.Slug, e.Name, e.Kind
		ec.Added = !e.CreatedAt.Before(since)
		out.Entries = append(out.Entries, *ec)
	}
	if tasks, err := s.DB.ListTasks(ctx, p.ID, store.TaskFilter{}); err == nil {
		for _, t := range tasks {
			if !t.DoneAt.IsZero() && !t.DoneAt.Before(since) {
				out.TasksDone = append(out.TasksDone, t.Title)
			}
		}
	}
	for _, n := range names {
		out.People = append(out.People, n)
	}
	sort.Strings(out.People)
	sort.Slice(out.Pages, func(i, j int) bool {
		return out.Pages[i].Edits+b2i(out.Pages[i].Created)*10 > out.Pages[j].Edits+b2i(out.Pages[j].Created)*10
	})
	sort.Slice(out.Entries, func(i, j int) bool { return out.Entries[i].Name < out.Entries[j].Name })
	return out, nil
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

const settingSummaryWeek = "summary.week"

// SendSummaries sends each project's weekly change summary to its editors
// and owners on Monday mornings (UTC), once per week. People mute it with the
// notification kind "project.summary".
func (s *Service) SendSummaries(ctx context.Context, now time.Time) {
	now = now.UTC()
	if now.Weekday() != time.Monday || now.Hour() < 6 {
		return
	}
	y, w := now.ISOWeek()
	week := strconv.Itoa(y) + "-" + strconv.Itoa(w)
	sys := store.System(ctx)
	if last, _ := s.DB.Setting(sys, settingSummaryWeek, ""); last == week {
		return
	}
	_ = s.DB.SetSetting(sys, settingSummaryWeek, week, "system")
	tenants, err := s.DB.Tenants(sys)
	if err != nil {
		return
	}
	for _, t := range tenants {
		tctx := store.WithTenant(ctx, t.ID)
		projects, err := s.DB.ListProjects(tctx, "", true)
		if err != nil {
			continue
		}
		for _, p := range projects {
			sum, err := s.changes(tctx, p, now.AddDate(0, 0, -7), func(string, bool) bool { return true })
			if err != nil || sum.Empty() {
				continue
			}
			titles := []string{}
			for i, pc := range sum.Pages {
				if i == 5 {
					break
				}
				titles = append(titles, pc.Title)
			}
			members, _ := s.DB.ListMembers(tctx, p.ID)
			for _, m := range members {
				if m.Role != store.RoleOwner && m.Role != store.RoleEditor {
					continue
				}
				u, err := s.DB.UserByID(tctx, m.UserID)
				if err != nil {
					continue
				}
				s.notify(tctx, u, Note{Kind: "project.summary", ProjectID: p.ID, Project: p.Name, Subject: p.ID + ":" + week, Title: p.Name,
					Body: strings.Join(titles, " · "), Link: "/p/" + p.Slug + "/insights"})
			}
		}
	}
}
