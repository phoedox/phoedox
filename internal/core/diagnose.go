package core

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/phoedox/phoedox/internal/store"
)

// Path diagnosis (concept: "Fehlersuche läuft einen Pfad ab"): from a device
// along uplink / wired_to / runs_on towards the internet, with a status for
// every station from what Phoedox knows (agent reports, state attributes).
// For a room, every device located in it gets its own path.

// pathRels are followed in this order of preference.
var pathRels = []string{"uplink", "wired_to", "runs_on"}

// gatewayKinds end a path: they connect to the internet themselves.
var gatewayKinds = []string{"router", "modem"}

const (
	maxPathHops  = 15
	maxRoomPaths = 25
)

// DiagRef names an entry.
type DiagRef struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
	Kind string `json:"kind"`
}

// DiagHop is one station of a path.
type DiagHop struct {
	DiagRef
	// Via is the relation that leads from the previous station here.
	Via string `json:"via,omitempty"`
	// Status is ok, warn, down or unknown.
	Status string `json:"status"`
	// Reason is an i18n key, with Params, explaining the status.
	Reason string         `json:"reason"`
	Params map[string]any `json:"params,omitempty"`
	IP     string         `json:"ip,omitempty"`
	// Power is what this station is powered by (a restart candidate).
	Power *DiagRef `json:"power,omitempty"`
}

// DiagPath is the way of one device.
type DiagPath struct {
	Hops []DiagHop `json:"hops"`
	// Reached means the path ends at a router or modem.
	Reached bool `json:"reached"`
	// Loop means the path ran into itself.
	Loop bool `json:"loop,omitempty"`
	// Broken is the index of the first station that is down, or -1.
	Broken int `json:"broken"`
}

// Diagnosis is the answer for one entry.
type Diagnosis struct {
	Entity DiagRef    `json:"entity"`
	Paths  []DiagPath `json:"paths"`
	// Result is ok, broken, incomplete or none.
	Result string `json:"result"`
	// Text explains the result in plain English for AI clients.
	Text string `json:"text"`
}

// Diagnose follows the network path of an entry (or of every device in a room).
func (s *Service) Diagnose(ctx context.Context, a Actor, ref, entRef string) (Diagnosis, error) {
	p, err := s.project(ctx, a, ref, store.RoleReader)
	if err != nil {
		return Diagnosis{}, err
	}
	start, err := s.DB.GetEntity(ctx, p.ID, entRef)
	if err != nil {
		return Diagnosis{}, err
	}
	d := Diagnosis{Entity: refOf(start), Paths: []DiagPath{}}
	starts := []store.Entity{start}
	if start.Kind == "room" || start.Kind == "floor" {
		starts = nil
		edges, err := s.DB.EdgesOf(ctx, start.ID)
		if err != nil {
			return d, err
		}
		for _, e := range edges {
			if e.To == start.ID && e.Rel == "located_in" && len(starts) < maxRoomPaths {
				if dev, err := s.DB.GetEntity(ctx, p.ID, e.From); err == nil {
					starts = append(starts, dev)
				}
			}
		}
	}
	now := time.Now()
	for _, st := range starts {
		path, err := s.path(ctx, p.ID, st, now)
		if err != nil {
			return d, err
		}
		if len(path.Hops) > 1 || st.ID == start.ID {
			d.Paths = append(d.Paths, path)
		}
	}
	d.Result, d.Text = summarize(d)
	return d, nil
}

func refOf(e store.Entity) DiagRef { return DiagRef{Slug: e.Slug, Name: e.Name, Kind: e.Kind} }

func (s *Service) path(ctx context.Context, projectID string, start store.Entity, now time.Time) (DiagPath, error) {
	path := DiagPath{Broken: -1}
	seen := map[string]bool{}
	cur, via := start, ""
	for range maxPathHops {
		seen[cur.ID] = true
		edges, err := s.DB.EdgesOf(ctx, cur.ID)
		if err != nil {
			return path, err
		}
		hop := stationStatus(cur, now)
		hop.Via = via
		var next *store.Edge
		for _, rel := range pathRels {
			for i := range edges {
				e := &edges[i]
				if e.From == cur.ID && e.Rel == rel && next == nil {
					next = e
				}
			}
		}
		for _, e := range edges {
			if e.From == cur.ID && e.Rel == "powered_by" {
				hop.Power = &DiagRef{Slug: e.ToSlug, Name: e.ToName, Kind: e.ToKind}
				break
			}
		}
		if hop.Status == "down" && path.Broken < 0 {
			path.Broken = len(path.Hops)
		}
		path.Hops = append(path.Hops, hop)
		if slices.Contains(gatewayKinds, cur.Kind) {
			path.Reached = true
			break
		}
		if next == nil {
			break
		}
		if seen[next.To] {
			path.Loop = true
			break
		}
		nxt, err := s.DB.GetEntity(ctx, projectID, next.To)
		if err != nil {
			break
		}
		cur, via = nxt, next.Rel
	}
	return path, nil
}

var (
	downStates = []string{"offline", "down", "off", "unavailable", "stopped", "exited", "dead", "error", "unreachable"}
	upStates   = []string{"online", "up", "on", "running", "ok", "healthy", "connected"}
)

// stationStatus judges one entry from what is stored about it.
func stationStatus(e store.Entity, now time.Time) DiagHop {
	h := DiagHop{DiagRef: refOf(e), Status: "unknown", Reason: "diag.no_data"}
	attrs := map[string]any{}
	_ = json.Unmarshal(e.Attrs, &attrs)
	if ip, ok := attrs["ip"].(string); ok {
		h.IP = ip
	}
	if seen, ok := attrs["agent_seen"].(string); ok {
		if t, err := time.Parse(time.RFC3339, seen); err == nil {
			age := now.Sub(t)
			h.Params = map[string]any{"minutes": int(age.Minutes()), "since": t.UTC().Format(time.RFC3339)}
			switch {
			case age < 3*time.Minute:
				h.Status, h.Reason = "ok", "diag.agent_ok"
			case age < 15*time.Minute:
				h.Status, h.Reason = "warn", "diag.agent_quiet"
			default:
				h.Status, h.Reason = "down", "diag.agent_offline"
			}
			return h
		}
	}
	for _, k := range []string{"state", "status"} {
		v, ok := attrs[k].(string)
		if !ok || v == "" {
			continue
		}
		h.Params = map[string]any{"state": v}
		switch l := strings.ToLower(v); {
		case slices.Contains(downStates, l):
			h.Status, h.Reason = "down", "diag.state_down"
		case slices.Contains(upStates, l):
			h.Status, h.Reason = "ok", "diag.state_ok"
		default:
			h.Reason = "diag.state_other"
		}
		return h
	}
	return h
}

func summarize(d Diagnosis) (string, string) {
	if len(d.Paths) == 0 {
		return "none", fmt.Sprintf("%s has no devices with a known network path.", d.Entity.Name)
	}
	var b strings.Builder
	result := "ok"
	for _, p := range d.Paths {
		names := make([]string, len(p.Hops))
		for i, h := range p.Hops {
			names[i] = fmt.Sprintf("%s [%s]", h.Name, h.Status)
			if i > 0 {
				names[i] = "–" + h.Via + "→ " + names[i]
			}
		}
		b.WriteString(strings.Join(names, " "))
		switch {
		case p.Broken >= 0:
			result = "broken"
			h := p.Hops[p.Broken]
			fmt.Fprintf(&b, ". First station down: %s (%s)", h.Name, h.Reason)
			if h.Power != nil {
				fmt.Fprintf(&b, "; it is powered by %s, which could restart it", h.Power.Name)
			}
		case !p.Reached:
			if result == "ok" {
				result = "incomplete"
			}
			b.WriteString(". The path does not reach a router or modem; the documentation is missing an uplink or wired_to relation")
		}
		if p.Loop {
			b.WriteString(" (the relations form a loop)")
		}
		b.WriteString(".\n")
	}
	return result, strings.TrimSpace(b.String())
}
