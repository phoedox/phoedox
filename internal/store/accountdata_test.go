package store

import (
	"context"
	"testing"
	"time"
)

func TestAccountData(t *testing.T) {
	for name, d := range testDBs(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			mk := func(email string) User {
				u, err := d.CreateUser(ctx, User{ID: id(), Email: email, Name: email})
				if err != nil {
					t.Fatal(err)
				}
				return u
			}
			a, b := mk("a@x.de"), mk("b@x.de")
			org, err := d.CreateOrg(ctx, Org{ID: id(), Slug: "acme", Name: "Acme", UsageType: "noncommercial"}, a.ID)
			if err != nil {
				t.Fatal(err)
			}
			solo, _ := d.CreateProject(ctx, Project{ID: id(), Slug: "solo", Name: "Solo", OwnerID: a.ID, OrgID: org.ID, CreatedAt: Now()})
			shared, _ := d.CreateProject(ctx, Project{ID: id(), Slug: "shared", Name: "Shared", OwnerID: a.ID, CreatedAt: Now()})
			if err := d.SetMember(ctx, shared.ID, b.ID, RoleOwner, Time{}); err != nil {
				t.Fatal(err)
			}
			owned, err := d.OwnedProjects(ctx, a.ID, Now())
			if err != nil || len(owned) != 2 {
				t.Fatalf("owned: %+v %v", owned, err)
			}
			for _, p := range owned {
				if p.ID == shared.ID && (p.OtherOwner != b.ID || p.OtherMembers != 1) || p.ID == solo.ID && (p.OtherOwner != "" || p.OtherMembers != 0) {
					t.Fatalf("owned project: %+v", p)
				}
			}
			orgs, err := d.AdminOrgs(ctx, a.ID)
			if err != nil || len(orgs) != 1 || orgs[0].OtherMembers != 0 {
				t.Fatalf("orgs: %+v %v", orgs, err)
			}
			if _, err := d.PurgeProject(ctx, solo.ID); err != nil {
				t.Fatal(err)
			}
			if n, _ := d.OrgProjectCount(ctx, org.ID); n != 0 {
				t.Fatalf("org projects: %d", n)
			}
			if err := d.DeleteOrg(ctx, org.ID); err != nil {
				t.Fatal(err)
			}
			now := Now()
			if err := d.CreateUserExport(ctx, UserExport{ID: id(), UserID: a.ID, CreatedAt: now, ExpiresAt: Time{now.Add(time.Hour)}}); err != nil {
				t.Fatal(err)
			}
			if err := d.AnonymizeUser(ctx, Anonymize{UserID: a.ID, Email: "deleted-1@deleted.invalid", Name: "Deleted person", NewOwners: map[string]string{shared.ID: b.ID}, At: now}); err != nil {
				t.Fatal(err)
			}
			u, _ := d.UserByID(ctx, a.ID)
			if !u.Deleted || !u.Disabled || u.Email != "deleted-1@deleted.invalid" {
				t.Fatalf("user: %+v", u)
			}
			if p, _ := d.ProjectFor(ctx, shared.ID, b.ID); p.OwnerID != b.ID {
				t.Fatalf("owner moved: %+v", p)
			}
			if list, _ := d.ListUsers(ctx); len(list) != 1 {
				t.Fatalf("deleted accounts are not listed: %d", len(list))
			}
			if es, _ := d.UserExports(ctx, a.ID); len(es) != 0 {
				t.Fatalf("exports removed: %d", len(es))
			}
		})
	}
}
