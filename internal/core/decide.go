package core

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/phoedox/phoedox/internal/store"
)

// Deciding proposed decisions (ADR-0062): a proposed decision can offer
// options. People with edit rights pick one of them, or answer "nothing
// fits", each time with an optional comment. Picking an option accepts the
// decision; "nothing fits" keeps it proposed so whoever prepared it (often
// an AI client) can offer new options. AI clients never decide.

// DecideInput is an answer to a proposed decision.
type DecideInput struct {
	Choice        string `json:"choice"` // option id or "none"
	Comment       string `json:"comment,omitempty"`
	ExpectVersion int    `json:"expectVersion,omitempty"`
}

const maxDecideComment = 4000

// Decide answers a proposed decision.
func (s *Service) Decide(ctx context.Context, a Actor, ref, docRef string, in DecideInput) (DocResult, error) {
	if a.Via == "mcp" || a.Plugin != "" || a.Restricted() || a.User.ID == "" {
		return DocResult{}, ErrForbidden
	}
	p, cur, err := s.docAccess(ctx, a, ref, docRef, store.RoleEditor)
	if err != nil {
		return DocResult{}, err
	}
	if cur.Type != store.DocADR {
		return DocResult{}, invalid("validation.decide_not_adr", nil, "only decisions can be decided")
	}
	var adr ADR
	_ = json.Unmarshal(cur.Content, &adr)
	if adr.Status != "proposed" || len(adr.Options) == 0 {
		return DocResult{}, invalid("validation.decide_not_open", nil, "this decision has no open options")
	}
	comment := strings.TrimSpace(in.Comment)
	if len([]rune(comment)) > maxDecideComment {
		return DocResult{}, invalid("validation.decide_comment_long", map[string]any{"max": maxDecideComment}, "the comment is too long")
	}
	resp := ADRResponse{Choice: strings.TrimSpace(in.Choice), Comment: comment, By: a.User.Name, ByID: a.User.ID, At: store.Now()}
	next := adr
	if resp.Choice == ChoiceNone {
		// Stays open: new options are expected.
	} else {
		i := slices.IndexFunc(adr.Options, func(o ADROption) bool { return o.ID == resp.Choice })
		if i < 0 {
			return DocResult{}, invalid("validation.decide_choice", nil, "choose one of the options or none")
		}
		o := adr.Options[i]
		resp.Option = o.Title
		next.Status, next.Chosen = "accepted", o.ID
		if next.Date == "" || next.Date < time.Now().UTC().Format(time.DateOnly) {
			next.Date = time.Now().UTC().Format(time.DateOnly)
		}
		chosen := "**" + o.Title + "**"
		if o.Description != "" {
			chosen += "\n\n" + o.Description
		}
		if strings.TrimSpace(adr.Decision) == "" {
			next.Decision = chosen
		} else {
			next.Decision = chosen + "\n\n" + adr.Decision
		}
		// The other options become the considered alternatives.
		var alt []string
		for _, x := range adr.Options {
			if x.ID == o.ID || strings.Contains(adr.Alternatives, x.Title) {
				continue
			}
			line := "- **" + x.Title + "**"
			if x.Description != "" {
				line += ": " + strings.ReplaceAll(x.Description, "\n", " ")
			}
			alt = append(alt, line)
		}
		if len(alt) > 0 {
			next.Alternatives = strings.TrimSpace(strings.TrimSpace(adr.Alternatives) + "\n\n" + strings.Join(alt, "\n"))
		}
		if a.User.Name != "" && !slices.Contains(next.Deciders, a.User.Name) {
			next.Deciders = append(slices.Clone(next.Deciders), a.User.Name)
		}
	}
	// Responses and the choice are server-maintained, so they are written here
	// after the patch keeps the old ones.
	next.Responses = append(slices.Clone(adr.Responses), resp)
	b, _ := json.Marshal(next)
	doc := cur
	doc.Content = b
	doc, err = s.DB.UpdateDocument(ctx, doc, in.ExpectVersion, a.String())
	if err != nil {
		return DocResult{}, err
	}
	s.contentChanged(ctx, a, doc.ID)
	s.event(ctx, a, "decision.decide", doc.ID, map[string]any{"choice": resp.Choice, "option": resp.Option, "version": doc.Version})
	s.indexDocument(ctx, doc)
	if id := store.ActorUserID(cur.CreatedBy); id != "" && id != a.User.ID {
		if u, err := s.DB.UserByID(ctx, id); err == nil {
			body := resp.Option
			if resp.Choice == ChoiceNone {
				body = "–"
			}
			if comment != "" {
				body = fmt.Sprintf("%s · %s", body, comment)
			}
			s.notify(ctx, u, Note{Kind: "decision.answered", ProjectID: p.ID, Project: p.Name, Subject: doc.ID, Title: doc.Title, Body: body,
				Link: "/p/" + p.Slug + "/d/" + doc.Slug, Actor: a.User.Name})
		}
	}
	return DocResult{Applied: true, Document: &doc}, nil
}

// OpenOptions renders the options of a proposed decision as Markdown
// ("" when the decision is not open or has none).
func OpenOptions(content []byte) string {
	var adr ADR
	if json.Unmarshal(content, &adr) != nil || adr.Status != "proposed" || len(adr.Options) == 0 {
		return ""
	}
	var b strings.Builder
	for _, o := range adr.Options {
		fmt.Fprintf(&b, "### %s\n\n", o.Title)
		if o.Description != "" {
			b.WriteString(o.Description + "\n\n")
		}
	}
	return strings.TrimSpace(b.String())
}

// ResponsesMarkdown lists the answers to a decision ("" when there are none).
func ResponsesMarkdown(content []byte) string {
	var adr ADR
	if json.Unmarshal(content, &adr) != nil || len(adr.Responses) == 0 {
		return ""
	}
	var b strings.Builder
	for _, r := range adr.Responses {
		choice := "nothing fits"
		if r.Choice != ChoiceNone {
			choice = "chose " + r.Option
		}
		fmt.Fprintf(&b, "- %s, %s: %s", r.At.UTC().Format(time.DateOnly), r.By, choice)
		if r.Comment != "" {
			fmt.Fprintf(&b, " — %s", strings.ReplaceAll(r.Comment, "\n", " "))
		}
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}
