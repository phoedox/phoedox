package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/phoedox/phoedox/internal/store"
)

// Code links (ADR-0063): a project connects GitHub or Gitea/Forgejo
// repositories; task cards link the commits, pull requests and releases
// that belong to them. A release that contains a linked commit is found
// and linked automatically. Phoedox only reads from the forge.

// CodeRepoInput connects a repository by its web address.
type CodeRepoInput struct {
	URL      string `json:"url" jsonschema:"web address of the repository, e.g. https://github.com/owner/repo or https://git.example.com/owner/repo"`
	TokenVar string `json:"tokenVar,omitempty" jsonschema:"secret of the project environment holding an access token (empty for public repositories)"`
}

// CodeLinkInput links a commit, release or pull request to a task.
type CodeLinkInput struct {
	Repo string `json:"repo" jsonschema:"repository id or owner/name"`
	Kind string `json:"kind" jsonschema:"commit, release or pull"`
	Ref  string `json:"ref" jsonschema:"commit SHA (at least 7 characters), release tag or pull request number"`
}

// CodeItem is a commit, release or pull request as the forge reports it.
type CodeItem struct {
	Kind  string     `json:"kind"`
	Ref   string     `json:"ref"`
	Title string     `json:"title"`
	URL   string     `json:"url"`
	At    store.Time `json:"at"`
	// Prerelease marks pre-releases (releases only).
	Prerelease bool `json:"prerelease,omitempty"`
}

var (
	repoPartRe = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,100}$`)
	shaRe      = regexp.MustCompile(`^[0-9a-fA-F]{7,64}$`)
	gitTagRe   = regexp.MustCompile(`^[^\s~^:?*\[\\]{1,200}$`)
)

// parseRepoURL turns a web address into provider, API base, owner and name.
func parseRepoURL(raw string) (store.CodeRepo, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" {
		return store.CodeRepo{}, invalid("validation.code_repo_url", nil, "use the https address of the repository")
	}
	parts := strings.Split(strings.Trim(strings.TrimSuffix(u.Path, ".git"), "/"), "/")
	if len(parts) < 2 {
		return store.CodeRepo{}, invalid("validation.code_repo_url", nil, "use the https address of the repository")
	}
	owner, name := parts[len(parts)-2], parts[len(parts)-1]
	if !repoPartRe.MatchString(owner) || !repoPartRe.MatchString(name) {
		return store.CodeRepo{}, invalid("validation.code_repo_url", nil, "use the https address of the repository")
	}
	prefix := strings.Join(parts[:len(parts)-2], "/")
	host := strings.ToLower(u.Host)
	r := store.CodeRepo{Owner: owner, Name: name}
	if host == "github.com" || host == "www.github.com" {
		if prefix != "" {
			return store.CodeRepo{}, invalid("validation.code_repo_url", nil, "use the https address of the repository")
		}
		r.Provider, r.WebURL, r.APIURL = "github", "https://github.com/"+owner+"/"+name, "https://api.github.com"
		return r, nil
	}
	base := "https://" + u.Host
	if prefix != "" {
		base += "/" + prefix
	}
	r.Provider, r.WebURL, r.APIURL = "gitea", base+"/"+owner+"/"+name, base+"/api/v1"
	return r, nil
}

func (s *Service) codeProject(ctx context.Context, a Actor, ref, need string) (store.Project, error) {
	p, err := s.project(ctx, a, ref, need)
	if err == nil && need == store.RoleOwner && (a.Via == "mcp" || a.Restricted()) {
		err = ErrForbidden
	}
	return p, err
}

// CodeRepos lists the repositories of a project (readers).
func (s *Service) CodeRepos(ctx context.Context, a Actor, ref string) ([]store.CodeRepo, error) {
	p, err := s.codeProject(ctx, a, ref, store.RoleReader)
	if err != nil {
		return nil, err
	}
	return s.DB.CodeRepos(ctx, p.ID)
}

// AddCodeRepo connects a repository (owners, not AI clients).
func (s *Service) AddCodeRepo(ctx context.Context, a Actor, ref string, in CodeRepoInput) (store.CodeRepo, error) {
	p, err := s.codeProject(ctx, a, ref, store.RoleOwner)
	if err != nil {
		return store.CodeRepo{}, err
	}
	r, err := parseRepoURL(in.URL)
	if err != nil {
		return r, err
	}
	if r.TokenVar = strings.TrimSpace(in.TokenVar); r.TokenVar != "" {
		if _, secrets, err := s.envSecretNames(ctx, p.ID); err != nil || !secrets[r.TokenVar] {
			return r, invalid("validation.git_token_var", map[string]any{"name": r.TokenVar}, "%s is not a secret of the project environment", r.TokenVar)
		}
	}
	r.ID, r.ProjectID, r.CreatedBy = newID(), p.ID, a.User.ID
	// The repository must answer, so typos and missing tokens show at once.
	f, err := s.forge(ctx, r)
	if err != nil {
		return r, err
	}
	if err := f.get(ctx, f.repoPath(""), nil); err != nil {
		return r, invalid("validation.code_repo_unreachable", map[string]any{"error": err.Error()}, "the repository does not answer: %v", err)
	}
	if err := s.DB.AddCodeRepo(ctx, r); errors.Is(err, store.ErrConflict) {
		return r, invalid("validation.code_repo_exists", nil, "this repository is already connected")
	} else if err != nil {
		return r, err
	}
	s.event(ctx, a, "code.repo_add", p.ID, map[string]any{"repo": r.Owner + "/" + r.Name, "provider": r.Provider})
	return s.DB.GetCodeRepo(ctx, p.ID, r.ID)
}

// DeleteCodeRepo disconnects a repository and removes its links (owners).
func (s *Service) DeleteCodeRepo(ctx context.Context, a Actor, ref, id string) error {
	p, err := s.codeProject(ctx, a, ref, store.RoleOwner)
	if err != nil {
		return err
	}
	r, err := s.DB.GetCodeRepo(ctx, p.ID, id)
	if err != nil {
		return err
	}
	if err := s.DB.DeleteCodeRepo(ctx, p.ID, id); err != nil {
		return err
	}
	s.event(ctx, a, "code.repo_remove", p.ID, map[string]any{"repo": r.Owner + "/" + r.Name})
	return nil
}

// findRepo resolves a repository by id or owner/name.
func (s *Service) findRepo(ctx context.Context, p store.Project, ref string) (store.CodeRepo, error) {
	if r, err := s.DB.GetCodeRepo(ctx, p.ID, ref); err == nil {
		return r, nil
	}
	repos, err := s.DB.CodeRepos(ctx, p.ID)
	if err != nil {
		return store.CodeRepo{}, err
	}
	for _, r := range repos {
		if strings.EqualFold(r.Owner+"/"+r.Name, strings.TrimSpace(ref)) {
			return r, nil
		}
	}
	return store.CodeRepo{}, invalid("validation.code_repo_unknown", map[string]any{"repo": ref}, "unknown repository %q", ref)
}

// CodeCommits searches recent commits of a repository (editors): a SHA
// finds that commit, other text filters the latest commit messages.
func (s *Service) CodeCommits(ctx context.Context, a Actor, ref, repo, q string) ([]CodeItem, error) {
	p, err := s.codeProject(ctx, a, ref, store.RoleEditor)
	if err != nil {
		return nil, err
	}
	r, err := s.findRepo(ctx, p, repo)
	if err != nil {
		return nil, err
	}
	f, err := s.forge(ctx, r)
	if err != nil {
		return nil, err
	}
	return f.commits(ctx, strings.TrimSpace(q))
}

// CodeReleases lists the latest releases of a repository (editors).
func (s *Service) CodeReleases(ctx context.Context, a Actor, ref, repo string) ([]CodeItem, error) {
	p, err := s.codeProject(ctx, a, ref, store.RoleEditor)
	if err != nil {
		return nil, err
	}
	r, err := s.findRepo(ctx, p, repo)
	if err != nil {
		return nil, err
	}
	f, err := s.forge(ctx, r)
	if err != nil {
		return nil, err
	}
	return f.releases(ctx)
}

// taskForCode loads a task the actor may edit.
func (s *Service) taskForCode(ctx context.Context, a Actor, ref, taskID string) (store.Project, store.Task, error) {
	p, err := s.project(ctx, a, ref, store.RoleDoc)
	if err != nil {
		return p, store.Task{}, err
	}
	t, err := s.DB.GetTask(ctx, p.ID, taskID)
	if err != nil {
		return p, t, err
	}
	_, _, err = s.boardAccess(ctx, a, ref, t.BoardID, store.RoleEditor)
	return p, t, err
}

// LinkTaskCode links a commit, release or pull request to a task; the
// title, address and date come from the forge.
func (s *Service) LinkTaskCode(ctx context.Context, a Actor, ref, taskID string, in CodeLinkInput) ([]store.TaskCodeLink, error) {
	p, t, err := s.taskForCode(ctx, a, ref, taskID)
	if err != nil {
		return nil, err
	}
	r, err := s.findRepo(ctx, p, in.Repo)
	if err != nil {
		return nil, err
	}
	in.Ref = strings.TrimSpace(in.Ref)
	f, err := s.forge(ctx, r)
	if err != nil {
		return nil, err
	}
	var item CodeItem
	switch in.Kind {
	case "commit":
		if !shaRe.MatchString(in.Ref) {
			return nil, invalid("validation.code_ref", nil, "a commit needs its SHA (at least 7 characters)")
		}
		item, err = f.commit(ctx, in.Ref)
	case "release":
		if !gitTagRe.MatchString(in.Ref) {
			return nil, invalid("validation.code_ref", nil, "a release needs its tag")
		}
		item, err = f.release(ctx, in.Ref)
	case "pull":
		if n := strings.TrimPrefix(in.Ref, "#"); n == "" || strings.Trim(n, "0123456789") != "" {
			return nil, invalid("validation.code_ref", nil, "a pull request needs its number")
		}
		item, err = f.pull(ctx, strings.TrimPrefix(in.Ref, "#"))
	default:
		return nil, invalid("validation.code_kind", nil, "kind must be commit, release or pull")
	}
	if err != nil {
		return nil, invalid("validation.code_not_found", map[string]any{"ref": in.Ref, "error": err.Error()}, "%s %s not found: %v", in.Kind, in.Ref, err)
	}
	if _, err := s.DB.AddTaskCodeLink(ctx, p.ID, store.TaskCodeLink{ID: newID(), TaskID: t.ID, RepoID: r.ID, Kind: item.Kind, Ref: item.Ref,
		Title: item.Title, URL: item.URL, At: item.At, CreatedBy: a.String()}); err != nil {
		return nil, err
	}
	s.event(ctx, a, "task.code_link", t.ID, map[string]any{"repo": r.Owner + "/" + r.Name, "kind": item.Kind, "ref": item.Ref})
	s.boardChanged(a, t.BoardID, t.ID, "task.update")
	return s.DB.TaskCodeLinks(ctx, p.ID, t.ID)
}

// UnlinkTaskCode removes a link from a task.
func (s *Service) UnlinkTaskCode(ctx context.Context, a Actor, ref, taskID, linkID string) error {
	p, t, err := s.taskForCode(ctx, a, ref, taskID)
	if err != nil {
		return err
	}
	if err := s.DB.DeleteTaskCodeLink(ctx, p.ID, t.ID, linkID); err != nil {
		return err
	}
	s.event(ctx, a, "task.code_unlink", t.ID, map[string]any{"link": linkID})
	s.boardChanged(a, t.BoardID, t.ID, "task.update")
	return nil
}

// TaskCode lists the links of one task (readers of its board).
func (s *Service) TaskCode(ctx context.Context, a Actor, ref, taskID string) ([]store.TaskCodeLink, error) {
	p, err := s.project(ctx, a, ref, store.RoleDoc)
	if err != nil {
		return nil, err
	}
	t, err := s.DB.GetTask(ctx, p.ID, taskID)
	if err != nil {
		return nil, err
	}
	if _, _, err := s.boardAccess(ctx, a, ref, t.BoardID, store.RoleReader); err != nil {
		return nil, err
	}
	return s.DB.TaskCodeLinks(ctx, p.ID, t.ID)
}

// attachCodeLinks fills the code links of listed tasks.
func (s *Service) attachCodeLinks(ctx context.Context, projectID string, tasks []store.Task) {
	if len(tasks) == 0 {
		return
	}
	links, err := s.DB.TaskCodeLinks(ctx, projectID, "")
	if err != nil || len(links) == 0 {
		return
	}
	by := map[string][]store.TaskCodeLink{}
	for _, l := range links {
		by[l.TaskID] = append(by[l.TaskID], l)
	}
	for i := range tasks {
		tasks[i].CodeLinks = by[tasks[i].ID]
	}
}

// RunCodeReleases links new releases to tasks whose commits they contain
// (all tenants; called every ten minutes, each repository at most every
// 30 minutes).
func (s *Service) RunCodeReleases(ctx context.Context, now time.Time) {
	tenants, err := s.DB.Tenants(store.System(ctx))
	if err != nil {
		return
	}
	for _, tn := range tenants {
		tctx := store.WithTenant(ctx, tn.ID)
		repos, err := s.DB.CodeRepos(tctx, "")
		if err != nil {
			continue
		}
		for _, r := range repos {
			// Pull requests with documentation drafts: every five minutes.
			if last, ok := s.prChecked.Load(r.ID); !ok || now.Sub(last.(time.Time)) >= 5*time.Minute {
				s.prChecked.Store(r.ID, now)
				s.checkCodeDrafts(tctx, r)
			}
			if !r.LastCheck.IsZero() && now.Sub(r.LastCheck.Time) < 30*time.Minute {
				continue
			}
			msg := ""
			// Each release tag names the docs' state at that time (snapshot).
			if f, err := s.forge(tctx, r); err == nil {
				if rels, err := f.releases(tctx); err == nil {
					s.codeSnapshotsFor(tctx, r, rels)
				}
			}
			if err := s.codeReleasesFor(tctx, r); err != nil {
				msg = err.Error()
				slog.Warn("code releases", "repo", r.Owner+"/"+r.Name, "err", err)
			}
			_ = s.DB.CodeRepoChecked(tctx, r.ID, msg)
		}
	}
}

func (s *Service) codeReleasesFor(ctx context.Context, r store.CodeRepo) error {
	links, err := s.DB.TaskCodeLinks(ctx, r.ProjectID, "")
	if err != nil {
		return err
	}
	released := map[string]bool{} // task ids with a release of this repo
	var commits []store.TaskCodeLink
	for _, l := range links {
		if l.RepoID != r.ID {
			continue
		}
		if l.Kind == "release" {
			released[l.TaskID] = true
		} else if l.Kind == "commit" {
			commits = append(commits, l)
		}
	}
	var pending []store.TaskCodeLink
	for _, c := range commits {
		if !released[c.TaskID] {
			pending = append(pending, c)
		}
	}
	if len(pending) == 0 {
		return nil
	}
	f, err := s.forge(ctx, r)
	if err != nil {
		return err
	}
	rels, err := f.releases(ctx)
	if err != nil {
		return err
	}
	// Oldest first: the first release that contains the commit.
	sort.Slice(rels, func(i, j int) bool { return rels[i].At.Before(rels[j].At.Time) })
	done := map[string]bool{}
	for _, c := range pending {
		if done[c.TaskID] {
			continue
		}
		for _, rel := range rels {
			if rel.Prerelease || (!c.At.IsZero() && rel.At.Before(c.At.Time)) {
				continue
			}
			ok, err := f.contains(ctx, rel.Ref, c.Ref, c.At)
			if err != nil {
				return err
			}
			if !ok {
				continue
			}
			added, err := s.DB.AddTaskCodeLink(ctx, r.ProjectID, store.TaskCodeLink{ID: newID(), TaskID: c.TaskID, RepoID: r.ID, Kind: "release",
				Ref: rel.Ref, Title: rel.Title, URL: rel.URL, At: rel.At, Auto: true, CreatedBy: "system:code"})
			if err != nil {
				return err
			}
			if added {
				s.event(ctx, Actor{Via: "system"}, "task.code_link", c.TaskID, map[string]any{"repo": r.Owner + "/" + r.Name, "kind": "release", "ref": rel.Ref, "auto": true})
			}
			done[c.TaskID] = true
			break
		}
	}
	return nil
}

// forge is a read-only client for the API of GitHub or Gitea/Forgejo.
type forge struct {
	repo   store.CodeRepo
	token  string
	client *http.Client
}

func (s *Service) forge(ctx context.Context, r store.CodeRepo) (forge, error) {
	f := forge{repo: r, client: s.RequestPolicy.Client()}
	if r.TokenVar != "" {
		values, _, err := s.envValues(ctx, r.ProjectID)
		if err != nil {
			return f, err
		}
		if f.token = values[r.TokenVar]; f.token == "" {
			return f, invalid("validation.git_token_var", map[string]any{"name": r.TokenVar}, "the secret %s is not set", r.TokenVar)
		}
	}
	return f, nil
}

func (f forge) repoPath(rest string) string {
	return "/repos/" + url.PathEscape(f.repo.Owner) + "/" + url.PathEscape(f.repo.Name) + rest
}

// get reads one API path into out (nil discards the body).
func (f forge) get(ctx context.Context, path string, out any) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.repo.APIURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Phoedox")
	if f.repo.Provider == "github" {
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		if f.token != "" {
			req.Header.Set("Authorization", "Bearer "+f.token)
		}
	} else if f.token != "" {
		req.Header.Set("Authorization", "token "+f.token)
	}
	res, err := f.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return err
	}
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", res.StatusCode)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(body, out)
}

type apiCommit struct {
	SHA     string `json:"sha"`
	HTMLURL string `json:"html_url"`
	Commit  struct {
		Message   string `json:"message"`
		Committer struct {
			Date time.Time `json:"date"`
		} `json:"committer"`
	} `json:"commit"`
}

func (c apiCommit) item() CodeItem {
	title, _, _ := strings.Cut(c.Commit.Message, "\n")
	return CodeItem{Kind: "commit", Ref: c.SHA, Title: strings.TrimSpace(title), URL: c.HTMLURL, At: store.Time{Time: c.Commit.Committer.Date.UTC()}}
}

type apiRelease struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	HTMLURL     string    `json:"html_url"`
	Draft       bool      `json:"draft"`
	Prerelease  bool      `json:"prerelease"`
	PublishedAt time.Time `json:"published_at"`
	CreatedAt   time.Time `json:"created_at"`
}

func (r apiRelease) item() CodeItem {
	at := r.PublishedAt
	if at.IsZero() {
		at = r.CreatedAt
	}
	title := r.Name
	if title == "" {
		title = r.TagName
	}
	return CodeItem{Kind: "release", Ref: r.TagName, Title: title, URL: r.HTMLURL, At: store.Time{Time: at.UTC()}, Prerelease: r.Prerelease}
}

func (f forge) commit(ctx context.Context, sha string) (CodeItem, error) {
	var c apiCommit
	path := f.repoPath("/commits/" + url.PathEscape(sha))
	if f.repo.Provider == "gitea" {
		path = f.repoPath("/git/commits/" + url.PathEscape(sha))
	}
	if err := f.get(ctx, path, &c); err != nil {
		return CodeItem{}, err
	}
	return c.item(), nil
}

func (f forge) commits(ctx context.Context, q string) ([]CodeItem, error) {
	if shaRe.MatchString(q) {
		if c, err := f.commit(ctx, q); err == nil {
			return []CodeItem{c}, nil
		}
	}
	var list []apiCommit
	path := f.repoPath("/commits?per_page=50")
	if f.repo.Provider == "gitea" {
		path = f.repoPath("/commits?limit=50&stat=false&verification=false&files=false")
	}
	if err := f.get(ctx, path, &list); err != nil {
		return nil, err
	}
	out := []CodeItem{}
	for _, c := range list {
		if q == "" || strings.Contains(strings.ToLower(c.Commit.Message), strings.ToLower(q)) || strings.HasPrefix(c.SHA, strings.ToLower(q)) {
			out = append(out, c.item())
		}
		if len(out) == 20 {
			break
		}
	}
	return out, nil
}

func (f forge) releases(ctx context.Context) ([]CodeItem, error) {
	var list []apiRelease
	path := f.repoPath("/releases?per_page=30")
	if f.repo.Provider == "gitea" {
		path = f.repoPath("/releases?limit=30&draft=false")
	}
	if err := f.get(ctx, path, &list); err != nil {
		return nil, err
	}
	out := []CodeItem{}
	for _, r := range list {
		if !r.Draft {
			out = append(out, r.item())
		}
	}
	return out, nil
}

func (f forge) release(ctx context.Context, tag string) (CodeItem, error) {
	var r apiRelease
	if err := f.get(ctx, f.repoPath("/releases/tags/"+url.PathEscape(tag)), &r); err != nil {
		return CodeItem{}, err
	}
	return r.item(), nil
}

func (f forge) pull(ctx context.Context, n string) (CodeItem, error) {
	var p struct {
		Number    int       `json:"number"`
		Title     string    `json:"title"`
		HTMLURL   string    `json:"html_url"`
		MergedAt  time.Time `json:"merged_at"`
		CreatedAt time.Time `json:"created_at"`
	}
	if err := f.get(ctx, f.repoPath("/pulls/"+url.PathEscape(n)), &p); err != nil {
		return CodeItem{}, err
	}
	at := p.MergedAt
	if at.IsZero() {
		at = p.CreatedAt
	}
	return CodeItem{Kind: "pull", Ref: fmt.Sprint(p.Number), Title: p.Title, URL: p.HTMLURL, At: store.Time{Time: at.UTC()}}, nil
}

// contains reports whether the release tag includes the commit. GitHub
// compares the two; Gitea's compare does not say whether the commit is an
// ancestor, so there a release published after the commit counts.
func (f forge) contains(ctx context.Context, tag, sha string, at store.Time) (bool, error) {
	if f.repo.Provider != "github" {
		return true, nil // releases before the commit were skipped by the caller
	}
	var c struct {
		Status string `json:"status"`
	}
	if err := f.get(ctx, f.repoPath("/compare/"+url.PathEscape(sha)+"..."+url.PathEscape(tag)), &c); err != nil {
		return false, err
	}
	return c.Status == "ahead" || c.Status == "identical", nil
}
