package core

import (
	"context"
	"encoding/base64"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/phoedox/phoedox/internal/store"
)

// dbmodel blocks from a repository (AI client feedback round 3): a block
// "from owner/repo db/migrations" shows the schema the SQL migrations of a
// connected repository build, and stays current with them.

type repoModel struct {
	sql string
	exp time.Time
}

var repoModels sync.Map // project|repo|path → repoModel

type contentEntry struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Type     string `json:"type"`
	Content  string `json:"content"`
	Encoding string `json:"encoding"`
}

func (f forge) contentsPath(p string) string {
	parts := strings.Split(strings.Trim(p, "/"), "/")
	for i, x := range parts {
		parts[i] = url.PathEscape(x)
	}
	return f.repoPath("/contents/" + strings.Join(parts, "/"))
}

var downMarker = regexp.MustCompile(`(?im)^\s*--\s*(\+goose\s+down|migrate:down)\b`)

// upSQL keeps the "up" part of a migration (goose, dbmate).
func upSQL(sql string) string {
	if loc := downMarker.FindStringIndex(sql); loc != nil {
		return sql[:loc[0]]
	}
	return sql
}

// RepoDBModel returns the SQL of a repository's migrations in order
// (readers of a project with that code connection; cached 10 minutes).
func (s *Service) RepoDBModel(ctx context.Context, a Actor, ref, repo, dir string) (string, error) {
	p, err := s.project(ctx, a, ref, store.RoleDoc)
	if err != nil {
		return "", err
	}
	owner, name, _ := strings.Cut(repo, "/")
	repos, err := s.DB.CodeRepos(ctx, p.ID)
	if err != nil {
		return "", err
	}
	var r *store.CodeRepo
	for i := range repos {
		if strings.EqualFold(repos[i].Owner, owner) && strings.EqualFold(repos[i].Name, name) {
			r = &repos[i]
		}
	}
	if r == nil {
		return "", invalid("validation.repo_not_connected", map[string]any{"repo": repo}, "%s is not connected to this project", repo)
	}
	dir = strings.Trim(path.Clean("/"+dir), "/")
	key := p.ID + "|" + r.ID + "|" + dir
	if v, ok := repoModels.Load(key); ok && time.Now().Before(v.(repoModel).exp) {
		return v.(repoModel).sql, nil
	}
	f, err := s.forge(ctx, *r)
	if err != nil {
		return "", err
	}
	var entries []contentEntry
	if err := f.get(ctx, f.contentsPath(dir), &entries); err != nil {
		return "", invalid("validation.repo_path", map[string]any{"path": dir, "error": err.Error()}, "cannot list %s: %v", dir, err)
	}
	var files []contentEntry
	for _, e := range entries {
		low := strings.ToLower(e.Name)
		if e.Type == "file" && strings.HasSuffix(low, ".sql") && !strings.HasSuffix(low, ".down.sql") {
			files = append(files, e)
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	if len(files) > 400 {
		files = files[len(files)-400:]
	}
	var b strings.Builder
	for _, e := range files {
		var file contentEntry
		if err := f.get(ctx, f.contentsPath(e.Path), &file); err != nil {
			return "", invalid("validation.repo_path", map[string]any{"path": e.Path, "error": err.Error()}, "cannot read %s: %v", e.Path, err)
		}
		raw, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(file.Content, "\n", ""))
		if err != nil {
			continue
		}
		b.WriteString("-- file: " + e.Name + "\n" + upSQL(string(raw)) + "\n")
		if b.Len() > 4<<20 {
			break
		}
	}
	sql := b.String()
	repoModels.Store(key, repoModel{sql: sql, exp: time.Now().Add(10 * time.Minute)})
	return sql, nil
}
