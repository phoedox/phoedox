package core

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"github.com/phoedox/phoedox/internal/store"
)

// TextEdit replaces one exact passage of a text: Old must occur exactly
// once. It lets clients change long pages without sending them whole.
type TextEdit struct {
	Old string `json:"old"`
	New string `json:"new"`
	// Field of a decision: context, decision, consequences or alternatives
	// (empty for pages and links).
	Field string `json:"field,omitempty"`
}

// applyEdits applies edits in order; all or nothing.
func applyEdits(text string, edits []TextEdit) (string, error) {
	for i, e := range edits {
		n := i + 1
		if e.Old == "" {
			return "", invalid("validation.edit_empty", map[string]any{"n": n}, "edit %d: old must not be empty", n)
		}
		switch c := strings.Count(text, e.Old); c {
		case 1:
			text = strings.Replace(text, e.Old, e.New, 1)
		case 0:
			return "", invalid("validation.edit_not_found", map[string]any{"n": n}, "edit %d: old text not found (it must match exactly, including spaces and line breaks)", n)
		default:
			return "", invalid("validation.edit_ambiguous", map[string]any{"n": n, "count": c}, "edit %d: old text occurs %d times; include more context so it occurs once", n, c)
		}
	}
	return text, nil
}

// adrFields are the decision texts edits can target.
var adrFields = []string{"context", "decision", "consequences", "alternatives"}

// editField returns a pointer to the edited text of a decision.
func editField(adr *ADR, field string) (*string, error) {
	switch field {
	case "context":
		return &adr.Context, nil
	case "decision":
		return &adr.Decision, nil
	case "consequences":
		return &adr.Consequences, nil
	case "alternatives":
		return &adr.Alternatives, nil
	}
	return nil, invalid("validation.edit_field", map[string]any{"fields": strings.Join(adrFields, ", ")}, "decisions: field must be one of %s", strings.Join(adrFields, ", "))
}

// resolveEdits turns patch.Edits into full fields of the patch, based on
// the text the write applies to (the open draft of a page, if any).
func (s *Service) resolveEdits(ctx context.Context, cur store.Document, patch *DocumentPatch) error {
	if len(patch.Edits) == 0 {
		return nil
	}
	edits := patch.Edits
	patch.Edits = nil
	switch cur.Type {
	case store.DocMarkdown:
		if patch.Markdown != nil {
			return invalid("validation.edit_and_full", nil, "send either markdown or edits, not both")
		}
		var c struct {
			Markdown string `json:"markdown"`
		}
		_ = json.Unmarshal(cur.Content, &c)
		base := c.Markdown
		if d, err := s.DB.GetDraft(ctx, cur.ID); err == nil {
			base = d.Markdown
		} else if !errors.Is(err, store.ErrNotFound) {
			return err
		}
		md, err := applyEdits(base, edits)
		if err != nil {
			return err
		}
		patch.Markdown = &md
	case store.DocADR:
		if patch.ADR != nil {
			return invalid("validation.edit_and_full", nil, "send either the decision texts or edits, not both")
		}
		adr := ADR{}
		_ = json.Unmarshal(cur.Content, &adr)
		for i, e := range edits {
			f, err := editField(&adr, e.Field)
			if err != nil {
				return err
			}
			t, err := applyEdits(*f, []TextEdit{e})
			if err != nil {
				var ve *InvalidError
				if errors.As(err, &ve) {
					ve.Params["n"] = i + 1
				}
				return err
			}
			*f = t
		}
		patch.ADR = &adr
	case store.DocLink:
		if patch.Link != nil {
			return invalid("validation.edit_and_full", nil, "send either the description or edits, not both")
		}
		link := Link{}
		_ = json.Unmarshal(cur.Content, &link)
		d, err := applyEdits(link.Description, edits)
		if err != nil {
			return err
		}
		link.Description = d
		patch.Link = &link
	default:
		return invalid("validation.edit_type", nil, "edits work on pages, decisions and links")
	}
	return nil
}

// placeAfter makes room in a category right after another document and
// returns the position for the moved or new document. Positions of the
// following documents shift by one; that is not a content change, so it
// creates no versions.
func (s *Service) placeAfter(ctx context.Context, a Actor, p store.Project, category, afterRef, selfID string) (int, string, error) {
	after, err := s.findDoc(ctx, p.ID, afterRef)
	if err != nil {
		return 0, "", invalid("validation.after", map[string]any{"after": afterRef}, "document %q to insert after not found", afterRef)
	}
	if category == "\x00" {
		category = after.Category
	}
	if after.Category != category {
		return 0, "", invalid("validation.after_category", map[string]any{"after": after.Title}, "%q is in another category", after.Title)
	}
	docs, err := s.DB.ListDocuments(ctx, p.ID, store.DocFilter{Category: category})
	if err != nil {
		return 0, "", err
	}
	// The sidebar order; renumber it 1..n with a gap after the target.
	sort.SliceStable(docs, func(i, j int) bool { return docs[i].Position < docs[j].Position })
	pos, n := map[string]int{}, 0
	target := 0
	for _, d := range docs {
		if d.ID == selfID || d.ParentID != "" || d.Category != category {
			continue
		}
		n++
		pos[d.ID] = n
		if d.ID == after.ID {
			n++
			target = n
		}
	}
	changed := map[string]int{}
	for _, d := range docs {
		if v, ok := pos[d.ID]; ok && v != d.Position {
			changed[d.ID] = v
		}
	}
	if len(changed) > 0 {
		if err := s.DB.SetPositions(ctx, p.ID, changed); err != nil {
			return 0, "", err
		}
		s.event(ctx, a, "document.reorder", p.ID, map[string]any{"category": category, "count": len(changed)})
	}
	return target, category, nil
}
