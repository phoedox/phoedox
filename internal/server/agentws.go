package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/phoedox/phoedox/internal/core"
)

// Agents can keep one WebSocket open instead of long-polling (ADR-0037,
// both transports): jobs are pushed, results and reports come back over
// the same connection. The token is checked before the upgrade, so a
// revoked agent gets 401 and falls back to nothing.
//
// Server → agent: {"type":"jobs","jobs":[…signed jobs…]}, {"type":"ping"}
// Agent → server: {"type":"result","id","ok","output"}, {"type":"report","report":{…}}, {"type":"pong"}

type agentMsg struct {
	Type   string           `json:"type"`
	ID     string           `json:"id,omitempty"`
	OK     bool             `json:"ok,omitempty"`
	Output string           `json:"output,omitempty"`
	Report json.RawMessage  `json:"report,omitempty"`
	Jobs   []core.SignedJob `json:"jobs,omitempty"`
}

func (a *api) agentWS(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if _, _, err := a.core.AgentFromToken(r.Context(), token); err != nil {
		writeJSON(w, http.StatusUnauthorized, errBody("unauthorized", "Agent token not accepted."))
		return
	}
	// Agents are not browsers: no Origin to check.
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	c.SetReadLimit(4 << 20)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	defer c.CloseNow()

	// Reader: results and reports.
	go func() {
		defer cancel()
		for {
			var m agentMsg
			if err := wsjson.Read(ctx, c, &m); err != nil {
				return
			}
			switch m.Type {
			case "result":
				if err := a.core.AgentJobResult(ctx, token, m.ID, m.OK, m.Output); err != nil && err == core.ErrForbidden {
					return
				}
			case "report":
				var rep core.Report
				if json.Unmarshal(m.Report, &rep) == nil {
					if err := a.core.AgentReport(ctx, token, rep); err == core.ErrForbidden {
						return
					}
				}
			}
		}
	}()
	for ctx.Err() == nil {
		// AgentJobs waits up to 25 s for jobs (the same queue as long-poll).
		jobs, err := a.core.AgentJobs(ctx, token, 25*time.Second)
		if err != nil {
			c.Close(websocket.StatusPolicyViolation, "token")
			return
		}
		m := agentMsg{Type: "ping"}
		if len(jobs) > 0 {
			m = agentMsg{Type: "jobs", Jobs: jobs}
		}
		wctx, wcancel := context.WithTimeout(ctx, 15*time.Second)
		err = wsjson.Write(wctx, c, m)
		wcancel()
		if err != nil {
			return
		}
	}
	c.Close(websocket.StatusNormalClosure, "")
}
