package core

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/phoedox/phoedox/internal/store"
)

func TestServiceProviders(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, stranger := setup(t)
	admin := Actor{User: store.User{ID: "sys", Name: "System", IsAdmin: true}, Via: "web"}
	// Customer "Kunde" (owner is admin), provider "Systemhaus" (stranger is admin, reader is a member).
	cust, err := s.CreateOrg(ctx, admin, OrgInput{Name: "Kunde", AdminEmail: "owner@x.de"})
	if err != nil {
		t.Fatal(err)
	}
	prov, err := s.CreateOrg(ctx, admin, OrgInput{Name: "Systemhaus", AdminEmail: "stranger@x.de"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetOrgMember(ctx, stranger, prov.Slug, "reader@x.de", store.OrgMemberRole); err != nil {
		t.Fatal(err)
	}
	net, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Netz", Org: &cust.Slug})
	hr, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Personal", Org: &cust.Slug})
	board, _ := s.CreateDocument(ctx, owner, net.Slug, DocumentInput{Type: store.DocBoard, Title: "Work", Board: &Board{}})
	task, err := s.CreateTask(ctx, owner, net.Slug, board.Document.Slug, TaskInput{Title: "Firewall update"})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := s.Project(ctx, reader, net.Slug); err == nil {
		t.Fatal("no access before the grant")
	}
	if _, err := s.GrantProvider(ctx, stranger, cust.Slug, ProviderInput{Provider: prov.Slug, Role: "editor"}); err == nil {
		t.Fatal("only customer admins grant")
	}
	if _, err := s.GrantProvider(ctx, owner, cust.Slug, ProviderInput{Provider: prov.Slug, Role: "god"}); invalidKey(err) != "validation.provider_role" {
		t.Fatalf("role: %v", err)
	}
	if _, err := s.GrantProvider(ctx, owner, cust.Slug, ProviderInput{Provider: prov.Slug, Role: "editor", Until: "2001-01-01"}); invalidKey(err) != "validation.provider_until" {
		t.Fatalf("past end: %v", err)
	}
	l, err := s.GrantProvider(ctx, owner, cust.Slug, ProviderInput{Provider: prov.Slug, Role: "editor", Projects: []string{net.Slug}, Until: "2099-12-31"})
	if err != nil || l.ProviderSlug != prov.Slug || len(l.Projects) != 1 {
		t.Fatalf("grant: %v %+v", err, l)
	}
	// Provider members work in the chosen project only.
	p, err := s.Project(ctx, reader, net.Slug)
	if err != nil || p.Role != store.RoleEditor {
		t.Fatalf("provider access: %v %+v", err, p.Role)
	}
	if _, err := s.Project(ctx, reader, hr.Slug); err == nil {
		t.Fatal("other projects stay closed")
	}
	if _, err := s.LogTime(ctx, reader, net.Slug, task.ID, TimeInput{Duration: "1h30m"}); err != nil {
		t.Fatal(err)
	}
	// The audit log names the provider.
	log, err := s.AuditLog(ctx, owner, net.Slug, AuditQuery{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range log {
		if e.Provider == prov.Slug && e.Who == reader.User.Name {
			found = true
		}
	}
	if !found {
		t.Fatalf("via provider missing: %+v", log)
	}

	// Overview and time per customer.
	ov, err := s.ProviderOverview(ctx, reader, prov.Slug)
	if err != nil || len(ov) != 1 || len(ov[0].Projects) != 1 || ov[0].Projects[0].OpenTasks != 1 || ov[0].Projects[0].MinutesMonth != 90 {
		t.Fatalf("overview: %v %+v", err, ov)
	}
	if _, err := s.ProviderTimeCSV(ctx, reader, prov.Slug, "2026-11"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("members cannot export: %v", err)
	}
	month := store.Now().UTC().Format("2006-01")
	csvData, err := s.ProviderTimeCSV(ctx, stranger, prov.Slug, month)
	if err != nil || !strings.Contains(string(csvData), "Kunde,Netz") || !strings.Contains(string(csvData), ",90,1.50,") {
		t.Fatalf("csv: %v %s", err, csvData)
	}
	links, _ := s.OrgProviderLinks(ctx, owner, cust.Slug)
	if len(links.Providers) != 1 || len(links.Customers) != 0 {
		t.Fatalf("customer view: %+v", links)
	}
	if links, _ := s.OrgProviderLinks(ctx, reader, prov.Slug); len(links.Customers) != 1 || len(links.Providers) != 0 {
		t.Fatalf("provider view: %+v", links)
	}

	// Revoking ends access at once.
	if err := s.RevokeProvider(ctx, owner, cust.Slug, l.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Project(ctx, reader, net.Slug); err == nil {
		t.Fatal("access after revoke")
	}
	// All projects, admin role.
	if _, err := s.GrantProvider(ctx, owner, cust.Slug, ProviderInput{Provider: prov.Slug, Role: "admin"}); err != nil {
		t.Fatal(err)
	}
	if p, err := s.Project(ctx, reader, hr.Slug); err != nil || p.Role != store.RoleOwner {
		t.Fatalf("all projects: %v %+v", err, p.Role)
	}
}
