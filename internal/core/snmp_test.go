package core

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/phoedox/phoedox/internal/store"
)

func TestBridgeSNMP(t *testing.T) {
	ctx := context.Background()
	s, owner, _, _ := setup(t)
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Net", AIWriteMode: "direct"})
	_, _ = s.UpsertEntity(ctx, owner, p.Slug, EntityInput{Kind: "server", Name: "NAS", Slug: "nas", Attrs: map[string]any{"mac": "AA-BB-CC-00-00-09"}})
	_, _ = s.UpsertEntity(ctx, owner, p.Slug, EntityInput{Kind: "router", Name: "Fritzbox", Slug: "fritzbox"})
	_, _ = s.UpsertEntity(ctx, owner, p.Slug, EntityInput{Kind: "server", Name: "Behind AP", Slug: "behind", Attrs: map[string]any{"mac": "aa:bb:cc:00:00:20"}})
	agent := Actor{User: owner.User, Via: "agent", TokenID: "ag"}
	dev := SNMPDevice{Host: "10.0.0.2", SysName: "core-sw", Descr: "Cisco IOS Software, C2960\nmore", Services: 2,
		Interfaces: []SNMPInterface{{Index: 1, Name: "Gi0/1", Type: 6, Up: true, MAC: "00:11:22:33:44:55"}, {Index: 2, Name: "Gi0/2", Type: 6, Up: true}, {Index: 3, Name: "Gi0/3", Type: 6}},
		Neighbors:  []SNMPNeighbor{{LocalPort: "Gi0/1", SysName: "fritzbox"}},
		FDB: []SNMPFDB{{MAC: "aa:bb:cc:00:00:09", Port: "Gi0/2"}, {MAC: "aa:bb:cc:00:00:01", Port: "Gi0/1"},
			{MAC: "aa:bb:cc:00:00:20", Port: "Gi0/3"}, {MAC: "aa:bb:cc:00:00:21", Port: "Gi0/3"}, {MAC: "aa:bb:cc:00:00:22", Port: "Gi0/3"}, {MAC: "aa:bb:cc:00:00:23", Port: "Gi0/3"}},
	}
	s.bridgeSNMP(ctx, agent, p.ID, []SNMPDevice{dev, {Host: "10.0.0.9", Error: "timeout"}})

	sw, err := s.DB.GetEntity(ctx, p.ID, "core-sw")
	if err != nil || sw.Kind != "switch" {
		t.Fatalf("switch: %+v %v", sw, err)
	}
	var attrs map[string]any
	_ = json.Unmarshal(sw.Attrs, &attrs)
	if attrs["ip"] != "10.0.0.2" || attrs["ports"] != float64(3) || attrs["ports_up"] != float64(2) || attrs["model"] != "Cisco IOS Software, C2960" {
		t.Fatalf("attrs: %v", attrs)
	}
	edges, _ := s.DB.EdgesOf(ctx, sw.ID)
	got := map[string]string{}
	for _, e := range edges {
		var ea map[string]any
		_ = json.Unmarshal(e.Attrs, &ea)
		got[e.FromSlug+">"+e.ToSlug] = ea["port"].(string)
	}
	if len(got) != 2 || got["core-sw>fritzbox"] != "Gi0/1" || got["nas>core-sw"] != "Gi0/2" {
		t.Fatalf("edges: %v", got)
	}
	// A second poll with nothing new adds no events.
	before, _ := s.DB.Events(ctx, store.EventFilter{Kind: "edge.link", Limit: 50})
	s.bridgeSNMP(ctx, agent, p.ID, []SNMPDevice{dev})
	after, _ := s.DB.Events(ctx, store.EventFilter{Kind: "edge.link", Limit: 50})
	if len(after) != len(before) {
		t.Fatalf("relinked: %d → %d", len(before), len(after))
	}
}
