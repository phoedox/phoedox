package core

import (
	"context"
	"testing"

	"github.com/phoedox/phoedox/internal/store"
)

func TestAdminSeesOtherProjectsOnlyWhenAsked(t *testing.T) {
	ctx := context.Background()
	s, owner, _, _ := setup(t)
	adminUser, err := s.DB.CreateUser(ctx, store.User{ID: newID(), Email: "root@x.de", Name: "Root", IsAdmin: true})
	if err != nil {
		t.Fatal(err)
	}
	admin := Actor{User: adminUser, Via: "web"}
	theirs, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Privat"})
	mine, _ := s.CreateProject(ctx, admin, ProjectInput{Name: "Meins"})

	ps, err := s.Projects(ctx, admin)
	if err != nil || len(ps) != 1 || ps[0].ID != mine.ID || ps[0].AdminAccess {
		t.Fatalf("own projects: %+v %v", ps, err)
	}
	all, _ := s.AllProjects(ctx, admin)
	found := false
	for _, p := range all {
		if p.ID == theirs.ID {
			found = p.AdminAccess && p.Role == store.RoleOwner
		}
	}
	if !found {
		t.Fatalf("all projects: %+v", all)
	}
	// Opening someone else's project still works and says where the rights come from.
	p, err := s.Project(ctx, admin, theirs.Slug)
	if err != nil || !p.AdminAccess {
		t.Fatalf("open: %+v %v", p, err)
	}
	// AI clients of an admin never get the whole instance.
	if all, _ := s.AllProjects(ctx, Actor{User: adminUser, Via: "mcp"}); len(all) != 1 {
		t.Fatalf("mcp: %d", len(all))
	}
	// Normal users are not affected.
	if all, _ := s.AllProjects(ctx, owner); len(all) != 1 || all[0].AdminAccess {
		t.Fatalf("owner: %+v", all)
	}
}
