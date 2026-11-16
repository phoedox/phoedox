package core

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strings"

	"github.com/phoedox/phoedox/internal/store"
)

// Find and replace across one, several or all documents of a project: the
// text of pages (their open draft, where writes go), decision texts and link
// descriptions, optionally titles, in the main language or one translation.
// Replacing writes ordinary versions, so drafts, approvals, AI suggestions,
// history and events apply as for any other change.

const (
	findMaxPerDoc = 200
	findMaxTotal  = 2000
	findContext   = 60
)

// FindInput describes a search, and with Replace a replacement.
type FindInput struct {
	Find          string   `json:"find"`
	Replace       *string  `json:"replace,omitempty"` // nil: search only
	Regex         bool     `json:"regex,omitempty"`
	CaseSensitive bool     `json:"caseSensitive,omitempty"`
	WholeWord     bool     `json:"wholeWord,omitempty"`
	Titles        bool     `json:"titles,omitempty"`
	Documents     []string `json:"documents,omitempty"` // slugs or ids; empty = all
	Lang          string   `json:"lang,omitempty"`      // a further language: its translations
	// Apply writes the replacement (otherwise only a preview).
	Apply bool `json:"apply,omitempty"`
	// Only: document id → numbers of the matches to replace (from the
	// preview); a document missing here gets all its matches replaced, an
	// empty list none.
	Only map[string][]int `json:"only,omitempty"`
	// Versions: document id → the version the preview saw; a document that
	// changed since is skipped.
	Versions map[string]int `json:"versions,omitempty"`
}

// FindMatch is one hit with its surroundings.
type FindMatch struct {
	N           int    `json:"n"`
	Field       string `json:"field"` // title, markdown, context, decision, consequences, alternatives, description
	Line        int    `json:"line"`
	Before      string `json:"before"`
	Text        string `json:"text"`
	After       string `json:"after"`
	Replacement string `json:"replacement,omitempty"`
}

// FindDoc are the hits in one document.
type FindDoc struct {
	ID      string      `json:"id"`
	Slug    string      `json:"slug"`
	Title   string      `json:"title"`
	Type    string      `json:"type"`
	Ref     string      `json:"ref,omitempty"`
	Version int         `json:"version"`
	Draft   bool        `json:"draft,omitempty"`
	Count   int         `json:"count"`
	Matches []FindMatch `json:"matches"`
	// Result of an apply: applied, suggested, skipped (with Reason) or failed.
	Result string `json:"result,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// FindResult is the answer of a search or replacement.
type FindResult struct {
	Documents []FindDoc `json:"documents"`
	Total     int       `json:"total"`
	Scanned   int       `json:"scanned"`
	Truncated bool      `json:"truncated,omitempty"`
	Replaced  int       `json:"replaced,omitempty"`
}

// findPattern builds the regular expression for the input.
func findPattern(in FindInput) (*regexp.Regexp, error) {
	if in.Find == "" {
		return nil, invalid("validation.find_empty", nil, "enter the text to find")
	}
	src := in.Find
	if !in.Regex {
		src = regexp.QuoteMeta(src)
	}
	if in.WholeWord {
		src = `\b(?:` + src + `)\b`
	}
	if !in.CaseSensitive {
		src = "(?i)" + src
	}
	re, err := regexp.Compile(src)
	if err != nil {
		return nil, invalid("validation.find_regex", map[string]any{"error": err.Error()}, "invalid regular expression: %v", err)
	}
	if re.MatchString("") {
		return nil, invalid("validation.find_matches_empty", nil, "the expression matches empty text; make it more specific")
	}
	return re, nil
}

// replacement of one match.
func replacement(re *regexp.Regexp, in FindInput, text string, loc []int) string {
	if in.Replace == nil {
		return ""
	}
	if !in.Regex {
		return *in.Replace
	}
	return string(re.ExpandString(nil, *in.Replace, text, loc))
}

// findFields are the searchable texts of a document in a fixed order.
type findField struct {
	name string
	text string
}

func cut(s string, n int, fromEnd bool) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if fromEnd {
		return "…" + string(r[len(r)-n:])
	}
	return string(r[:n]) + "…"
}

// scanFields finds the matches in fields, numbered across them.
func scanFields(re *regexp.Regexp, in FindInput, fields []findField) []FindMatch {
	var out []FindMatch
	n := 0
	for _, f := range fields {
		for _, loc := range re.FindAllStringSubmatchIndex(f.text, -1) {
			n++
			if len(out) >= findMaxPerDoc {
				out = append(out, FindMatch{N: n}) // counted, not shown
				continue
			}
			before := f.text[:loc[0]]
			if i := strings.LastIndex(before, "\n"); i >= 0 {
				before = before[i+1:]
			}
			after := f.text[loc[1]:]
			if i := strings.Index(after, "\n"); i >= 0 {
				after = after[:i]
			}
			out = append(out, FindMatch{N: n, Field: f.name, Line: strings.Count(f.text[:loc[0]], "\n") + 1,
				Before: cut(before, findContext, true), Text: f.text[loc[0]:loc[1]], After: cut(after, findContext, false),
				Replacement: replacement(re, in, f.text, loc)})
		}
	}
	return out
}

// replaceSelected replaces the chosen matches (numbers continue from start).
func replaceSelected(re *regexp.Regexp, in FindInput, text string, start int, pick func(int) bool) (string, int, int) {
	var b strings.Builder
	last, n, done := 0, start, 0
	for _, loc := range re.FindAllStringSubmatchIndex(text, -1) {
		n++
		if !pick(n) {
			continue
		}
		b.WriteString(text[last:loc[0]])
		b.WriteString(replacement(re, in, text, loc))
		last = loc[1]
		done++
	}
	b.WriteString(text[last:])
	return b.String(), n, done
}

// docTexts are the fields of a document as the write would see them.
type docTexts struct {
	doc    store.Document
	draft  bool
	fields []findField
	adr    *ADR
	link   *Link
	trans  *store.Translation
}

func (s *Service) docTexts(ctx context.Context, p store.Project, d store.Document, in FindInput) (docTexts, bool) {
	t := docTexts{doc: d}
	content := d.Content
	title := d.Title
	if in.Lang != "" {
		tr, err := s.DB.GetTranslation(ctx, d.ID, in.Lang)
		if err != nil {
			return t, false
		}
		t.trans = &tr
		title = tr.Title
		var c map[string]any
		_ = json.Unmarshal(d.Content, &c)
		var over map[string]any
		_ = json.Unmarshal(tr.Content, &over)
		for k, v := range over {
			c[k] = v
		}
		content, _ = json.Marshal(c)
	}
	if in.Titles {
		t.fields = append(t.fields, findField{"title", title})
	}
	switch d.Type {
	case store.DocMarkdown:
		var c struct {
			Markdown string `json:"markdown"`
		}
		_ = json.Unmarshal(content, &c)
		if in.Lang == "" {
			if dr, err := s.DB.GetDraft(ctx, d.ID); err == nil {
				c.Markdown, t.draft = dr.Markdown, true
				if in.Titles {
					t.fields[0].text = dr.Title
				}
			}
		}
		t.fields = append(t.fields, findField{"markdown", c.Markdown})
	case store.DocADR:
		adr := ADR{}
		_ = json.Unmarshal(content, &adr)
		t.adr = &adr
		t.fields = append(t.fields, findField{"context", adr.Context}, findField{"decision", adr.Decision},
			findField{"consequences", adr.Consequences}, findField{"alternatives", adr.Alternatives})
	case store.DocLink:
		link := Link{}
		_ = json.Unmarshal(content, &link)
		t.link = &link
		t.fields = append(t.fields, findField{"description", link.Description})
	default:
		if !in.Titles {
			return t, false
		}
	}
	return t, true
}

// FindInDocuments searches, previews a replacement or applies it.
func (s *Service) FindInDocuments(ctx context.Context, a Actor, ref string, in FindInput) (FindResult, error) {
	re, err := findPattern(in)
	if err != nil {
		return FindResult{}, err
	}
	need := store.RoleReader
	if in.Apply {
		if in.Replace == nil {
			return FindResult{}, invalid("validation.replace_missing", nil, "give the replacement text")
		}
		need = store.RoleEditor
	}
	p, err := s.project(ctx, a, ref, need)
	if err != nil {
		return FindResult{}, err
	}
	if in.Lang != "" {
		if err := translatable(p, in.Lang); err != nil {
			return FindResult{}, err
		}
	}
	docs, err := s.Documents(ctx, a, p.Slug, store.DocFilter{})
	if err != nil {
		return FindResult{}, err
	}
	var want map[string]bool
	if len(in.Documents) > 0 {
		want = map[string]bool{}
		for _, r := range in.Documents {
			d, err := s.findDoc(ctx, p.ID, r)
			if err != nil {
				return FindResult{}, invalid("validation.find_document", map[string]any{"document": r}, "document %q not found", r)
			}
			want[d.ID] = true
		}
	}
	res := FindResult{Documents: []FindDoc{}}
	for _, d := range docs {
		if want != nil && !want[d.ID] {
			continue
		}
		full, err := s.DB.GetDocument(ctx, p.ID, d.ID)
		if err != nil {
			continue
		}
		t, ok := s.docTexts(ctx, p, full, in)
		if !ok {
			continue
		}
		res.Scanned++
		ms := scanFields(re, in, t.fields)
		if len(ms) == 0 {
			continue
		}
		fd := FindDoc{ID: full.ID, Slug: full.Slug, Title: full.Title, Type: full.Type, Ref: docNumberRef(full), Version: full.Version, Draft: t.draft, Count: len(ms)}
		shown := ms
		if n := len(shown); n > 0 && shown[n-1].Field == "" {
			for len(shown) > 0 && shown[len(shown)-1].Field == "" {
				shown = shown[:len(shown)-1]
			}
			res.Truncated = true
		}
		if res.Total+len(shown) > findMaxTotal {
			shown = shown[:max(0, findMaxTotal-res.Total)]
			res.Truncated = true
		}
		fd.Matches = shown
		res.Total += len(ms)
		if in.Apply {
			s.applyReplace(ctx, a, p, re, in, t, &fd, &res)
		}
		res.Documents = append(res.Documents, fd)
	}
	if in.Apply {
		s.event(ctx, a, "project.replace", p.ID, map[string]any{"find": cut(in.Find, 100, false), "regex": in.Regex, "lang": in.Lang,
			"replaced": res.Replaced, "documents": len(res.Documents)})
	}
	return res, nil
}

// applyReplace writes the replacement of one document.
func (s *Service) applyReplace(ctx context.Context, a Actor, p store.Project, re *regexp.Regexp, in FindInput, t docTexts, fd *FindDoc, res *FindResult) {
	only, limited := in.Only[fd.ID]
	if limited && len(only) == 0 {
		fd.Result = "skipped"
		return
	}
	if v, ok := in.Versions[fd.ID]; ok && v != fd.Version {
		fd.Result, fd.Reason = "skipped", "changed since the preview"
		return
	}
	pick := func(n int) bool { return !limited || slices.Contains(only, n) }
	vals := map[string]string{}
	n, total := 0, 0
	for _, f := range t.fields {
		var done int
		vals[f.name], n, done = replaceSelected(re, in, f.text, n, pick)
		total += done
	}
	if total == 0 {
		fd.Result = "skipped"
		return
	}
	var err error
	applied := true
	if t.trans != nil {
		ti := TranslationInput{Title: t.trans.Title, keepSource: true}
		if v, ok := vals["title"]; ok {
			ti.Title = v
		}
		for _, k := range []string{"markdown", "context", "decision", "consequences", "alternatives", "description"} {
			if v, ok := vals[k]; ok {
				v := v
				switch k {
				case "markdown":
					ti.Markdown = &v
				case "context":
					ti.Context = &v
				case "decision":
					ti.Decision = &v
				case "consequences":
					ti.Consequences = &v
				case "alternatives":
					ti.Alternatives = &v
				case "description":
					ti.Description = &v
				}
			}
		}
		_, err = s.SaveTranslation(ctx, a, p.Slug, fd.ID, in.Lang, ti)
	} else {
		patch := DocumentPatch{}
		if v, ok := vals["title"]; ok && v != t.fields[0].text {
			patch.Title = &v
		}
		switch {
		case t.adr != nil:
			adr := *t.adr
			adr.Context, adr.Decision, adr.Consequences, adr.Alternatives = vals["context"], vals["decision"], vals["consequences"], vals["alternatives"]
			patch.ADR = &adr
		case t.link != nil:
			link := *t.link
			link.Description = vals["description"]
			patch.Link = &link
		case fd.Type == store.DocMarkdown:
			md := vals["markdown"]
			patch.Markdown = &md
		}
		if !t.draft {
			patch.ExpectVersion = fd.Version
		}
		var r DocResult
		r, err = s.UpdateDocument(ctx, a, p.Slug, fd.ID, patch)
		applied = r.Applied
	}
	switch {
	case errors.Is(err, store.ErrConflict):
		fd.Result, fd.Reason = "skipped", "changed since the preview"
	case err != nil:
		fd.Result, fd.Reason = "failed", err.Error()
	case !applied:
		fd.Result = "suggested"
		res.Replaced += total
	default:
		fd.Result = "applied"
		res.Replaced += total
	}
}

// docNumberRef is ADR-0003 for decisions.
func docNumberRef(d store.Document) string {
	if d.Type != store.DocADR || d.Number == 0 {
		return ""
	}
	return DecisionRef(d.Number)
}
