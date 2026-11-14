package core

import (
	"context"
	"encoding/json"
	"slices"
	"time"

	"github.com/phoedox/phoedox/internal/store"
)

// Topology (concept doc type "topology"): the network plan drawn from the
// graph itself – uplink, wired_to and runs_on – with a status per station.

var topoRels = []string{"uplink", "wired_to", "runs_on", "part_of"}

// TopoNode is a station of the plan.
type TopoNode struct {
	Slug   string `json:"slug"`
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Status string `json:"status"` // ok, warn, down, unknown
	IP     string `json:"ip,omitempty"`
}

// TopoEdge points from a station to the one it connects through (slugs).
type TopoEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Rel  string `json:"rel"`
}

// Topology is the network plan of a project.
type Topology struct {
	Nodes []TopoNode `json:"nodes"`
	Edges []TopoEdge `json:"edges"`
}

// Topology returns the network plan of a project.
func (s *Service) Topology(ctx context.Context, a Actor, ref string) (Topology, error) {
	out := Topology{Nodes: []TopoNode{}, Edges: []TopoEdge{}}
	p, err := s.project(ctx, a, ref, store.RoleReader)
	if err != nil {
		return out, err
	}
	ents, err := s.DB.ListEntities(ctx, p.ID, "")
	if err != nil {
		return out, err
	}
	edges, err := s.DB.EdgesOfProject(ctx, p.ID)
	if err != nil {
		return out, err
	}
	used := map[string]bool{}
	for _, e := range edges {
		if slices.Contains(topoRels, e.Rel) {
			out.Edges = append(out.Edges, TopoEdge{From: e.FromSlug, To: e.ToSlug, Rel: e.Rel})
			used[e.From], used[e.To] = true, true
		}
	}
	now := time.Now()
	for _, e := range ents {
		if !used[e.ID] && !slices.Contains(gatewayKinds, e.Kind) {
			continue
		}
		h := stationStatus(e, now)
		attrs := map[string]any{}
		_ = json.Unmarshal(e.Attrs, &attrs)
		ip, _ := attrs["ip"].(string)
		out.Nodes = append(out.Nodes, TopoNode{Slug: e.Slug, Name: e.Name, Kind: e.Kind, Status: h.Status, IP: ip})
	}
	return out, nil
}
