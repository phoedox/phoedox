package core

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/phoedox/phoedox/internal/store"
)

// The software tree of a host (concept: hardware → hypervisor → VM →
// container platform → stack → container): everything that is installed on,
// runs on or is part of it, recursively.

// treeRels lead from a child to its parent.
var treeRels = map[string]bool{"installed_on": true, "runs_on": true, "part_of": true}

const maxTreeNodes = 500

// TreeNode is one entry of the tree.
type TreeNode struct {
	Slug    string `json:"slug"`
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Version string `json:"version,omitempty"`
	State   string `json:"state,omitempty"`
	Via     string `json:"via,omitempty"`
	// Update: the registry has a newer image for this container's tag.
	Update   bool       `json:"update,omitempty"`
	Children []TreeNode `json:"children,omitempty"`
}

// SoftwareTree returns what runs on an entry, as a tree.
func (s *Service) SoftwareTree(ctx context.Context, a Actor, ref, entRef string) (TreeNode, error) {
	p, err := s.project(ctx, a, ref, store.RoleReader)
	if err != nil {
		return TreeNode{}, err
	}
	root, err := s.DB.GetEntity(ctx, p.ID, entRef)
	if err != nil {
		return TreeNode{}, err
	}
	seen := map[string]bool{root.ID: true}
	count := 0
	var build func(e store.Entity, via string, depth int) TreeNode
	build = func(e store.Entity, via string, depth int) TreeNode {
		n := nodeOf(e, via)
		if depth >= 6 {
			return n
		}
		edges, _ := s.DB.EdgesOf(ctx, e.ID)
		// A stack's containers run on the host too; show them under the stack only.
		for _, x := range edges {
			if x.To != e.ID || !treeRels[x.Rel] || seen[x.From] || count >= maxTreeNodes {
				continue
			}
			if x.Rel == "runs_on" && s.partOfStack(ctx, x.From) {
				continue
			}
			child, err := s.DB.GetEntity(ctx, p.ID, x.From)
			if err != nil {
				continue
			}
			seen[x.From] = true
			count++
			n.Children = append(n.Children, build(child, x.Rel, depth+1))
		}
		sort.SliceStable(n.Children, func(i, j int) bool {
			if treeRank(n.Children[i].Kind) != treeRank(n.Children[j].Kind) {
				return treeRank(n.Children[i].Kind) < treeRank(n.Children[j].Kind)
			}
			return n.Children[i].Name < n.Children[j].Name
		})
		return n
	}
	return build(root, "", 0), nil
}

// partOfStack reports whether an entry is part of a stack, so it is shown
// below the stack rather than twice.
func (s *Service) partOfStack(ctx context.Context, id string) bool {
	edges, err := s.DB.EdgesOf(ctx, id)
	if err != nil {
		return false
	}
	for _, e := range edges {
		if e.From == id && e.Rel == "part_of" {
			return true
		}
	}
	return false
}

func treeRank(kind string) int {
	switch kind {
	case "software":
		return 0
	case "vm", "lxc":
		return 1
	case "stack":
		return 2
	}
	return 3
}

func nodeOf(e store.Entity, via string) TreeNode {
	attrs := map[string]any{}
	_ = json.Unmarshal(e.Attrs, &attrs)
	str := func(k string) string { v, _ := attrs[k].(string); return v }
	state := str("state")
	if state == "" {
		state = str("status")
	}
	update, _ := attrs["image_update"].(bool)
	return TreeNode{Slug: e.Slug, Name: e.Name, Kind: e.Kind, Version: str("version"), State: state, Via: via, Update: update}
}
