package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/phoedox/phoedox/internal/graph"
	"github.com/phoedox/phoedox/internal/store"
)

// ADR is the content of a decision record.
type ADR struct {
	Status       string   `json:"status"` // proposed | accepted | rejected | deprecated | superseded
	Date         string   `json:"date,omitempty"`
	Deciders     []string `json:"deciders,omitempty"`
	Context      string   `json:"context"`
	Decision     string   `json:"decision"`
	Consequences string   `json:"consequences,omitempty"`
	Alternatives string   `json:"alternatives,omitempty"`
	Supersedes   string   `json:"supersedes,omitempty"` // slug of an older decision
	// SupersededBy is maintained by the server: the slug of the decision
	// that replaced this one. Input values are ignored.
	SupersededBy string `json:"supersededBy,omitempty"`
}

// DecisionRef formats a decision number, e.g. ADR-0007.
func DecisionRef(n int) string { return fmt.Sprintf("ADR-%04d", n) }

// adrRef matches decision references like ADR-0007, adr-7 or ADR7.
var adrRef = regexp.MustCompile(`(?i)^adr-?0*([1-9][0-9]*)$`)

// findDoc resolves a document by id, slug or decision reference (ADR-0007).
func (s *Service) findDoc(ctx context.Context, projectID, ref string) (store.Document, error) {
	ref = strings.TrimSpace(ref)
	doc, err := s.DB.GetDocument(ctx, projectID, ref)
	if errors.Is(err, store.ErrNotFound) {
		if m := adrRef.FindStringSubmatch(ref); m != nil {
			n, _ := strconv.Atoi(m[1])
			return s.DB.GetDecision(ctx, projectID, n)
		}
	}
	return doc, err
}

// decisionLinks validates adr.Supersedes, normalises it to the target's slug
// and returns the decisions that change together with this one: the one it
// supersedes (marked as superseded) and, after a slug change from oldSlug,
// the one that supersedes it (pointed to the new slug).
func (s *Service) decisionLinks(ctx context.Context, p store.Project, adr *ADR, selfID, selfSlug, oldSlug string) ([]store.Document, error) {
	var related []store.Document
	adr.Supersedes = strings.TrimSpace(adr.Supersedes)
	if ref := adr.Supersedes; ref != "" {
		target, err := s.findDoc(ctx, p.ID, ref)
		if errors.Is(err, store.ErrNotFound) {
			return nil, invalid("validation.adr_supersedes_not_found", map[string]any{"ref": ref}, "decision %q to supersede not found", ref)
		}
		if err != nil {
			return nil, err
		}
		if target.Type != store.DocADR {
			return nil, invalid("validation.adr_supersedes_type", map[string]any{"ref": ref}, "%q is not a decision", ref)
		}
		if target.ID == selfID {
			return nil, invalid("validation.adr_supersedes_self", nil, "a decision cannot supersede itself")
		}
		adr.Supersedes = target.Slug
		var old ADR
		_ = json.Unmarshal(target.Content, &old)
		if old.Status != "superseded" || old.SupersededBy != selfSlug {
			old.Status, old.SupersededBy = "superseded", selfSlug
			target.Content, _ = json.Marshal(old)
			related = append(related, target)
		}
	}
	if oldSlug != "" && oldSlug != selfSlug && adr.SupersededBy != "" {
		newer, err := s.DB.GetDocument(ctx, p.ID, adr.SupersededBy)
		if err == nil && newer.Type == store.DocADR && newer.ID != selfID && (len(related) == 0 || related[0].ID != newer.ID) {
			var n ADR
			_ = json.Unmarshal(newer.Content, &n)
			if n.Supersedes == oldSlug {
				n.Supersedes = selfSlug
				newer.Content, _ = json.Marshal(n)
				related = append(related, newer)
			}
		}
	}
	return related, nil
}

// Link is the content of an external link document.
type Link struct {
	URL         string `json:"url"`
	Description string `json:"description,omitempty"`
}

var adrStatus = map[string]bool{"proposed": true, "accepted": true, "rejected": true, "deprecated": true, "superseded": true}

// DocumentInput creates a document.
type DocumentInput struct {
	Type     string `json:"type"` // markdown | adr | link
	Title    string `json:"title"`
	Slug     string `json:"slug,omitempty"`
	Category string `json:"category,omitempty"`
	Position int    `json:"position,omitempty"`
	Markdown string `json:"markdown,omitempty"`
	ADR      *ADR   `json:"adr,omitempty"`
	Link     *Link  `json:"link,omitempty"`
}

// DocumentPatch changes a document; nil fields stay as they are.
type DocumentPatch struct {
	Title         *string `json:"title,omitempty"`
	Slug          *string `json:"slug,omitempty"`
	Category      *string `json:"category,omitempty"`
	Position      *int    `json:"position,omitempty"`
	Markdown      *string `json:"markdown,omitempty"`
	ADR           *ADR    `json:"adr,omitempty"`
	Link          *Link   `json:"link,omitempty"`
	ExpectVersion int     `json:"expectVersion,omitempty"`
}

// DocResult is either an applied change or a pending suggestion.
type DocResult struct {
	Applied    bool              `json:"applied"`
	Document   *store.Document   `json:"document,omitempty"`
	Suggestion *store.Suggestion `json:"suggestion,omitempty"`
	// Draft is set when the change went into the open draft of the page
	// (ADR-0027) instead of the published content.
	Draft *DraftInfo `json:"draft,omitempty"`
}

// content builds and validates the JSON content for a type and returns the
// text used for mention extraction.
func content(typ, markdown string, adr *ADR, link *Link) (store.JSON, string, error) {
	switch typ {
	case store.DocMarkdown:
		b, _ := json.Marshal(map[string]string{"markdown": markdown})
		return b, markdown, nil
	case store.DocADR:
		if adr == nil {
			return nil, "", invalid("validation.adr_required", nil, "adr content is required for type adr")
		}
		if adr.Status == "" {
			adr.Status = "proposed"
		}
		if !adrStatus[adr.Status] {
			return nil, "", invalid("validation.adr_status", nil, "adr.status must be proposed, accepted, rejected, deprecated or superseded")
		}
		if strings.TrimSpace(adr.Decision) == "" {
			return nil, "", invalid("validation.adr_decision_required", nil, "adr.decision is required")
		}
		b, _ := json.Marshal(adr)
		return b, strings.Join([]string{adr.Context, adr.Decision, adr.Consequences, adr.Alternatives}, "\n"), nil
	case store.DocLink:
		if link == nil {
			return nil, "", invalid("validation.link_required", nil, "link content is required for type link")
		}
		u, err := url.Parse(link.URL)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return nil, "", invalid("validation.link_url", nil, "link.url must be an http(s) URL")
		}
		b, _ := json.Marshal(link)
		return b, link.Description, nil
	default:
		return nil, "", invalid("validation.doc_type", nil, "type must be markdown, adr or link")
	}
}

// Documents lists documents of a project without content.
func (s *Service) Documents(ctx context.Context, a Actor, ref string, f store.DocFilter) ([]store.Document, error) {
	p, err := s.project(ctx, a, ref, store.RoleDoc)
	if err != nil {
		return nil, err
	}
	docs, err := s.DB.ListDocuments(ctx, p.ID, f)
	if err != nil {
		return nil, err
	}
	see, err := s.docFilter(ctx, a, p)
	if err != nil {
		return nil, err
	}
	out := docs[:0]
	for _, d := range docs {
		if see(d.ID, d.Restricted) {
			out = append(out, d)
		}
	}
	return out, nil
}

// Document returns one document with content.
func (s *Service) Document(ctx context.Context, a Actor, ref, docRef string) (store.Document, error) {
	p, doc, err := s.docAccess(ctx, a, ref, docRef, store.RoleReader)
	doc.Role = p.Role
	return doc, err
}

// Versions lists the history of a document.
func (s *Service) Versions(ctx context.Context, a Actor, ref, docRef string) ([]store.Version, error) {
	doc, err := s.Document(ctx, a, ref, docRef)
	if err != nil {
		return nil, err
	}
	return s.DB.ListVersions(ctx, doc.ID)
}

// CreateDocument creates a document (or a suggestion for AI clients).
func (s *Service) CreateDocument(ctx context.Context, a Actor, ref string, in DocumentInput) (DocResult, error) {
	p, err := s.project(ctx, a, ref, store.RoleEditor)
	if err != nil {
		return DocResult{}, err
	}
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" {
		return DocResult{}, invalid("validation.title_required", nil, "title is required")
	}
	if in.Slug == "" {
		in.Slug = graph.Slugify(in.Title)
	}
	if err := graph.ValidateSlug(in.Slug); err != nil {
		return DocResult{}, invalid("validation.slug", nil, "slug: %v", err)
	}
	if _, _, err := content(in.Type, in.Markdown, in.ADR, in.Link); err != nil {
		return DocResult{}, err
	}
	if in.Type == store.DocADR {
		adr := *in.ADR
		in.ADR = &adr
		in.ADR.SupersededBy = ""
		if _, err := s.decisionLinks(ctx, p, in.ADR, "", in.Slug, ""); err != nil {
			return DocResult{}, err
		}
	}
	if sug, ok, err := s.maybeSuggest(ctx, a, p, "document.create", "Neues Dokument „"+in.Title+"“", opPayload{Project: p.ID, Create: &in}); ok || err != nil {
		return DocResult{Suggestion: sug}, err
	}
	return s.createDocument(ctx, a, p, in)
}

func (s *Service) createDocument(ctx context.Context, a Actor, p store.Project, in DocumentInput) (DocResult, error) {
	var related []store.Document
	if in.Type == store.DocADR && in.ADR != nil {
		adr := *in.ADR
		adr.SupersededBy = ""
		in.ADR = &adr
		var err error
		if related, err = s.decisionLinks(ctx, p, in.ADR, "", in.Slug, ""); err != nil {
			return DocResult{}, err
		}
	}
	c, text, err := content(in.Type, in.Markdown, in.ADR, in.Link)
	if err != nil {
		return DocResult{}, err
	}
	doc, err := s.DB.CreateDocument(ctx, store.Document{ID: newID(), ProjectID: p.ID, Type: in.Type, Slug: in.Slug, Title: in.Title,
		Category: strings.TrimSpace(in.Category), Position: in.Position, Content: c, CreatedBy: a.User.ID}, a.String(), related...)
	if err != nil {
		return DocResult{}, err
	}
	s.linkMentions(ctx, p, doc.ID, text)
	s.event(ctx, a, "document.create", doc.ID, map[string]any{"project": p.ID, "slug": doc.Slug, "number": doc.Number})
	s.relatedEvents(ctx, a, doc, related)
	s.indexDocument(ctx, doc)
	for _, r := range related {
		s.indexDocument(ctx, r)
	}
	return DocResult{Applied: true, Document: &doc}, nil
}

// relatedEvents records changes to decisions that changed with doc.
func (s *Service) relatedEvents(ctx context.Context, a Actor, doc store.Document, related []store.Document) {
	for _, r := range related {
		s.event(ctx, a, "decision.link", r.ID, map[string]string{"because": doc.ID})
	}
}

// UpdateDocument applies a patch and stores a new version.
func (s *Service) UpdateDocument(ctx context.Context, a Actor, ref, docRef string, patch DocumentPatch) (DocResult, error) {
	p, cur, err := s.docAccess(ctx, a, ref, docRef, store.RoleEditor)
	if err != nil {
		return DocResult{}, err
	}
	if _, _, _, err := s.patched(ctx, p, cur, patch); err != nil {
		return DocResult{}, err
	}
	if sug, ok, err := s.maybeSuggest(ctx, a, p, "document.update", "Änderung an „"+cur.Title+"“", opPayload{Project: p.ID, Doc: cur.ID, Patch: &patch}); ok || err != nil {
		return DocResult{Suggestion: sug}, err
	}
	return s.updateDocument(ctx, a, p, cur, patch)
}

// patched returns the document with the patch applied, the mention text and
// the decisions that change with it.
func (s *Service) patched(ctx context.Context, p store.Project, cur store.Document, patch DocumentPatch) (store.Document, string, []store.Document, error) {
	doc := cur
	if patch.Title != nil {
		doc.Title = strings.TrimSpace(*patch.Title)
		if doc.Title == "" {
			return doc, "", nil, invalid("validation.title_required", nil, "title must not be empty")
		}
	}
	if patch.Slug != nil {
		if err := graph.ValidateSlug(*patch.Slug); err != nil {
			return doc, "", nil, invalid("validation.slug", nil, "slug: %v", err)
		}
		doc.Slug = *patch.Slug
	}
	if patch.Category != nil {
		doc.Category = strings.TrimSpace(*patch.Category)
	}
	if patch.Position != nil {
		doc.Position = *patch.Position
	}
	// Start from the current content and replace what the patch brings.
	var md string
	var adr *ADR
	var link *Link
	switch cur.Type {
	case store.DocMarkdown:
		var c struct {
			Markdown string `json:"markdown"`
		}
		_ = json.Unmarshal(cur.Content, &c)
		md = c.Markdown
		if patch.Markdown != nil {
			md = *patch.Markdown
		}
	case store.DocADR:
		adr = &ADR{}
		_ = json.Unmarshal(cur.Content, adr)
		if patch.ADR != nil {
			next := *patch.ADR
			next.SupersededBy = adr.SupersededBy // server-maintained
			adr = &next
		}
	case store.DocLink:
		link = &Link{}
		_ = json.Unmarshal(cur.Content, link)
		if patch.Link != nil {
			link = patch.Link
		}
	}
	var related []store.Document
	if adr != nil {
		var err error
		if related, err = s.decisionLinks(ctx, p, adr, cur.ID, doc.Slug, cur.Slug); err != nil {
			return doc, "", nil, err
		}
	}
	c, text, err := content(cur.Type, md, adr, link)
	doc.Content = c
	return doc, text, related, err
}

// updateDocument applies a change: into the open draft of the page if there
// is one (ADR-0027), otherwise to the published content.
func (s *Service) updateDocument(ctx context.Context, a Actor, p store.Project, cur store.Document, patch DocumentPatch) (DocResult, error) {
	if res, ok, err := s.draftWrite(ctx, a, p, cur, patch); ok || err != nil {
		return res, err
	}
	return s.publishPatch(ctx, a, p, cur, patch)
}

// publishPatch changes the published content and stores a version.
func (s *Service) publishPatch(ctx context.Context, a Actor, p store.Project, cur store.Document, patch DocumentPatch) (DocResult, error) {
	doc, text, related, err := s.patched(ctx, p, cur, patch)
	if err != nil {
		return DocResult{}, err
	}
	doc, err = s.DB.UpdateDocument(ctx, doc, patch.ExpectVersion, a.String(), related...)
	if err != nil {
		return DocResult{}, err
	}
	if !sameJSON(cur.Content, doc.Content) {
		s.contentChanged(ctx, a, doc.ID)
	}
	s.linkMentions(ctx, p, doc.ID, text)
	s.event(ctx, a, "document.update", doc.ID, map[string]int{"version": doc.Version})
	s.relatedEvents(ctx, a, doc, related)
	s.indexDocument(ctx, doc)
	for _, r := range related {
		s.indexDocument(ctx, r)
	}
	return DocResult{Applied: true, Document: &doc}, nil
}

// DeleteDocument moves a document to the trash.
func (s *Service) DeleteDocument(ctx context.Context, a Actor, ref, docRef string) (DocResult, error) {
	p, cur, err := s.docAccess(ctx, a, ref, docRef, store.RoleEditor)
	if err != nil {
		return DocResult{}, err
	}
	if sug, ok, err := s.maybeSuggest(ctx, a, p, "document.delete", "„"+cur.Title+"“ löschen", opPayload{Project: p.ID, Doc: cur.ID}); ok || err != nil {
		return DocResult{Suggestion: sug}, err
	}
	if err := s.DB.DeleteDocument(ctx, cur.ID); err != nil {
		return DocResult{}, err
	}
	if _, err := s.DB.GetDraft(ctx, cur.ID); err == nil {
		_ = s.DB.DeleteDraft(ctx, cur.ID)
		s.draftEnded(ctx, a, cur.ID, "deleted")
	}
	s.unindex(ctx, cur.ID)
	s.contentChanged(ctx, a, cur.ID)
	s.event(ctx, a, "document.delete", cur.ID, nil)
	return DocResult{Applied: true, Document: &cur}, nil
}

// linkMentions stores which entities a document mentions (@slug or @project/slug).
func (s *Service) linkMentions(ctx context.Context, p store.Project, docID, text string) {
	var slugs []string
	for _, m := range graph.ParseMentions(text) {
		if m.Project == "" || m.Project == p.Slug {
			slugs = append(slugs, m.Slug)
		}
	}
	ents, err := s.DB.EntitiesBySlugs(ctx, p.ID, slugs)
	if err != nil {
		return
	}
	ids := make([]string, len(ents))
	for i, e := range ents {
		ids[i] = e.ID
	}
	_ = s.DB.SetDocumentLinks(ctx, docID, ids)
}

// DecisionSummary is one row of a project's decision log.
type DecisionSummary struct {
	Ref          string     `json:"ref"` // ADR-0007
	Number       int        `json:"number"`
	Slug         string     `json:"slug"`
	Title        string     `json:"title"`
	Status       string     `json:"status"`
	Date         string     `json:"date,omitempty"`
	Category     string     `json:"category,omitempty"`
	Supersedes   string     `json:"supersedes,omitempty"`
	SupersededBy string     `json:"supersededBy,omitempty"`
	Version      int        `json:"version"`
	UpdatedAt    store.Time `json:"updatedAt"`
}

// Decisions lists the decisions of a project by number, optionally only
// those with one status.
// Decisions lists the decision log. status filters by status; query keeps
// decisions whose number, title or text contain every word of it.
func (s *Service) Decisions(ctx context.Context, a Actor, ref, status, query string) ([]DecisionSummary, error) {
	words := strings.Fields(strings.ToLower(query))
	if status != "" && !adrStatus[status] {
		return nil, invalid("validation.adr_status", nil, "status must be proposed, accepted, rejected, deprecated or superseded")
	}
	p, err := s.project(ctx, a, ref, store.RoleDoc)
	if err != nil {
		return nil, err
	}
	docs, err := s.DB.ListDocuments(ctx, p.ID, store.DocFilter{Type: store.DocADR, WithContent: true})
	if err != nil {
		return nil, err
	}
	see, err := s.docFilter(ctx, a, p)
	if err != nil {
		return nil, err
	}
	out := []DecisionSummary{}
	for _, d := range docs {
		if !see(d.ID, d.Restricted) {
			continue
		}
		var c ADR
		_ = json.Unmarshal(d.Content, &c)
		if status != "" && c.Status != status {
			continue
		}
		if len(words) > 0 {
			text := strings.ToLower(strings.Join([]string{DecisionRef(d.Number), d.Title, c.Context, c.Decision, c.Consequences, c.Alternatives}, "\n"))
			if !containsAll(text, words) {
				continue
			}
		}
		out = append(out, DecisionSummary{Ref: DecisionRef(d.Number), Number: d.Number, Slug: d.Slug, Title: d.Title, Status: c.Status, Date: c.Date,
			Category: d.Category, Supersedes: c.Supersedes, SupersededBy: c.SupersededBy, Version: d.Version, UpdatedAt: d.UpdatedAt})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Number < out[j].Number })
	return out, nil
}

func containsAll(text string, words []string) bool {
	for _, w := range words {
		if !strings.Contains(text, w) {
			return false
		}
	}
	return true
}
