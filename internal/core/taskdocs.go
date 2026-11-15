package core

import (
	"context"
	"log/slog"
	"regexp"
	"slices"
	"strings"

	"github.com/phoedox/phoedox/internal/store"
)

// Documents on task cards (ADR-0064): a card links any number of pages,
// decisions or other documents of its project. Pages show the cards that
// link them. Readers only see documents they may open.

const maxTaskDocs = 50

// resolveTaskDocs turns slugs, ids or decision numbers into document ids
// the actor can see (deduplicated, in the given order).
func (s *Service) resolveTaskDocs(ctx context.Context, a Actor, p store.Project, refs []string) ([]string, error) {
	if len(refs) > maxTaskDocs {
		return nil, invalid("validation.task_docs_max", map[string]any{"max": maxTaskDocs}, "at most %d documents per card", maxTaskDocs)
	}
	see, err := s.docFilter(ctx, a, p)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for _, ref := range refs {
		doc, err := s.findDoc(ctx, p.ID, ref)
		if err != nil || !see(doc.ID, doc.Restricted) {
			return nil, invalid("validation.task_doc_unknown", map[string]any{"document": ref}, "unknown document %q", ref)
		}
		if !slices.Contains(ids, doc.ID) {
			ids = append(ids, doc.ID)
		}
	}
	return ids, nil
}

// descRefRe finds page links [[ref]] / [[ref|label]] in a card description.
var descRefRe = regexp.MustCompile(`\[\[([^\]|\n]+?)(?:\|[^\]\n]+)?\]\]`)

// descDocs resolves the page links of a description to documents of the
// project the actor can see (links to other projects are left out).
func (s *Service) descDocs(ctx context.Context, a Actor, p store.Project, desc string) []string {
	m := descRefRe.FindAllStringSubmatch(desc, maxTaskDocs)
	if len(m) == 0 {
		return nil
	}
	see, err := s.docFilter(ctx, a, p)
	if err != nil {
		return nil
	}
	var ids []string
	for _, x := range m {
		ref := strings.TrimSpace(x[1])
		if proj, rest, ok := strings.Cut(ref, "/"); ok {
			if proj != p.Slug {
				continue
			}
			ref = rest
		}
		if doc, err := s.findDoc(ctx, p.ID, ref); err == nil && see(doc.ID, doc.Restricted) && !slices.Contains(ids, doc.ID) {
			ids = append(ids, doc.ID)
		}
	}
	return ids
}

// attachTaskDocs fills the documents of listed tasks that the actor may see.
func (s *Service) attachTaskDocs(ctx context.Context, a Actor, p store.Project, tasks []store.Task) {
	if len(tasks) == 0 {
		return
	}
	docs, err := s.DB.TaskDocs(ctx, p.ID, "")
	if err != nil {
		slog.Warn("task documents", "err", err)
		return
	}
	if len(docs) == 0 {
		return
	}
	see, err := s.docFilter(ctx, a, p)
	if err != nil {
		return
	}
	by := map[string][]store.TaskDoc{}
	for _, d := range docs {
		if see(d.ID, d.Restricted) {
			by[d.TaskID] = append(by[d.TaskID], d)
		}
	}
	for i := range tasks {
		tasks[i].Documents = by[tasks[i].ID]
	}
}

// withTaskExtras adds code links and documents to one task.
func (s *Service) withTaskExtras(ctx context.Context, a Actor, p store.Project, t store.Task) store.Task {
	list := []store.Task{t}
	s.attachCodeLinks(ctx, p.ID, list)
	s.attachTaskDocs(ctx, a, p, list)
	s.attachMinutes(ctx, p.ID, list)
	return list[0]
}

// DocumentTasks lists the cards that link a document (cards on boards the
// actor can see).
func (s *Service) DocumentTasks(ctx context.Context, a Actor, ref, docRef string) ([]store.Task, error) {
	p, doc, err := s.docAccess(ctx, a, ref, docRef, store.RoleReader)
	if err != nil {
		return nil, err
	}
	list, err := s.DB.TasksOfDocument(ctx, p.ID, doc.ID)
	if err != nil {
		return nil, err
	}
	see, err := s.docFilter(ctx, a, p)
	if err != nil {
		return nil, err
	}
	out := []store.Task{}
	for _, t := range list {
		if see(t.BoardID, false) {
			out = append(out, t)
		}
	}
	return out, nil
}
