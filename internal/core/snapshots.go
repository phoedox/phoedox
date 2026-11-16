package core

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/phoedox/phoedox/internal/store"
)

// Documentation snapshots (AI client feedback round 3): a named point in
// the project history, e.g. "0.27.4" – the docs as they were at a release.
// Made by hand or from release tags of the project's code connection; a
// read-only view shows the documents of that time, and two snapshots can
// be compared.

// SnapshotInput creates a snapshot.
type SnapshotInput struct {
	Name string `json:"name"`
	Note string `json:"note,omitempty"`
	// At is the point in time (RFC 3339); empty = now.
	At string `json:"at,omitempty"`
}

// SnapshotDoc is a document as it was at a snapshot.
type SnapshotDoc struct {
	ID       string `json:"id"`
	Slug     string `json:"slug"`
	Type     string `json:"type"`
	Title    string `json:"title"`
	Category string `json:"category"`
	Number   int    `json:"number,omitempty"`
	Version  int    `json:"version"`
}

// SnapshotView is a snapshot with its documents.
type SnapshotView struct {
	Snapshot  store.Snapshot `json:"snapshot"`
	Documents []SnapshotDoc  `json:"documents"`
}

// SnapshotChange is a document that differs between two snapshots.
type SnapshotChange struct {
	SnapshotDoc
	Change string `json:"change"`      // changed | created | deleted
	From   int    `json:"fromVersion"` // 0: did not exist
	To     int    `json:"toVersion"`   // 0: gone
}

// SnapshotDiff compares two snapshots (or one with now).
type SnapshotDiff struct {
	From      string           `json:"from"`
	To        string           `json:"to"`
	Changes   []SnapshotChange `json:"changes"`
	Unchanged int              `json:"unchanged"`
}

func validSnapshotName(n string) error {
	if n == "" || len(n) > 60 || strings.ContainsAny(n, "/\\\n") {
		return invalid("validation.snapshot_name", nil, "the name is 1–60 characters without slashes")
	}
	return nil
}

// CreateSnapshot names a point in the project history (editors).
func (s *Service) CreateSnapshot(ctx context.Context, a Actor, ref string, in SnapshotInput) (store.Snapshot, error) {
	p, err := s.project(ctx, a, ref, store.RoleEditor)
	if err != nil {
		return store.Snapshot{}, err
	}
	in.Name = strings.TrimSpace(in.Name)
	if err := validSnapshotName(in.Name); err != nil {
		return store.Snapshot{}, err
	}
	at := time.Now().UTC()
	if in.At != "" {
		t, err := time.Parse(time.RFC3339, in.At)
		if err != nil || t.After(time.Now().Add(time.Minute)) {
			return store.Snapshot{}, invalid("validation.snapshot_at", nil, "at must be a past time (RFC 3339)")
		}
		at = t.UTC()
	}
	sn := store.Snapshot{ID: newID(), ProjectID: p.ID, Name: in.Name, Note: excerpt(strings.TrimSpace(in.Note), 500), At: store.Time{Time: at},
		Source: "manual", CreatedBy: a.String(), CreatedAt: store.Now()}
	if err := s.DB.AddSnapshot(ctx, sn); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return sn, invalid("validation.snapshot_exists", map[string]any{"name": in.Name}, "a snapshot %q exists", in.Name)
		}
		return sn, err
	}
	s.event(ctx, a, "project.snapshot", p.ID, map[string]string{"name": sn.Name})
	return sn, nil
}

// Snapshots lists a project's snapshots (readers).
func (s *Service) Snapshots(ctx context.Context, a Actor, ref string) ([]store.Snapshot, error) {
	p, err := s.project(ctx, a, ref, store.RoleReader)
	if err != nil {
		return nil, err
	}
	return s.DB.Snapshots(ctx, p.ID)
}

// DeleteSnapshot removes a snapshot's name (owners); the history stays.
func (s *Service) DeleteSnapshot(ctx context.Context, a Actor, ref, name string) error {
	p, err := s.project(ctx, a, ref, store.RoleOwner)
	if err != nil {
		return err
	}
	if _, err := s.DB.SnapshotByName(ctx, p.ID, name); err != nil {
		return err
	}
	s.event(ctx, a, "project.snapshot_delete", p.ID, map[string]string{"name": name})
	return s.DB.DeleteSnapshot(ctx, p.ID, name)
}

func (s *Service) docsAt(ctx context.Context, a Actor, p store.Project, at time.Time) (map[string]store.Document, error) {
	docs, err := s.DB.DocumentsAt(ctx, p.ID, store.Time{Time: at})
	if err != nil {
		return nil, err
	}
	see, err := s.docFilter(ctx, a, p)
	if err != nil {
		return nil, err
	}
	out := map[string]store.Document{}
	for _, d := range docs {
		if d.ParentID == "" && see(d.ID, d.Restricted) {
			out[d.ID] = d
		}
	}
	return out, nil
}

func snapDoc(d store.Document) SnapshotDoc {
	return SnapshotDoc{ID: d.ID, Slug: d.Slug, Type: d.Type, Title: d.Title, Category: d.Category, Number: d.Number, Version: d.Version}
}

// Snapshot shows the documents as they were at a snapshot (readers); read a
// document's text with DocumentVersion and the version listed here.
func (s *Service) Snapshot(ctx context.Context, a Actor, ref, name string) (SnapshotView, error) {
	p, err := s.project(ctx, a, ref, store.RoleReader)
	if err != nil {
		return SnapshotView{}, err
	}
	sn, err := s.DB.SnapshotByName(ctx, p.ID, name)
	if err != nil {
		return SnapshotView{}, err
	}
	docs, err := s.docsAt(ctx, a, p, sn.At.Time)
	if err != nil {
		return SnapshotView{}, err
	}
	out := SnapshotView{Snapshot: sn, Documents: []SnapshotDoc{}}
	for _, d := range docs {
		out.Documents = append(out.Documents, snapDoc(d))
	}
	sortSnapDocs(out.Documents)
	return out, nil
}

func sortSnapDocs(ds []SnapshotDoc) {
	slices.SortFunc(ds, func(x, y SnapshotDoc) int {
		if c := strings.Compare(x.Category, y.Category); c != 0 {
			return c
		}
		return strings.Compare(strings.ToLower(x.Title), strings.ToLower(y.Title))
	})
}

// CompareSnapshots lists what changed between two snapshots; to "" = now.
func (s *Service) CompareSnapshots(ctx context.Context, a Actor, ref, from, to string) (SnapshotDiff, error) {
	p, err := s.project(ctx, a, ref, store.RoleReader)
	if err != nil {
		return SnapshotDiff{}, err
	}
	fs, err := s.DB.SnapshotByName(ctx, p.ID, from)
	if err != nil {
		return SnapshotDiff{}, err
	}
	toAt := time.Now()
	if to != "" {
		ts, err := s.DB.SnapshotByName(ctx, p.ID, to)
		if err != nil {
			return SnapshotDiff{}, err
		}
		toAt = ts.At.Time
	}
	a1, err := s.docsAt(ctx, a, p, fs.At.Time)
	if err != nil {
		return SnapshotDiff{}, err
	}
	a2, err := s.docsAt(ctx, a, p, toAt)
	if err != nil {
		return SnapshotDiff{}, err
	}
	out := SnapshotDiff{From: from, To: to, Changes: []SnapshotChange{}}
	for id, d := range a1 {
		n, ok := a2[id]
		switch {
		case !ok:
			out.Changes = append(out.Changes, SnapshotChange{SnapshotDoc: snapDoc(d), Change: "deleted", From: d.Version})
		case n.Version != d.Version && (n.Title != d.Title || !sameJSON(n.Content, d.Content)):
			out.Changes = append(out.Changes, SnapshotChange{SnapshotDoc: snapDoc(n), Change: "changed", From: d.Version, To: n.Version})
		default:
			out.Unchanged++
		}
	}
	for id, n := range a2 {
		if _, ok := a1[id]; !ok {
			out.Changes = append(out.Changes, SnapshotChange{SnapshotDoc: snapDoc(n), Change: "created", To: n.Version})
		}
	}
	slices.SortFunc(out.Changes, func(x, y SnapshotChange) int {
		if c := strings.Compare(x.Change, y.Change); c != 0 {
			return c
		}
		return strings.Compare(strings.ToLower(x.Title), strings.ToLower(y.Title))
	})
	return out, nil
}

// codeSnapshotsFor names the project's state at each release of a
// connected repository (tag name), once.
func (s *Service) codeSnapshotsFor(ctx context.Context, r store.CodeRepo, rels []CodeItem) {
	for _, rel := range rels {
		if rel.Prerelease || rel.Ref == "" || validSnapshotName(rel.Ref) != nil {
			continue
		}
		sn := store.Snapshot{ID: newID(), ProjectID: r.ProjectID, Name: rel.Ref, Note: rel.Title, At: rel.At, Source: "tag",
			SourceRef: r.Owner + "/" + r.Name + "@" + rel.Ref, CreatedBy: "system:code", CreatedAt: store.Now()}
		if err := s.DB.AddSnapshot(ctx, sn); err == nil {
			s.event(ctx, Actor{Via: "system"}, "project.snapshot", r.ProjectID, map[string]string{"name": sn.Name, "source": sn.SourceRef})
		}
	}
}

func slicesSortFunc[T any](s []T, cmp func(a, b T) int) { slices.SortFunc(s, cmp) }
