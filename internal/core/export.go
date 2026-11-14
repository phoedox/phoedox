package core

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/phoedox/phoedox/internal/graph"
	"github.com/phoedox/phoedox/internal/store"
)

// Project export (format "phoedox-project", version 1): a ZIP with the
// project's settings, pages (with translations), entries, relations,
// kinds, boards with their cards and files. The JSON files are lossless
// and used for imports; docs/ is a readable Markdown mirror (for Git).
// Not included: members, sharing, comments, checks, versions, plugins.

const exportFormat = "phoedox-project"

type exportManifest struct {
	Format     string        `json:"format"`
	Version    int           `json:"version"`
	ExportedAt *time.Time    `json:"exportedAt,omitempty"` // not in Git mirrors (would change every sync)
	Project    exportProject `json:"project"`
}

type exportProject struct {
	Name         string   `json:"name"`
	Slug         string   `json:"slug"`
	Short        string   `json:"short,omitempty"`
	Description  string   `json:"description,omitempty"`
	PrimaryColor string   `json:"primaryColor,omitempty"`
	AIWriteMode  string   `json:"aiWriteMode,omitempty"`
	MainLocale   string   `json:"mainLocale,omitempty"`
	Locales      []string `json:"locales,omitempty"`
	Features     []string `json:"features,omitempty"`
}

type exportDoc struct {
	ID           string              `json:"id"`
	Type         string              `json:"type"`
	Slug         string              `json:"slug"`
	Title        string              `json:"title"`
	Category     string              `json:"category,omitempty"`
	Position     int                 `json:"position,omitempty"`
	Number       int                 `json:"number,omitempty"`
	Parent       string              `json:"parent,omitempty"` // slug
	Content      json.RawMessage     `json:"content"`
	Translations []exportTranslation `json:"translations,omitempty"`
}

type exportTranslation struct {
	Locale  string          `json:"locale"`
	Title   string          `json:"title"`
	Content json.RawMessage `json:"content"`
	Machine bool            `json:"machine,omitempty"`
}

type exportEntity struct {
	Kind  string          `json:"kind"`
	Slug  string          `json:"slug"`
	Name  string          `json:"name"`
	Attrs json.RawMessage `json:"attrs,omitempty"`
}

type exportEdge struct {
	From  string          `json:"from"`
	Rel   string          `json:"rel"`
	To    string          `json:"to"`
	Attrs json.RawMessage `json:"attrs,omitempty"`
}

type exportTask struct {
	Board       string   `json:"board"` // slug
	Column      string   `json:"column"`
	Position    float64  `json:"position"`
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Assignee    string   `json:"assignee,omitempty"` // e-mail of a member or free text
	Due         string   `json:"due,omitempty"`
	Priority    int      `json:"priority,omitempty"`
	Labels      []string `json:"labels,omitempty"`
}

type exportFile struct {
	Path     string `json:"path"`
	Name     string `json:"name"`
	Document string `json:"document,omitempty"` // slug
	Entity   string `json:"entity,omitempty"`   // slug
}

type exportKind struct {
	Kind string          `json:"kind"`
	Def  json.RawMessage `json:"def"`
}

// ExportProject writes the project as a ZIP (owners only).
func (s *Service) ExportProject(ctx context.Context, a Actor, ref string, w io.Writer) error {
	// Owners only, but a read-only token is enough (moving to another instance).
	p, err := s.project(ctx, a, ref, store.RoleReader)
	if err != nil {
		return err
	}
	if a.Via == "mcp" || p.Role != store.RoleOwner {
		return ErrForbidden
	}
	tree, err := s.exportTree(ctx, a, p, true)
	if err != nil {
		return err
	}
	z := zip.NewWriter(w)
	names := make([]string, 0, len(tree))
	for n := range tree {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		f, err := z.Create(n)
		if err != nil {
			return err
		}
		if _, err := f.Write(tree[n]); err != nil {
			return err
		}
	}
	s.event(ctx, a, "project.export", p.ID, map[string]any{"files": len(tree)})
	return z.Close()
}

// exportTree builds all files of an export (path → content).
func (s *Service) exportTree(ctx context.Context, a Actor, p store.Project, withFiles bool) (map[string][]byte, error) {
	var stamp *time.Time
	if withFiles { // full exports carry their time; Git mirrors stay stable
		now := time.Now().UTC().Truncate(time.Second)
		stamp = &now
	}
	tree := map[string][]byte{}
	put := func(name string, v any) {
		b, _ := json.MarshalIndent(v, "", "  ")
		tree[name] = append(b, '\n')
	}
	put("phoedox.json", exportManifest{Format: exportFormat, Version: 1, ExportedAt: stamp, Project: exportProject{
		Name: p.Name, Slug: p.Slug, Short: p.Short, Description: p.Description, PrimaryColor: p.PrimaryColor, AIWriteMode: p.AIWriteMode,
		MainLocale: p.MainLocale, Locales: p.Locales, Features: p.Features}})

	docs, err := s.DB.ListDocuments(ctx, p.ID, store.DocFilter{WithContent: true})
	if err != nil {
		return nil, err
	}
	slugOf := map[string]string{}
	for _, d := range docs {
		slugOf[d.ID] = d.Slug
	}
	out := make([]exportDoc, 0, len(docs))
	for _, d := range docs {
		ed := exportDoc{ID: d.ID, Type: d.Type, Slug: d.Slug, Title: d.Title, Category: d.Category, Position: d.Position, Number: d.Number,
			Parent: slugOf[d.ParentID], Content: json.RawMessage(d.Content)}
		list, _ := s.DB.Translations(ctx, d.ID)
		for _, t := range list {
			full, err := s.DB.GetTranslation(ctx, d.ID, t.Locale)
			if err != nil {
				continue
			}
			ed.Translations = append(ed.Translations, exportTranslation{Locale: t.Locale, Title: full.Title, Content: json.RawMessage(full.Content), Machine: full.Machine})
			tree[mirrorPath(d, t.Locale)] = []byte(mirrorMarkdown(store.Document{Type: d.Type, Title: full.Title, Content: full.Content, Number: d.Number}))
		}
		out = append(out, ed)
		tree[mirrorPath(d, "")] = []byte(mirrorMarkdown(d))
	}
	put("documents.json", out)

	g, err := s.ProjectGraph(ctx, a, p.Slug)
	if err != nil {
		return nil, err
	}
	entSlug := map[string]string{}
	ents := make([]exportEntity, 0, len(g.Entities))
	for _, e := range g.Entities {
		entSlug[e.ID] = e.Slug
		ents = append(ents, exportEntity{Kind: e.Kind, Slug: e.Slug, Name: e.Name, Attrs: json.RawMessage(e.Attrs)})
	}
	put("entities.json", ents)
	edges := make([]exportEdge, 0, len(g.Edges))
	for _, e := range g.Edges {
		from, to := entSlug[e.From], entSlug[e.To]
		if from == "" || to == "" {
			continue
		}
		edges = append(edges, exportEdge{From: from, Rel: e.Rel, To: to, Attrs: json.RawMessage(e.Attrs)})
	}
	put("edges.json", edges)

	kinds, err := s.DB.ProjectKinds(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	ks := make([]exportKind, 0, len(kinds))
	for _, k := range kinds {
		ks = append(ks, exportKind{Kind: k.Kind, Def: json.RawMessage(k.Def)})
	}
	put("kinds.json", ks)

	tasks, err := s.DB.ListTasks(ctx, p.ID, store.TaskFilter{})
	if err != nil {
		return nil, err
	}
	members, _ := s.DB.ListMembers(ctx, p.ID)
	email := map[string]string{}
	for _, m := range members {
		email[m.UserID] = m.Email
	}
	ts := make([]exportTask, 0, len(tasks))
	for _, t := range tasks {
		as := t.AssigneeText
		if t.AssigneeID != "" {
			if as = email[t.AssigneeID]; as == "" {
				as = t.AssigneeName
			}
		}
		ts = append(ts, exportTask{Board: slugOf[t.BoardID], Column: t.Column, Position: t.Position, Title: t.Title, Description: t.Description,
			Assignee: as, Due: t.Due, Priority: t.Priority, Labels: t.Labels})
	}
	put("tasks.json", ts)

	files, err := s.Files(ctx, a, p.Slug, FileFilter{})
	if err != nil {
		return nil, err
	}
	fs := make([]exportFile, 0, len(files))
	for _, f := range files {
		ef := exportFile{Path: "files/" + f.ID + "/" + safeName(f.Name), Name: f.Name, Document: slugOf[f.DocumentID], Entity: entSlug[f.EntityID]}
		if withFiles {
			r, err := s.Blobs.Open(ctx, f.SHA256)
			if err != nil {
				continue
			}
			b, err := io.ReadAll(r)
			r.Close()
			if err != nil {
				return nil, err
			}
			tree[ef.Path] = b
		}
		fs = append(fs, ef)
	}
	put("files.json", fs)
	return tree, nil
}

var unsafeChars = regexp.MustCompile(`[^\p{L}\p{N}._ -]+`)

func safeName(n string) string {
	n = strings.TrimSpace(unsafeChars.ReplaceAllString(n, "_"))
	if n == "" || n == "." || n == ".." {
		return "file"
	}
	return n
}

// mirrorPath is where a page lives in the readable mirror.
func mirrorPath(d store.Document, locale string) string {
	dir := "docs"
	for _, part := range strings.Split(d.Category, "/") {
		if part = strings.TrimSpace(part); part != "" {
			dir = path.Join(dir, safeName(part))
		}
	}
	name := d.Slug
	if locale != "" {
		name += "." + locale
	}
	return path.Join(dir, name+".md")
}

// mirrorMarkdown renders a page as readable Markdown with a short header.
func mirrorMarkdown(d store.Document) string {
	var b strings.Builder
	fmt.Fprintf(&b, "---\ntitle: %q\ntype: %s\n", d.Title, d.Type)
	switch d.Type {
	case store.DocMarkdown:
		var c struct {
			Markdown string `json:"markdown"`
		}
		_ = json.Unmarshal(d.Content, &c)
		b.WriteString("---\n\n" + c.Markdown)
	case store.DocADR:
		var c ADR
		_ = json.Unmarshal(d.Content, &c)
		fmt.Fprintf(&b, "number: %s\nstatus: %s\ndate: %s\n---\n\n# %s %s\n", DecisionRef(d.Number), c.Status, c.Date, DecisionRef(d.Number), d.Title)
		for _, part := range [][2]string{{"Context", c.Context}, {"Decision", c.Decision}, {"Consequences", c.Consequences}, {"Alternatives", c.Alternatives}} {
			if strings.TrimSpace(part[1]) != "" {
				fmt.Fprintf(&b, "\n## %s\n\n%s\n", part[0], strings.TrimSpace(part[1]))
			}
		}
	case store.DocLink:
		var c Link
		_ = json.Unmarshal(d.Content, &c)
		fmt.Fprintf(&b, "url: %s\n---\n\n%s\n", c.URL, c.Description)
	default:
		var c struct {
			Description string `json:"description"`
		}
		_ = json.Unmarshal(d.Content, &c)
		b.WriteString("---\n\n" + c.Description + "\n\n_(" + d.Type + ": see documents.json)_\n")
	}
	return b.String()
}

// ImportResultProject tells what an import created.
type ImportResultProject struct {
	Project  store.Project `json:"project"`
	Pages    int           `json:"pages"`
	Entries  int           `json:"entries"`
	Tasks    int           `json:"tasks"`
	Files    int           `json:"files"`
	Problems []string      `json:"problems"`
}

const maxImportBytes = 2 << 30

// ImportProject creates a new project from an export ZIP.
func (s *Service) ImportProject(ctx context.Context, a Actor, data []byte) (ImportResultProject, error) {
	res := ImportResultProject{Problems: []string{}}
	if a.Via == "mcp" {
		return res, ErrForbidden
	}
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return res, invalid("validation.import_zip", nil, "not a ZIP file")
	}
	files := map[string]*zip.File{}
	for _, f := range z.File {
		files[f.Name] = f
	}
	read := func(name string, v any) error {
		f, ok := files[name]
		if !ok {
			return fmt.Errorf("%s missing", name)
		}
		r, err := f.Open()
		if err != nil {
			return err
		}
		defer r.Close()
		return json.NewDecoder(io.LimitReader(r, 512<<20)).Decode(v)
	}
	var man exportManifest
	if err := read("phoedox.json", &man); err != nil || man.Format != exportFormat {
		return res, invalid("validation.import_format", nil, "not a Phoedox project export")
	}
	if man.Version > 1 {
		return res, invalid("validation.import_version", map[string]any{"version": man.Version}, "export version %d is newer than this instance", man.Version)
	}
	var docs []exportDoc
	var ents []exportEntity
	var edges []exportEdge
	var kinds []exportKind
	var tasks []exportTask
	var fls []exportFile
	for name, v := range map[string]any{"documents.json": &docs, "entities.json": &ents, "edges.json": &edges, "kinds.json": &kinds, "tasks.json": &tasks, "files.json": &fls} {
		if err := read(name, v); err != nil && !strings.HasSuffix(err.Error(), "missing") {
			return res, invalid("validation.import_format", nil, "%s: %v", name, err)
		}
	}

	mp := man.Project
	in := ProjectInput{Name: mp.Name, Slug: mp.Slug, Short: mp.Short, Description: mp.Description, PrimaryColor: mp.PrimaryColor, AIWriteMode: mp.AIWriteMode}
	if mp.MainLocale != "" {
		in.MainLocale, in.Locales = &mp.MainLocale, &mp.Locales
	}
	in.Features = &mp.Features
	// Keep the slug if it is free on this instance, else slug-2, slug-3 …
	base := graph.Slugify(mp.Slug)
	if base == "" {
		base = graph.Slugify(mp.Name)
	}
	in.Slug = base
	for i := 2; i < 100; i++ {
		if _, err := s.DB.ProjectFor(ctx, in.Slug, ""); errors.Is(err, store.ErrNotFound) {
			break
		}
		in.Slug = fmt.Sprintf("%s-%d", base, i)
	}
	p, err := s.CreateProject(ctx, a, in)
	if err != nil {
		return res, err
	}
	if mp.MainLocale != "" { // languages are set by an update after creation
		_, _ = s.UpdateProject(ctx, a, p.Slug, ProjectInput{Name: p.Name, Short: p.Short, PrimaryColor: p.PrimaryColor, AIWriteMode: p.AIWriteMode, MainLocale: &mp.MainLocale, Locales: &mp.Locales})
	}
	res.Project = p
	problem := func(format string, args ...any) {
		if len(res.Problems) < 50 {
			res.Problems = append(res.Problems, fmt.Sprintf(format, args...))
		}
	}

	for _, k := range kinds {
		if err := s.DB.SetProjectKind(ctx, store.ProjectKind{ProjectID: p.ID, Kind: k.Kind, Def: store.JSON(k.Def), UpdatedBy: a.String(), UpdatedAt: store.Now()}); err != nil {
			problem("kind %s: %v", k.Kind, err)
		}
	}
	for _, e := range ents {
		attrs := map[string]any{}
		_ = json.Unmarshal(e.Attrs, &attrs)
		if _, err := s.UpsertEntity(ctx, a, p.ID, EntityInput{Kind: e.Kind, Name: e.Name, Slug: e.Slug, Attrs: attrs}); err != nil {
			problem("entry %s: %v", e.Slug, err)
			continue
		}
		res.Entries++
	}
	for _, e := range edges {
		attrs := map[string]any{}
		_ = json.Unmarshal(e.Attrs, &attrs)
		if _, err := s.Link(ctx, a, p.ID, EdgeInput{From: e.From, Rel: e.Rel, To: e.To, Attrs: attrs}); err != nil {
			problem("relation %s %s %s: %v", e.From, e.Rel, e.To, err)
		}
	}

	// Decisions in number order keep their numbers; parents before children.
	sort.SliceStable(docs, func(i, j int) bool {
		if (docs[i].Parent == "") != (docs[j].Parent == "") {
			return docs[i].Parent == ""
		}
		if docs[i].Number != docs[j].Number {
			return docs[i].Number < docs[j].Number
		}
		return false
	})
	for _, d := range docs {
		di := DocumentInput{Type: d.Type, Title: d.Title, Slug: d.Slug, Category: d.Category, Position: d.Position, Parent: d.Parent}
		switch d.Type {
		case store.DocMarkdown:
			var c struct {
				Markdown string `json:"markdown"`
			}
			_ = json.Unmarshal(d.Content, &c)
			di.Markdown = c.Markdown
		case store.DocADR:
			di.ADR = &ADR{}
			_ = json.Unmarshal(d.Content, di.ADR)
			di.ADR.SupersededBy = ""
		case store.DocLink:
			di.Link = &Link{}
			_ = json.Unmarshal(d.Content, di.Link)
		case store.DocBoard:
			di.Board = &Board{}
			_ = json.Unmarshal(d.Content, di.Board)
		case store.DocFloorplan:
			di.Floorplan = &Floorplan{}
			_ = json.Unmarshal(d.Content, di.Floorplan)
		default:
			problem("page %s: unknown type %s", d.Slug, d.Type)
			continue
		}
		r, err := s.CreateDocument(ctx, a, p.Slug, di)
		if err != nil || r.Document == nil {
			problem("page %s: %v", d.Slug, err)
			continue
		}
		res.Pages++
		for _, t := range d.Translations {
			var c map[string]*string
			_ = json.Unmarshal(t.Content, &c)
			ti := TranslationInput{Title: t.Title, Markdown: c["markdown"], Context: c["context"], Decision: c["decision"],
				Consequences: c["consequences"], Alternatives: c["alternatives"], Description: c["description"], Machine: t.Machine}
			if _, err := s.SaveTranslation(ctx, a, p.Slug, r.Document.Slug, t.Locale, ti); err != nil {
				problem("translation %s (%s): %v", d.Slug, t.Locale, err)
			}
		}
	}
	for _, t := range tasks {
		_, err := s.CreateTask(ctx, a, p.Slug, t.Board, TaskInput{Title: t.Title, Description: t.Description, Column: t.Column, Due: t.Due, Priority: t.Priority, Labels: t.Labels})
		if err != nil {
			problem("card %q: %v", t.Title, err)
			continue
		}
		res.Tasks++
	}
	if len(tasks) > 0 {
		s.assignImported(ctx, a, p, tasks, &res)
	}
	for _, f := range fls {
		zf, ok := files[f.Path]
		if !ok {
			continue
		}
		r, err := zf.Open()
		if err != nil {
			continue
		}
		_, err = s.UploadFile(ctx, a, p.Slug, FileInput{Name: f.Name, Document: f.Document, Entity: f.Entity}, io.LimitReader(r, maxImportBytes))
		r.Close()
		if err != nil {
			problem("file %s: %v", f.Name, err)
			continue
		}
		res.Files++
	}
	s.event(ctx, a, "project.import", p.ID, map[string]any{"pages": res.Pages, "entries": res.Entries, "tasks": res.Tasks, "files": res.Files})
	return res, nil
}

// assignImported sets assignees and card order after all cards exist:
// members by e-mail, everything else as free text.
func (s *Service) assignImported(ctx context.Context, a Actor, p store.Project, tasks []exportTask, res *ImportResultProject) {
	created, err := s.DB.ListTasks(ctx, p.ID, store.TaskFilter{})
	if err != nil {
		return
	}
	used := map[string]bool{}
	for _, t := range tasks {
		for _, c := range created {
			if used[c.ID] || c.Title != t.Title || c.BoardSlug != t.Board {
				continue
			}
			used[c.ID] = true
			patch := TaskPatch{Position: &t.Position}
			if t.Assignee != "" {
				as := t.Assignee
				patch.Assignee = &as
			}
			if _, err := s.UpdateTask(ctx, a, p.Slug, c.ID, patch); err != nil && t.Assignee != "" {
				// Not a member here: keep the name as free text.
				text := strings.SplitN(t.Assignee, "@", 2)[0]
				patch.Assignee = &text
				_, _ = s.UpdateTask(ctx, a, p.Slug, c.ID, patch)
			}
			break
		}
	}
}

// RemoteProject is a project offered by another instance.
type RemoteProject struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
	Role string `json:"role"`
}

// RemoteSource is another Phoedox instance and an API token of it.
type RemoteSource struct {
	URL     string `json:"url"`
	Token   string `json:"token"`
	Project string `json:"project,omitempty"`
}

func (s *Service) remote(ctx context.Context, src RemoteSource, path string) (*http.Response, error) {
	u, err := url.Parse(strings.TrimRight(strings.TrimSpace(src.URL), "/"))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return nil, invalid("validation.remote_url", nil, "the address of the other instance is invalid")
	}
	if strings.TrimSpace(src.Token) == "" {
		return nil, invalid("validation.remote_token", nil, "an API token of the other instance is needed")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String()+"/api/v1"+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(src.Token))
	c := *s.RequestPolicy.Client()
	c.Timeout = 30 * time.Minute
	res, err := c.Do(req)
	if err != nil {
		return nil, invalid("validation.remote_unreachable", map[string]any{"error": err.Error()}, "the other instance is not reachable: %v", err)
	}
	if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
		res.Body.Close()
		return nil, invalid("validation.remote_token", nil, "the other instance rejected the token")
	}
	if res.StatusCode != http.StatusOK {
		res.Body.Close()
		return nil, invalid("validation.remote_unreachable", map[string]any{"error": res.Status}, "the other instance answered %s", res.Status)
	}
	return res, nil
}

// RemoteProjects lists the projects the token may export on another instance.
func (s *Service) RemoteProjects(ctx context.Context, a Actor, src RemoteSource) ([]RemoteProject, error) {
	if a.Via == "mcp" || a.Restricted() {
		return nil, ErrForbidden
	}
	res, err := s.remote(ctx, src, "/projects")
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	var list []RemoteProject
	if err := json.NewDecoder(io.LimitReader(res.Body, 8<<20)).Decode(&list); err != nil {
		return nil, invalid("validation.remote_unreachable", map[string]any{"error": "not a Phoedox instance"}, "not a Phoedox instance")
	}
	out := []RemoteProject{}
	for _, p := range list {
		if p.Role == store.RoleOwner {
			out = append(out, p)
		}
	}
	return out, nil
}

// ImportRemote moves a project from another instance: it downloads the
// export with the token and imports it here.
func (s *Service) ImportRemote(ctx context.Context, a Actor, src RemoteSource) (ImportResultProject, error) {
	if a.Via == "mcp" || a.Restricted() {
		return ImportResultProject{}, ErrForbidden
	}
	if src.Project == "" {
		return ImportResultProject{}, invalid("validation.remote_project", nil, "choose a project")
	}
	res, err := s.remote(ctx, src, "/projects/"+url.PathEscape(src.Project)+"/export")
	if err != nil {
		return ImportResultProject{}, err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, maxImportBytes))
	if err != nil {
		return ImportResultProject{}, err
	}
	return s.ImportProject(ctx, a, data)
}
