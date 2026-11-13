package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestAgents(t *testing.T) {
	for name, db := range testDBs(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			u, err := db.CreateUser(ctx, User{ID: "u1", Email: "a@x.de", Name: "A"})
			if err != nil {
				t.Fatal(err)
			}
			p, err := db.CreateProject(ctx, Project{ID: "p1", Slug: "lab", Name: "Lab", Short: "L", PrimaryColor: "#F26A1B", AIWriteMode: "suggest", OwnerID: u.ID})
			if err != nil {
				t.Fatal(err)
			}
			if err := db.AddEnrollment(ctx, "h1", p.ID, u.ID, Time{Time: time.Now().Add(time.Hour)}, 1); err != nil {
				t.Fatal(err)
			}
			if err := db.AddEnrollment(ctx, "old", p.ID, u.ID, Time{Time: time.Now().Add(-time.Hour)}, 5); err != nil {
				t.Fatal(err)
			}
			if _, proj, user, err := db.UseEnrollment(ctx, "h1"); err != nil || proj != p.ID || user != u.ID {
				t.Fatalf("use: %v %s %s", err, proj, user)
			}
			if _, _, _, err := db.UseEnrollment(ctx, "h1"); !errors.Is(err, ErrNotFound) {
				t.Fatalf("uses are counted: %v", err)
			}
			if _, _, _, err := db.UseEnrollment(ctx, "old"); !errors.Is(err, ErrNotFound) {
				t.Fatalf("expired: %v", err)
			}
			if err := db.CreateAgent(ctx, Agent{ID: "a1", ProjectID: p.ID, UserID: u.ID, TokenHash: "t1", Hostname: "pve", CreatedAt: Now()}); err != nil {
				t.Fatal(err)
			}
			if err := db.SaveAgentReport(ctx, "a1", "pve", "pve", "0.2.0", []byte(`{"cpus":4}`)); err != nil {
				t.Fatal(err)
			}
			a, err := db.AgentByToken(ctx, "t1")
			if err != nil || a.Version != "0.2.0" || a.LastSeen.IsZero() || !a.RevokedAt.IsZero() {
				t.Fatalf("by token: %v %+v", err, a)
			}
			if err := db.RevokeAgent(ctx, p.ID, "a1"); err != nil {
				t.Fatal(err)
			}
			if _, err := db.AgentByToken(ctx, "t1"); !errors.Is(err, ErrNotFound) {
				t.Fatalf("revoked: %v", err)
			}
			list, err := db.ProjectAgents(ctx, p.ID)
			if err != nil || len(list) != 1 || list[0].RevokedAt.IsZero() {
				t.Fatalf("list: %v %+v", err, list)
			}
		})
	}
}
