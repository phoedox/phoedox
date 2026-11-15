package core

import (
	"context"
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/phoedox/phoedox/internal/store"
)

// The timeline of an entry ("timeline per device"): what happened to it,
// newest first – changes (by people, AI, agents, plugins), check results,
// switching actions, agent jobs, reviews, comments and cards that mention
// it. It is put together from the audit log, comments and tasks; nothing
// extra is stored.

// TimelineItem is one moment in the life of an entry.
type TimelineItem struct {
	At time.Time `json:"at"`
	// Kind: created, changed, deleted, restored, merged, reviewed, meta,
	// check_fail, check_recover, device, device_grant, agent_job,
	// agent_result, comment, task, task_done.
	Kind string `json:"kind"`
	// Who did it, and how (web, mcp, agent, plugin, token, cli, system).
	Who string `json:"who,omitempty"`
	Via string `json:"via,omitempty"`
	// Title is a short label (check or card name, action); Text a longer
	// detail (comment text, check message).
	Title string `json:"title,omitempty"`
	Text  string `json:"text,omitempty"`
	// Changed lists the fields that changed (for "changed").
	Changed []string `json:"changed,omitempty"`
	// OK tells whether an action or job worked.
	OK *bool `json:"ok,omitempty"`
	// Link leads to the page, board card or check.
	Link string `json:"link,omitempty"`
}

// volatile fields change on every report and are left out of "changed".
var volatileAttrs = map[string]bool{"checked_at": true, "last_seen": true, "uptime": true, "uptime_s": true, "reported_at": true, "updated_at": true, "load": true, "agent_seen": true, "snmp_seen": true, "ports_up": true}

// EntityTimeline returns the timeline of an entry, at most limit items.
func (s *Service) EntityTimeline(ctx context.Context, a Actor, ref, entRef string, limit int) ([]TimelineItem, error) {
	p, err := s.project(ctx, a, ref, store.RoleReader)
	if err != nil {
		return nil, err
	}
	e, err := s.DB.GetEntity(ctx, p.ID, entRef)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 300 {
		limit = 100
	}
	own, err := s.DB.Events(ctx, store.EventFilter{ProjectID: p.ID, Subject: e.ID, Limit: 1000})
	if err != nil {
		return nil, err
	}
	checks, _ := s.DB.Events(ctx, store.EventFilter{ProjectID: p.ID, Kind: "check.", Limit: 2000})
	jobs, _ := s.DB.Events(ctx, store.EventFilter{ProjectID: p.ID, Kind: "agent.job.", Limit: 2000})
	var events []store.Event
	events = append(events, own...)
	jobIDs := map[string]bool{}
	for _, ev := range checks {
		if payloadString(ev.Payload, "entity") == e.Slug {
			events = append(events, ev)
		}
	}
	for _, ev := range jobs {
		if ev.Kind == "agent.job.create" && payloadString(ev.Payload, "entity") == e.Slug {
			jobIDs[ev.Subject] = true
			events = append(events, ev)
		}
	}
	for _, ev := range jobs {
		if ev.Kind == "agent.job.result" && jobIDs[ev.Subject] {
			events = append(events, ev)
		}
	}
	described := s.describeEvents(ctx, events)
	// Oldest first, so changes can be compared with the state before.
	sort.SliceStable(described, func(i, j int) bool { return described[i].ID < described[j].ID })

	var out []TimelineItem
	var lastAttrs map[string]any
	for _, ev := range described {
		it := TimelineItem{At: ev.CreatedAt.Time, Who: ev.Who, Via: ev.Via}
		switch ev.Kind {
		case "entity.upsert":
			var in EntityInput
			_ = json.Unmarshal(ev.Payload, &in)
			if lastAttrs == nil {
				it.Kind = "created"
			} else {
				it.Kind = "changed"
				it.Changed = changedAttrs(lastAttrs, in.Attrs)
				// A check writes the state it found; the check result says it already.
				if _, byCheck := in.Attrs["checked_at"]; byCheck && len(it.Changed) == 1 && it.Changed[0] == "state" {
					it.Changed = nil
				}
				if len(it.Changed) == 0 {
					lastAttrs = in.Attrs
					continue // a report without news
				}
			}
			lastAttrs = in.Attrs
			if lastAttrs == nil {
				lastAttrs = map[string]any{}
			}
		case "entity.delete":
			it.Kind = "deleted"
		case "entity.restore":
			it.Kind = "restored"
		case "entity.merge":
			it.Kind = "merged"
		case "entity.reviewed":
			it.Kind = "reviewed"
		case "entity.meta":
			it.Kind = "meta"
		case "check.fail", "check.recover":
			it.Kind = strings.Replace(ev.Kind, ".", "_", 1)
			it.Title = payloadString(ev.Payload, "name")
			it.Text = payloadString(ev.Payload, "detail")
			if doc := payloadString(ev.Payload, "document"); doc != "" {
				if d, err := s.DB.GetDocument(ctx, p.ID, doc); err == nil {
					it.Link = "/p/" + p.Slug + "/d/" + d.Slug
				}
			}
		case "device.control":
			it.Kind = "device"
			it.Title = payloadString(ev.Payload, "tool")
			it.OK = payloadBool(ev.Payload, "ok")
		case "device.grant", "device.revoke", "device.ai_control":
			it.Kind = "device_grant"
			it.Title = ev.Kind
		case "agent.job.create":
			it.Kind = "agent_job"
			it.Title = payloadString(ev.Payload, "action")
		case "agent.job.result":
			it.Kind = "agent_result"
			it.OK = payloadBool(ev.Payload, "ok")
		default:
			continue
		}
		out = append(out, it)
	}

	mention := "@" + e.Slug
	exact := regexp.MustCompile(`@` + regexp.QuoteMeta(e.Slug) + `\b(?:[^-/]|$)`)
	if cs, err := s.DB.CommentsContaining(ctx, p.ID, mention, 200); err == nil {
		for _, c := range cs {
			if !exact.MatchString(c.Body + " ") {
				continue
			}
			out = append(out, TimelineItem{At: c.CreatedAt.Time, Kind: "comment", Who: c.AuthorName, Via: viaOf(c.Actor), Title: c.DocTitle,
				Text: clipRunes(c.Body, 280), Link: "/p/" + p.Slug + "/d/" + c.DocSlug + "?comment=" + c.ID})
		}
	}
	if ts, err := s.DB.ListTasks(ctx, p.ID, store.TaskFilter{Mentions: mention}); err == nil {
		for _, t := range ts {
			if !exact.MatchString(t.Title + " " + t.Description + " ") {
				continue
			}
			link := "/p/" + p.Slug + "/d/" + t.BoardSlug + "?task=" + t.ID
			out = append(out, TimelineItem{At: t.CreatedAt.Time, Kind: "task", Title: t.Title, Text: t.BoardTitle, Link: link})
			if !t.DoneAt.IsZero() {
				out = append(out, TimelineItem{At: t.DoneAt.Time, Kind: "task_done", Title: t.Title, Text: t.BoardTitle, Link: link})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.After(out[j].At) })
	if len(out) > limit {
		out = out[:limit]
	}
	if out == nil {
		out = []TimelineItem{}
	}
	return out, nil
}

// changedAttrs lists the fields that differ, leaving out volatile ones.
func changedAttrs(before, after map[string]any) []string {
	var out []string
	seen := map[string]bool{}
	for k, v := range after {
		seen[k] = true
		if volatileAttrs[k] {
			continue
		}
		if old, ok := before[k]; !ok || !sameValue(old, v) {
			out = append(out, k)
		}
	}
	for k := range before {
		if !seen[k] && !volatileAttrs[k] {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func sameValue(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

func payloadString(p store.JSON, key string) string {
	var m map[string]any
	if json.Unmarshal(p, &m) != nil {
		return ""
	}
	v, _ := m[key].(string)
	return v
}

func payloadBool(p store.JSON, key string) *bool {
	var m map[string]any
	if json.Unmarshal(p, &m) != nil {
		return nil
	}
	if v, ok := m[key].(bool); ok {
		return &v
	}
	return nil
}

// viaOf names the channel of an actor string (user:…, mcp:…, agent:…).
func viaOf(actor string) string {
	v, _, _ := strings.Cut(actor, ":")
	if v == "user" {
		return "web"
	}
	return v
}

func clipRunes(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n-1]) + "…"
}
