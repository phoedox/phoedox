package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/phoedox/phoedox/internal/graph"
	"github.com/phoedox/phoedox/internal/store"
)

// ADR is the content of a decision record.
type ADR struct {
	Status       string   `json:"status"` // proposed | accepted | rejected | deprecated | partially_superseded | superseded
	Date         string   `json:"date,omitempty"`
	Deciders     []string `json:"deciders,omitempty"`
	Context      string   `json:"context"`
	Decision     string   `json:"decision"`
	Consequences string   `json:"consequences,omitempty"`
	Alternatives string   `json:"alternatives,omitempty"`
	Supersedes   string   `json:"supersedes,omitempty"` // slug of an older decision
	// SupersedesPartly: this decision replaces only part of the older one
	// (which then becomes partially_superseded); SupersedesScope says which
	// part, e.g. "only the choice of database".
	SupersedesPartly bool   `json:"supersedesPartly,omitempty"`
	SupersedesScope  string `json:"supersedesScope,omitempty"`
	// SupersededBy is maintained by the server: the slug of the decision
	// that replaced this one. Input values are ignored.
	SupersededBy string `json:"supersededBy,omitempty"`
	// PartlySupersededBy is maintained by the server: the decisions that
	// each replaced a part of this one, oldest first.
	PartlySupersededBy []string `json:"partlySupersededBy,omitempty"`
	// Options a proposed decision offers to choose from (ADR-0062). While
	// there are options, Decision may stay empty until someone decides.
	Options []ADROption `json:"options,omitempty"`
	// Responses are maintained by the server: every "decide" with the
	// chosen option or "none" (nothing fits) and a comment, oldest first.
	Responses []ADRResponse `json:"responses,omitempty"`
	// Chosen is maintained by the server: the id of the chosen option.
	Chosen string `json:"chosen,omitempty"`
}

// ADROption is one option of a proposed decision.
type ADROption struct {
	ID          string `json:"id,omitempty"` // assigned by the server when empty (o1, o2, …)
	Title       string `json:"title"`
	Description string `json:"description,omitempty"` // Markdown, e.g. pros and cons
}

// ADRResponse is one answer to a proposed decision.
type ADRResponse struct {
	Choice  string     `json:"choice"`           // option id or "none"
	Option  string     `json:"option,omitempty"` // title of the chosen option at that time
	Comment string     `json:"comment,omitempty"`
	By      string     `json:"by"`
	ByID    string     `json:"byId,omitempty"`
	At      store.Time `json:"at"`
}

// ChoiceNone answers a proposed decision with "nothing fits".
const ChoiceNone = "none"

const maxADROptions = 12

// normalizeOptions trims options, checks them and gives every option a
// stable id.
func normalizeOptions(opts []ADROption) ([]ADROption, error) {
	if len(opts) > maxADROptions {
		return nil, invalid("validation.adr_options_max", map[string]any{"max": maxADROptions}, "at most %d options", maxADROptions)
	}
	used := map[string]bool{}
	for _, o := range opts {
		if o.ID != "" {
			used[o.ID] = true
		}
	}
	out := make([]ADROption, 0, len(opts))
	seen := map[string]bool{}
	n := 0
	for _, o := range opts {
		o.Title, o.Description, o.ID = strings.TrimSpace(o.Title), strings.TrimSpace(o.Description), strings.TrimSpace(o.ID)
		if o.Title == "" {
			if o.Description == "" {
				continue
			}
			return nil, invalid("validation.adr_option_title", nil, "every option needs a title")
		}
		if len([]rune(o.Title)) > 200 {
			return nil, invalid("validation.adr_option_title", nil, "option titles are at most 200 characters")
		}
		if o.ID == "" || o.ID == ChoiceNone || seen[o.ID] || len(o.ID) > 40 {
			for {
				n++
				if id := fmt.Sprintf("o%d", n); !used[id] && !seen[id] {
					o.ID = id
					break
				}
			}
		}
		seen[o.ID] = true
		out = append(out, o)
	}
	return out, nil
}

// DecisionRef formats a decision number, e.g. ADR-0007.
func DecisionRef(n int) string { return fmt.Sprintf("ADR-%04d", n) }

// adrRef matches decision references like ADR-0007, adr-7 or ADR7.
var adrRef = regexp.MustCompile(`(?i)^adr-?0*([1-9][0-9]*)$`)

// findDoc resolves a document by id, slug, decision reference (ADR-0007)
// or, failing those, a title that starts with ref.
func (s *Service) findDoc(ctx context.Context, projectID, ref string) (store.Document, error) {
	ref = strings.TrimSpace(ref)
	doc, err := s.DB.GetDocument(ctx, projectID, ref)
	if !errors.Is(err, store.ErrNotFound) || ref == "" {
		return doc, err
	}
	if m := adrRef.FindStringSubmatch(ref); m != nil {
		n, _ := strconv.Atoi(m[1])
		return s.DB.GetDecision(ctx, projectID, n)
	}
	// The document's own number from another system ("D-640").
	if doc, err = s.DB.DocumentByExtRef(ctx, projectID, ref); !errors.Is(err, store.ErrNotFound) {
		return doc, err
	}
	// [[D-321]] and similar: slugs are lowercase, and identifiers from other
	// systems often live only at the start of a title.
	if low := strings.ToLower(ref); low != ref {
		if doc, err = s.DB.GetDocument(ctx, projectID, low); !errors.Is(err, store.ErrNotFound) {
			return doc, err
		}
	}
	return s.DB.DocumentByTitle(ctx, projectID, ref)
}

// extRefRe: own numbers from other systems – letters, digits and . _ : # / -,
// starting with a letter or digit, at most 40 characters.
var extRefRe = regexp.MustCompile(`^[\p{L}\p{N}][\p{L}\p{N}._:#/-]{0,39}$`)

// checkExtRef validates a document's own number: its format, that it is not
// one of Phoedox's numbers (ADR-0007) and that no other document of the
// project uses it.
func (s *Service) checkExtRef(ctx context.Context, p store.Project, ref, selfID string) error {
	if ref == "" {
		return nil
	}
	if !extRefRe.MatchString(ref) || adrRef.MatchString(ref) {
		return invalid("validation.ext_ref", map[string]any{"ref": ref}, "own number %q: letters, digits and . _ : # / - (up to 40), not ADR-0000", ref)
	}
	other, err := s.DB.DocumentByExtRef(ctx, p.ID, ref)
	if err == nil && other.ID != selfID {
		return invalid("validation.ext_ref_taken", map[string]any{"ref": ref, "title": other.Title}, "%q is already the number of %q", ref, other.Title)
	}
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	return nil
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
		before, _ := json.Marshal(old)
		// Drop this decision from the older one's lists (also under its
		// previous slug), then add it again as a full or partial replacement.
		partly := old.PartlySupersededBy[:0:0]
		for _, sl := range old.PartlySupersededBy {
			if sl != selfSlug && (oldSlug == "" || sl != oldSlug) {
				partly = append(partly, sl)
			}
		}
		old.PartlySupersededBy = partly
		if old.SupersededBy == selfSlug || (oldSlug != "" && old.SupersededBy == oldSlug) {
			old.SupersededBy = ""
		}
		if adr.SupersedesPartly {
			old.PartlySupersededBy = append(old.PartlySupersededBy, selfSlug)
			if old.SupersededBy == "" && (old.Status == "superseded" || old.Status == "accepted" || old.Status == "proposed" || old.Status == "") {
				old.Status = "partially_superseded"
			}
		} else {
			old.Status, old.SupersededBy = "superseded", selfSlug
		}
		if after, _ := json.Marshal(old); string(after) != string(before) {
			target.Content = after
			related = append(related, target)
		}
	} else {
		adr.SupersedesPartly, adr.SupersedesScope = false, ""
	}
	adr.SupersedesScope = strings.TrimSpace(adr.SupersedesScope)
	if oldSlug != "" && oldSlug != selfSlug {
		// Newer decisions that replace part of this one follow its new slug.
		for _, sl := range adr.PartlySupersededBy {
			newer, err := s.DB.GetDocument(ctx, p.ID, sl)
			if err != nil || newer.Type != store.DocADR || newer.ID == selfID {
				continue
			}
			var n ADR
			_ = json.Unmarshal(newer.Content, &n)
			if n.Supersedes == oldSlug {
				n.Supersedes = selfSlug
				newer.Content, _ = json.Marshal(n)
				related = append(related, newer)
			}
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
	// Archive is a copy of the linked document kept by Phoedox.
	Archive *LinkArchive `json:"archive,omitempty"`
}

// LinkArchive is the stored copy of a link's target.
type LinkArchive struct {
	File string     `json:"file"` // project file id
	At   store.Time `json:"at"`
	Mime string     `json:"mime"`
}

var adrStatus = map[string]bool{"proposed": true, "accepted": true, "rejected": true, "deprecated": true, "partially_superseded": true, "superseded": true}

// DocumentInput creates a document.
type DocumentInput struct {
	Type     string `json:"type"` // markdown | adr | link | board | floorplan | whiteboard
	Title    string `json:"title"`
	Slug     string `json:"slug,omitempty"`
	Category string `json:"category,omitempty"`
	Position int    `json:"position,omitempty"`
	Markdown string `json:"markdown,omitempty"`
	ADR      *ADR   `json:"adr,omitempty"`
	Link     *Link  `json:"link,omitempty"`
	Board    *Board `json:"board,omitempty"`
	// Floorplan is the content of a floor plan document.
	Floorplan *Floorplan `json:"floorplan,omitempty"`
	// Whiteboard is the content of a whiteboard document.
	Whiteboard *Whiteboard `json:"whiteboard,omitempty"`
	// Parent makes the document part of a page (slug or id), e.g. a board
	// created from the page's "/" menu; it stays out of the sidebar.
	Parent string `json:"parent,omitempty"`
	// ExtRef is the document's own number in another system ("D-640").
	ExtRef string `json:"extRef,omitempty"`
	// After places the new document right after another one (slug or id);
	// without a category it goes into that document's category.
	After string `json:"after,omitempty"`
}

// DocumentPatch changes a document; nil fields stay as they are.
type DocumentPatch struct {
	Title         *string     `json:"title,omitempty"`
	Slug          *string     `json:"slug,omitempty"`
	Category      *string     `json:"category,omitempty"`
	Position      *int        `json:"position,omitempty"`
	Markdown      *string     `json:"markdown,omitempty"`
	ADR           *ADR        `json:"adr,omitempty"`
	Link          *Link       `json:"link,omitempty"`
	Board         *Board      `json:"board,omitempty"`
	Floorplan     *Floorplan  `json:"floorplan,omitempty"`
	Whiteboard    *Whiteboard `json:"whiteboard,omitempty"`
	ExtRef        *string     `json:"extRef,omitempty"`
	ExpectVersion int         `json:"expectVersion,omitempty"`
	// Edits change exact passages instead of sending the whole text.
	Edits []TextEdit `json:"edits,omitempty"`
	// After moves the document right after another one (slug or id) in
	// its category.
	After string `json:"after,omitempty"`
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
func content(typ, markdown string, adr *ADR, link *Link, board *Board, fp *Floorplan, wb ...*Whiteboard) (store.JSON, string, error) {
	switch typ {
	case store.DocWhiteboard:
		var w *Whiteboard
		if len(wb) > 0 {
			w = wb[0]
		}
		return whiteboardContent(w)
	case store.DocBoard:
		return boardContent(board)
	case store.DocFloorplan:
		return floorplanContent(fp)
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
			return nil, "", invalid("validation.adr_status", nil, "adr.status must be proposed, accepted, rejected, deprecated, partially_superseded or superseded")
		}
		opts, err := normalizeOptions(adr.Options)
		if err != nil {
			return nil, "", err
		}
		adr.Options = opts
		if strings.TrimSpace(adr.Decision) == "" && (adr.Status != "proposed" || len(adr.Options) == 0) {
			return nil, "", invalid("validation.adr_decision_required", nil, "adr.decision is required (a proposed decision may offer options instead)")
		}
		b, _ := json.Marshal(adr)
		parts := []string{adr.Context, adr.Decision, adr.Consequences, adr.Alternatives}
		for _, o := range adr.Options {
			parts = append(parts, o.Title, o.Description)
		}
		return b, strings.Join(parts, "\n"), nil
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
		return nil, "", invalid("validation.doc_type", nil, "type must be markdown, adr, link, board, floorplan or whiteboard")
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
	var titles map[string]string
	if f.Locale != "" && f.Locale != p.MainLocale && slices.Contains(p.Locales, f.Locale) {
		if titles, err = s.DB.TranslatedTitles(ctx, p.ID, f.Locale); err != nil {
			return nil, err
		}
	}
	out := docs[:0]
	for _, d := range docs {
		if see(d.ID, d.Restricted) {
			if t, ok := titles[d.ID]; ok {
				d.Title, d.Locale = t, f.Locale
			}
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
	if _, _, err := content(in.Type, in.Markdown, in.ADR, in.Link, in.Board, in.Floorplan, in.Whiteboard); err != nil {
		return DocResult{}, err
	}
	in.ExtRef = strings.TrimSpace(in.ExtRef)
	if err := s.checkExtRef(ctx, p, in.ExtRef, ""); err != nil {
		return DocResult{}, err
	}
	if in.Type == store.DocWhiteboard {
		if err := s.wbFiles(ctx, p, in.Whiteboard); err != nil {
			return DocResult{}, err
		}
	}
	if in.Type == store.DocFloorplan {
		if err := s.planFiles(ctx, p, in.Floorplan); err != nil {
			return DocResult{}, err
		}
	}
	if in.Type == store.DocADR {
		adr := *in.ADR
		in.ADR = &adr
		in.ADR.SupersededBy, in.ADR.PartlySupersededBy = "", nil
		in.ADR.Responses, in.ADR.Chosen = nil, ""
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
		adr.SupersededBy, adr.PartlySupersededBy = "", nil
		adr.Responses, adr.Chosen = nil, ""
		in.ADR = &adr
		var err error
		if related, err = s.decisionLinks(ctx, p, in.ADR, "", in.Slug, ""); err != nil {
			return DocResult{}, err
		}
	}
	c, text, err := content(in.Type, in.Markdown, in.ADR, in.Link, in.Board, in.Floorplan, in.Whiteboard)
	if err != nil {
		return DocResult{}, err
	}
	if in.After != "" {
		cat := strings.TrimSpace(in.Category)
		if cat == "" {
			cat = "\x00" // the category of the document it follows
		}
		pos, c, err := s.placeAfter(ctx, a, p, cat, in.After, "")
		if err != nil {
			return DocResult{}, err
		}
		in.Position, in.Category, in.After = pos, c, ""
	}
	parentID := ""
	if in.Parent != "" {
		parent, err := s.DB.GetDocument(ctx, p.ID, in.Parent)
		if err != nil {
			return DocResult{}, invalid("validation.parent", map[string]any{"parent": in.Parent}, "parent page %q not found", in.Parent)
		}
		parentID = parent.ID
		if in.Category == "" {
			in.Category = parent.Category
		}
	}
	doc, err := s.DB.CreateDocument(ctx, store.Document{ID: newID(), ProjectID: p.ID, Type: in.Type, Slug: in.Slug, Title: in.Title,
		Category: strings.TrimSpace(in.Category), Position: in.Position, Content: c, CreatedBy: a.User.ID, ParentID: parentID, ExtRef: in.ExtRef}, a.String(), related...)
	if err != nil {
		return DocResult{}, err
	}
	s.linkMentions(ctx, p, doc.ID, text)
	if doc.Type == store.DocFloorplan {
		s.syncFloorplan(ctx, a, p, doc)
	}
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
	if err := s.resolveEdits(ctx, cur, &patch); err != nil {
		return DocResult{}, err
	}
	if _, _, _, err := s.patched(ctx, p, cur, patch); err != nil {
		return DocResult{}, err
	}
	gate := p
	if a.Via == "mcp" && gate.AIWriteMode == AIDirect {
		// AI changes to a skill page always need a person: the page
		// instructs AI clients (ADR-0049).
		if _, err := s.DB.GetSkill(ctx, cur.ID); err == nil {
			gate.AIWriteMode = AISuggest
		}
	}
	if sug, ok, err := s.maybeSuggest(ctx, a, gate, "document.update", "Änderung an „"+cur.Title+"“", opPayload{Project: p.ID, Doc: cur.ID, Patch: &patch}); ok || err != nil {
		return DocResult{Suggestion: sug}, err
	}
	return s.updateDocument(ctx, a, p, cur, patch)
}

// CheckUpdate validates a change without writing it (for batches).
func (s *Service) CheckUpdate(ctx context.Context, a Actor, ref, docRef string, patch DocumentPatch) error {
	p, cur, err := s.docAccess(ctx, a, ref, docRef, store.RoleEditor)
	if err != nil {
		return err
	}
	patch.Edits = slices.Clone(patch.Edits)
	if err := s.resolveEdits(ctx, cur, &patch); err != nil {
		return err
	}
	if patch.After != "" {
		after, err := s.findDoc(ctx, p.ID, patch.After)
		cat := cur.Category
		if patch.Category != nil {
			cat = strings.TrimSpace(*patch.Category)
		}
		if err != nil {
			return invalid("validation.after", map[string]any{"after": patch.After}, "document %q to insert after not found", patch.After)
		}
		if after.Category != cat {
			return invalid("validation.after_category", map[string]any{"after": after.Title}, "%q is in another category", after.Title)
		}
	}
	if patch.ExpectVersion > 0 && patch.ExpectVersion != cur.Version {
		return store.ErrConflict
	}
	_, _, _, err = s.patched(ctx, p, cur, patch)
	return err
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
	if patch.ExtRef != nil {
		doc.ExtRef = strings.TrimSpace(*patch.ExtRef)
		if err := s.checkExtRef(ctx, p, doc.ExtRef, cur.ID); err != nil {
			return doc, "", nil, err
		}
	}
	// Start from the current content and replace what the patch brings.
	var md string
	var adr *ADR
	var link *Link
	var board *Board
	var fp *Floorplan
	var wb *Whiteboard
	switch cur.Type {
	case store.DocWhiteboard:
		wb = &Whiteboard{}
		_ = json.Unmarshal(cur.Content, wb)
		if patch.Whiteboard != nil {
			wb = patch.Whiteboard
			if err := s.wbFiles(ctx, p, wb); err != nil {
				return doc, "", nil, err
			}
		}
	case store.DocFloorplan:
		fp = &Floorplan{}
		_ = json.Unmarshal(cur.Content, fp)
		if patch.Floorplan != nil {
			fp = patch.Floorplan
			if err := s.planFiles(ctx, p, fp); err != nil {
				return doc, "", nil, err
			}
		}
	case store.DocBoard:
		board = &Board{}
		_ = json.Unmarshal(cur.Content, board)
		if patch.Board != nil {
			board = patch.Board
		}
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
			next.SupersededBy, next.PartlySupersededBy = adr.SupersededBy, adr.PartlySupersededBy // server-maintained
			next.Responses, next.Chosen = adr.Responses, adr.Chosen
			adr = &next
		}
	case store.DocLink:
		link = &Link{}
		_ = json.Unmarshal(cur.Content, link)
		if patch.Link != nil {
			// The archived copy stays as long as the address does.
			if patch.Link.Archive == nil && link.Archive != nil && patch.Link.URL == link.URL {
				patch.Link.Archive = link.Archive
			}
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
	c, text, err := content(cur.Type, md, adr, link, board, fp, wb)
	doc.Content = c
	return doc, text, related, err
}

// updateDocument applies a change: into the open draft of the page if there
// is one (ADR-0027), otherwise to the published content.
func (s *Service) updateDocument(ctx context.Context, a Actor, p store.Project, cur store.Document, patch DocumentPatch) (DocResult, error) {
	if res, ok, err := s.draftWrite(ctx, a, p, cur, patch); ok || err != nil {
		return res, err
	}
	// Approval projects: editors' changes go into a draft for review (ADR-0042).
	if hasDrafts(cur.Type) && (patch.Markdown != nil || patch.Floorplan != nil || patch.Title != nil) && s.needsApproval(ctx, p) {
		if err := s.openDraft(ctx, a, cur); err != nil {
			return DocResult{}, err
		}
		res, _, err := s.draftWrite(ctx, a, p, cur, patch)
		return res, err
	}
	return s.publishPatch(ctx, a, p, cur, patch)
}

// publishPatch changes the published content and stores a version.
func (s *Service) publishPatch(ctx context.Context, a Actor, p store.Project, cur store.Document, patch DocumentPatch) (DocResult, error) {
	if patch.After != "" {
		cat := cur.Category
		if patch.Category != nil {
			cat = strings.TrimSpace(*patch.Category)
		}
		pos, _, err := s.placeAfter(ctx, a, p, cat, patch.After, cur.ID)
		if err != nil {
			return DocResult{}, err
		}
		patch.Position, patch.After = &pos, ""
	}
	doc, text, related, err := s.patched(ctx, p, cur, patch)
	if err != nil {
		return DocResult{}, err
	}
	// Moving or renaming the address only: no new version (ADR-0014/0028).
	if doc.Title == cur.Title && sameJSON(cur.Content, doc.Content) && len(related) == 0 {
		if doc.Slug == cur.Slug && doc.Category == cur.Category && doc.Position == cur.Position && doc.ExtRef == cur.ExtRef {
			return DocResult{Applied: true, Document: &doc}, nil
		}
		if doc, err = s.DB.UpdateDocumentPlace(ctx, doc, patch.ExpectVersion); err != nil {
			return DocResult{}, err
		}
		kind := "document.move"
		if doc.Category == cur.Category {
			kind = "document.place"
		}
		s.event(ctx, a, kind, doc.ID, map[string]any{"from": cur.Category, "to": doc.Category, "slug": doc.Slug, "position": doc.Position})
		s.indexDocument(ctx, doc)
		return DocResult{Applied: true, Document: &doc}, nil
	}
	doc, err = s.DB.UpdateDocument(ctx, doc, patch.ExpectVersion, a.String(), related...)
	if err != nil {
		return DocResult{}, err
	}
	if !sameJSON(cur.Content, doc.Content) {
		s.contentChanged(ctx, a, doc.ID)
		if doc.Type == store.DocBoard {
			s.afterBoardChange(ctx, doc)
		}
		if doc.Type == store.DocFloorplan {
			s.syncFloorplan(ctx, a, p, doc)
		}
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
	// Boards also mention what their cards mention.
	if tasks, err := s.DB.ListTasks(ctx, p.ID, store.TaskFilter{BoardID: docID}); err == nil {
		for _, t := range tasks {
			text += "\n" + t.Title + "\n" + t.Description
		}
	}
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
	ID         string `json:"id"`
	Ref        string `json:"ref"` // ADR-0007
	Number     int    `json:"number"`
	ExtRef     string `json:"extRef,omitempty"` // own number from another system (D-640)
	Slug       string `json:"slug"`
	Title      string `json:"title"`
	Status     string `json:"status"`
	Date       string `json:"date,omitempty"`
	Category   string `json:"category,omitempty"`
	Supersedes string `json:"supersedes,omitempty"`
	// SupersedesPartly: Supersedes is only partly replaced (see SupersedesScope).
	SupersedesPartly   bool     `json:"supersedesPartly,omitempty"`
	SupersedesScope    string   `json:"supersedesScope,omitempty"`
	SupersededBy       string   `json:"supersededBy,omitempty"`
	PartlySupersededBy []string `json:"partlySupersededBy,omitempty"`
	// Options is the number of options of a proposed decision; NeedsOptions
	// is true when the last answer was "nothing fits" (ADR-0062).
	Options      int        `json:"options,omitempty"`
	NeedsOptions bool       `json:"needsOptions,omitempty"`
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
		return nil, invalid("validation.adr_status", nil, "status must be proposed, accepted, rejected, deprecated, partially_superseded or superseded")
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
			text := strings.ToLower(strings.Join([]string{DecisionRef(d.Number), d.ExtRef, d.Title, c.Context, c.Decision, c.Consequences, c.Alternatives}, "\n"))
			if !containsAll(text, words) {
				continue
			}
		}
		out = append(out, DecisionSummary{ID: d.ID, Ref: DecisionRef(d.Number), Number: d.Number, ExtRef: d.ExtRef, Slug: d.Slug, Title: d.Title, Status: c.Status, Date: c.Date,
			Category: d.Category, Supersedes: c.Supersedes, SupersedesPartly: c.SupersedesPartly, SupersedesScope: c.SupersedesScope,
			SupersededBy: c.SupersededBy, PartlySupersededBy: c.PartlySupersededBy, Version: d.Version, UpdatedAt: d.UpdatedAt,
			Options: len(c.Options), NeedsOptions: c.Status == "proposed" && len(c.Responses) > 0 && c.Responses[len(c.Responses)-1].Choice == ChoiceNone})
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
