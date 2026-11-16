package core

import (
	"context"
	"errors"
	"time"

	"github.com/phoedox/phoedox/internal/store"
)

// Project history: every saved version of every document is a point the
// project can be looked at or rolled back to. A rollback writes new
// versions (nothing is lost) and can itself be rolled back.

// ProjectPoint is a saved change: the state right after it is a point.
type ProjectPoint struct {
	store.ProjectVersion
	ActorName string `json:"actorName,omitempty"`
	ViaAI     bool   `json:"viaAI,omitempty"`
}

// ProjectPoints lists the newest changes of a project (points in time).
// docRef limits the list to one document (slug, id or ADR number).
func (s *Service) ProjectPoints(ctx context.Context, a Actor, ref, docRef string, before time.Time, limit int) ([]ProjectPoint, error) {
	p, err := s.project(ctx, a, ref, store.RoleEditor)
	if err != nil {
		return nil, err
	}
	docID := ""
	if docRef != "" {
		d, err := s.findDoc(ctx, p.ID, docRef)
		if err != nil {
			return nil, err
		}
		docID = d.ID
	}
	if limit <= 0 || limit > 300 {
		limit = 100
	}
	vs, err := s.DB.ProjectVersions(ctx, p.ID, docID, store.Time{Time: before}, limit)
	if err != nil {
		return nil, err
	}
	see, err := s.docFilter(ctx, a, p)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, v := range vs {
		if id := store.ActorUserID(v.Actor); id != "" {
			ids = append(ids, id)
		}
	}
	names, err := s.DB.UserNames(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := []ProjectPoint{}
	for _, v := range vs {
		if !see(v.DocumentID, false) {
			continue
		}
		out = append(out, ProjectPoint{ProjectVersion: v, ActorName: names[store.ActorUserID(v.Actor)], ViaAI: len(v.Actor) > 4 && v.Actor[:4] == "mcp:"})
	}
	return out, nil
}

// DocAtChange says how a document differs between a point and now.
type DocAtChange struct {
	ID       string `json:"id"`
	Slug     string `json:"slug"`
	Type     string `json:"type"`
	Title    string `json:"title"`    // title at the point (or now, for new documents)
	Category string `json:"category"` // at the point
	Number   int    `json:"number,omitempty"`
	Change   string `json:"change"`    // changed | deleted | created
	At       int    `json:"versionAt"` // version at the point (0: did not exist)
	Now      int    `json:"version"`   // current version (0: deleted now)
}

// ProjectAt describes the project at a point in time: what a rollback to it
// would change. Unchanged counts the documents that are the same.
type ProjectAt struct {
	At        store.Time    `json:"at"`
	Changes   []DocAtChange `json:"changes"`
	Unchanged int           `json:"unchanged"`
}

// ProjectStateAt compares the project at a point in time with now.
func (s *Service) ProjectStateAt(ctx context.Context, a Actor, ref string, at time.Time) (ProjectAt, error) {
	p, err := s.project(ctx, a, ref, store.RoleEditor)
	if err != nil {
		return ProjectAt{}, err
	}
	return s.projectStateAt(ctx, a, p, at)
}

func (s *Service) projectStateAt(ctx context.Context, a Actor, p store.Project, at time.Time) (ProjectAt, error) {
	out := ProjectAt{At: store.Time{Time: at.UTC()}, Changes: []DocAtChange{}}
	then, err := s.DB.DocumentsAt(ctx, p.ID, out.At)
	if err != nil {
		return out, err
	}
	now, err := s.DB.ListDocuments(ctx, p.ID, store.DocFilter{WithContent: true})
	if err != nil {
		return out, err
	}
	see, err := s.docFilter(ctx, a, p)
	if err != nil {
		return out, err
	}
	current := map[string]store.Document{}
	for _, d := range now {
		current[d.ID] = d
	}
	existed := map[string]bool{}
	for _, d := range then {
		existed[d.ID] = true
		if !see(d.ID, d.Restricted) {
			continue
		}
		c := DocAtChange{ID: d.ID, Slug: d.Slug, Type: d.Type, Title: d.Title, Category: d.Category, Number: d.Number, At: d.Version}
		cur, ok := current[d.ID]
		switch {
		case !ok:
			c.Change = "deleted"
		case cur.Version != d.Version && (cur.Title != d.Title || !sameJSON(cur.Content, d.Content)):
			c.Change, c.Now = "changed", cur.Version
		default:
			out.Unchanged++
			continue
		}
		out.Changes = append(out.Changes, c)
	}
	for _, d := range now {
		if existed[d.ID] || d.ParentID != "" || !see(d.ID, d.Restricted) {
			continue
		}
		out.Changes = append(out.Changes, DocAtChange{ID: d.ID, Slug: d.Slug, Type: d.Type, Title: d.Title, Category: d.Category, Number: d.Number, Change: "created", Now: d.Version})
	}
	return out, nil
}

// RollbackResult reports a rollback per document.
type RollbackResult struct {
	At       store.Time       `json:"at"`
	Restored int              `json:"restored"`
	Revived  int              `json:"revived"`
	Trashed  int              `json:"trashed"`
	Failed   []RollbackFailed `json:"failed,omitempty"`
}

type RollbackFailed struct {
	Title string `json:"title"`
	Error string `json:"error"`
}

// RollbackProject brings every document back to its state at a point in
// time (owners): changed ones get their old content as a new version,
// deleted ones come back from the trash, new ones go to the trash. Cards,
// entries and files are not touched.
func (s *Service) RollbackProject(ctx context.Context, a Actor, ref string, at time.Time) (RollbackResult, error) {
	p, err := s.project(ctx, a, ref, store.RoleOwner)
	if err != nil {
		return RollbackResult{}, err
	}
	if a.Via == "mcp" {
		return RollbackResult{}, ErrForbidden // a person decides this, in Phoedox
	}
	if at.After(time.Now()) {
		return RollbackResult{}, invalid("validation.rollback_future", nil, "the point lies in the future")
	}
	st, err := s.projectStateAt(ctx, a, p, at)
	if err != nil {
		return RollbackResult{}, err
	}
	res := RollbackResult{At: st.At}
	fail := func(c DocAtChange, err error) {
		res.Failed = append(res.Failed, RollbackFailed{Title: c.Title, Error: err.Error()})
	}
	for _, c := range st.Changes {
		switch c.Change {
		case "created":
			if _, err := s.DeleteDocument(ctx, a, p.Slug, c.ID); err != nil {
				fail(c, err)
				continue
			}
			res.Trashed++
		case "deleted":
			if _, err := s.RestoreFromTrash(ctx, a, p.Slug, c.ID); err != nil {
				fail(c, err)
				continue
			}
			res.Revived++
			cur, err := s.DB.GetDocument(ctx, p.ID, c.ID)
			if err == nil && cur.Version != c.At {
				if _, err := s.RestoreVersion(ctx, a, p.Slug, c.ID, c.At); err != nil {
					fail(c, err)
				}
			}
		case "changed":
			if _, err := s.RestoreVersion(ctx, a, p.Slug, c.ID, c.At); err != nil {
				fail(c, err)
				continue
			}
			res.Restored++
		}
	}
	s.event(ctx, a, "project.rollback", p.ID, map[string]any{"at": st.At, "restored": res.Restored, "revived": res.Revived, "trashed": res.Trashed, "failed": len(res.Failed)})
	if len(res.Failed) > 0 && res.Restored+res.Revived+res.Trashed == 0 {
		return res, errors.New(res.Failed[0].Error)
	}
	return res, nil
}
