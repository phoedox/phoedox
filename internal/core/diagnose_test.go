package core

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestDiagnose: a laptop behind a repeater behind the router; the repeater's
// agent is silent, so the path breaks there and names its smart plug.
func TestDiagnose(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, stranger := setup(t)
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Wohnung"})
	_ = s.SetMember(ctx, owner, p.Slug, "reader@x.de", "reader")
	fresh := time.Now().UTC().Format(time.RFC3339)
	old := time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339)
	for _, e := range []EntityInput{
		{Kind: "room", Name: "Arbeitszimmer", Slug: "az"},
		{Kind: "router", Name: "Fritzbox", Slug: "router", Attrs: map[string]any{"agent_seen": fresh}},
		{Kind: "repeater", Name: "Repeater AZ", Slug: "rep", Attrs: map[string]any{"agent_seen": old}},
		{Kind: "smart_plug", Name: "Steckdose Flur", Slug: "plug"},
		{Kind: "device", Name: "Laptop", Slug: "laptop", Attrs: map[string]any{"status": "online", "ip": "10.0.0.7"}},
		{Kind: "device", Name: "Drucker", Slug: "drucker"},
	} {
		if _, err := s.UpsertEntity(ctx, owner, p.Slug, e); err != nil {
			t.Fatal(err)
		}
	}
	for _, l := range []EdgeInput{
		{From: "laptop", Rel: "located_in", To: "az"}, {From: "drucker", Rel: "located_in", To: "az"},
		{From: "laptop", Rel: "related_to", To: "drucker"},
		{From: "rep", Rel: "uplink", To: "router"}, {From: "rep", Rel: "powered_by", To: "plug"},
	} {
		if _, err := s.Link(ctx, owner, p.Slug, l); err != nil {
			t.Fatal(l, err)
		}
	}
	if _, err := s.Link(ctx, owner, p.Slug, EdgeInput{From: "laptop", Rel: "wired_to", To: "router"}); err != nil {
		t.Fatal(err)
	}
	d, err := s.Diagnose(ctx, reader, p.Slug, "laptop")
	if err != nil || len(d.Paths) != 1 {
		t.Fatalf("%v %+v", err, d)
	}
	path := d.Paths[0]
	if len(path.Hops) != 2 || !path.Reached || path.Broken != -1 || path.Hops[0].Status != "ok" || path.Hops[0].IP != "10.0.0.7" || path.Hops[1].Via != "wired_to" || d.Result != "ok" {
		t.Fatalf("laptop: %+v", d)
	}

	d, _ = s.Diagnose(ctx, reader, p.Slug, "rep")
	path = d.Paths[0]
	if path.Broken != 0 || path.Hops[0].Reason != "diag.agent_offline" || path.Hops[0].Power == nil || path.Hops[0].Power.Slug != "plug" || d.Result != "broken" {
		t.Fatalf("repeater: %+v", d)
	}
	if !strings.Contains(d.Text, "Steckdose Flur") {
		t.Fatalf("text names the plug: %s", d.Text)
	}

	// A room diagnoses its devices; the printer has no path, so it is incomplete.
	d, _ = s.Diagnose(ctx, owner, p.Slug, "az")
	if len(d.Paths) != 1 || d.Result != "ok" {
		t.Fatalf("room: %+v", d)
	}
	d, _ = s.Diagnose(ctx, owner, p.Slug, "drucker")
	if d.Result != "incomplete" || d.Paths[0].Reached {
		t.Fatalf("printer: %+v", d)
	}
	if _, err := s.Diagnose(ctx, stranger, p.Slug, "laptop"); err == nil {
		t.Fatal("stranger must not diagnose")
	}
}

func TestDiagnoseLoop(t *testing.T) {
	ctx := context.Background()
	s, owner, _, _ := setup(t)
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "L"})
	for _, slug := range []string{"a", "b"} {
		_, _ = s.UpsertEntity(ctx, owner, p.Slug, EntityInput{Kind: "repeater", Name: slug, Slug: slug})
	}
	_, _ = s.Link(ctx, owner, p.Slug, EdgeInput{From: "a", Rel: "uplink", To: "b"})
	_, _ = s.Link(ctx, owner, p.Slug, EdgeInput{From: "b", Rel: "uplink", To: "a"})
	d, err := s.Diagnose(ctx, owner, p.Slug, "a")
	if err != nil || !d.Paths[0].Loop || len(d.Paths[0].Hops) != 2 || d.Result != "incomplete" {
		t.Fatalf("%v %+v", err, d)
	}
}
