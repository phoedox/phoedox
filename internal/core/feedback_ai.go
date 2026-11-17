package core

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/phoedox/phoedox/internal/store"
	"github.com/phoedox/phoedox/resources"
)

// AI triage of feedback (feedback-concept, ADR-0077): a new report gets a
// summary, category and tags from the portal's lists, a priority with a
// reason, a suspected duplicate and a spam flag. It runs on the project's
// background AI; without it (switched off, limit reached) the rules alone
// sort the report as before.

type triageAnswer struct {
	Summary     string   `json:"summary"`
	Category    string   `json:"category"`
	Tags        []string `json:"tags"`
	Priority    int      `json:"priority"`
	Reason      string   `json:"reason"`
	DuplicateOf string   `json:"duplicate_of"`
	Spam        bool     `json:"spam"`
}

// parseTriage reads the model's JSON (also when wrapped in a code fence).
func parseTriage(text string) (triageAnswer, error) {
	var t triageAnswer
	text = strings.TrimSpace(text)
	if i := strings.Index(text, "{"); i >= 0 {
		if j := strings.LastIndex(text, "}"); j > i {
			text = text[i : j+1]
		}
	}
	err := json.Unmarshal([]byte(text), &t)
	return t, err
}

// triageReport asks the AI about a report and applies the answer to its topic.
func (s *Service) triageReport(ctx context.Context, p store.Project, portal store.FeedbackPortal, ps PortalSettings, topicID string, isNew bool, r store.FeedbackReport) {
	if !ps.AITriage {
		return
	}
	open, _ := s.DB.OpenFeedbackTopics(ctx, portal.ID, 500)
	type cand struct {
		id, title string
		score     float64
	}
	var cands []cand
	for _, t := range open {
		if t.ID != topicID {
			cands = append(cands, cand{t.ID, t.Title, similarity(t.Title, r.Title+" "+excerpt(r.Body, 200))})
		}
	}
	slices.SortFunc(cands, func(a, b cand) int {
		switch {
		case a.score > b.score:
			return -1
		case a.score < b.score:
			return 1
		}
		return 0
	})
	var b strings.Builder
	fmt.Fprintf(&b, "Categories: %s\nTags: %s\n\nOpen topics:\n", strings.Join(ps.Categories, ", "), strings.Join(ps.Tags, ", "))
	for _, c := range cands[:min(len(cands), 20)] {
		fmt.Fprintf(&b, "- %s: %s\n", c.id, c.title)
	}
	fields, _ := json.Marshal(r.Fields)
	fmt.Fprintf(&b, "\nNew report (type %s):\n<report>\nTitle: %s\n%s\nFields: %s\n</report>\n", r.Type, r.Title, excerpt(r.Body, 4000), fields)
	text, err := s.BackgroundAsk(ctx, p, "feedback-triage", []string{resources.Prompt("feedback-triage")}, b.String(), 800)
	if err != nil {
		if err != ErrNoBackgroundAI {
			slog.Warn("feedback triage", "portal", portal.Handle, "err", err)
		}
		return
	}
	ans, err := parseTriage(text)
	if err != nil {
		slog.Warn("feedback triage answer", "portal", portal.Handle, "err", err)
		return
	}
	t, err := s.DB.FeedbackTopic(ctx, p.ID, topicID)
	if err != nil {
		return
	}
	if isNew && strings.TrimSpace(ans.Summary) != "" {
		t.Summary = excerpt(strings.TrimSpace(ans.Summary), 600)
	}
	if t.Category == "" && slices.Contains(ps.Categories, ans.Category) {
		t.Category = ans.Category
	}
	for _, tag := range ans.Tags {
		if slices.Contains(ps.Tags, tag) && !slices.Contains(t.Tags, tag) && len(t.Tags) < 10 {
			t.Tags = append(t.Tags, tag)
		}
	}
	// Priority only rises with more reports; a new topic takes the AI's.
	if ans.Priority >= 1 && ans.Priority <= 4 && (isNew || ans.Priority < t.Priority) {
		t.Priority = ans.Priority
	}
	t.AINote = excerpt(strings.TrimSpace(ans.Reason), 300)
	if isNew && ans.DuplicateOf != "" && slices.ContainsFunc(cands, func(c cand) bool { return c.id == ans.DuplicateOf }) {
		t.DuplicateOf = ans.DuplicateOf
	}
	if isNew {
		t.Spam = ans.Spam
	}
	if err := s.DB.SaveFeedbackTopic(ctx, t); err != nil {
		return
	}
	s.event(ctx, Actor{Via: "system"}, "feedback.triage", t.ID, map[string]any{"priority": t.Priority, "category": t.Category, "spam": t.Spam, "duplicate": t.DuplicateOf != ""})
}
