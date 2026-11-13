package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/phoedox/phoedox/internal/store"
)

// Limits.
const (
	maxMessage      = 4 << 20 // one update (large pastes)
	sendBuffer      = 256
	compactCount    = 200     // updates before asking for a snapshot
	compactBytes    = 2 << 20 // or total size
	writeTimeout    = 10 * time.Second
	compactDeadline = 30 * time.Second
)

// Hub holds one room per document with live editors.
type Hub struct {
	DB *store.DB

	mu    sync.Mutex
	rooms map[string]*room
}

// NewHub creates a hub.
func NewHub(db *store.DB) *Hub { return &Hub{DB: db, rooms: map[string]*room{}} }

type room struct {
	id      string
	mu      sync.Mutex
	loaded  bool
	nextSeq int64
	count   int
	size    int
	clients map[*client]struct{}
	// bootstrapper fills an empty document from Markdown.
	bootstrapper *client
	// compaction in progress: who was asked and up to which seq.
	compactor *client
	compactTo int64
	compactAt time.Time
}

type client struct {
	conn   *websocket.Conn
	send   chan []byte
	name   string
	aware  map[uint64]awarenessEntry // latest awareness per Yjs client id
	closed bool
}

// Control is a JSON message to clients (type 100).
type Control struct {
	Type string `json:"type"`           // bootstrap | compact | reset
	Upto int64  `json:"upto,omitempty"` // compact: include updates up to this seq
	By   string `json:"by,omitempty"`   // reset: who changed the document
}

func controlMsg(c Control) []byte {
	b, _ := json.Marshal(c)
	return (&encoder{}).uint(msgControl).string(string(b)).b
}

func (h *Hub) room(id string) *room {
	h.mu.Lock()
	defer h.mu.Unlock()
	r := h.rooms[id]
	if r == nil {
		r = &room{id: id, clients: map[*client]struct{}{}}
		h.rooms[id] = r
	}
	return r
}

// Serve upgrades the request and runs a live editing session for the
// document. The caller has checked that the user may edit it.
func (h *Hub) Serve(w http.ResponseWriter, r *http.Request, documentID, userName string) {
	conn, err := websocket.Accept(w, r, nil) // checks Origin == Host
	if err != nil {
		return
	}
	conn.SetReadLimit(maxMessage)
	c := &client{conn: conn, send: make(chan []byte, sendBuffer), name: userName, aware: map[uint64]awarenessEntry{}}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	go c.writeLoop(ctx, cancel)

	rm := h.room(documentID)
	if err := h.join(ctx, rm, c); err != nil {
		slog.Warn("live editing join failed", "doc", documentID, "err", err)
		conn.Close(websocket.StatusInternalError, "join failed")
		return
	}
	defer h.leave(rm, c)
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		if err := h.handle(ctx, rm, c, data); err != nil {
			slog.Debug("live editing message dropped", "doc", documentID, "err", err)
		}
	}
}

func (c *client) writeLoop(ctx context.Context, cancel context.CancelFunc) {
	defer cancel()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-c.send:
			if !ok {
				_ = c.conn.Close(websocket.StatusNormalClosure, "")
				return
			}
			wctx, done := context.WithTimeout(ctx, writeTimeout)
			err := c.conn.Write(wctx, websocket.MessageBinary, msg)
			done()
			if err != nil {
				return
			}
		}
	}
}

// queue sends a message without blocking; slow clients are disconnected.
func (r *room) queue(c *client, msg []byte) {
	if c.closed {
		return
	}
	select {
	case c.send <- msg:
	default:
		c.closed = true
		close(c.send)
	}
}

func (r *room) broadcast(from *client, msg []byte) {
	for c := range r.clients {
		if c != from {
			r.queue(c, msg)
		}
	}
}

func (h *Hub) load(ctx context.Context, r *room) ([]store.DocUpdate, error) {
	ups, err := h.DB.DocUpdates(ctx, r.id)
	if err != nil {
		return nil, err
	}
	if !r.loaded {
		r.loaded = true
		r.count, r.size, r.nextSeq = len(ups), 0, 1
		for _, u := range ups {
			r.size += len(u.Data)
			r.nextSeq = u.Seq + 1
		}
	}
	return ups, nil
}

func (h *Hub) join(ctx context.Context, r *room, c *client) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	ups, err := h.load(ctx, r)
	if err != nil {
		return err
	}
	r.clients[c] = struct{}{}
	// Ask for the client's state (offline edits), then send the log.
	r.queue(c, syncMsg(syncStep1, emptyState))
	for _, u := range ups {
		r.queue(c, syncMsg(syncUpdate, u.Data))
	}
	if len(ups) == 0 && r.bootstrapper == nil {
		r.bootstrapper = c
		r.queue(c, controlMsg(Control{Type: "bootstrap"}))
	}
	// Step 2 marks the client as synced.
	r.queue(c, syncMsg(syncStep2, emptyUpdate))
	// Who is here already (names and cursors).
	var entries []awarenessEntry
	for o := range r.clients {
		for _, e := range o.aware {
			entries = append(entries, e)
		}
	}
	if len(entries) > 0 {
		r.queue(c, (&encoder{}).uint(msgAwareness).bytes(encodeAwareness(entries)).b)
	}
	return nil
}

func (h *Hub) leave(r *room, c *client) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.clients, c)
	if !c.closed {
		c.closed = true
		close(c.send)
	}
	// Tell the others that this client's cursors are gone.
	if len(c.aware) > 0 {
		var gone []awarenessEntry
		for id, e := range c.aware {
			gone = append(gone, awarenessEntry{ClientID: id, Clock: e.Clock + 1, State: "null"})
		}
		r.broadcast(c, (&encoder{}).uint(msgAwareness).bytes(encodeAwareness(gone)).b)
	}
	if r.bootstrapper == c {
		r.bootstrapper = nil
		if r.count == 0 {
			for o := range r.clients {
				r.bootstrapper = o
				r.queue(o, controlMsg(Control{Type: "bootstrap"}))
				break
			}
		}
	}
	if r.compactor == c {
		r.compactor = nil
	}
	if len(r.clients) == 0 {
		h.mu.Lock()
		if h.rooms[r.id] == r {
			delete(h.rooms, r.id)
		}
		h.mu.Unlock()
	}
}

func (h *Hub) handle(ctx context.Context, r *room, c *client, data []byte) error {
	d := &decoder{b: data}
	typ, err := d.uint()
	if err != nil {
		return err
	}
	switch typ {
	case msgSync:
		kind, err := d.uint()
		if err != nil {
			return err
		}
		switch kind {
		case syncStep1:
			// We already sent everything on join; nothing to add.
			return nil
		case syncStep2, syncUpdate:
			update, err := d.bytes()
			if err != nil {
				return err
			}
			return h.store(ctx, r, c, update)
		}
	case msgAwareness:
		payload, err := d.bytes()
		if err != nil {
			return err
		}
		entries, err := decodeAwareness(payload)
		if err != nil {
			return err
		}
		r.mu.Lock()
		for _, e := range entries {
			if e.State == "null" {
				delete(c.aware, e.ClientID)
			} else {
				c.aware[e.ClientID] = e
			}
		}
		r.broadcast(c, data)
		r.mu.Unlock()
	case 3: // query awareness: clients answer each other
		r.mu.Lock()
		r.broadcast(c, data)
		r.mu.Unlock()
	case msgSnapshot:
		upto, err := d.uint()
		if err != nil {
			return err
		}
		snapshot, err := d.bytes()
		if err != nil {
			return err
		}
		return h.compact(ctx, r, c, int64(upto), snapshot)
	}
	return nil
}

func (h *Hub) store(ctx context.Context, r *room, c *client, update []byte) error {
	if len(update) <= len(emptyUpdate) {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	seq := r.nextSeq
	if err := h.DB.AppendDocUpdate(ctx, r.id, seq, update); err != nil {
		return err
	}
	r.nextSeq++
	r.count++
	r.size += len(update)
	if r.bootstrapper == c {
		r.bootstrapper = nil // the document has content now
	}
	r.broadcast(c, syncMsg(syncUpdate, update))
	expired := r.compactor != nil && time.Since(r.compactAt) > compactDeadline
	if (r.count > compactCount || r.size > compactBytes) && (r.compactor == nil || expired) {
		r.compactor, r.compactTo, r.compactAt = c, seq, time.Now()
		r.queue(c, controlMsg(Control{Type: "compact", Upto: seq}))
	}
	return nil
}

func (h *Hub) compact(ctx context.Context, r *room, c *client, upto int64, snapshot []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.compactor != c || upto != r.compactTo || len(snapshot) == 0 {
		return errors.New("unexpected snapshot")
	}
	r.compactor = nil
	if err := h.DB.CompactDocUpdates(ctx, r.id, upto, snapshot); err != nil {
		return err
	}
	ups, err := h.DB.DocUpdates(ctx, r.id)
	if err != nil {
		return err
	}
	r.count, r.size = len(ups), 0
	for _, u := range ups {
		r.size += len(u.Data)
	}
	return nil
}

// Reset clears the live state of a document after it changed outside the
// editor (API, MCP, restore, delete). Connected editors reload.
func (h *Hub) Reset(ctx context.Context, documentID, by string) {
	if err := h.DB.ClearDocUpdates(ctx, documentID); err != nil {
		slog.Warn("clearing live state failed", "doc", documentID, "err", err)
	}
	h.mu.Lock()
	r := h.rooms[documentID]
	h.mu.Unlock()
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.loaded, r.count, r.size, r.nextSeq = false, 0, 0, 1
	r.bootstrapper, r.compactor = nil, nil
	for c := range r.clients {
		r.queue(c, controlMsg(Control{Type: "reset", By: by}))
	}
}

// Editors returns the number of connected editors of a document.
func (h *Hub) Editors(documentID string) int {
	h.mu.Lock()
	r := h.rooms[documentID]
	h.mu.Unlock()
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.clients)
}

// TotalEditors counts all connected live editors (for metrics).
func (h *Hub) TotalEditors() int {
	h.mu.Lock()
	rooms := make([]*room, 0, len(h.rooms))
	for _, r := range h.rooms {
		rooms = append(rooms, r)
	}
	h.mu.Unlock()
	n := 0
	for _, r := range rooms {
		r.mu.Lock()
		n += len(r.clients)
		r.mu.Unlock()
	}
	return n
}

// CloseAll disconnects every live editor and forgets the rooms (after a
// restore the stored state changed underneath them); editors reconnect.
func (h *Hub) CloseAll() {
	h.mu.Lock()
	rooms := h.rooms
	h.rooms = map[string]*room{}
	h.mu.Unlock()
	for _, r := range rooms {
		r.mu.Lock()
		for c := range r.clients {
			_ = c.conn.Close(websocket.StatusServiceRestart, "restored")
		}
		r.mu.Unlock()
	}
}
