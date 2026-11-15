package core

import (
	"context"
	"testing"

	"github.com/phoedox/phoedox/internal/store"
)

func TestDiscovery(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, _ := setup(t)
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Home", AIWriteMode: "direct"})
	_ = s.SetMember(ctx, owner, p.Slug, "reader@x.de", store.RoleReader)
	_, _ = s.UpsertEntity(ctx, owner, p.Slug, EntityInput{Kind: "server", Name: "NAS", Slug: "nas", Attrs: map[string]any{"ip": "10.0.0.5"}})
	ag := store.Agent{ID: "ag1", ProjectID: p.ID}
	s.saveDiscovered(ctx, ag, []DiscoveredIn{
		{IP: "10.0.0.5", MAC: "AA:BB:CC:00:00:05", Via: []string{"arp"}},
		{IP: "10.0.0.7", Hostname: "printer", Services: []string{"_ipp._tcp"}, Via: []string{"mdns"}},
		{IP: "127.0.0.1"}, {IP: "not an ip"},
	})
	// A later round without the host name keeps it.
	s.saveDiscovered(ctx, ag, []DiscoveredIn{{IP: "10.0.0.7", Via: []string{"arp"}}})
	list, err := s.Discovered(ctx, owner, p.Slug, false)
	if err != nil || len(list) != 2 {
		t.Fatalf("%+v %v", list, err)
	}
	var printer DiscoveredView
	for _, d := range list {
		switch d.IP {
		case "10.0.0.5":
			if d.Entity != "nas" || d.MAC != "aa:bb:cc:00:00:05" {
				t.Fatalf("nas: %+v", d)
			}
		case "10.0.0.7":
			printer = d
		}
	}
	if printer.Hostname != "printer" || printer.Kind != "device" || printer.Entity != "" {
		t.Fatalf("printer: %+v", printer)
	}
	if _, err := s.Discovered(ctx, reader, p.Slug, false); err == nil {
		t.Fatal("reader sees discovered devices")
	}
	res, err := s.AdoptDiscovered(ctx, owner, p.Slug, printer.ID, AdoptInput{Kind: "device", Name: "Office printer"})
	if err != nil || res.Entity == nil || res.Entity.Slug != "office-printer" {
		t.Fatalf("adopt: %+v %v", res, err)
	}
	list, _ = s.Discovered(ctx, owner, p.Slug, false)
	for _, d := range list {
		if d.IP == "10.0.0.7" && d.Entity != "office-printer" {
			t.Fatalf("not matched after adopt: %+v", d)
		}
	}
	if err := s.DismissDiscovered(ctx, owner, p.Slug, printer.ID, true); err != nil {
		t.Fatal(err)
	}
	if l, _ := s.Discovered(ctx, owner, p.Slug, false); len(l) != 1 {
		t.Fatalf("dismissed still listed: %+v", l)
	}

	// An organization that forbids discovery drops reports and old results.
	o, err := s.CreateOrg(ctx, Actor{User: store.User{ID: owner.User.ID, IsAdmin: true}, Via: "web"}, OrgInput{Name: "Firma"})
	if err != nil {
		t.Fatal(err)
	}
	off := true
	if _, err := s.UpdateOrg(ctx, Actor{User: store.User{ID: owner.User.ID, IsAdmin: true}, Via: "web"}, o.Slug, OrgInput{DiscoveryOff: &off}); err != nil {
		t.Fatal(err)
	}
	q, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Work", Org: &o.Slug})
	s.saveDiscovered(ctx, store.Agent{ID: "ag2", ProjectID: q.ID}, []DiscoveredIn{{IP: "10.1.0.1"}})
	if l, _ := s.DB.ListDiscovered(ctx, q.ID, true); len(l) != 0 {
		t.Fatalf("kept despite org rule: %+v", l)
	}
}
