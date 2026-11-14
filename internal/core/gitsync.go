package core

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/memfs"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/client"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/storage/memory"

	"github.com/phoedox/phoedox/internal/store"
)

// Git sync: the readable project export (docs/ as Markdown, JSON files;
// no file contents) is committed into a folder of a Git repository after
// changes, at most every ten minutes, and on request. One way only:
// Phoedox stays the source, the repository is a mirror for review,
// history and tools.

const gitSyncEvery = 10 * time.Minute

var (
	gitBranchRe = regexp.MustCompile(`^[A-Za-z0-9._/-]{1,100}$`)
	gitFolderRe = regexp.MustCompile(`^[A-Za-z0-9._-]+(/[A-Za-z0-9._-]+)*$`)
	gitOnce     sync.Once
)

// GitInput changes the Git sync of a project.
type GitInput struct {
	URL      string `json:"url"`
	Branch   string `json:"branch"`
	Folder   string `json:"folder"`
	TokenVar string `json:"tokenVar"`
	Enabled  bool   `json:"enabled"`
}

func (s *Service) gitProject(ctx context.Context, a Actor, ref string) (store.Project, error) {
	p, err := s.project(ctx, a, ref, store.RoleOwner)
	if err == nil && (a.Via == "mcp" || a.Restricted()) {
		err = ErrForbidden
	}
	return p, err
}

// ProjectGit returns the Git sync of a project (owners).
func (s *Service) ProjectGit(ctx context.Context, a Actor, ref string) (store.ProjectGit, error) {
	p, err := s.gitProject(ctx, a, ref)
	if err != nil {
		return store.ProjectGit{}, err
	}
	g, err := s.DB.GetProjectGit(ctx, p.ID)
	if errors.Is(err, store.ErrNotFound) {
		return store.ProjectGit{Branch: "main", Folder: "phoedox"}, nil
	}
	return g, err
}

// SaveProjectGit sets up or changes the Git sync (owners).
func (s *Service) SaveProjectGit(ctx context.Context, a Actor, ref string, in GitInput) (store.ProjectGit, error) {
	p, err := s.gitProject(ctx, a, ref)
	if err != nil {
		return store.ProjectGit{}, err
	}
	in.URL, in.Branch, in.Folder, in.TokenVar = strings.TrimSpace(in.URL), strings.TrimSpace(in.Branch), strings.Trim(strings.TrimSpace(in.Folder), "/"), strings.TrimSpace(in.TokenVar)
	if in.Branch == "" {
		in.Branch = "main"
	}
	if u, err := url.Parse(in.URL); err != nil || (u.Scheme != "https" && !(s.gitAllowFile && u.Scheme == "file")) || (u.Scheme == "https" && u.Host == "") || u.User != nil {
		return store.ProjectGit{}, invalid("validation.git_url", nil, "the repository needs an https:// address without credentials")
	}
	if !gitBranchRe.MatchString(in.Branch) || strings.Contains(in.Branch, "..") {
		return store.ProjectGit{}, invalid("validation.git_branch", nil, "invalid branch name")
	}
	if in.Folder != "" && (!gitFolderRe.MatchString(in.Folder) || strings.Contains(in.Folder, "..") || strings.HasPrefix(in.Folder, ".")) {
		return store.ProjectGit{}, invalid("validation.git_folder", nil, "invalid folder")
	}
	if in.TokenVar != "" {
		if _, secrets, err := s.envSecretNames(ctx, p.ID); err != nil || !secrets[in.TokenVar] {
			return store.ProjectGit{}, invalid("validation.git_token_var", map[string]any{"name": in.TokenVar}, "%s is not a secret of the project environment", in.TokenVar)
		}
	}
	g := store.ProjectGit{ProjectID: p.ID, URL: in.URL, Branch: in.Branch, Folder: in.Folder, TokenVar: in.TokenVar, Enabled: in.Enabled, UpdatedBy: a.User.ID}
	if err := s.DB.SaveProjectGit(ctx, g); err != nil {
		return g, err
	}
	s.event(ctx, a, "project.git", p.ID, map[string]any{"url": in.URL, "branch": in.Branch, "folder": in.Folder, "enabled": in.Enabled})
	return s.DB.GetProjectGit(ctx, p.ID)
}

// envSecretNames lists the variables of the project environment and which are secrets.
func (s *Service) envSecretNames(ctx context.Context, projectID string) (map[string]bool, map[string]bool, error) {
	vars, err := s.DB.Env(ctx, projectID)
	if err != nil {
		return nil, nil, err
	}
	all, secrets := map[string]bool{}, map[string]bool{}
	for _, v := range vars {
		all[v.Name] = true
		if v.Secret {
			secrets[v.Name] = true
		}
	}
	return all, secrets, nil
}

// SyncProjectGit syncs now (owners) and returns the new state.
func (s *Service) SyncProjectGit(ctx context.Context, a Actor, ref string) (store.ProjectGit, error) {
	p, err := s.gitProject(ctx, a, ref)
	if err != nil {
		return store.ProjectGit{}, err
	}
	g, err := s.DB.GetProjectGit(ctx, p.ID)
	if err != nil {
		return g, invalid("validation.git_missing", nil, "Git sync is not set up")
	}
	s.gitSync(ctx, a, p, g, time.Now())
	return s.DB.GetProjectGit(ctx, p.ID)
}

// RunGitSyncs syncs projects that changed since their last sync (all tenants).
func (s *Service) RunGitSyncs(ctx context.Context, now time.Time) {
	list, err := s.DB.EnabledProjectGits(ctx)
	if err != nil {
		slog.Warn("git sync", "err", err)
		return
	}
	for _, g := range list {
		if !g.LastSync.IsZero() && now.Sub(g.LastSync.Time) < gitSyncEvery {
			continue
		}
		tctx := store.WithTenant(ctx, g.TenantID)
		if !g.LastSync.IsZero() {
			if changed, err := s.DB.ProjectChangedSince(tctx, g.ProjectID, g.LastSync); err != nil || !changed {
				continue
			}
		}
		u, err := s.DB.UserByID(tctx, g.UpdatedBy)
		if err != nil || u.Disabled {
			continue
		}
		p, err := s.DB.ProjectFor(tctx, g.ProjectID, u.ID)
		if err != nil || p.Role != store.RoleOwner && !u.IsAdmin {
			_ = s.DB.SaveGitResult(tctx, g.ProjectID, store.Time{Time: now}, "", "the person who set up the sync is no longer an owner")
			continue
		}
		s.gitSync(tctx, Actor{User: u, Via: "web"}, p, g, now)
	}
}

func (s *Service) gitSync(ctx context.Context, a Actor, p store.Project, g store.ProjectGit, now time.Time) {
	commit, err := s.gitPush(ctx, a, p, g)
	msg := ""
	if err != nil {
		msg = err.Error()
		slog.Warn("git sync failed", "project", p.ID, "err", err)
	}
	_ = s.DB.SaveGitResult(ctx, p.ID, store.Time{Time: now}, commit, msg)
	s.event(ctx, a, "project.git_sync", p.ID, map[string]any{"commit": commit, "error": msg})
}

// gitPush writes the export into the repository folder and pushes a commit
// if anything changed. It returns the commit hash ("" = nothing to do).
func (s *Service) gitPush(ctx context.Context, a Actor, p store.Project, g store.ProjectGit) (string, error) {
	gitOnce.Do(func() {
		// All Git traffic goes through the request policy (no private
		// networks on hosted instances).
		client.InstallProtocol("https", githttp.NewClient(s.RequestPolicy.Client()))
	})
	if u, err := url.Parse(g.URL); err != nil || (u.Scheme != "https" && !(s.gitAllowFile && u.Scheme == "file")) {
		return "", errors.New("the repository needs an https:// address")
	}
	var auth transport.AuthMethod
	if g.TokenVar != "" {
		values, _, err := s.envValues(ctx, p.ID)
		if err != nil {
			return "", err
		}
		token := values[g.TokenVar]
		if token == "" {
			return "", fmt.Errorf("the secret %s is not set", g.TokenVar)
		}
		auth = &githttp.BasicAuth{Username: "phoedox", Password: token}
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	branch := plumbing.NewBranchReferenceName(g.Branch)
	fs := memfs.New()
	repo, err := git.CloneContext(ctx, memory.NewStorage(), fs, &git.CloneOptions{URL: g.URL, Auth: auth, ReferenceName: branch, SingleBranch: true, Depth: 1})
	switch {
	case errors.Is(err, transport.ErrEmptyRemoteRepository), err != nil && strings.Contains(err.Error(), "couldn't find remote ref"):
		// Empty repository or new branch: start a fresh history on it.
		fs = memfs.New()
		if repo, err = git.Init(memory.NewStorage(), fs); err != nil {
			return "", err
		}
		if _, err = repo.CreateRemote(&config.RemoteConfig{Name: "origin", URLs: []string{g.URL}}); err != nil {
			return "", err
		}
		if err = repo.Storer.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, branch)); err != nil {
			return "", err
		}
	case err != nil:
		return "", fmt.Errorf("clone: %w", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		return "", err
	}
	tree, err := s.exportTree(ctx, a, p, false)
	if err != nil {
		return "", err
	}
	if err := removeAll(fs, g.Folder); err != nil {
		return "", err
	}
	names := make([]string, 0, len(tree))
	for n := range tree {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		full := path.Join(g.Folder, n)
		if err := fs.MkdirAll(path.Dir(full), 0o755); err != nil {
			return "", err
		}
		f, err := fs.Create(full)
		if err != nil {
			return "", err
		}
		_, werr := f.Write(tree[n])
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		if werr != nil {
			return "", werr
		}
	}
	if err := wt.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		return "", err
	}
	status, err := wt.Status()
	if err != nil {
		return "", err
	}
	if status.IsClean() {
		return "", nil
	}
	hash, err := wt.Commit(fmt.Sprintf("Sync from Phoedox: %s", p.Name), &git.CommitOptions{
		Author: &object.Signature{Name: "Phoedox (" + a.User.Name + ")", Email: "noreply@phoedox.invalid", When: time.Now()},
	})
	if err != nil {
		return "", err
	}
	err = repo.PushContext(ctx, &git.PushOptions{RemoteName: "origin", Auth: auth,
		RefSpecs: []config.RefSpec{config.RefSpec(branch.String() + ":" + branch.String())}})
	if err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		return "", fmt.Errorf("push: %w", err)
	}
	return hash.String(), nil
}

// removeAll deletes the folder's content (or, for the root, everything
// except dot files like .git and .github).
func removeAll(fs billy.Filesystem, dir string) error {
	if dir == "" {
		dir = "."
	}
	entries, err := fs.ReadDir(dir)
	if err != nil {
		return nil // the folder does not exist yet
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		p := path.Join(dir, e.Name())
		if e.IsDir() {
			if err := removeAll(fs, p); err != nil {
				return err
			}
		}
		if err := fs.Remove(p); err != nil {
			return err
		}
	}
	return nil
}
