package store

import (
	"context"
	"testing"
	"time"
)

func TestCodeLinks(t *testing.T) {
	for name, d := range testDBs(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			u, _ := d.CreateUser(ctx, User{ID: id(), Email: "c@x.de", Name: "C"})
			p, _ := d.CreateProject(ctx, Project{ID: id(), Slug: "code", Name: "Code", OwnerID: u.ID, CreatedAt: Now()})
			board, err := d.CreateDocument(ctx, Document{ID: id(), ProjectID: p.ID, Type: DocBoard, Slug: "b", Title: "B", Content: JSON(`{"columns":[{"id":"todo","name":"To do"}]}`), CreatedBy: u.ID}, "t")
			if err != nil {
				t.Fatal(err)
			}
			task := Task{ID: id(), ProjectID: p.ID, BoardID: board.ID, Column: "todo", Title: "T", Labels: []string{}, CreatedBy: "user:" + u.ID, CreatedAt: Now(), UpdatedAt: Now()}
			if err := d.SaveTask(ctx, task); err != nil {
				t.Fatal(err)
			}
			r := CodeRepo{ID: id(), ProjectID: p.ID, Provider: "github", WebURL: "https://github.com/a/b", APIURL: "https://api.github.com", Owner: "a", Name: "b", CreatedBy: u.ID}
			if err := d.AddCodeRepo(ctx, r); err != nil {
				t.Fatal(err)
			}
			r2 := r
			r2.ID = id()
			if err := d.AddCodeRepo(ctx, r2); err != ErrConflict {
				t.Fatalf("duplicate repo: %v", err)
			}
			got, err := d.GetCodeRepo(ctx, p.ID, r.ID)
			if err != nil || !got.LastCheck.IsZero() || got.Owner != "a" {
				t.Fatalf("get: %+v %v", got, err)
			}
			if err := d.CodeRepoChecked(ctx, r.ID, "HTTP 401"); err != nil {
				t.Fatal(err)
			}
			if got, _ = d.GetCodeRepo(ctx, p.ID, r.ID); got.LastCheck.IsZero() || got.LastError != "HTTP 401" {
				t.Fatalf("checked: %+v", got)
			}
			l := TaskCodeLink{ID: id(), TaskID: task.ID, RepoID: r.ID, Kind: "commit", Ref: "abc1234", Title: "Fix", URL: "https://x", At: Time{time.Now().UTC().Truncate(time.Millisecond)}, CreatedBy: "user:" + u.ID}
			if ok, err := d.AddTaskCodeLink(ctx, p.ID, l); !ok || err != nil {
				t.Fatalf("add: %v %v", ok, err)
			}
			l.ID = id()
			if ok, _ := d.AddTaskCodeLink(ctx, p.ID, l); ok {
				t.Fatal("duplicate link added")
			}
			rel := TaskCodeLink{ID: id(), TaskID: task.ID, RepoID: r.ID, Kind: "release", Ref: "v1", Auto: true, CreatedBy: "system:code"}
			if ok, err := d.AddTaskCodeLink(ctx, p.ID, rel); !ok || err != nil {
				t.Fatalf("release: %v %v", ok, err)
			}
			links, err := d.TaskCodeLinks(ctx, p.ID, task.ID)
			if err != nil || len(links) != 2 || links[0].Repo != "a/b" || links[1].Kind != "release" || !links[1].Auto || !links[1].At.IsZero() {
				t.Fatalf("links: %+v %v", links, err)
			}
			if err := d.DeleteTaskCodeLink(ctx, p.ID, task.ID, links[0].ID); err != nil {
				t.Fatal(err)
			}
			if err := d.DeleteCodeRepo(ctx, p.ID, r.ID); err != nil {
				t.Fatal(err)
			}
			if links, _ = d.TaskCodeLinks(ctx, p.ID, ""); len(links) != 0 {
				t.Fatalf("links after delete: %+v", links)
			}
		})
	}
}
