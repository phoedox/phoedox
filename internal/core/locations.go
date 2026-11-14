package core

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/phoedox/phoedox/internal/store"
)

// LocationNode is a place in the location hierarchy site → building → floor → room.
type LocationNode struct {
	Slug     string          `json:"slug"`
	Name     string          `json:"name"`
	Kind     string          `json:"kind"`
	Attrs    map[string]any  `json:"attrs,omitempty"`
	Devices  int             `json:"devices"` // entries located in it (rooms: also mounted in its racks)
	Children []*LocationNode `json:"children"`
}

var locationOrder = map[string]int{"site": 0, "building": 1, "floor": 2, "room": 3}

// Locations returns the location hierarchy of a project; places without a
// parent are roots (readers).
func (s *Service) Locations(ctx context.Context, a Actor, ref string) ([]*LocationNode, error) {
	p, err := s.project(ctx, a, ref, store.RoleReader)
	if err != nil {
		return nil, err
	}
	ents, err := s.DB.ListEntities(ctx, p.ID, "")
	if err != nil {
		return nil, err
	}
	edges, err := s.DB.EdgesOfProject(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	nodes := map[string]*LocationNode{}
	kindOf := map[string]string{}
	for _, e := range ents {
		kindOf[e.ID] = e.Kind
		if _, ok := locationOrder[e.Kind]; !ok {
			continue
		}
		n := &LocationNode{Slug: e.Slug, Name: e.Name, Kind: e.Kind, Children: []*LocationNode{}}
		_ = json.Unmarshal(e.Attrs, &n.Attrs)
		nodes[e.ID] = n
	}
	parent := map[string]string{}
	rackRoom := map[string]string{}
	for _, e := range edges {
		switch {
		case e.Rel == "part_of" && nodes[e.From] != nil && nodes[e.To] != nil && locationOrder[kindOf[e.To]] < locationOrder[kindOf[e.From]]:
			parent[e.From] = e.To
		case e.Rel == "located_in" && nodes[e.To] != nil:
			nodes[e.To].Devices++
			if kindOf[e.From] == "rack" {
				rackRoom[e.From] = e.To
			}
		}
	}
	for _, e := range edges {
		if e.Rel == "mounted_in" && rackRoom[e.To] != "" {
			nodes[rackRoom[e.To]].Devices++
		}
	}
	roots := []*LocationNode{}
	for id, n := range nodes {
		if pid, ok := parent[id]; ok {
			nodes[pid].Children = append(nodes[pid].Children, n)
		} else {
			roots = append(roots, n)
		}
	}
	var order func([]*LocationNode)
	order = func(list []*LocationNode) {
		sort.Slice(list, func(i, j int) bool {
			a, b := list[i], list[j]
			if locationOrder[a.Kind] != locationOrder[b.Kind] {
				return locationOrder[a.Kind] < locationOrder[b.Kind]
			}
			if a.Kind == "floor" {
				la, _ := a.Attrs["level"].(float64)
				lb, _ := b.Attrs["level"].(float64)
				if la != lb {
					return la < lb
				}
			}
			return strings.ToLower(a.Name) < strings.ToLower(b.Name)
		})
		for _, n := range list {
			order(n.Children)
		}
	}
	order(roots)
	return roots, nil
}
