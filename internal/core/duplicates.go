package core

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/phoedox/phoedox/internal/store"
)

// Duplicate detection: the same device often arrives twice, e.g. from the
// agent and from Home Assistant. Entries sharing a MAC address, serial
// number or hostname are offered for merging.

// Duplicate is a pair of entries that look like the same thing.
type Duplicate struct {
	A      DiagRef `json:"a"`
	B      DiagRef `json:"b"`
	Reason string  `json:"reason"` // mac, serial, hostname
	Value  string  `json:"value"`
}

func normMAC(s string) string {
	s = strings.ToLower(strings.NewReplacer(":", "", "-", "", ".", "", " ", "").Replace(s))
	if len(s) != 12 || s == "000000000000" {
		return ""
	}
	return s
}

func normHost(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s, _, _ = strings.Cut(s, ".") // nas.local and nas are the same host
	if s == "localhost" {
		return ""
	}
	return s
}

// Duplicates lists likely duplicates of a project (editors).
func (s *Service) Duplicates(ctx context.Context, a Actor, ref string) ([]Duplicate, error) {
	p, err := s.project(ctx, a, ref, store.RoleReader)
	if err != nil {
		return nil, err
	}
	ents, err := s.DB.ListEntities(ctx, p.ID, "")
	if err != nil {
		return nil, err
	}
	keys := map[string][]store.Entity{}
	for _, e := range ents {
		attrs := map[string]any{}
		_ = json.Unmarshal(e.Attrs, &attrs)
		str := func(k string) string { v, _ := attrs[k].(string); return v }
		if m := normMAC(str("mac")); m != "" {
			keys["mac\x00"+m] = append(keys["mac\x00"+m], e)
		}
		if sn := strings.ToLower(strings.TrimSpace(str("serial"))); len(sn) >= 4 {
			keys["serial\x00"+sn] = append(keys["serial\x00"+sn], e)
		}
		if h := normHost(str("hostname")); h != "" && e.Kind != "container" {
			keys["hostname\x00"+h] = append(keys["hostname\x00"+h], e)
		}
	}
	seen := map[string]bool{}
	out := []Duplicate{}
	for k, list := range keys {
		if len(list) < 2 {
			continue
		}
		reason, value, _ := strings.Cut(k, "\x00")
		for i := 0; i < len(list); i++ {
			for j := i + 1; j < len(list); j++ {
				x, y := list[i], list[j]
				if x.ID > y.ID {
					x, y = y, x
				}
				pair := x.ID + y.ID
				if seen[pair] {
					continue
				}
				seen[pair] = true
				out = append(out, Duplicate{A: refOf(x), B: refOf(y), Reason: reason, Value: value})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].A.Name+out[i].B.Name < out[j].A.Name+out[j].B.Name })
	return out, nil
}

// MergeEntities merges remove into keep: relations, page links and
// attachments move over, attributes of keep win and missing ones are taken
// from remove, remove goes to the trash.
func (s *Service) MergeEntities(ctx context.Context, a Actor, ref, keepRef, removeRef string) (store.Entity, error) {
	p, err := s.project(ctx, a, ref, store.RoleEditor)
	if err != nil {
		return store.Entity{}, err
	}
	if a.Via == "mcp" && p.AIWriteMode != AIDirect {
		return store.Entity{}, ErrForbidden
	}
	keep, err := s.DB.GetEntity(ctx, p.ID, keepRef)
	if err != nil {
		return keep, err
	}
	remove, err := s.DB.GetEntity(ctx, p.ID, removeRef)
	if err != nil {
		return keep, err
	}
	if keep.ID == remove.ID {
		return keep, invalid("validation.merge_self", nil, "an entry cannot be merged with itself")
	}
	ka, ra := map[string]any{}, map[string]any{}
	_ = json.Unmarshal(keep.Attrs, &ka)
	_ = json.Unmarshal(remove.Attrs, &ra)
	for k, v := range ra {
		if cur, ok := ka[k]; !ok || cur == nil || cur == "" {
			ka[k] = v
		}
	}
	if aliases, _ := ka["aliases"].(string); !strings.Contains(", "+aliases+", ", ", "+remove.Slug+", ") {
		ka["aliases"] = strings.TrimPrefix(aliases+", "+remove.Slug, ", ")
	}
	raw, _ := json.Marshal(ka)
	keep.Attrs = raw
	if keep, _, err = s.DB.UpsertEntity(ctx, keep); err != nil {
		return keep, err
	}
	if err := s.DB.MergeEntities(ctx, keep.ID, remove.ID); err != nil {
		return keep, err
	}
	s.unindex(ctx, remove.ID)
	s.indexEntity(ctx, keep)
	s.event(ctx, a, "entity.merge", keep.ID, map[string]string{"removed": remove.Slug, "into": keep.Slug, "summary": fmt.Sprintf("%s → %s", remove.Name, keep.Name)})
	return keep, nil
}
