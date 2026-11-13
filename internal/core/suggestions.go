package core

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/phoedox/phoedox/internal/store"
)

// opPayload is stored with a suggestion and replayed on accept.
type opPayload struct {
	Project   string         `json:"project"`
	Doc       string         `json:"doc,omitempty"`
	Create    *DocumentInput `json:"create,omitempty"`
	Patch     *DocumentPatch `json:"patch,omitempty"`
	Entity    *EntityInput   `json:"entity,omitempty"`
	EntityRef string         `json:"entityRef,omitempty"`
	Edge      *EdgeInput     `json:"edge,omitempty"`
}

// maybeSuggest stores a suggestion instead of applying the change when an
// AI client writes to a project in "suggest" mode.
func (s *Service) maybeSuggest(ctx context.Context, a Actor, p store.Project, op, summary string, payload opPayload) (*store.Suggestion, bool, error) {
	if a.Via != "mcp" || p.AIWriteMode != "suggest" {
		return nil, false, nil
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, true, err
	}
	sug, err := s.DB.CreateSuggestion(ctx, store.Suggestion{ID: newID(), ProjectID: p.ID, Actor: a.String(), Op: op, Summary: summary, Payload: b})
	if err != nil {
		return nil, true, err
	}
	s.event(ctx, a, "suggestion.create", sug.ID, map[string]string{"op": op})
	return &sug, true, nil
}

// Suggestions lists suggestions of a project (status "" = all).
func (s *Service) Suggestions(ctx context.Context, a Actor, ref, status string) ([]store.Suggestion, error) {
	p, err := s.project(ctx, a, ref, store.RoleReader)
	if err != nil {
		return nil, err
	}
	return s.DB.ListSuggestions(ctx, p.ID, status)
}

// DecideSuggestion accepts (applies) or rejects a suggestion. Editors only.
func (s *Service) DecideSuggestion(ctx context.Context, a Actor, id string, accept bool) (any, error) {
	sug, err := s.DB.GetSuggestion(ctx, id)
	if err != nil {
		return nil, err
	}
	p, err := s.project(ctx, a, sug.ProjectID, store.RoleEditor)
	if err != nil {
		return nil, err
	}
	if a.Via == "mcp" {
		return nil, ErrForbidden // AI clients cannot approve their own suggestions
	}
	if sug.Status != "pending" {
		return nil, invalid("validation.suggestion_decided", map[string]any{"status": sug.Status}, "suggestion is already %s", sug.Status)
	}
	if !accept {
		if err := s.DB.DecideSuggestion(ctx, id, "rejected", a.User.ID); err != nil {
			return nil, err
		}
		s.event(ctx, a, "suggestion.reject", id, nil)
		return sug, nil
	}
	var op opPayload
	if err := json.Unmarshal(sug.Payload, &op); err != nil {
		return nil, err
	}
	var res any
	switch sug.Op {
	case "document.create":
		res, err = s.createDocument(ctx, a, p, *op.Create)
	case "document.update":
		var cur store.Document
		if cur, err = s.DB.GetDocument(ctx, p.ID, op.Doc); err == nil {
			op.Patch.ExpectVersion = 0 // apply on top of the latest version
			res, err = s.updateDocument(ctx, a, p, cur, *op.Patch)
		}
	case "document.delete":
		if err = s.DB.DeleteDocument(ctx, op.Doc); err == nil {
			s.unindex(ctx, op.Doc)
			s.contentChanged(ctx, a, op.Doc)
		}
	case "entity.upsert":
		res, err = s.upsertEntity(ctx, a, p, *op.Entity)
	case "entity.delete":
		if err = s.DB.DeleteEntity(ctx, op.EntityRef); err == nil {
			s.unindex(ctx, op.EntityRef)
		}
	case "edge.link":
		res, err = s.link(ctx, a, *op.Edge)
	case "edge.unlink":
		err = s.DB.UnlinkEntities(ctx, op.Edge.From, op.Edge.Rel, op.Edge.To)
	default:
		err = fmt.Errorf("unknown suggestion op %q", sug.Op)
	}
	if err != nil {
		return nil, err
	}
	if err := s.DB.DecideSuggestion(ctx, id, "accepted", a.User.ID); err != nil {
		return nil, err
	}
	s.event(ctx, a, "suggestion.accept", id, map[string]string{"op": sug.Op})
	return res, nil
}
