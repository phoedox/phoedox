package core

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/phoedox/phoedox/internal/store"
)

// Git export: the project as a Git repository with its history, one commit
// per document version (author and time of the version), then a last
// commit with the full export (JSON, translations, files). Sent as a ZIP of
// the working tree with its .git folder.

// maxGitCommits keeps very old, very busy projects from building forever.
const maxGitCommits = 20000

type gitStep struct {
	at      time.Time
	doc     store.Document
	version store.Version
}

// ExportProjectGit writes the project as a ZIP of a Git repository with
// history (owners; read-only tokens work).
func (s *Service) ExportProjectGit(ctx context.Context, a Actor, ref string, w io.Writer) error {
	p, err := s.project(ctx, a, ref, store.RoleReader)
	if err != nil {
		return err
	}
	if a.Via == "mcp" || p.Role != store.RoleOwner {
		return ErrForbidden
	}
	dir, err := os.MkdirTemp(s.TempDir, "phx-git-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	if err := s.buildGitExport(ctx, a, p, dir); err != nil {
		return err
	}
	if err := zipDir(dir, p.Slug, w); err != nil {
		return err
	}
	s.event(ctx, a, "project.export", p.ID, map[string]any{"format": "git"})
	return nil
}

func (s *Service) buildGitExport(ctx context.Context, a Actor, p store.Project, dir string) error {
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		return err
	}
	wt, err := repo.Worktree()
	if err != nil {
		return err
	}
	docs, err := s.DB.ListDocuments(ctx, p.ID, store.DocFilter{})
	if err != nil {
		return err
	}
	var steps []gitStep
	for _, d := range docs {
		vs, err := s.DB.ListVersions(ctx, d.ID)
		if err != nil {
			return err
		}
		for _, v := range vs {
			steps = append(steps, gitStep{at: v.CreatedAt.Time, doc: d, version: v})
		}
	}
	sort.SliceStable(steps, func(i, j int) bool { return steps[i].at.Before(steps[j].at) })
	if len(steps) > maxGitCommits {
		steps = steps[len(steps)-maxGitCommits:]
	}
	// Names of the people behind the versions.
	ids := []string{}
	for _, st := range steps {
		if id := store.ActorUserID(st.version.Actor); id != "" {
			ids = append(ids, id)
		}
	}
	names, _ := s.DB.UserNames(ctx, ids)
	write := func(name string, data []byte) error {
		full := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}
		return os.WriteFile(full, data, 0o644)
	}
	for _, st := range steps {
		if err := ctx.Err(); err != nil {
			return err
		}
		v, err := s.DB.GetVersion(ctx, st.doc.ID, st.version.Version)
		if err != nil {
			continue
		}
		page := store.Document{Type: st.doc.Type, Title: v.Title, Content: v.Content, Number: st.doc.Number}
		name := mirrorPath(st.doc, "")
		if err := write(name, []byte(mirrorMarkdown(page))); err != nil {
			return err
		}
		if _, err := wt.Add(name); err != nil {
			return err
		}
		who := names[store.ActorUserID(v.Actor)]
		switch {
		case who != "":
		case strings.HasPrefix(v.Actor, "mcp:"):
			who = "AI client"
		default:
			who = "Phoedox"
		}
		msg := fmt.Sprintf("%s: version %d", v.Title, v.Version)
		if v.Version == 1 {
			msg = fmt.Sprintf("Create %s", v.Title)
		}
		sig := &object.Signature{Name: who, Email: "noreply@phoedox.invalid", When: v.CreatedAt.Time}
		if _, err := wt.Commit(msg, &git.CommitOptions{Author: sig, Committer: sig, AllowEmptyCommits: true}); err != nil {
			return err
		}
	}
	// The full export as the last commit: what an import needs.
	tree, err := s.exportTree(ctx, a, p, true)
	if err != nil {
		return err
	}
	tree["README.md"] = []byte(fmt.Sprintf("# %s\n\nExported from Phoedox on %s. `docs/` holds the pages and decisions as Markdown with their history; the JSON files and `files/` are the full export and can be imported into Phoedox again.\n",
		p.Name, time.Now().UTC().Format("2006-01-02")))
	for name, data := range tree {
		if err := write(name, data); err != nil {
			return err
		}
	}
	if err := wt.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		return err
	}
	now := &object.Signature{Name: "Phoedox", Email: "noreply@phoedox.invalid", When: time.Now()}
	_, err = wt.Commit("Phoedox export: "+p.Name, &git.CommitOptions{Author: now, Committer: now, AllowEmptyCommits: true})
	return err
}

// zipDir writes a directory (with hidden folders such as .git) as a ZIP
// under one top folder.
func zipDir(dir, top string, w io.Writer) error {
	z := zip.NewWriter(w)
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		f, err := z.Create(path.Join(top, filepath.ToSlash(rel)))
		if err != nil {
			return err
		}
		src, err := os.Open(p)
		if err != nil {
			return err
		}
		defer src.Close()
		_, err = io.Copy(f, src)
		return err
	})
	if err != nil {
		return err
	}
	return z.Close()
}

// --- links for AI clients ---------------------------------------------------

type exportClaims struct {
	Project string `json:"p"`
	User    string `json:"u"`
	Format  string `json:"f"` // zip | git
	Exp     int64  `json:"e"`
}

// ExportLink is a short-lived download address of a project export, so AI
// clients (which cannot receive files) can hand it to the person.
type ExportLink struct {
	URL     string    `json:"url"`
	Format  string    `json:"format"`
	Expires time.Time `json:"expires"`
}

// ProjectExportLink makes a download link valid for 15 minutes (owners;
// also over MCP).
func (s *Service) ProjectExportLink(ctx context.Context, a Actor, ref, format string) (ExportLink, error) {
	p, err := s.project(ctx, a, ref, store.RoleReader)
	if err != nil {
		return ExportLink{}, err
	}
	if p.Role != store.RoleOwner {
		return ExportLink{}, ErrForbidden
	}
	if format != "git" {
		format = "zip"
	}
	exp := time.Now().Add(15 * time.Minute)
	raw, _ := json.Marshal(exportClaims{Project: p.ID, User: a.User.ID, Format: format, Exp: exp.Unix()})
	tok, err := s.Box.Seal(raw, "export-link")
	if err != nil {
		return ExportLink{}, err
	}
	s.event(ctx, a, "project.export_link", p.ID, map[string]any{"format": format})
	return ExportLink{URL: s.PublicURL + "/api/v1/exports/" + tok, Format: format, Expires: exp}, nil
}

// OpenExportLink writes the export a link stands for; it returns the
// project slug and format for the file name.
func (s *Service) OpenExportLink(ctx context.Context, tok string, w io.Writer) (string, string, error) {
	raw, err := s.Box.Open(tok, "export-link")
	if err != nil {
		return "", "", ErrNotFound
	}
	var c exportClaims
	if json.Unmarshal(raw, &c) != nil || time.Now().Unix() > c.Exp {
		return "", "", ErrNotFound
	}
	u, err := s.DB.UserByID(ctx, c.User)
	if err != nil {
		return "", "", ErrNotFound
	}
	a := Actor{User: u, Via: "link"}
	p, err := s.project(ctx, a, c.Project, store.RoleReader)
	if err != nil {
		return "", "", err
	}
	if c.Format == "git" {
		return p.Slug, c.Format, s.ExportProjectGit(ctx, a, p.ID, w)
	}
	return p.Slug, c.Format, s.ExportProject(ctx, a, p.ID, w)
}
