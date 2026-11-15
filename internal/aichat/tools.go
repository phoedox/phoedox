package aichat

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/phoedox/phoedox/internal/ai"
	"github.com/phoedox/phoedox/internal/core"
	"github.com/phoedox/phoedox/internal/store"
)

var editPageTool = ai.Tool{
	Name: "edit_page",
	Description: "Change a text page live: the change goes into the page's draft (opened if nobody edits it), people editing the page see it at once, " +
		"and a person publishes the draft. Prefer `edits` (each `find` must occur exactly once in the current draft or page; read it first). " +
		"Send `markdown` only to replace the whole text. Only text pages (type markdown).",
	Schema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "project": {"type": "string", "description": "project slug"},
    "document": {"type": "string", "description": "page slug or id"},
    "edits": {"type": "array", "description": "replacements applied in order", "items": {
      "type": "object",
      "properties": {
        "find": {"type": "string", "description": "exact text in the page, unique"},
        "replace": {"type": "string", "description": "new text (empty removes it)"}
      },
      "required": ["find", "replace"]
    }},
    "markdown": {"type": "string", "description": "the whole new text instead of edits"},
    "title": {"type": "string", "description": "new title (optional)"}
  },
  "required": ["project", "document"]
}`),
}

type editInput struct {
	Project  string `json:"project"`
	Document string `json:"document"`
	Edits    []struct {
		Find    string `json:"find"`
		Replace string `json:"replace"`
	} `json:"edits"`
	Markdown *string `json:"markdown"`
	Title    *string `json:"title"`
}

func (s *Service) editPage(ctx context.Context, a core.Actor, raw json.RawMessage) (any, error) {
	var in editInput
	if err := json.Unmarshal(raw, &in); err != nil || in.Project == "" || in.Document == "" {
		return nil, core.Invalid("validation.ai_tool_input", nil, "project and document are required")
	}
	text, _, err := s.Core.DraftText(ctx, a, in.Project, in.Document)
	if err != nil {
		return nil, err
	}
	switch {
	case in.Markdown != nil:
		text = *in.Markdown
	case len(in.Edits) > 0:
		for i, e := range in.Edits {
			if e.Find == "" {
				return nil, core.Invalid("validation.ai_tool_input", nil, "edit %d: find is empty", i+1)
			}
			switch n := strings.Count(text, e.Find); n {
			case 1:
				text = strings.Replace(text, e.Find, e.Replace, 1)
			case 0:
				return nil, core.Invalid("validation.ai_tool_input", nil, "edit %d: the find text is not in the page; read it again (get_document shows the draft)", i+1)
			default:
				return nil, core.Invalid("validation.ai_tool_input", nil, "edit %d: the find text occurs %d times; add surrounding text so it is unique", i+1, n)
			}
		}
	case in.Title == nil:
		return nil, core.Invalid("validation.ai_tool_input", nil, "give edits, markdown or title")
	}
	var md *string
	if in.Markdown != nil || len(in.Edits) > 0 {
		md = &text
	}
	d, err := s.Core.AIDraftEdit(ctx, a, in.Project, in.Document, md, in.Title)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"applied": true, "draft": true, "document": in.Document,
		"url":  strings.TrimRight(s.Core.PublicURL, "/") + "/p/" + in.Project + "/d/" + firstNonEmpty(d.Slug, in.Document),
		"note": "Written into the draft. People editing the page see it now; a person reviews and publishes the draft.",
	}, nil
}

var proposeCardsTool = ai.Tool{
	Name: "propose_cards",
	Description: "Propose cards for a task board, e.g. the next steps of a concept. The person sees the cards in the chat and chooses which to create; " +
		"nothing is created by this call. `documents` are pages and decisions linked to every card (slugs or ADR numbers).",
	Schema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "project": {"type": "string", "description": "project slug"},
    "board": {"type": "string", "description": "board slug or id"},
    "column": {"type": "string", "description": "column id or name; default: the first column"},
    "documents": {"type": "array", "items": {"type": "string"}, "description": "pages and decisions to link to each card"},
    "cards": {"type": "array", "minItems": 1, "maxItems": 30, "items": {
      "type": "object",
      "properties": {
        "title": {"type": "string"},
        "description": {"type": "string", "description": "Markdown"},
        "labels": {"type": "array", "items": {"type": "string"}},
        "priority": {"type": "integer", "description": "0 none, 1 low, 2 medium, 3 high"},
        "due": {"type": "string", "description": "YYYY-MM-DD"}
      },
      "required": ["title"]
    }}
  },
  "required": ["project", "board", "cards"]
}`),
}

// Proposal is the input of propose_cards.
type Proposal struct {
	Project   string   `json:"project"`
	Board     string   `json:"board"`
	Column    string   `json:"column"`
	Documents []string `json:"documents"`
	Cards     []struct {
		Title       string   `json:"title"`
		Description string   `json:"description"`
		Labels      []string `json:"labels"`
		Priority    int      `json:"priority"`
		Due         string   `json:"due"`
	} `json:"cards"`
}

func (s *Service) proposeCards(ctx context.Context, a core.Actor, raw json.RawMessage) (any, error) {
	var in Proposal
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, core.Invalid("validation.ai_tool_input", nil, "invalid proposal: %v", err)
	}
	if len(in.Cards) == 0 || len(in.Cards) > 30 {
		return nil, core.Invalid("validation.ai_tool_input", nil, "propose 1 to 30 cards")
	}
	doc, err := s.Core.Document(ctx, a, in.Project, in.Board)
	if err != nil {
		return nil, err
	}
	if doc.Type != store.DocBoard {
		return nil, core.Invalid("validation.ai_tool_input", nil, "%s is not a board", in.Board)
	}
	for i, c := range in.Cards {
		if strings.TrimSpace(c.Title) == "" {
			return nil, core.Invalid("validation.ai_tool_input", nil, "card %d has no title", i+1)
		}
	}
	for _, d := range in.Documents {
		if _, err := s.Core.Document(ctx, a, in.Project, d); err != nil {
			return nil, core.Invalid("validation.ai_tool_input", nil, "unknown document %q", d)
		}
	}
	return map[string]any{
		"shown": true, "cards": len(in.Cards), "board": doc.Title,
		"note": "The proposal is shown to the person. Nothing is created until they confirm; you will be told what they chose.",
	}, nil
}

// Decide applies (accept) or rejects a card proposal of the model: the
// person chooses the cards by their position in the proposal (empty = all).
// Accepted cards are created by the person, not by the AI.
func (s *Service) Decide(ctx context.Context, a core.Actor, ref, chatID, toolUseID string, accept bool, pick []int) ([]store.Task, error) {
	_, c, err := s.chat(ctx, a, ref, chatID)
	if err != nil {
		return nil, err
	}
	msgs, err := s.Core.DB.AIChatMessages(ctx, c.ID)
	if err != nil {
		return nil, err
	}
	var prop *Proposal
	for _, m := range msgs {
		if m.Role != "assistant" {
			continue
		}
		for _, call := range ai.OpenCalls(m.Content) {
			if call.ID == toolUseID && call.Name == proposeCardsTool.Name {
				var p Proposal
				if json.Unmarshal(call.Input, &p) == nil {
					prop = &p
				}
			}
		}
	}
	if prop == nil {
		return nil, store.ErrNotFound
	}
	rec := store.AIProposal{ToolUseID: toolUseID, Status: "rejected", DecidedBy: a.String(), DecidedAt: store.Now()}
	var created []store.Task
	if accept {
		chosen := map[int]bool{}
		for _, i := range pick {
			chosen[i] = true
		}
		// Check every card first, so a bad card creates none.
		for i, card := range prop.Cards {
			if (len(chosen) == 0 || chosen[i]) && strings.TrimSpace(card.Title) == "" {
				return nil, core.Invalid("validation.title_required", nil, "title is required")
			}
		}
		for i, card := range prop.Cards {
			if len(chosen) > 0 && !chosen[i] {
				continue
			}
			t, err := s.Core.CreateTask(ctx, a, ref, prop.Board, core.TaskInput{Title: card.Title, Description: card.Description, Column: prop.Column,
				Labels: card.Labels, Priority: min(max(card.Priority, 0), 3), Due: card.Due, Documents: prop.Documents})
			if err != nil {
				if len(created) == 0 {
					return nil, err
				}
				break
			}
			created = append(created, t)
		}
		rec.Status = "accepted"
		summary := make([]map[string]string, 0, len(created))
		for _, t := range created {
			summary = append(summary, map[string]string{"id": t.ID, "title": t.Title})
		}
		rec.Result, _ = json.Marshal(summary)
	}
	if err := s.Core.DB.SaveAIProposal(ctx, c.ID, rec); err != nil {
		return created, err
	}
	return created, nil
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}
