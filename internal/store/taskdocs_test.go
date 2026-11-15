package store

import (
	"context"
	"testing"
)

func TestTaskDocs(t *testing.T) {
	for name, d := range testDBs(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			u, _ := d.CreateUser(ctx, User{ID: id(), Email: "d@x.de", Name: "D"})
			p, _ := d.CreateProject(ctx, Project{ID: id(), Slug: "docs", Name: "Docs", OwnerID: u.ID, CreatedAt: Now()})
			mk := func(typ, slug string) Document {
				doc, err := d.CreateDocument(ctx, Document{ID: id(), ProjectID: p.ID, Type: typ, Slug: slug, Title: slug, Content: JSON(`{"columns":[{"id":"todo","name":"To do"}]}`), CreatedBy: u.ID}, "t")
				if err != nil {
					t.Fatal(err)
				}
				return doc
			}
			board, page, adr := mk(DocBoard, "b"), mk(DocMarkdown, "page"), mk(DocADR, "adr")
			task := Task{ID: id(), ProjectID: p.ID, BoardID: board.ID, Column: "todo", Title: "T", Labels: []string{}, CreatedBy: "user:" + u.ID, CreatedAt: Now(), UpdatedAt: Now()}
			if err := d.SaveTask(ctx, task); err != nil {
				t.Fatal(err)
			}
			if err := d.SetTaskDocs(ctx, p.ID, task.ID, []string{adr.ID, page.ID}, "user:"+u.ID); err != nil {
				t.Fatal(err)
			}
			docs, err := d.TaskDocs(ctx, p.ID, task.ID)
			if err != nil || len(docs) != 2 || docs[0].ID != adr.ID || docs[0].Type != DocADR || docs[1].Slug != "page" {
				t.Fatalf("docs: %+v %v", docs, err)
			}
			if err := d.SetTaskDocs(ctx, p.ID, task.ID, []string{page.ID}, "user:"+u.ID); err != nil {
				t.Fatal(err)
			}
			cards, err := d.TasksOfDocument(ctx, p.ID, page.ID)
			if err != nil || len(cards) != 1 || cards[0].ID != task.ID || cards[0].BoardSlug != "b" {
				t.Fatalf("cards: %+v %v", cards, err)
			}
			if cards, _ = d.TasksOfDocument(ctx, p.ID, adr.ID); len(cards) != 0 {
				t.Fatalf("replaced link still there: %+v", cards)
			}
		})
	}
}
