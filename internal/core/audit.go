package core

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/phoedox/phoedox/internal/store"
)

// Audit log: everything done in someone's name, with the way it came in
// (UI, MCP, API token, plugin, agent, CLI).

// AuditEvent is an event with a readable actor.
type AuditEvent struct {
	store.Event
	// Who is the person (or agent host) behind the actor.
	Who string `json:"who"`
	// Via is web, mcp, token, plugin, agent, cli or oauth.
	Via string `json:"via"`
	// Detail names the plugin or token where it helps.
	Detail string `json:"detail,omitempty"`
}

// AuditQuery filters the log (query parameters of the API).
type AuditQuery struct {
	Via    string // web, mcp, token, plugin, agent, cli
	User   string // user id
	Kind   string // prefix
	Days   int
	Before int64
	Limit  int
}

// AuditLog returns the log of a project (owners) or, with ref "", of the
// whole instance (admins).
func (s *Service) AuditLog(ctx context.Context, a Actor, ref string, q AuditQuery) ([]AuditEvent, error) {
	if a.Via == "mcp" || a.Plugin != "" {
		return nil, ErrForbidden
	}
	f := store.EventFilter{Kind: q.Kind, Before: q.Before, Limit: q.Limit}
	if ref == "" {
		if !a.User.IsAdmin {
			return nil, ErrForbidden
		}
	} else {
		p, err := s.project(ctx, a, ref, store.RoleOwner)
		if err != nil {
			return nil, err
		}
		f.ProjectID = p.ID
	}
	switch q.Via {
	case "web":
		f.Actor = "user:" + q.User
	case "mcp", "token", "plugin":
		f.Actor = q.Via + ":" + q.User
	case "agent", "cli", "oauth":
		f.Actor = q.Via
	case "":
		if q.User != "" {
			f.Actor = "user:" + q.User
		}
	}
	if q.Days > 0 {
		f.Since = store.Time{Time: time.Now().AddDate(0, 0, -q.Days)}
	}
	events, err := s.DB.Events(ctx, f)
	if err != nil {
		return nil, err
	}
	names := map[string]string{}
	name := func(id string) string {
		if n, ok := names[id]; ok {
			return n
		}
		n := id
		if u, err := s.DB.UserByID(ctx, id); err == nil {
			n = u.Name
			if n == "" {
				n = u.Email
			}
		}
		names[id] = n
		return n
	}
	out := make([]AuditEvent, len(events))
	for i, e := range events {
		ae := AuditEvent{Event: e}
		parts := strings.SplitN(e.Actor, ":", 3)
		ae.Via = parts[0]
		switch parts[0] {
		case "user":
			ae.Via = "web"
			if len(parts) > 1 {
				ae.Who = name(parts[1])
			}
		case "mcp", "token", "plugin":
			if len(parts) > 1 {
				ae.Who = name(parts[1])
			}
			if parts[0] == "plugin" && len(parts) > 2 {
				ae.Detail = parts[2]
			}
		case "agent":
			if len(parts) > 1 {
				ae.Who = "agent " + parts[1]
				if h := s.DB.AgentHostname(ctx, parts[1]); h != "" {
					ae.Who = h
				}
			}
		default:
			ae.Who = e.Actor
		}
		out[i] = ae
	}
	return out, nil
}

// ParseAuditQuery reads the query parameters of the audit API.
func ParseAuditQuery(get func(string) string) AuditQuery {
	atoi := func(k string) int { n, _ := strconv.Atoi(get(k)); return n }
	before, _ := strconv.ParseInt(get("before"), 10, 64)
	return AuditQuery{Via: get("via"), User: get("user"), Kind: get("kind"), Days: atoi("days"), Before: before, Limit: atoi("limit")}
}
