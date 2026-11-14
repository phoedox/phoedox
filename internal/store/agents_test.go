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
			// Jobs: unsigned -> approved -> sent -> done, delivered once.
			if err := db.AddSigningKey(ctx, SigningKey{ID: "k1", UserID: u.ID, Name: "L", PublicKey: "pk", CreatedAt: Now()}); err != nil {
				t.Fatal(err)
			}
			if err := db.CreateAgentJob(ctx, AgentJob{ID: "j1", ProjectID: p.ID, AgentID: "a1", EntitySlug: "pve", Action: "system.update", Payload: "{}",
				Status: "unsigned", CreatedBy: u.ID, CreatedAt: Now(), ExpiresAt: Time{Time: time.Now().Add(time.Hour)}}); err != nil {
				t.Fatal(err)
			}
			if got, _ := db.TakeAgentJobs(ctx, "a1", Now()); len(got) != 0 {
				t.Fatal("unsigned jobs are not delivered")
			}
			if err := db.ApproveAgentJob(ctx, "j1", "k1", "sig"); err != nil {
				t.Fatal(err)
			}
			if got, err := db.TakeAgentJobs(ctx, "a1", Now()); err != nil || len(got) != 1 || got[0].Signature != "sig" {
				t.Fatalf("take: %v %+v", err, got)
			}
			if got, _ := db.TakeAgentJobs(ctx, "a1", Now()); len(got) != 0 {
				t.Fatal("delivered once")
			}
			if err := db.FinishAgentJob(ctx, "a1", "j1", true, "ok", Now()); err != nil {
				t.Fatal(err)
			}
			if jobs, err := db.EntityJobs(ctx, p.ID, "pve", 5); err != nil || len(jobs) != 1 || !jobs[0].OK || jobs[0].Status != "done" || jobs[0].FinishedAt.IsZero() {
				t.Fatalf("jobs: %v %+v", err, jobs)
			}
			if keys, _ := db.SigningKeys(ctx, u.ID); len(keys) != 1 || db.DeleteSigningKey(ctx, u.ID, "k1") != nil {
				t.Fatal("keys")
			}

			list, err := db.ProjectAgents(ctx, p.ID)
			if err != nil || len(list) != 1 || list[0].RevokedAt.IsZero() {
				t.Fatalf("list: %v %+v", err, list)
			}
		})
	}
}
