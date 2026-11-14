package core

import (
	"context"
	"sync"

	"github.com/phoedox/phoedox/internal/store"
)

// BoardChange tells open boards that cards or columns changed; the
// browser reloads the board (simple and robust, no merging of states).
type BoardChange struct {
	Board string `json:"board"`
	Task  string `json:"task,omitempty"`
	Kind  string `json:"kind"` // task.create | task.update | task.delete | board.update
	Actor string `json:"actor,omitempty"`
}

// feed fans out changes to everyone watching a key (a board, a project)
// in this process.
type feed[T any] struct {
	mu   sync.Mutex
	subs map[string]map[chan T]struct{}
}

type boardFeed = feed[BoardChange]

func (f *feed[T]) publishTo(key string, c T) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for ch := range f.subs[key] {
		select {
		case ch <- c:
		default: // a slow client misses a change; it reloads on the next one
		}
	}
}

func (f *feed[T]) subscribe(key string) (chan T, func()) {
	ch := make(chan T, 16)
	f.mu.Lock()
	if f.subs == nil {
		f.subs = map[string]map[chan T]struct{}{}
	}
	if f.subs[key] == nil {
		f.subs[key] = map[chan T]struct{}{}
	}
	f.subs[key][ch] = struct{}{}
	f.mu.Unlock()
	return ch, func() {
		f.mu.Lock()
		delete(f.subs[key], ch)
		if len(f.subs[key]) == 0 {
			delete(f.subs, key)
		}
		f.mu.Unlock()
	}
}

// ProjectChange tells open pages of a project that something changed
// (a page, decision, entry, file …), so lists reload. It carries ids and
// kinds only, no titles or content; clients fetch through the API, which
// checks access again.
type ProjectChange struct {
	Kind    string `json:"kind"`    // event kind, e.g. document.create
	Subject string `json:"subject"` // id of the changed object
	Actor   string `json:"actor"`   // user id of who changed it ("" for system)
}

// WatchProject subscribes to changes of a project the actor can read.
func (s *Service) WatchProject(ctx context.Context, a Actor, ref string) (<-chan ProjectChange, func(), error) {
	p, err := s.project(ctx, a, ref, store.RoleReader)
	if err != nil {
		return nil, nil, err
	}
	ch, stop := s.projects.subscribe(p.ID)
	return ch, stop, nil
}

func (s *Service) projectChanged(a Actor, project, kind, subject string) {
	if project == "" {
		return
	}
	s.projects.publishTo(project, ProjectChange{Kind: kind, Subject: subject, Actor: a.User.ID})
}

// WatchBoard subscribes to changes of a board the actor can read. Call the
// returned function to stop.
func (s *Service) WatchBoard(ctx context.Context, a Actor, ref, boardRef string) (<-chan BoardChange, func(), error) {
	_, doc, err := s.boardAccess(ctx, a, ref, boardRef, store.RoleReader)
	if err != nil {
		return nil, nil, err
	}
	ch, stop := s.boards.subscribe(doc.ID)
	return ch, stop, nil
}

func (s *Service) boardChanged(a Actor, board, task, kind string) {
	s.boards.publishTo(board, BoardChange{Board: board, Task: task, Kind: kind, Actor: a.User.Name})
}
