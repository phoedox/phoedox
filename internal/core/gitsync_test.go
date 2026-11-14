package core

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-billy/v5/memfs"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/storage/memory"

	"github.com/phoedox/phoedox/internal/store"
)

func TestGitSync(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, _ := setup(t)
	s.gitAllowFile = true
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Haus"})
	_ = s.SetMember(ctx, owner, p.Slug, "reader@x.de", store.RoleEditor)
	_, _ = s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "markdown", Title: "Netz", Category: "Betrieb", Markdown: "Router im Flur."})

	dir := t.TempDir()
	if _, err := git.PlainInit(dir, true); err != nil {
		t.Fatal(err)
	}
	url := "file://" + dir
	if _, err := s.SaveProjectGit(ctx, reader, p.Slug, GitInput{URL: url}); err == nil {
		t.Fatal("editor set up git sync")
	}
	if _, err := s.SaveProjectGit(ctx, owner, p.Slug, GitInput{URL: "https://user:pw@example.org/r.git"}); invalidKey(err) != "validation.git_url" {
		t.Fatalf("credentials in URL: %v", err)
	}
	if _, err := s.SaveProjectGit(ctx, owner, p.Slug, GitInput{URL: url, Folder: "../x"}); invalidKey(err) != "validation.git_folder" {
		t.Fatalf("folder: %v", err)
	}
	if _, err := s.SaveProjectGit(ctx, owner, p.Slug, GitInput{URL: url, TokenVar: "nope"}); invalidKey(err) != "validation.git_token_var" {
		t.Fatalf("token var: %v", err)
	}
	g, err := s.SaveProjectGit(ctx, owner, p.Slug, GitInput{URL: url, Branch: "main", Folder: "docs-mirror", Enabled: true})
	if err != nil || !g.Enabled {
		t.Fatalf("save: %v %+v", err, g)
	}

	g, err = s.SyncProjectGit(ctx, owner, p.Slug)
	if err != nil || g.LastError != "" || g.LastCommit == "" {
		t.Fatalf("first sync: %v %+v", err, g)
	}
	read := func(name string) string {
		r, err := git.Clone(memory.NewStorage(), memfs.New(), &git.CloneOptions{URL: url, ReferenceName: plumbing.NewBranchReferenceName("main")})
		if err != nil {
			t.Fatal(err)
		}
		wt, _ := r.Worktree()
		f, err := wt.Filesystem.Open(name)
		if err != nil {
			return ""
		}
		defer f.Close()
		b, _ := io.ReadAll(f)
		return string(b)
	}
	if got := read("docs-mirror/docs/Betrieb/netz.md"); got == "" || !contains(got, "Router im Flur.") {
		t.Fatalf("mirror: %q", got)
	}
	first := g.LastCommit

	// nothing changed: no new commit
	g, _ = s.SyncProjectGit(ctx, owner, p.Slug)
	if g.LastCommit != first || g.LastError != "" {
		t.Fatalf("second sync: %+v", g)
	}
	// a change is picked up by the scheduler after ten minutes
	_, _ = s.UpdateDocument(ctx, owner, p.Slug, "netz", DocumentPatch{Markdown: strp("Router im Keller.")})
	s.RunGitSyncs(store.System(ctx), time.Now())
	if g2, _ := s.ProjectGit(ctx, owner, p.Slug); g2.LastCommit != first {
		t.Fatal("synced again within ten minutes")
	}
	s.RunGitSyncs(store.System(ctx), time.Now().Add(11*time.Minute))
	g, _ = s.ProjectGit(ctx, owner, p.Slug)
	if g.LastCommit == first || !contains(read("docs-mirror/docs/Betrieb/netz.md"), "Keller") {
		t.Fatalf("scheduled sync: %+v", g)
	}
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }
