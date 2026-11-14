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

// boardFeed fans out changes to everyone watching a board in this process.
type boardFeed struct {
	mu   sync.Mutex
	subs map[string]map[chan BoardChange]struct{}
}

func (f *boardFeed) publish(c BoardChange) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for ch := range f.subs[c.Board] {
		select {
		case ch <- c:
		default: // a slow client misses a change; it reloads on the next one
		}
	}
}

func (f *boardFeed) subscribe(board string) (chan BoardChange, func()) {
	ch := make(chan BoardChange, 8)
	f.mu.Lock()
	if f.subs == nil {
		f.subs = map[string]map[chan BoardChange]struct{}{}
	}
	if f.subs[board] == nil {
		f.subs[board] = map[chan BoardChange]struct{}{}
	}
	f.subs[board][ch] = struct{}{}
	f.mu.Unlock()
	return ch, func() {
		f.mu.Lock()
		delete(f.subs[board], ch)
		if len(f.subs[board]) == 0 {
			delete(f.subs, board)
		}
		f.mu.Unlock()
	}
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
	s.boards.publish(BoardChange{Board: board, Task: task, Kind: kind, Actor: a.User.Name})
}
