package core

import (
	"context"
	"testing"
)

func TestIPAM(t *testing.T) {
	ctx := context.Background()
	s, owner, _, _ := setup(t)
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Net"})
	for _, in := range []EntityInput{
		{Kind: "subnet", Name: "LAN", Attrs: map[string]any{"cidr": "192.168.1.0/24", "gateway": "192.168.1.1", "dhcp_start": "192.168.1.100", "dhcp_end": "192.168.1.199", "vlan": 1}},
		{Kind: "subnet", Name: "Servers", Attrs: map[string]any{"cidr": "192.168.1.0/28"}},
		{Kind: "subnet", Name: "Broken", Attrs: map[string]any{"cidr": "nope"}},
		{Kind: "router", Name: "Router", Attrs: map[string]any{"ip": "192.168.1.1"}},
		{Kind: "server", Name: "NAS", Attrs: map[string]any{"ip": "192.168.1.5"}},
		{Kind: "server", Name: "Web", Attrs: map[string]any{"ip": "192.168.1.20"}},
		{Kind: "device", Name: "Printer", Attrs: map[string]any{"ip": "192.168.1.20"}},
		{Kind: "device", Name: "Phone", Attrs: map[string]any{"ip": "192.168.1.150"}},
		{Kind: "device", Name: "Elsewhere", Attrs: map[string]any{"ip": "10.9.9.9"}},
	} {
		if _, err := s.UpsertEntity(ctx, owner, p.ID, in); err != nil {
			t.Fatal(in.Name, err)
		}
	}
	r, err := s.IPAM(ctx, owner, p.Slug)
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]SubnetUsage{}
	for _, sn := range r.Subnets {
		by[sn.Name] = sn
	}
	lan, srv := by["LAN"], by["Servers"]
	if srv.Used != 2 || srv.Size != "14" || srv.NextFree != "192.168.1.2" {
		t.Fatalf("servers /28: %+v", srv)
	}
	// The /24 counts all its addresses; its next free one skips the /28 carved out of it.
	if lan.Size != "254" || lan.Used != 4 || lan.NextFree != "192.168.1.16" || !lan.Addresses[3].InDHCP || !lan.Addresses[0].Gateway {
		t.Fatalf("lan: %+v", lan)
	}
	if !by["Broken"].Invalid {
		t.Fatal("bad cidr not flagged")
	}
	if len(r.Conflicts) != 1 || r.Conflicts[0].IP != "192.168.1.20" || len(r.Conflicts[0].Entries) != 2 {
		t.Fatalf("conflicts: %+v", r.Conflicts)
	}
	if len(r.Outside) != 1 || r.Outside[0].IP != "10.9.9.9" {
		t.Fatalf("outside: %+v", r.Outside)
	}
}
