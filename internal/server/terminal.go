package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"
	"github.com/phoedox/phoedox/internal/core"
	"github.com/phoedox/phoedox/internal/store"
)

// Web terminal relay (ADR-0048): the browser and the agent both connect
// here (the agent outbound, it opens no port); bytes are relayed and
// recorded as an asciicast v2 file for the entry.

const (
	termWait      = 2 * time.Minute  // how long one side waits for the other
	termMax       = 65 * time.Minute // hard limit (the agent ends at 60)
	termRecordMax = 5 << 20
)

type termPair struct {
	browser, agent *websocket.Conn
	ready          chan struct{} // closed when both sides are there
	done           chan struct{} // closed when the relay ended
}

type termHub struct {
	mu    sync.Mutex
	pairs map[string]*termPair
}

var terminals = &termHub{pairs: map[string]*termPair{}}

// join registers one side. The second side gets ok=true and runs the relay;
// the first waits until it ended. A side that is already there is refused.
func (h *termHub) join(id string, agent bool, c *websocket.Conn) (*termPair, bool, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	p := h.pairs[id]
	if p == nil {
		p = &termPair{ready: make(chan struct{}), done: make(chan struct{})}
		h.pairs[id] = p
	}
	if (agent && p.agent != nil) || (!agent && p.browser != nil) {
		return nil, false, false
	}
	if agent {
		p.agent = c
	} else {
		p.browser = c
	}
	if p.agent != nil && p.browser != nil {
		close(p.ready)
		return p, true, true
	}
	return p, false, true
}

func (h *termHub) drop(id string, p *termPair) {
	h.mu.Lock()
	if h.pairs[id] == p {
		delete(h.pairs, id)
	}
	h.mu.Unlock()
}

// recorder writes an asciicast v2 stream: a header line, then
// [seconds, "o"|"i", data] events.
type recorder struct {
	mu      sync.Mutex
	buf     bytes.Buffer
	start   time.Time
	in, out int64
	full    bool
}

func newRecorder(width, height int) *recorder {
	r := &recorder{start: time.Now()}
	h, _ := json.Marshal(map[string]any{"version": 2, "width": width, "height": height, "timestamp": r.start.Unix(), "env": map[string]string{"TERM": "xterm-256color"}})
	r.buf.Write(h)
	r.buf.WriteByte('\n')
	return r
}

func (r *recorder) add(kind string, data []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if kind == "i" {
		r.in += int64(len(data))
	} else {
		r.out += int64(len(data))
	}
	if r.full {
		return
	}
	line, _ := json.Marshal([]any{time.Since(r.start).Seconds(), kind, strings.ToValidUTF8(string(data), "�")})
	if r.buf.Len()+len(line) > termRecordMax {
		r.full = true
		line, _ = json.Marshal([]any{time.Since(r.start).Seconds(), "o", "\r\n[recording truncated at 5 MB; the session went on]\r\n"})
	}
	r.buf.Write(line)
	r.buf.WriteByte('\n')
}

// relay copies messages until one side closes: binary data both ways,
// text (control) only from the browser to the agent. When one side ends,
// the other is closed normally so the browser sees a clean end.
func relay(ctx context.Context, p *termPair, rec *recorder) {
	ctx, cancel := context.WithTimeout(ctx, termMax)
	defer cancel()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { // browser → agent
		defer wg.Done()
		defer p.agent.Close(websocket.StatusNormalClosure, "closed")
		for {
			typ, data, err := p.browser.Read(ctx)
			if err != nil {
				return
			}
			if typ == websocket.MessageBinary {
				rec.add("i", data)
			} else if len(data) > 1024 {
				continue
			}
			if p.agent.Write(ctx, typ, data) != nil {
				return
			}
		}
	}()
	go func() { // agent → browser
		defer wg.Done()
		defer p.browser.Close(websocket.StatusNormalClosure, "ended")
		for {
			typ, data, err := p.agent.Read(ctx)
			if err != nil {
				return
			}
			if typ != websocket.MessageBinary {
				continue
			}
			rec.add("o", data)
			if p.browser.Write(ctx, websocket.MessageBinary, data) != nil {
				return
			}
		}
	}()
	wg.Wait()
}

func (a *api) runTerminal(w http.ResponseWriter, r *http.Request, id string, agent bool, onReady func() func(*recorder)) {
	c, err := websocket.Accept(w, r, nil) // checks Origin against Host for browsers
	if err != nil {
		return
	}
	c.SetReadLimit(1 << 20)
	p, second, ok := terminals.join(id, agent, c)
	if !ok {
		c.Close(websocket.StatusPolicyViolation, "already connected")
		return
	}
	if !second {
		select {
		case <-p.ready:
			<-p.done // the other handler relays
		case <-time.After(termWait):
			terminals.drop(id, p)
			c.Close(websocket.StatusTryAgainLater, "the other side did not connect")
			return
		case <-r.Context().Done():
			terminals.drop(id, p)
		}
		return
	}
	defer terminals.drop(id, p)
	defer close(p.done)
	finish := onReady()
	rec := newRecorder(120, 30)
	relay(context.Background(), p, rec)
	p.browser.Close(websocket.StatusNormalClosure, "ended")
	p.agent.Close(websocket.StatusNormalClosure, "ended")
	finish(rec)
}

// terminalBrowser is the browser side: GET /projects/{p}/terminal/{id}.
func (a *api) terminalBrowser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	j, err := a.core.TerminalForBrowser(r.Context(), actorOf(r), chi.URLParam(r, "p"), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	ctx := context.WithoutCancel(r.Context())
	a.runTerminal(w, r, j.ID, false, a.terminalLog(ctx, j))
}

// terminalAgent is the agent side: GET /agent/terminal/{id} with its token.
func (a *api) terminalAgent(w http.ResponseWriter, r *http.Request) {
	j, ctx, err := a.core.TerminalForAgent(r.Context(), strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	a.runTerminal(w, r, j.ID, true, a.terminalLog(context.WithoutCancel(ctx), j))
}

func (a *api) terminalLog(ctx context.Context, j store.AgentJob) func() func(*recorder) {
	return func() func(*recorder) {
		a.core.TerminalStarted(ctx, j)
		return func(rec *recorder) {
			rec.mu.Lock()
			data := bytes.Clone(rec.buf.Bytes())
			in, out, took := rec.in, rec.out, time.Since(rec.start)
			rec.mu.Unlock()
			a.core.TerminalEnded(ctx, j, data, in, out, took)
		}
	}
}

// Speed test endpoints for agents (token instead of a session).
func (a *api) agentOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, _, err := a.core.AgentFromToken(r.Context(), strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")); err != nil {
			writeErr(w, core.ErrForbidden)
			return
		}
		next(w, r)
	}
}
