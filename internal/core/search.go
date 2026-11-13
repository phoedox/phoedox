package core

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"

	"github.com/phoedox/phoedox/internal/store"
)

// The search index holds title and plain text of documents and entities.
// It is updated after every write; failures are logged, never fatal, and
// `phx admin reindex` rebuilds it.

var (
	mdFence  = regexp.MustCompile("(?m)^```.*$")
	mdLink   = regexp.MustCompile(`!?\[([^\]]*)\]\([^)]*\)`)
	mdMarks  = regexp.MustCompile("(?m)^\\s{0,3}(#{1,6}|>|[-*+]|\\d+\\.)\\s+")
	mdInline = regexp.MustCompile("[*_`~]+")
)

// plainMarkdown strips Markdown syntax for searching and snippets.
func plainMarkdown(md string) string {
	md = mdFence.ReplaceAllString(md, "")
	md = mdLink.ReplaceAllString(md, "$1")
	md = mdMarks.ReplaceAllString(md, "")
	return mdInline.ReplaceAllString(md, "")
}

func documentText(doc store.Document) string {
	switch doc.Type {
	case store.DocMarkdown:
		var c struct {
			Markdown string `json:"markdown"`
		}
		_ = json.Unmarshal(doc.Content, &c)
		return plainMarkdown(c.Markdown)
	case store.DocADR:
		var a ADR
		_ = json.Unmarshal(doc.Content, &a)
		// Content first so snippets show it; the number keeps "ADR-0003" findable.
		var parts []string
		for _, md := range []string{a.Context, a.Decision, a.Consequences, a.Alternatives} {
			parts = append(parts, plainMarkdown(md))
		}
		parts = append(parts, strings.Join(a.Deciders, ", "), DecisionRef(doc.Number))
		return strings.Join(parts, "\n")
	case store.DocLink:
		var l Link
		_ = json.Unmarshal(doc.Content, &l)
		return l.URL + "\n" + plainMarkdown(l.Description)
	}
	return ""
}

func entityText(e store.Entity) string {
	var attrs map[string]any
	_ = json.Unmarshal(e.Attrs, &attrs)
	keys := make([]string, 0, len(attrs))
	for k := range attrs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	lines := []string{e.Kind, e.Slug}
	for _, k := range keys {
		lines = append(lines, fmt.Sprintf("%s: %v", k, attrs[k]))
	}
	return strings.Join(lines, "\n")
}

func (s *Service) indexDocument(ctx context.Context, doc store.Document) {
	if err := s.DB.IndexPut(ctx, store.IndexEntry{RefID: doc.ID, Kind: "document", ProjectID: doc.ProjectID, Slug: doc.Slug,
		Subtype: doc.Type, Title: doc.Title, Body: documentText(doc)}); err != nil {
		slog.Warn("search index update failed", "document", doc.ID, "err", err)
	}
}

func (s *Service) indexEntity(ctx context.Context, e store.Entity) {
	if err := s.DB.IndexPut(ctx, store.IndexEntry{RefID: e.ID, Kind: "entity", ProjectID: e.ProjectID, Slug: e.Slug,
		Subtype: e.Kind, Title: e.Name, Body: entityText(e)}); err != nil {
		slog.Warn("search index update failed", "entity", e.ID, "err", err)
	}
}

func (s *Service) unindex(ctx context.Context, id string) {
	if err := s.DB.IndexDelete(ctx, id); err != nil {
		slog.Warn("search index delete failed", "id", id, "err", err)
	}
}

// Reindex rebuilds the search index from all documents and entities.
func (s *Service) Reindex(ctx context.Context) (int, error) {
	docs, err := s.DB.AllDocuments(ctx)
	if err != nil {
		return 0, err
	}
	ents, err := s.DB.AllEntities(ctx)
	if err != nil {
		return 0, err
	}
	for _, d := range docs {
		s.indexDocument(ctx, d)
	}
	for _, e := range ents {
		s.indexEntity(ctx, e)
	}
	files, err := s.ReprocessFiles(ctx) // file text lives only in the index
	if err != nil {
		return 0, err
	}
	return len(docs) + len(ents) + files, nil
}

// EnsureSearchIndex fills an empty index (first start after the update).
func (s *Service) EnsureSearchIndex(ctx context.Context) error {
	n, err := s.DB.IndexCount(ctx)
	if err != nil || n > 0 {
		return err
	}
	count, err := s.Reindex(ctx)
	if err == nil && count > 0 {
		slog.Info("search index built", "entries", count)
	}
	return err
}
