package core

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/phoedox/phoedox/internal/store"
)

func TestSnapshots(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, _ := setup(t)
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Spiel"})
	_ = s.SetMember(ctx, owner, p.Slug, reader.User.Email, store.RoleReader)
	a, _ := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "markdown", Title: "Install", Markdown: "v1"})
	_, _ = s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "markdown", Title: "Old", Markdown: "gone soon"})
	time.Sleep(20 * time.Millisecond)
	if _, err := s.CreateSnapshot(ctx, reader, p.Slug, SnapshotInput{Name: "0.1.0"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("readers must not create snapshots: %v", err)
	}
	if _, err := s.CreateSnapshot(ctx, owner, p.Slug, SnapshotInput{Name: "a/b"}); invalidKey(err) != "validation.snapshot_name" {
		t.Fatalf("name: %v", err)
	}
	if _, err := s.CreateSnapshot(ctx, owner, p.Slug, SnapshotInput{Name: "0.1.0", Note: "first"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateSnapshot(ctx, owner, p.Slug, SnapshotInput{Name: "0.1.0"}); invalidKey(err) != "validation.snapshot_exists" {
		t.Fatalf("duplicate: %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	md := "v2"
	if _, err := s.UpdateDocument(ctx, owner, p.Slug, a.Document.Slug, DocumentPatch{Markdown: &md}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DeleteDocument(ctx, owner, p.Slug, "old"); err != nil {
		t.Fatal(err)
	}
	_, _ = s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "markdown", Title: "Upgrade", Markdown: "new"})
	time.Sleep(20 * time.Millisecond)
	if _, err := s.CreateSnapshot(ctx, owner, p.Slug, SnapshotInput{Name: "0.2.0"}); err != nil {
		t.Fatal(err)
	}
	v, err := s.Snapshot(ctx, reader, p.Slug, "0.1.0")
	if err != nil || len(v.Documents) != 2 {
		t.Fatalf("view: %v %+v", err, v)
	}
	for _, d := range v.Documents {
		if d.Slug == "install" && d.Version != 1 {
			t.Fatalf("install at 0.1.0 is version 1: %+v", d)
		}
	}
	diff, err := s.CompareSnapshots(ctx, reader, p.Slug, "0.1.0", "0.2.0")
	if err != nil || len(diff.Changes) != 3 {
		t.Fatalf("diff: %v %+v", err, diff)
	}
	kinds := map[string]string{}
	for _, c := range diff.Changes {
		kinds[c.Slug] = c.Change
	}
	if kinds["install"] != "changed" || kinds["old"] != "deleted" || kinds["upgrade"] != "created" {
		t.Fatalf("changes: %v", kinds)
	}
	list, _ := s.Snapshots(ctx, reader, p.Slug)
	if len(list) != 2 || list[0].Name != "0.2.0" {
		t.Fatalf("list: %+v", list)
	}
	// Release tags become snapshots once.
	s.codeSnapshotsFor(ctx, store.CodeRepo{ProjectID: p.ID, Owner: "acme", Name: "game"}, []CodeItem{{Kind: "release", Ref: "v1.0.0", At: store.Now()}, {Kind: "release", Ref: "v1.1.0-rc1", Prerelease: true}, {Kind: "release", Ref: "0.2.0"}})
	list, _ = s.Snapshots(ctx, reader, p.Slug)
	if len(list) != 3 {
		t.Fatalf("tags: %+v", list)
	}
	if err := s.DeleteSnapshot(ctx, reader, p.Slug, "v1.0.0"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("readers must not delete: %v", err)
	}
	if err := s.DeleteSnapshot(ctx, owner, p.Slug, "v1.0.0"); err != nil {
		t.Fatal(err)
	}
}
