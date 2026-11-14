package store

import (
	"context"
	"testing"
)

func TestEvents(t *testing.T) {
	for name, db := range testDBs(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			u, _ := db.CreateUser(ctx, User{ID: "u1", Email: "a@x.de", Name: "A"})
			p, err := db.CreateProject(ctx, Project{ID: "p1", Slug: "lab", Name: "Lab", Short: "L", PrimaryColor: "#F26A1B", AIWriteMode: "suggest", OwnerID: u.ID})
			if err != nil {
				t.Fatal(err)
			}
			if got := db.ProjectOfSubject(ctx, p.ID); got != p.ID {
				t.Fatalf("project subject: %q", got)
			}
			_ = db.AddProjectEvent(ctx, "user:u1", "document.create", "d1", p.ID, map[string]string{"a": "b"})
			_ = db.AddProjectEvent(ctx, "mcp:u1:tok_1", "document.update", "d1", p.ID, nil)
			_ = db.AddProjectEvent(ctx, "user:u1", "auth.login", "u1", "", nil)
			if ev, err := db.Events(ctx, EventFilter{ProjectID: p.ID}); err != nil || len(ev) != 2 || ev[0].Kind != "document.update" {
				t.Fatalf("project: %v %+v", err, ev)
			}
			if ev, _ := db.Events(ctx, EventFilter{Actor: "mcp:"}); len(ev) != 1 {
				t.Fatalf("actor prefix: %+v", ev)
			}
			if ev, _ := db.Events(ctx, EventFilter{Kind: "document_"}); len(ev) != 0 {
				t.Fatalf("_ is no wildcard: %+v", ev)
			}
			if ev, _ := db.Events(ctx, EventFilter{Kind: "auth."}); len(ev) != 1 || ev[0].ProjectID != "" {
				t.Fatalf("kind prefix: %+v", ev)
			}
		})
	}
}
