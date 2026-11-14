package core

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/phoedox/phoedox/internal/blob"
	"github.com/phoedox/phoedox/internal/graph"
	"github.com/phoedox/phoedox/internal/store"
)

// Publications (ADR-0006, T5e): one or more projects as a site with its own
// menu, public or behind a password, on /pub/<slug> or an own domain.
// Only published content is shown: no drafts, no restricted pages.

var hostRe = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)

// PublicationInput creates or changes a publication.
type PublicationInput struct {
	Slug        string   `json:"slug,omitempty"`
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Projects    []string `json:"projects"` // slugs or ids, in menu order
	Access      string   `json:"access,omitempty"`
	// Password sets a new password (access "password"); nil keeps the old one.
	Password *string `json:"password,omitempty"`
	Domain   string  `json:"domain,omitempty"`
	Color    string  `json:"color,omitempty"`
	CSS      string  `json:"css,omitempty"`
	NoIndex  bool    `json:"noindex,omitempty"`
}

// PublicationInfo is a publication with the names of its projects.
type PublicationInfo struct {
	store.Publication
	ProjectNames []string `json:"projectNames"`
	HasPassword  bool     `json:"hasPassword"`
	CanEdit      bool     `json:"canEdit"`
}

// owns tells whether the actor owns all given projects (system admins always).
func (s *Service) ownsAll(ctx context.Context, a Actor, ids []string) bool {
	if a.User.IsAdmin {
		return true
	}
	for _, id := range ids {
		p, err := s.DB.ProjectFor(ctx, id, a.User.ID)
		if err != nil || p.Role != store.RoleOwner {
			return false
		}
	}
	return true
}

func (s *Service) pubInfo(ctx context.Context, a Actor, p store.Publication) PublicationInfo {
	info := PublicationInfo{Publication: p, HasPassword: p.PasswordHash != "", CanEdit: s.ownsAll(ctx, a, p.Projects), ProjectNames: []string{}}
	for _, id := range p.Projects {
		if pr, err := s.DB.ProjectFor(ctx, id, a.User.ID); err == nil {
			info.ProjectNames = append(info.ProjectNames, pr.Name)
		}
	}
	return info
}

// Publications lists the publications that include a project the actor owns.
func (s *Service) Publications(ctx context.Context, a Actor) ([]PublicationInfo, error) {
	if a.Via == "mcp" {
		return nil, ErrForbidden
	}
	all, err := s.DB.ListPublications(ctx)
	if err != nil {
		return nil, err
	}
	out := []PublicationInfo{}
	for _, p := range all {
		mine := a.User.IsAdmin || p.CreatedBy == a.User.ID
		for _, id := range p.Projects {
			if pr, err := s.DB.ProjectFor(ctx, id, a.User.ID); err == nil && pr.Role == store.RoleOwner {
				mine = true
			}
		}
		if mine {
			out = append(out, s.pubInfo(ctx, a, p))
		}
	}
	return out, nil
}

func (s *Service) normalizePublication(ctx context.Context, a Actor, in *PublicationInput, p *store.Publication) error {
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" {
		return invalid("validation.title_required", nil, "title is required")
	}
	if in.Slug == "" {
		in.Slug = graph.Slugify(in.Title)
	}
	if err := graph.ValidateSlug(in.Slug); err != nil {
		return invalid("validation.slug", nil, "slug: %v", err)
	}
	if len(in.Projects) == 0 || len(in.Projects) > 50 {
		return invalid("validation.pub_projects", nil, "a publication has 1 to 50 projects")
	}
	ids := []string{}
	for _, ref := range in.Projects {
		pr, err := s.project(ctx, a, ref, store.RoleOwner)
		if err != nil {
			return err
		}
		if !slices.Contains(ids, pr.ID) {
			ids = append(ids, pr.ID)
		}
	}
	switch in.Access {
	case "", "public":
		in.Access = "public"
	case "password", "members":
	default:
		return invalid("validation.pub_access", nil, "access must be public, password or members")
	}
	in.Domain = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(in.Domain, ".")))
	if in.Domain != "" && (!hostRe.MatchString(in.Domain) || in.Domain == strings.ToLower(s.appHost())) {
		return invalid("validation.pub_domain", map[string]any{"domain": in.Domain}, "%q is not a usable domain", in.Domain)
	}
	if in.Color != "" && !colorRe.MatchString(in.Color) {
		return invalid("validation.color", nil, "color must look like #F26A1B")
	}
	// No "<": the CSS goes into a <style> element of the site.
	if strings.Contains(in.CSS, "<") {
		return invalid("validation.pub_css", nil, "custom CSS must not contain <")
	}
	if len(in.CSS) > 20000 {
		return invalid("validation.pub_css", nil, "custom CSS is limited to 20 000 characters")
	}
	if in.Password != nil {
		if *in.Password == "" {
			p.PasswordHash = ""
		} else {
			if len(*in.Password) < 8 {
				return invalid("validation.password_short", map[string]any{"min": 8}, "password too short")
			}
			if s.HashPassword == nil {
				return errors.New("no password hasher")
			}
			h, err := s.HashPassword(*in.Password)
			if err != nil {
				return err
			}
			p.PasswordHash = h
		}
	}
	if in.Access == "password" && p.PasswordHash == "" {
		return invalid("validation.pub_password", nil, "set a password for a protected publication")
	}
	p.Slug, p.Title, p.Description, p.Projects, p.Access = in.Slug, in.Title, strings.TrimSpace(in.Description), ids, in.Access
	p.Domain, p.Color, p.CSS, p.NoIndex = in.Domain, in.Color, in.CSS, in.NoIndex
	return nil
}

func (s *Service) appHost() string {
	h := strings.TrimPrefix(strings.TrimPrefix(s.PublicURL, "https://"), "http://")
	if i := strings.IndexAny(h, ":/"); i >= 0 {
		h = h[:i]
	}
	return h
}

// CreatePublication creates a publication of projects the actor owns.
func (s *Service) CreatePublication(ctx context.Context, a Actor, in PublicationInput) (PublicationInfo, error) {
	if a.Via == "mcp" {
		return PublicationInfo{}, ErrForbidden
	}
	now := store.Now()
	p := store.Publication{ID: newID(), CreatedBy: a.User.ID, CreatedAt: now, UpdatedAt: now}
	if err := s.normalizePublication(ctx, a, &in, &p); err != nil {
		return PublicationInfo{}, err
	}
	if err := s.DB.SavePublication(ctx, p); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return PublicationInfo{}, invalid("validation.pub_taken", nil, "slug or domain already in use")
		}
		return PublicationInfo{}, err
	}
	s.event(ctx, a, "publication.create", p.ID, map[string]any{"slug": p.Slug, "domain": p.Domain})
	return s.pubInfo(ctx, a, p), nil
}

// editablePublication loads a publication the actor may change.
func (s *Service) editablePublication(ctx context.Context, a Actor, ref string) (store.Publication, error) {
	if a.Via == "mcp" {
		return store.Publication{}, ErrForbidden
	}
	p, err := s.DB.GetPublication(ctx, ref)
	if err != nil {
		return p, err
	}
	if !s.ownsAll(ctx, a, p.Projects) {
		return p, ErrForbidden
	}
	return p, nil
}

// UpdatePublication changes a publication.
func (s *Service) UpdatePublication(ctx context.Context, a Actor, ref string, in PublicationInput) (PublicationInfo, error) {
	p, err := s.editablePublication(ctx, a, ref)
	if err != nil {
		return PublicationInfo{}, err
	}
	if err := s.normalizePublication(ctx, a, &in, &p); err != nil {
		return PublicationInfo{}, err
	}
	p.UpdatedAt = store.Now()
	if err := s.DB.SavePublication(ctx, p); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return PublicationInfo{}, invalid("validation.pub_taken", nil, "slug or domain already in use")
		}
		return PublicationInfo{}, err
	}
	s.event(ctx, a, "publication.update", p.ID, map[string]any{"slug": p.Slug, "domain": p.Domain})
	return s.pubInfo(ctx, a, p), nil
}

// DeletePublication removes a publication (the projects stay).
func (s *Service) DeletePublication(ctx context.Context, a Actor, ref string) error {
	p, err := s.editablePublication(ctx, a, ref)
	if err != nil {
		return err
	}
	if err := s.DB.DeletePublication(ctx, p.ID); err != nil {
		return err
	}
	s.event(ctx, a, "publication.delete", p.ID, map[string]any{"slug": p.Slug})
	return nil
}

// ---------- Reading a publication (no actor) ----------

// PubProject is a project of a publication with its pages.
type PubProject struct {
	Project store.Project
	Docs    []store.Document
}

// Site loads a publication with the published pages of its projects. ctx
// must carry the publication's tenant.
func (s *Service) Site(ctx context.Context, p store.Publication) ([]PubProject, error) {
	return s.SiteAt(ctx, p, nil)
}

// SiteAt is Site as of a release (nil = now): pages that existed then, in
// their last version before it.
func (s *Service) SiteAt(ctx context.Context, p store.Publication, release *store.PublicationRelease) ([]PubProject, error) {
	out := []PubProject{}
	for _, id := range p.Projects {
		pr, err := s.DB.ProjectFor(ctx, id, "")
		if err != nil {
			continue // deleted meanwhile
		}
		var docs []store.Document
		if release != nil {
			docs, err = s.DB.DocumentsAt(ctx, pr.ID, release.CreatedAt)
		} else {
			docs, err = s.DB.ListDocuments(ctx, pr.ID, store.DocFilter{WithContent: true})
		}
		if err != nil {
			return nil, err
		}
		pub := docs[:0]
		for _, d := range docs {
			if !d.Restricted {
				pub = append(pub, d)
			}
		}
		out = append(out, PubProject{Project: pr, Docs: pub})
	}
	return out, nil
}

// PubDocument returns a published page in a language ("" = main).
func (s *Service) PubDocument(ctx context.Context, site []PubProject, projectSlug, docSlug, locale string) (PubProject, store.Document, error) {
	for _, pp := range site {
		if pp.Project.Slug != projectSlug {
			continue
		}
		for _, d := range pp.Docs {
			if d.Slug == docSlug {
				if locale != "" && locale != pp.Project.MainLocale && slices.Contains(pp.Project.Locales, locale) {
					if t, err := s.DB.GetTranslation(ctx, d.ID, locale); err == nil {
						localize(&d, t)
						d.Locale = locale
						t.Content = nil
						d.Translation = &t
					}
				}
				return pp, d, nil
			}
		}
	}
	return PubProject{}, store.Document{}, store.ErrNotFound
}

// PubTasks returns the tasks of a published board.
func (s *Service) PubTasks(ctx context.Context, pp PubProject, board store.Document) ([]store.Task, error) {
	return s.DB.ListTasks(ctx, pp.Project.ID, store.TaskFilter{BoardID: board.ID})
}

// PubFile opens a file attached to a published page.
func (s *Service) PubFile(ctx context.Context, site []PubProject, id string, preview bool) (FileContent, error) {
	f, err := s.DB.GetFile(ctx, id)
	if err != nil {
		return FileContent{}, err
	}
	ok := false
	for _, pp := range site {
		if pp.Project.ID != f.ProjectID {
			continue
		}
		for _, d := range pp.Docs {
			if d.ID == f.DocumentID {
				ok = true
			}
		}
	}
	if !ok || s.Blobs == nil {
		return FileContent{}, ErrNotFound
	}
	out := FileContent{File: f, Mime: f.Mime, SHA256: f.SHA256}
	if preview {
		if f.ThumbSHA256 == "" {
			return out, ErrNotFound
		}
		out.SHA256, out.Mime = f.ThumbSHA256, ""
	}
	rc, err := s.Blobs.Open(ctx, out.SHA256)
	if errors.Is(err, blob.ErrNotFound) {
		return out, ErrNotFound
	}
	out.Reader = rc
	return out, err
}

// PubHit is a search result in a publication.
type PubHit struct {
	Project PubProject
	Doc     store.Document
	Snippet string
}

// PubSearch finds pages whose title or text contain all words.
func PubSearch(site []PubProject, q string) []PubHit {
	words := strings.Fields(strings.ToLower(q))
	if len(words) == 0 {
		return nil
	}
	var hits []PubHit
	for _, pp := range site {
		for _, d := range pp.Docs {
			text := docText(d)
			hay := strings.ToLower(d.Title + "\n" + text)
			if !containsAll(hay, words) {
				continue
			}
			i := strings.Index(strings.ToLower(text), words[0])
			snip := ""
			if i >= 0 {
				from, to := max(0, i-60), min(len(text), i+120)
				snip = strings.TrimSpace(strings.ToValidUTF8(text[from:to], ""))
			}
			hits = append(hits, PubHit{Project: pp, Doc: d, Snippet: snip})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		return strings.Contains(strings.ToLower(hits[i].Doc.Title), words[0]) && !strings.Contains(strings.ToLower(hits[j].Doc.Title), words[0])
	})
	return hits
}

// docText is the readable text of a document for search.
func docText(d store.Document) string {
	var c map[string]any
	_ = json.Unmarshal(d.Content, &c)
	var parts []string
	for _, k := range []string{"markdown", "context", "decision", "consequences", "alternatives", "description"} {
		if v, ok := c[k].(string); ok {
			parts = append(parts, v)
		}
	}
	return strings.Join(parts, "\n")
}

// CreateRelease freezes the current state of a publication as the next
// numbered release (/v/<number>/).
func (s *Service) CreateRelease(ctx context.Context, a Actor, ref, note string) (store.PublicationRelease, error) {
	if a.Via == "mcp" {
		return store.PublicationRelease{}, ErrForbidden
	}
	p, err := s.editablePublication(ctx, a, ref)
	if err != nil {
		return store.PublicationRelease{}, err
	}
	if r := []rune(note); len(r) > 200 {
		note = string(r[:200])
	}
	// One second later than now, so edits saved in this second are included.
	r, err := s.DB.AddRelease(ctx, store.PublicationRelease{ID: newID(), PublicationID: p.ID, Note: strings.TrimSpace(note), CreatedBy: a.User.ID,
		CreatedAt: store.Time{Time: time.Now().UTC().Add(time.Second)}})
	if err != nil {
		return r, err
	}
	s.event(ctx, a, "publication.release", p.ID, map[string]any{"number": r.Number})
	return r, nil
}

// Releases lists the releases of a publication.
func (s *Service) Releases(ctx context.Context, a Actor, ref string) ([]store.PublicationRelease, error) {
	p, err := s.editablePublication(ctx, a, ref)
	if err != nil {
		return nil, err
	}
	return s.DB.Releases(ctx, p.ID)
}
