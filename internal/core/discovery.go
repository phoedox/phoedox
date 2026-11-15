package core

import (
	"context"
	"encoding/json"
	"net"
	"strings"

	"github.com/phoedox/phoedox/internal/graph"
	"github.com/phoedox/phoedox/internal/store"
)

// Network discovery (ADR-0072): agents that have discovery switched on
// report devices from their neighbour table and mDNS. Phoedox keeps them
// in a list for people to add as entries or ignore; it never creates
// entries on its own. Organizations can forbid discovery for all their
// projects, then reports are dropped.

// DiscoveredIn is a device in an agent report.
type DiscoveredIn struct {
	IP       string   `json:"ip"`
	MAC      string   `json:"mac,omitempty"`
	Hostname string   `json:"hostname,omitempty"`
	Services []string `json:"services,omitempty"`
	Via      []string `json:"via,omitempty"`
}

// DiscoveredView is a discovered device with the entry it matches.
type DiscoveredView struct {
	store.Discovered
	Entity     string `json:"entity,omitempty"` // slug of the matching entry
	EntityName string `json:"entityName,omitempty"`
	// Kind is a suggestion for a new entry.
	Kind string `json:"kind"`
}

// discoveryAllowed is false when the project's organization forbids discovery.
func (s *Service) discoveryAllowed(ctx context.Context, projectID string) bool {
	p, err := s.DB.ProjectFor(ctx, projectID, "")
	if err != nil {
		return false
	}
	if p.OrgID == "" {
		return true
	}
	o, err := s.DB.OrgFor(ctx, p.OrgID, "")
	return err == nil && !o.DiscoveryOff
}

func (s *Service) saveDiscovered(ctx context.Context, ag store.Agent, in []DiscoveredIn) {
	if !s.discoveryAllowed(ctx, ag.ProjectID) {
		_ = s.DB.DeleteDiscovered(ctx, ag.ProjectID)
		return
	}
	if len(in) > 2000 {
		in = in[:2000]
	}
	list := make([]store.Discovered, 0, len(in))
	for _, d := range in {
		ip := net.ParseIP(strings.TrimSpace(d.IP))
		if ip == nil || ip.IsLoopback() || ip.IsMulticast() {
			continue
		}
		mac := ""
		if hw, err := net.ParseMAC(strings.TrimSpace(d.MAC)); err == nil && normMAC(hw.String()) != "" {
			mac = hw.String()
		}
		svc := make([]string, 0, len(d.Services))
		for _, x := range d.Services {
			if x = strings.TrimSpace(x); x != "" && len(x) < 80 && len(svc) < 20 {
				svc = append(svc, x)
			}
		}
		list = append(list, store.Discovered{ID: newID(), IP: ip.String(), MAC: mac, Hostname: clipRunes(d.Hostname, 100), Services: svc, Via: d.Via})
	}
	_ = s.DB.SaveDiscovered(ctx, ag.ProjectID, ag.ID, list)
}

// guessKind suggests a core kind from the announced services.
func guessKind(services []string) string {
	has := func(p string) bool {
		for _, s := range services {
			if strings.HasPrefix(s, p) {
				return true
			}
		}
		return false
	}
	switch {
	case has("_home-assistant") || has("_ssh") || has("_smb") || has("_afpovertcp") || has("_nfs") || has("_workstation"):
		return "server"
	case has("_sip"):
		return "phone"
	}
	return "device"
}

// Discovered lists the devices agents saw in the project (editors).
func (s *Service) Discovered(ctx context.Context, a Actor, ref string, withDismissed bool) ([]DiscoveredView, error) {
	p, err := s.project(ctx, a, ref, store.RoleEditor)
	if err != nil {
		return nil, err
	}
	list, err := s.DB.ListDiscovered(ctx, p.ID, withDismissed)
	if err != nil {
		return nil, err
	}
	ents, _ := s.DB.ListEntities(ctx, p.ID, "")
	byMAC, byIP := map[string]store.Entity{}, map[string]store.Entity{}
	for _, e := range ents {
		attrs := map[string]any{}
		_ = json.Unmarshal(e.Attrs, &attrs)
		if v, ok := attrs["mac"].(string); ok && normMAC(v) != "" {
			byMAC[normMAC(v)] = e
		}
		for _, k := range []string{"ip", "ips"} {
			if v, ok := attrs[k].(string); ok {
				for _, ip := range strings.Split(v, ",") {
					byIP[strings.TrimSpace(ip)] = e
				}
			}
		}
	}
	out := make([]DiscoveredView, 0, len(list))
	for _, d := range list {
		v := DiscoveredView{Discovered: d, Kind: guessKind(d.Services)}
		e, ok := byMAC[normMAC(d.MAC)]
		if !ok || d.MAC == "" {
			e, ok = byIP[d.IP]
		}
		if ok {
			v.Entity, v.EntityName = e.Slug, e.Name
		}
		out = append(out, v)
	}
	return out, nil
}

// AdoptInput turns a discovered device into an entry.
type AdoptInput struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

// AdoptDiscovered creates an entry from a discovered device (editors).
func (s *Service) AdoptDiscovered(ctx context.Context, a Actor, ref, id string, in AdoptInput) (EntityResult, error) {
	p, err := s.project(ctx, a, ref, store.RoleEditor)
	if err != nil {
		return EntityResult{}, err
	}
	d, err := s.DB.GetDiscovered(ctx, p.ID, id)
	if err != nil {
		return EntityResult{}, err
	}
	name := firstNonEmpty(in.Name, d.Hostname, d.IP)
	kind := firstNonEmpty(in.Kind, guessKind(d.Services))
	attrs := map[string]any{"ip": d.IP}
	if d.MAC != "" {
		attrs["mac"] = d.MAC
	}
	if d.Hostname != "" {
		attrs["hostname"] = d.Hostname
	}
	if len(d.Services) > 0 {
		attrs["services"] = strings.Join(d.Services, ", ")
	}
	res, err := s.UpsertEntity(ctx, a, p.Slug, EntityInput{Kind: kind, Name: name, Slug: graph.Slugify(name), Attrs: attrs})
	if err == nil {
		s.event(ctx, a, "discovery.adopt", d.ID, map[string]string{"ip": d.IP, "entity": graph.Slugify(name)})
	}
	return res, err
}

// DismissDiscovered hides a device from the list (or shows it again).
func (s *Service) DismissDiscovered(ctx context.Context, a Actor, ref, id string, dismissed bool) error {
	p, err := s.project(ctx, a, ref, store.RoleEditor)
	if err != nil {
		return err
	}
	if err := s.DB.DismissDiscovered(ctx, p.ID, id, dismissed); err != nil {
		return err
	}
	s.event(ctx, a, "discovery.dismiss", id, map[string]bool{"dismissed": dismissed})
	return nil
}
