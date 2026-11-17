package core

import (
	"context"
	"encoding/json"
	"reflect"

	"github.com/phoedox/phoedox/internal/store"
)

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
	s.boardChanged(a, docID, "", "board.update") // no-op unless someone watches it as a board
	// Publishing a draft ends the live session instead (editors must not
	// reload and reopen a draft).
	if ctx.Value(publishingKey{}) == docID {
		return
	}
	if s.OnContentChange != nil {
		s.OnContentChange(ctx, docID, a.User.Name, a.Via == "mcp")
	}
}

type publishingKey struct{}

// LiveDocument checks that the actor may edit a document live: editors in
// the browser, Markdown pages and floor plans, and only while a draft is open (live editing
// always works on the draft, ADR-0027).
func (s *Service) LiveDocument(ctx context.Context, a Actor, ref, docRef string) (store.Document, error) {
	doc, _, err := s.LiveSession(ctx, a, ref, docRef)
	return doc, err
}

// LiveSession is LiveDocument that also lets people who may only read
// watch a whiteboard live (readOnly). Whiteboards have no draft: the live
// session is the board itself (ADR-0068).
func (s *Service) LiveSession(ctx context.Context, a Actor, ref, docRef string) (store.Document, bool, error) {
	if a.Via != "web" {
		return store.Document{}, false, ErrForbidden
	}
	p, doc, err := s.docAccess(ctx, a, ref, docRef, store.RoleReader)
	if err != nil {
		return doc, false, err
	}
	if doc.Type == store.DocWhiteboard {
		editor := rank[p.Role] >= rank[store.RoleEditor] && !s.readOnly(a, p) && !s.needsApproval(ctx, p)
		return doc, !editor, nil
	}
	_, doc, err = s.docAccess(ctx, a, ref, docRef, store.RoleEditor)
	if err != nil {
		return doc, false, err
	}
	if !hasDrafts(doc.Type) {
		return doc, false, ErrForbidden
	}
	if _, err := s.DB.GetDraft(ctx, doc.ID); err != nil {
		return doc, false, err
	}
	return doc, false, nil
}
