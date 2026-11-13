package core

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"time"

	"github.com/phoedox/phoedox/internal/store"
)

// LiveWindow merges live saves of one person into one version.
const LiveWindow = 10 * time.Minute

// sameJSON compares JSON documents by value (Postgres normalizes jsonb).
func sameJSON(a, b []byte) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return string(a) == string(b)
	}
	return reflect.DeepEqual(x, y)
}

// contentChanged tells live editors to reload after a change made outside
// the live editor.
func (s *Service) contentChanged(ctx context.Context, a Actor, docID string) {
	if s.OnContentChange != nil {
		s.OnContentChange(ctx, docID, a.User.Name)
	}
}

// LiveDocument checks that the actor may edit a document live and returns it.
func (s *Service) LiveDocument(ctx context.Context, a Actor, ref, docRef string) (store.Document, error) {
	_, doc, err := s.docAccess(ctx, a, ref, docRef, store.RoleEditor)
	if err != nil {
		return doc, err
	}
	if doc.Type != store.DocMarkdown || a.Via != "web" {
		return doc, ErrForbidden
	}
	return doc, nil
}

// LiveSave stores the Markdown of the live editor. Saves within LiveWindow
// by the same person update one version.
func (s *Service) LiveSave(ctx context.Context, a Actor, ref, docRef, markdown string) (store.Document, error) {
	doc, err := s.LiveDocument(ctx, a, ref, docRef)
	if err != nil {
		return doc, err
	}
	p, _, err := s.docAccess(ctx, a, ref, docRef, store.RoleEditor)
	if err != nil {
		return doc, err
	}
	c, text, _ := content(store.DocMarkdown, strings.TrimRight(markdown, "\n")+"\n", nil, nil)
	if sameJSON(c, doc.Content) {
		// Someone else saved the shared state a moment ago: credit this
		// person as co-author of that live version.
		_ = s.DB.AddCoauthor(ctx, doc.ID, a.User.ID, LiveWindow)
		return doc, nil
	}
	doc.Content = c
	saved, created, err := s.DB.SaveLive(ctx, doc, a.String(), LiveWindow)
	if err != nil {
		return saved, err
	}
	s.linkMentions(ctx, p, saved.ID, text)
	s.indexDocument(ctx, saved)
	if created {
		s.event(ctx, a, "document.update", saved.ID, map[string]any{"version": saved.Version, "live": true})
	}
	return saved, nil
}
