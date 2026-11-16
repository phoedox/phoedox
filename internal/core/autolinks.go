package core

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/phoedox/phoedox/internal/store"
)

// Auto-links (AI client feedback round 3): references in page text become
// links when a page is shown; the text itself stays unchanged.
//
// Built in: decision numbers (ADR-0012), documents' own numbers (ext refs
// such as D-640) and section numbers (§6.4 → the heading in the same page).
// Own rules per project: a pattern and where it points (an address with $0,
// $1 … or the document with that ext ref).

// AutoLinkRule is a project's own rule.
type AutoLinkRule struct {
	Pattern string `json:"pattern"`        // regular expression (RE2, also valid in JavaScript)
	To      string `json:"to"`             // url | ref
	URL     string `json:"url,omitempty"`  // https://… or /…, with $0 (the match) and $1…$9 (groups)
	Note    string `json:"note,omitempty"` // shown as the link's title
}

// AutoLinkRef is where a reference points.
type AutoLinkRef struct {
	Href  string `json:"href"`
	Title string `json:"title"`
}

// AutoLinks is what a page view needs to make the links.
type AutoLinks struct {
	Rules []AutoLinkRule `json:"rules"`
	// Refs maps decision numbers and ext refs to their documents.
	Refs map[string]AutoLinkRef `json:"refs"`
}

const maxAutoLinkRules = 30

var urlTemplateRe = regexp.MustCompile(`^(https://|/)[^\s"'<>]*$`)

func autoLinkKey(projectID string) string { return "autolinks:" + projectID }

func validAutoLinks(rules []AutoLinkRule) error {
	if len(rules) > maxAutoLinkRules {
		return invalid("validation.autolink_count", map[string]any{"max": maxAutoLinkRules}, "at most %d rules", maxAutoLinkRules)
	}
	for i, r := range rules {
		n := i + 1
		if strings.TrimSpace(r.Pattern) == "" || len(r.Pattern) > 200 {
			return invalid("validation.autolink_pattern", map[string]any{"n": n}, "rule %d: pattern missing or too long", n)
		}
		re, err := regexp.Compile(r.Pattern)
		// Patterns must be usable in browsers too: no named groups or
		// flags Go and JavaScript read differently.
		if err != nil || strings.Contains(r.Pattern, "(?P<") || strings.Contains(r.Pattern, "(?i") || re.MatchString("") {
			return invalid("validation.autolink_pattern", map[string]any{"n": n}, "rule %d: invalid pattern", n)
		}
		switch r.To {
		case "url":
			if !urlTemplateRe.MatchString(r.URL) {
				return invalid("validation.autolink_url", map[string]any{"n": n}, "rule %d: the address must start with https:// or /", n)
			}
		case "ref":
		default:
			return invalid("validation.autolink_to", map[string]any{"n": n}, "rule %d: to must be url or ref", n)
		}
	}
	return nil
}

func (s *Service) autoLinkRules(ctx context.Context, projectID string) []AutoLinkRule {
	raw, _ := s.DB.Setting(ctx, autoLinkKey(projectID), "")
	rules := []AutoLinkRule{}
	if raw != "" {
		_ = json.Unmarshal([]byte(raw), &rules)
	}
	return rules
}

// ProjectAutoLinks returns the rules and references of a project (readers).
func (s *Service) ProjectAutoLinks(ctx context.Context, a Actor, ref string) (AutoLinks, error) {
	p, err := s.project(ctx, a, ref, store.RoleDoc)
	if err != nil {
		return AutoLinks{}, err
	}
	out := AutoLinks{Rules: s.autoLinkRules(ctx, p.ID), Refs: map[string]AutoLinkRef{}}
	docs, err := s.Documents(ctx, a, p.Slug, store.DocFilter{})
	if err != nil {
		return out, err
	}
	for _, d := range docs {
		href := "/p/" + p.Slug + "/d/" + d.Slug
		if d.Type == store.DocADR && d.Number > 0 {
			out.Refs[fmt.Sprintf("ADR-%04d", d.Number)] = AutoLinkRef{Href: href, Title: d.Title}
		}
		if d.ExtRef != "" {
			out.Refs[d.ExtRef] = AutoLinkRef{Href: href, Title: d.Title}
		}
	}
	return out, nil
}

// SetAutoLinkRules replaces a project's own rules (owners).
func (s *Service) SetAutoLinkRules(ctx context.Context, a Actor, ref string, rules []AutoLinkRule) ([]AutoLinkRule, error) {
	p, err := s.project(ctx, a, ref, store.RoleOwner)
	if err != nil {
		return nil, err
	}
	if rules == nil {
		rules = []AutoLinkRule{}
	}
	for i := range rules {
		rules[i].Pattern = strings.TrimSpace(rules[i].Pattern)
		rules[i].Note = excerpt(strings.TrimSpace(rules[i].Note), 120)
	}
	if err := validAutoLinks(rules); err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(rules)
	if err := s.DB.SetSetting(ctx, autoLinkKey(p.ID), string(raw), a.String()); err != nil {
		return nil, err
	}
	s.event(ctx, a, "project.autolinks", p.ID, map[string]int{"rules": len(rules)})
	return rules, nil
}
