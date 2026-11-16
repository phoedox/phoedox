package core

import (
	"context"
	"encoding/json"
	"regexp"
	"slices"
	"strings"

	"github.com/phoedox/phoedox/internal/store"
)

// Glossary and style guide (AI client feedback round 3): the terms of a
// project and how they are written in each language. Translations must
// use them (or leave a term untranslated); a check after translating marks
// deviations. Spellings to avoid ("email" instead of "e-mail") are marked
// in every language.

// GlossaryEntry is one term.
type GlossaryEntry struct {
	// Term is the preferred spelling in the project's main language.
	Term string `json:"term"`
	// Translations: language → the term in that language.
	Translations map[string]string `json:"translations,omitempty"`
	// Keep: never translate the term (product names, commands).
	Keep bool `json:"keep,omitempty"`
	// Avoid lists spellings not to use, per language ("" = main language).
	Avoid map[string][]string `json:"avoid,omitempty"`
	Note  string              `json:"note,omitempty"`
}

// GlossaryProblem is a deviation from the glossary in a text.
type GlossaryProblem struct {
	Kind     string `json:"kind"` // missing_term | avoid
	Term     string `json:"term"`
	Expected string `json:"expected"`
	Found    string `json:"found,omitempty"`
}

const maxGlossary = 1000

func glossaryKey(projectID string) string { return "glossary:" + projectID }

func (s *Service) glossaryOf(ctx context.Context, projectID string) []GlossaryEntry {
	raw, _ := s.DB.Setting(ctx, glossaryKey(projectID), "")
	out := []GlossaryEntry{}
	if raw != "" {
		_ = json.Unmarshal([]byte(raw), &out)
	}
	return out
}

// Glossary returns a project's glossary (readers).
func (s *Service) Glossary(ctx context.Context, a Actor, ref string) ([]GlossaryEntry, error) {
	p, err := s.project(ctx, a, ref, store.RoleReader)
	if err != nil {
		return nil, err
	}
	return s.glossaryOf(ctx, p.ID), nil
}

// SetGlossary replaces a project's glossary (editors).
func (s *Service) SetGlossary(ctx context.Context, a Actor, ref string, entries []GlossaryEntry) ([]GlossaryEntry, error) {
	p, err := s.project(ctx, a, ref, store.RoleEditor)
	if err != nil {
		return nil, err
	}
	if len(entries) > maxGlossary {
		return nil, invalid("validation.glossary_count", map[string]any{"max": maxGlossary}, "at most %d terms", maxGlossary)
	}
	seen := map[string]bool{}
	out := make([]GlossaryEntry, 0, len(entries))
	for i, e := range entries {
		e.Term = strings.TrimSpace(e.Term)
		key := strings.ToLower(e.Term)
		if e.Term == "" || len(e.Term) > 100 || seen[key] {
			return nil, invalid("validation.glossary_term", map[string]any{"n": i + 1}, "term %d is empty, too long or twice", i+1)
		}
		seen[key] = true
		tr := map[string]string{}
		for l, v := range e.Translations {
			if v = strings.TrimSpace(v); v != "" && languageCode.MatchString(l) {
				tr[l] = excerpt(v, 100)
			}
		}
		e.Translations = tr
		av := map[string][]string{}
		for l, list := range e.Avoid {
			if l != "" && !languageCode.MatchString(l) {
				continue
			}
			for _, v := range list {
				if v = strings.TrimSpace(v); v != "" && !strings.EqualFold(v, e.Term) {
					av[l] = append(av[l], excerpt(v, 100))
				}
			}
		}
		e.Avoid = av
		e.Note = excerpt(strings.TrimSpace(e.Note), 300)
		out = append(out, e)
	}
	slices.SortFunc(out, func(x, y GlossaryEntry) int { return strings.Compare(strings.ToLower(x.Term), strings.ToLower(y.Term)) })
	raw, _ := json.Marshal(out)
	if err := s.DB.SetSetting(ctx, glossaryKey(p.ID), string(raw), a.String()); err != nil {
		return nil, err
	}
	s.event(ctx, a, "project.glossary", p.ID, map[string]int{"terms": len(out)})
	return out, nil
}

var languageCode = regexp.MustCompile(`^[a-z]{2,3}(-[A-Za-z]{2,4})?$`)

// contains finds a term as a whole word (case-insensitive).
func containsTerm(text, term string) (string, bool) {
	re, err := regexp.Compile(`(?i)(^|[^\pL\pN])(` + regexp.QuoteMeta(term) + `)($|[^\pL\pN])`)
	if err != nil {
		return "", false
	}
	m := re.FindStringSubmatch(text)
	if m == nil {
		return "", false
	}
	return m[2], true
}

// glossaryFor returns the entries whose term occurs in a source text.
func glossaryFor(entries []GlossaryEntry, source string) []GlossaryEntry {
	out := []GlossaryEntry{}
	for _, e := range entries {
		if _, ok := containsTerm(source, e.Term); ok {
			out = append(out, e)
		}
	}
	return out
}

// checkGlossary compares a text in lang (main = the main language) with
// the glossary: spellings to avoid, and for translations the terms of the
// source that must appear in their translated form (or unchanged).
func checkGlossary(entries []GlossaryEntry, source, text, lang string, main bool) []GlossaryProblem {
	out := []GlossaryProblem{}
	avoidKey := lang
	if main {
		avoidKey = ""
	}
	for _, e := range entries {
		want := e.Term
		if !main && !e.Keep {
			want = e.Translations[lang]
		}
		for _, bad := range e.Avoid[avoidKey] {
			if found, ok := containsTerm(text, bad); ok {
				out = append(out, GlossaryProblem{Kind: "avoid", Term: e.Term, Expected: want, Found: found})
			}
		}
		if main || want == "" {
			continue
		}
		if _, inSource := containsTerm(source, e.Term); !inSource {
			continue
		}
		if _, ok := containsTerm(text, want); !ok {
			out = append(out, GlossaryProblem{Kind: "missing_term", Term: e.Term, Expected: want})
		}
	}
	return out
}

// GlossaryCheck checks a document (main language or a translation) against
// the project's glossary.
func (s *Service) GlossaryCheck(ctx context.Context, a Actor, ref, docRef, lang string) ([]GlossaryProblem, error) {
	p, doc, err := s.docAccess(ctx, a, ref, docRef, store.RoleReader)
	if err != nil {
		return nil, err
	}
	entries := s.glossaryOf(ctx, p.ID)
	if len(entries) == 0 {
		return []GlossaryProblem{}, nil
	}
	source := documentText(doc)
	if lang == "" || lang == p.MainLocale {
		return checkGlossary(entries, source, source, "", true), nil
	}
	t, err := s.DB.GetTranslation(ctx, doc.ID, lang)
	if err != nil {
		return nil, err
	}
	tr := doc
	tr.Content = t.Content
	return checkGlossary(entries, source, t.Title+"\n"+documentText(tr), lang, false), nil
}

// GlossaryForDocument returns the glossary terms that occur in a document
// (for translating it).
func (s *Service) GlossaryForDocument(ctx context.Context, a Actor, ref, docRef string) ([]GlossaryEntry, error) {
	p, doc, err := s.docAccess(ctx, a, ref, docRef, store.RoleReader)
	if err != nil {
		return nil, err
	}
	return glossaryFor(s.glossaryOf(ctx, p.ID), doc.Title+"\n"+documentText(doc)), nil
}
