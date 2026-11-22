package core

import (
	"context"
	"testing"

	"github.com/phoedox/phoedox/internal/store"
)

func TestSingleOrgMode(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, stranger := setup(t)

	// Off: nothing happens.
	if err := s.JoinTheOrg(ctx, owner.User); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.TheOrg(ctx); ok || s.SingleOrgInfo(ctx) != nil {
		t.Fatal("no organization outside the mode")
	}

	s.SingleOrg, s.SingleOrgName = true, "Acme Engineering"
	// The first person creates it and becomes its admin.
	if err := s.JoinTheOrg(ctx, owner.User); err != nil {
		t.Fatal(err)
	}
	o, ok := s.TheOrg(ctx)
	if !ok || o.Name != "Acme Engineering" || o.Slug != "acme-engineering" {
		t.Fatalf("organization: %v %+v", ok, o)
	}
	role := func(a Actor) string {
		r, err := s.DB.OrgFor(ctx, o.ID, a.User.ID)
		if err != nil {
			t.Fatal(err)
		}
		return r.Role
	}
	if role(owner) != store.OrgAdmin {
		t.Fatalf("first person: %q", role(owner))
	}
	// Everyone else joins as member; system admins as admins; twice is fine.
	_ = s.JoinTheOrg(ctx, reader.User)
	_ = s.JoinTheOrg(ctx, reader.User)
	if role(reader) != store.OrgMemberRole {
		t.Fatalf("member: %q", role(reader))
	}
	admin := stranger
	admin.User.IsAdmin = true
	_ = s.JoinTheOrg(ctx, admin.User)
	if role(stranger) != store.OrgAdmin {
		t.Fatalf("system admin: %q", role(stranger))
	}
	// Joining never lowers a role.
	notAdmin := stranger
	notAdmin.User.IsAdmin = false
	_ = s.JoinTheOrg(ctx, notAdmin.User)
	if role(stranger) != store.OrgAdmin {
		t.Fatal("role lowered")
	}

	// No further organizations.
	if _, err := s.CreateOrg(ctx, admin, OrgInput{Name: "Other"}); invalidKey(err) != "validation.single_org" {
		t.Fatalf("second organization: %v", err)
	}

	// New projects belong to the organization and stay there.
	p, err := s.CreateProject(ctx, reader, ProjectInput{Name: "Handbook"})
	if err != nil || p.OrgID != o.ID {
		t.Fatalf("project: %v %+v", err, p)
	}
	none := ""
	p, err = s.UpdateProject(ctx, reader, p.Slug, ProjectInput{Name: "Handbook", Org: &none})
	if err != nil || p.OrgID != o.ID {
		t.Fatalf("moved out: %v %+v", err, p)
	}
	if info := s.SingleOrgInfo(ctx); info == nil || info.Slug != o.Slug {
		t.Fatalf("info: %+v", info)
	}
}
