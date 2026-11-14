package store

import (
	"context"
	"testing"
)

func TestMergeEntities(t *testing.T) {
	for name, db := range testDBs(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			u, _ := db.CreateUser(ctx, User{ID: "u1", Email: "a@x.de", Name: "A"})
			p, _ := db.CreateProject(ctx, Project{ID: "p1", Slug: "lab", Name: "Lab", Short: "L", PrimaryColor: "#F26A1B", AIWriteMode: "suggest", OwnerID: u.ID})
			mk := func(id string) {
				if _, _, err := db.UpsertEntity(ctx, Entity{ID: id, ProjectID: p.ID, Kind: "device", Slug: id, Name: id, Attrs: JSON(`{}`), Source: "manual"}); err != nil {
					t.Fatal(err)
				}
			}
			for _, id := range []string{"keep", "drop", "room", "sw"} {
				mk(id)
			}
			link := func(id, from, rel, to string) {
				if _, err := db.LinkEntities(ctx, Edge{ID: id, From: from, Rel: rel, To: to, Attrs: JSON(`{}`), Source: "manual"}); err != nil {
					t.Fatal(err)
				}
			}
			link("e1", "keep", "located_in", "room")
			link("e2", "drop", "located_in", "room")
			link("e3", "drop", "wired_to", "sw")
			link("e4", "drop", "related_to", "keep")
			doc, _ := db.CreateDocument(ctx, Document{ID: "d1", ProjectID: p.ID, Type: DocMarkdown, Slug: "d", Title: "D", Content: JSON(`{}`), CreatedBy: u.ID}, "t")
			_ = db.SetDocumentLinks(ctx, doc.ID, []string{"keep", "drop"})
			if err := db.MergeEntities(ctx, "keep", "drop"); err != nil {
				t.Fatal(err)
			}
			edges, _ := db.EdgesOf(ctx, "keep")
			if len(edges) != 2 {
				t.Fatalf("edges: %+v", edges)
			}
			if ds, _ := db.DocumentsMentioning(ctx, "keep"); len(ds) != 1 {
				t.Fatalf("links: %+v", ds)
			}
			if _, err := db.GetEntity(ctx, p.ID, "drop"); err == nil {
				t.Fatal("drop must be in the trash")
			}
		})
	}
}
