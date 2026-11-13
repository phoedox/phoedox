package core

import (
	"context"
	"encoding/json"
	"net/url"
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
	p, err := s.project(ctx, a, ref, store.RoleReader)
	if err != nil {
		return nil, err
	}
	return s.DB.ListDocuments(ctx, p.ID, f)
}

// Document returns one document with content.
func (s *Service) Document(ctx context.Context, a Actor, ref, docRef string) (store.Document, error) {
	p, err := s.project(ctx, a, ref, store.RoleReader)
	if err != nil {
		return store.Document{}, err
	}
	return s.DB.GetDocument(ctx, p.ID, docRef)
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
	if sug, ok, err := s.maybeSuggest(ctx, a, p, "document.create", "Neues Dokument „"+in.Title+"“", opPayload{Project: p.ID, Create: &in}); ok || err != nil {
		return DocResult{Suggestion: sug}, err
	}
	return s.createDocument(ctx, a, p, in)
}

func (s *Service) createDocument(ctx context.Context, a Actor, p store.Project, in DocumentInput) (DocResult, error) {
	c, text, err := content(in.Type, in.Markdown, in.ADR, in.Link)
	if err != nil {
		return DocResult{}, err
	}
	doc, err := s.DB.CreateDocument(ctx, store.Document{ID: newID(), ProjectID: p.ID, Type: in.Type, Slug: in.Slug, Title: in.Title,
		Category: strings.TrimSpace(in.Category), Position: in.Position, Content: c, CreatedBy: a.User.ID}, a.String())
	if err != nil {
		return DocResult{}, err
	}
	s.linkMentions(ctx, p, doc.ID, text)
	s.event(ctx, a, "document.create", doc.ID, map[string]string{"project": p.ID, "slug": doc.Slug})
	return DocResult{Applied: true, Document: &doc}, nil
}

// UpdateDocument applies a patch and stores a new version.
func (s *Service) UpdateDocument(ctx context.Context, a Actor, ref, docRef string, patch DocumentPatch) (DocResult, error) {
	p, err := s.project(ctx, a, ref, store.RoleEditor)
	if err != nil {
		return DocResult{}, err
	}
	cur, err := s.DB.GetDocument(ctx, p.ID, docRef)
	if err != nil {
		return DocResult{}, err
	}
	if _, _, err := s.patched(cur, patch); err != nil {
		return DocResult{}, err
	}
	if sug, ok, err := s.maybeSuggest(ctx, a, p, "document.update", "Änderung an „"+cur.Title+"“", opPayload{Project: p.ID, Doc: cur.ID, Patch: &patch}); ok || err != nil {
		return DocResult{Suggestion: sug}, err
	}
	return s.updateDocument(ctx, a, p, cur, patch)
}

// patched returns the document with the patch applied and the mention text.
func (s *Service) patched(cur store.Document, patch DocumentPatch) (store.Document, string, error) {
	doc := cur
	if patch.Title != nil {
		doc.Title = strings.TrimSpace(*patch.Title)
		if doc.Title == "" {
			return doc, "", invalid("validation.title_required", nil, "title must not be empty")
		}
	}
	if patch.Slug != nil {
		if err := graph.ValidateSlug(*patch.Slug); err != nil {
			return doc, "", invalid("validation.slug", nil, "slug: %v", err)
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
			adr = patch.ADR
		}
	case store.DocLink:
		link = &Link{}
		_ = json.Unmarshal(cur.Content, link)
		if patch.Link != nil {
			link = patch.Link
		}
	}
	c, text, err := content(cur.Type, md, adr, link)
	doc.Content = c
	return doc, text, err
}

func (s *Service) updateDocument(ctx context.Context, a Actor, p store.Project, cur store.Document, patch DocumentPatch) (DocResult, error) {
	doc, text, err := s.patched(cur, patch)
	if err != nil {
		return DocResult{}, err
	}
	doc, err = s.DB.UpdateDocument(ctx, doc, patch.ExpectVersion, a.String())
	if err != nil {
		return DocResult{}, err
	}
	s.linkMentions(ctx, p, doc.ID, text)
	s.event(ctx, a, "document.update", doc.ID, map[string]int{"version": doc.Version})
	return DocResult{Applied: true, Document: &doc}, nil
}

// DeleteDocument moves a document to the trash.
func (s *Service) DeleteDocument(ctx context.Context, a Actor, ref, docRef string) (DocResult, error) {
	p, err := s.project(ctx, a, ref, store.RoleEditor)
	if err != nil {
		return DocResult{}, err
	}
	cur, err := s.DB.GetDocument(ctx, p.ID, docRef)
	if err != nil {
		return DocResult{}, err
	}
	if sug, ok, err := s.maybeSuggest(ctx, a, p, "document.delete", "„"+cur.Title+"“ löschen", opPayload{Project: p.ID, Doc: cur.ID}); ok || err != nil {
		return DocResult{Suggestion: sug}, err
	}
	if err := s.DB.DeleteDocument(ctx, cur.ID); err != nil {
		return DocResult{}, err
	}
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
