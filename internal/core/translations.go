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
	// Machine marks an unreviewed machine translation. AI clients always
	// write machine translations; people clear the mark by saving.
	Machine bool `json:"machine,omitempty"`
}

// adrText are the translatable parts of a decision.
type adrText struct {
	Context      string `json:"context"`
	Decision     string `json:"decision"`
	Consequences string `json:"consequences,omitempty"`
	Alternatives string `json:"alternatives,omitempty"`
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
		if strings.TrimSpace(str(in.Decision)) == "" {
			return nil, invalid("validation.adr_decision_required", nil, "decision is required")
		}
		v = adrText{Context: str(in.Context), Decision: str(in.Decision), Consequences: str(in.Consequences), Alternatives: str(in.Alternatives)}
	case store.DocLink, store.DocBoard:
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
	p, doc, err := s.docAccess(ctx, a, ref, docRef, store.RoleEditor)
	if err != nil {
		return TranslationInfo{}, err
	}
	if err := translatable(p, locale); err != nil {
		return TranslationInfo{}, err
	}
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" {
		return TranslationInfo{}, invalid("validation.title_required", nil, "title is required")
	}
	c, err := translationContent(doc.Type, in)
	if err != nil {
		return TranslationInfo{}, err
	}
	t := store.Translation{DocumentID: doc.ID, Locale: locale, Title: in.Title, Content: c, SourceVersion: doc.Version,
		Machine: in.Machine || a.Via == "mcp", UpdatedBy: a.String(), UpdatedAt: store.Now()}
	if err := s.DB.SaveTranslation(ctx, t); err != nil {
		return TranslationInfo{}, err
	}
	s.event(ctx, a, "document.translation", doc.ID, map[string]any{"locale": locale, "machine": t.Machine, "sourceVersion": t.SourceVersion})
	return TranslationInfo{Translation: t, MainVersion: doc.Version}, nil
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
	return nil
}
