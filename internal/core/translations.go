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

// Multilingual documents (ADR-0028): one document, a version in the main
// language and translations in the project's further languages. Number,
// slug, category, status, permissions, files and mentions belong to the
// document; a translation holds the title and the translatable text.

var localeRe = regexp.MustCompile(`^[a-z]{2,3}(-[A-Z]{2})?$`)

// TranslationInput is the text of a translation. Only the fields of the
// document's type are used: Markdown for pages, the ADR texts for
// decisions, Description for links.
type TranslationInput struct {
	Title        string  `json:"title"`
	Markdown     *string `json:"markdown,omitempty"`
	Context      *string `json:"context,omitempty"`
	Decision     *string `json:"decision,omitempty"`
	Consequences *string `json:"consequences,omitempty"`
	Alternatives *string `json:"alternatives,omitempty"`
	Description  *string `json:"description,omitempty"`
	// SupersedesScope translates which part of an older decision this one
	// replaces (partial supersede).
	SupersedesScope *string `json:"supersedesScope,omitempty"`
	// Machine marks an unreviewed machine translation. AI clients always
	// write machine translations; people clear the mark by saving.
	Machine bool `json:"machine,omitempty"`
	// Original: the text was written in this language by the user (e.g. an
	// import of German originals), not translated. It is never shown as
	// machine translation; the event records who said so.
	Original bool `json:"original,omitempty"`
	// Edits change exact passages of the existing translation (Field for
	// decisions) instead of sending the whole text.
	Edits []TextEdit `json:"edits,omitempty"`
	// keepSource: a find-and-replace change; the translation keeps the main
	// version it refers to and its review state.
	keepSource bool
}

// bodyGiven: the input brings translated text (not only the title).
func (in TranslationInput) bodyGiven() bool {
	return in.Markdown != nil || in.Context != nil || in.Decision != nil || in.Consequences != nil || in.Alternatives != nil ||
		in.Description != nil || in.SupersedesScope != nil || len(in.Edits) > 0
}

// mergeTranslation fills the fields the input leaves out (nil) from the
// existing translation and applies edits: a missing field keeps its text,
// only an explicit empty string clears it.
func mergeTranslation(typ string, old store.JSON, in *TranslationInput) error {
	var prev map[string]string
	_ = json.Unmarshal(old, &prev)
	keep := func(dst **string, key string) {
		if *dst == nil {
			if v, ok := prev[key]; ok {
				*dst = &v
			}
		}
	}
	keep(&in.Markdown, "markdown")
	keep(&in.Description, "description")
	keep(&in.Context, "context")
	keep(&in.Decision, "decision")
	keep(&in.Consequences, "consequences")
	keep(&in.Alternatives, "alternatives")
	keep(&in.SupersedesScope, "supersedesScope")
	for i, e := range in.Edits {
		var f **string
		switch typ {
		case store.DocMarkdown:
			f = &in.Markdown
		case store.DocADR:
			switch e.Field {
			case "context":
				f = &in.Context
			case "decision":
				f = &in.Decision
			case "consequences":
				f = &in.Consequences
			case "alternatives":
				f = &in.Alternatives
			default:
				return invalid("validation.edit_field", map[string]any{"fields": strings.Join(adrFields, ", ")}, "decisions: field must be one of %s", strings.Join(adrFields, ", "))
			}
		default:
			f = &in.Description
		}
		base := ""
		if *f != nil {
			base = **f
		}
		t, err := applyEdits(base, []TextEdit{e})
		if err != nil {
			var ie *InvalidError
			if errors.As(err, &ie) {
				ie.Params["n"] = i + 1
			}
			return err
		}
		*f = &t
	}
	in.Edits = nil
	return nil
}

// adrText are the translatable parts of a decision.
type adrText struct {
	Context      string `json:"context"`
	Decision     string `json:"decision"`
	Consequences string `json:"consequences,omitempty"`
	Alternatives string `json:"alternatives,omitempty"`
	// SupersedesScope is only translated when the decision has one.
	SupersedesScope string `json:"supersedesScope,omitempty"`
}

// normalizeLocales checks main and further languages of a project.
func normalizeLocales(main string, others []string) (string, []string, error) {
	main = strings.TrimSpace(main)
	out := []string{}
	if main != "" && !localeRe.MatchString(main) {
		return "", nil, invalid("validation.locale", map[string]any{"locale": main}, "invalid language code %q", main)
	}
	for _, l := range others {
		l = strings.TrimSpace(l)
		if l == "" || l == main || slices.Contains(out, l) {
			continue
		}
		if !localeRe.MatchString(l) {
			return "", nil, invalid("validation.locale", map[string]any{"locale": l}, "invalid language code %q", l)
		}
		out = append(out, l)
	}
	if main == "" && len(out) > 0 {
		return "", nil, invalid("validation.main_locale_required", nil, "set the main language before further languages")
	}
	return main, out, nil
}

func translatable(p store.Project, locale string) error {
	if p.MainLocale == "" || !slices.Contains(p.Locales, locale) {
		return invalid("validation.locale_not_in_project", map[string]any{"locale": locale}, "%q is not a further language of this project", locale)
	}
	return nil
}

// translationContent builds the stored text of a translation for a type.
func translationContent(typ string, in TranslationInput) (store.JSON, error) {
	str := func(p *string) string {
		if p == nil {
			return ""
		}
		return *p
	}
	var v any
	switch typ {
	case store.DocMarkdown:
		v = map[string]string{"markdown": str(in.Markdown)}
	case store.DocADR:
		// Proposed decisions may have no decision text yet.
		v = adrText{Context: str(in.Context), Decision: str(in.Decision), Consequences: str(in.Consequences), Alternatives: str(in.Alternatives),
			SupersedesScope: strings.TrimSpace(str(in.SupersedesScope))}
	case store.DocLink, store.DocBoard, store.DocFloorplan, store.DocWhiteboard:
		v = map[string]string{"description": str(in.Description)}
	default:
		return nil, invalid("validation.doc_type", nil, "unknown document type")
	}
	return json.Marshal(v)
}

// localize replaces title and text with a translation; the rest of the
// content (status, date, URL, …) stays from the main language.
func localize(doc *store.Document, t store.Translation) {
	doc.Title = t.Title
	base := map[string]any{}
	_ = json.Unmarshal(doc.Content, &base)
	over := map[string]any{}
	_ = json.Unmarshal(t.Content, &over)
	for k, v := range over {
		base[k] = v
	}
	doc.Content, _ = json.Marshal(base)
}

func outdated(doc store.Document, t store.Translation) bool { return t.SourceVersion < doc.Version }

// LocalizedDocument returns a document in a language: the translation if
// there is one (marked when the main language changed since), otherwise the
// main language. An empty locale returns the main language.
func (s *Service) LocalizedDocument(ctx context.Context, a Actor, ref, docRef, locale string) (store.Document, error) {
	p, doc, err := s.docAccess(ctx, a, ref, docRef, store.RoleReader)
	if err != nil {
		return doc, err
	}
	doc.Role = p.Role
	if p.MainLocale == "" {
		return doc, nil
	}
	doc.Locale = p.MainLocale
	list, err := s.DB.Translations(ctx, doc.ID)
	if err != nil {
		return doc, err
	}
	doc.Translations = list
	if locale == "" || locale == p.MainLocale || !slices.Contains(p.Locales, locale) {
		return doc, nil
	}
	t, err := s.DB.GetTranslation(ctx, doc.ID, locale)
	if errors.Is(err, store.ErrNotFound) {
		return doc, nil // main language; the client shows a hint
	}
	if err != nil {
		return doc, err
	}
	localize(&doc, t)
	doc.Locale = locale
	t.Content = nil
	doc.Translation = &t
	return doc, nil
}

// TranslationInfo is a translation with its state.
type TranslationInfo struct {
	store.Translation
	Outdated bool `json:"outdated"`
	// MainVersion is the current version of the main language.
	MainVersion int `json:"mainVersion"`
}

// Translation returns one translation with its text.
func (s *Service) Translation(ctx context.Context, a Actor, ref, docRef, locale string) (TranslationInfo, error) {
	p, doc, err := s.docAccess(ctx, a, ref, docRef, store.RoleReader)
	if err != nil {
		return TranslationInfo{}, err
	}
	if err := translatable(p, locale); err != nil {
		return TranslationInfo{}, err
	}
	t, err := s.DB.GetTranslation(ctx, doc.ID, locale)
	if err != nil {
		return TranslationInfo{}, err
	}
	return TranslationInfo{Translation: t, Outdated: outdated(doc, t), MainVersion: doc.Version}, nil
}

// SaveTranslation creates or replaces a translation. It refers to the
// current version of the main language, so it is up to date afterwards.
func (s *Service) SaveTranslation(ctx context.Context, a Actor, ref, docRef, locale string, in TranslationInput) (TranslationInfo, error) {
	t, doc, err := s.prepareTranslation(ctx, a, ref, docRef, locale, in)
	if err != nil {
		return TranslationInfo{}, err
	}
	if err := s.DB.SaveTranslation(ctx, t); err != nil {
		return TranslationInfo{}, err
	}
	s.event(ctx, a, "document.translation", doc.ID, map[string]any{"locale": locale, "machine": t.Machine, "original": t.Original, "sourceVersion": t.SourceVersion})
	s.indexDocument(ctx, doc)
	return TranslationInfo{Translation: t, Outdated: outdated(doc, t), MainVersion: doc.Version}, nil
}

// CheckTranslation validates a translation without saving it.
func (s *Service) CheckTranslation(ctx context.Context, a Actor, ref, docRef, locale string, in TranslationInput) error {
	_, _, err := s.prepareTranslation(ctx, a, ref, docRef, locale, in)
	return err
}

// prepareTranslation builds the translation to store.
func (s *Service) prepareTranslation(ctx context.Context, a Actor, ref, docRef, locale string, in TranslationInput) (store.Translation, store.Document, error) {
	fail := func(err error) (store.Translation, store.Document, error) {
		return store.Translation{}, store.Document{}, err
	}
	p, doc, err := s.docAccess(ctx, a, ref, docRef, store.RoleEditor)
	if err != nil {
		return fail(err)
	}
	if err := translatable(p, locale); err != nil {
		return fail(err)
	}
	prev, err := s.DB.GetTranslation(ctx, doc.ID, locale)
	exists := err == nil
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return fail(err)
	}
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" && exists {
		in.Title = prev.Title
	}
	if in.Title == "" {
		return fail(invalid("validation.title_required", nil, "title is required"))
	}
	if len(in.Edits) > 0 && !exists {
		return fail(invalid("validation.edit_no_translation", map[string]any{"locale": locale}, "there is no %s translation to edit yet; send the whole text", locale))
	}
	// The version of the main language this translation now matches: the
	// current one when text comes with it; for a title-only change, the
	// current one only if the main text did not change since the old
	// translation (otherwise it stays outdated).
	source := doc.Version
	if exists && in.keepSource {
		source = prev.SourceVersion
		in.Original = prev.Original
		in.Machine = prev.Machine
	}
	if exists && !in.keepSource {
		if !in.bodyGiven() {
			source = prev.SourceVersion
			if prev.SourceVersion < doc.Version {
				if v, err := s.DB.GetVersion(ctx, doc.ID, prev.SourceVersion); err == nil &&
					store.TranslatableText(doc.Type, "", v.Content) == store.TranslatableText(doc.Type, "", doc.Content) {
					source = doc.Version
				}
			}
			in.Original = in.Original || prev.Original
		}
	}
	if exists {
		if err := mergeTranslation(doc.Type, prev.Content, &in); err != nil {
			return fail(err)
		}
	}
	c, err := translationContent(doc.Type, in)
	if err != nil {
		return fail(err)
	}
	t := store.Translation{DocumentID: doc.ID, Locale: locale, Title: in.Title, Content: c, SourceVersion: source,
		Machine: !in.Original && (in.Machine || a.Via == "mcp"), Original: in.Original, UpdatedBy: a.String(), UpdatedAt: store.Now()}
	return t, doc, nil
}

// DeleteTranslation removes a translation.
func (s *Service) DeleteTranslation(ctx context.Context, a Actor, ref, docRef, locale string) error {
	_, doc, err := s.docAccess(ctx, a, ref, docRef, store.RoleEditor)
	if err != nil {
		return err
	}
	if err := s.DB.DeleteTranslation(ctx, doc.ID, locale); err != nil {
		return err
	}
	s.event(ctx, a, "document.translation_delete", doc.ID, map[string]any{"locale": locale})
	s.unindex(ctx, store.TranslationRef(doc.ID, locale))
	return nil
}

// TranslationStates tells for each document and further language whether
// its translation is "current", "outdated" or "missing" (nil when the
// project is not multilingual).
func (s *Service) TranslationStates(ctx context.Context, a Actor, ref string, docs []store.Document) (map[string]map[string]string, error) {
	p, err := s.project(ctx, a, ref, store.RoleDoc)
	if err != nil || p.MainLocale == "" || len(p.Locales) == 0 {
		return nil, err
	}
	all, err := s.DB.ProjectTranslations(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	out := map[string]map[string]string{}
	for _, d := range docs {
		st := map[string]string{}
		for _, l := range p.Locales {
			st[l] = "missing"
		}
		for _, t := range all[d.ID] {
			if _, ok := st[t.Locale]; !ok {
				continue
			}
			st[t.Locale] = "current"
			if t.SourceVersion < d.Version {
				st[t.Locale] = "outdated"
			}
		}
		out[d.ID] = st
	}
	return out, nil
}
