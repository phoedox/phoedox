package core

import (
	"context"
	"testing"

	"github.com/phoedox/phoedox/internal/store"
)

func TestAppPolicy(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, _ := setup(t)
	admin := Actor{User: store.User{ID: "sys", Name: "System", IsAdmin: true}, Via: "web"}
	o, _ := s.CreateOrg(ctx, admin, OrgInput{Name: "Firma", AdminEmail: "owner@x.de"})
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Apps", Org: &o.Slug})
	_ = s.SetMember(ctx, owner, p.Slug, "reader@x.de", store.RoleReader)
	res, err := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: store.DocApp, Title: "Tool", App: &App{Level: 3, AI: true}})
	if err != nil {
		t.Fatal(err)
	}
	f, err := s.OpenApp(ctx, reader, p.Slug, res.Document.ID)
	if err != nil || f.MaxLevel != 3 || !f.AI {
		t.Fatalf("open: %v %+v", err, f)
	}
	if _, err := s.SetInstanceAppPolicy(ctx, owner, AppPolicy{Off: true}); err == nil {
		t.Fatal("only system admins")
	}
	one := 1
	if _, err := s.SetInstanceAppPolicy(ctx, admin, AppPolicy{MaxLevel: &one, Hosts: []string{"https://API.example.com", "bad host"}}); invalidKey(err) != "validation.app_host" {
		t.Fatalf("host check: %v", err)
	}
	if _, err := s.SetInstanceAppPolicy(ctx, admin, AppPolicy{MaxLevel: &one, Hosts: []string{"https://API.example.com"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetOrgAppPolicy(ctx, owner, o.Slug, AppPolicy{NoAI: true, Hosts: []string{"org.example.com"}}); err != nil {
		t.Fatal(err)
	}
	f, _ = s.OpenApp(ctx, reader, p.Slug, res.Document.ID)
	if f.MaxLevel != 1 || f.AI {
		t.Fatalf("capped: %+v", f)
	}
	if _, err := s.GrantApp(ctx, reader, p.Slug, res.Document.ID, 3); err != nil {
		t.Fatal(err)
	}
	if _, _, _, lvl, _ := s.appFor(ctx, reader, p.Slug, res.Document.ID); lvl != 1 {
		t.Fatalf("level after grant: %d", lvl)
	}
	pr, _ := s.DB.ProjectFor(ctx, p.ID, "")
	if pol := s.effectiveAppPolicy(ctx, pr); len(pol.Hosts) != 1 || pol.Hosts[0] != "api.example.com" {
		t.Fatalf("org hosts need the instance's ok: %+v", pol.Hosts)
	}
	_, _ = s.SetInstanceAppPolicy(ctx, admin, AppPolicy{OrgHosts: true, Hosts: []string{"api.example.com"}})
	if pol := s.effectiveAppPolicy(ctx, pr); len(pol.Hosts) != 2 {
		t.Fatalf("org hosts: %+v", pol.Hosts)
	}
	// The organization switches apps off.
	_, _ = s.SetOrgAppPolicy(ctx, owner, o.Slug, AppPolicy{Off: true})
	if _, err := s.OpenApp(ctx, reader, p.Slug, res.Document.ID); invalidKey(err) != "validation.apps_off" {
		t.Fatalf("off: %v", err)
	}
	if _, err := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: store.DocApp, Title: "Other"}); invalidKey(err) != "validation.apps_off" {
		t.Fatalf("create while off: %v", err)
	}
}
