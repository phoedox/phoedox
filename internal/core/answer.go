package core

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/phoedox/phoedox/internal/store"
	"github.com/phoedox/phoedox/resources"
)

// Short spoken answers for voice assistants such as Home Assistant Assist:
// "Wo steht das NAS?", "Welche IP hat die Fritzbox?", "Seriennummer vom
// Drucker?". Without an AI provider the answer comes from the entries
// (location, values) and otherwise from the best search hit. Secrets are
// never read out: they do not live in entries, and attributes whose names
// look like secrets are skipped as well.

// AnswerResult is a short answer and where it comes from.
type AnswerResult struct {
	Answer string `json:"answer"`
	// Found is false when nothing matched (Answer then says so).
	Found bool `json:"found"`
	// Source is the entry or page the answer comes from (path in the app).
	Source      string `json:"source,omitempty"`
	SourceTitle string `json:"sourceTitle,omitempty"`
}

var (
	secretAttr = regexp.MustCompile(`(?i)pass|secret|token|pin|key|kennwort|geheim`)
	whereWords = []string{"wo", "where", "standort", "location", "steht", "stehen", "hängt", "liegt", "ort"}
	tagRe      = regexp.MustCompile(`<[^>]+>|[*_#>` + "`" + `\[\]]`)
)

var answerTexts = map[string]map[string]string{
	"de": {"where": "%s ist hier: %s.", "attr": "%s von %s: %s.", "summary": "%s ist ein %s.", "summary_values": "%s ist ein %s. %s.",
		"page": "Laut der Seite „%s“: %s", "none": "Dazu habe ich in Phoedox nichts gefunden.", "nowhere": "Für %s ist kein Ort eingetragen."},
	"en": {"where": "%s is here: %s.", "attr": "%s of %s: %s.", "summary": "%s is a %s.", "summary_values": "%s is a %s. %s.",
		"page": "According to the page \"%s\": %s", "none": "I found nothing about that in Phoedox.", "nowhere": "No location is recorded for %s."},
}

// Answer answers a short question about a project.
func (s *Service) Answer(ctx context.Context, a Actor, ref, question, lang string) (AnswerResult, error) {
	p, err := s.project(ctx, a, ref, store.RoleReader)
	if err != nil {
		return AnswerResult{}, err
	}
	if lang != "de" {
		lang = "en"
	}
	tx := answerTexts[lang]
	q := " " + normalize(question) + " "
	if strings.TrimSpace(q) == "" {
		return AnswerResult{Answer: tx["none"]}, nil
	}
	labels := map[string]string{}
	_ = json.Unmarshal(resources.PublishLabels(lang), &labels)
	label := func(prefix, id string) string {
		if v, ok := labels[prefix+"."+id]; ok {
			return v
		}
		return strings.ReplaceAll(id, "_", " ")
	}

	ents, err := s.Entities(ctx, a, ref, "")
	if err != nil {
		return AnswerResult{}, err
	}
	// The entry whose name appears in the question; the longest name wins
	// ("Switch Keller" before "Switch").
	var best *store.Entity
	for i, e := range ents {
		n := normalize(e.Name)
		if len(n) < 2 || !strings.Contains(q, " "+n+" ") {
			continue
		}
		if best == nil || len(n) > len(normalize(best.Name)) {
			best = &ents[i]
		}
	}
	if best != nil {
		e := *best
		res := AnswerResult{Found: true, Source: "/p/" + p.Slug + "/e/" + e.Slug, SourceTitle: e.Name}
		m, _ := s.DataModel(ctx, a, ref)
		kind, _ := m.Find(e.Kind)
		attrs := map[string]any{}
		_ = json.Unmarshal(e.Attrs, &attrs)
		attrLabel := func(name string) string {
			for _, at := range kind.Attrs {
				if at.Name == name && at.Label != "" {
					return at.Label
				}
			}
			return label("attr", name)
		}
		value := func(name string) string {
			v := attrs[name]
			out := fmt.Sprint(v)
			if s, ok := v.(string); ok {
				out = s
			}
			for _, at := range kind.Attrs {
				if at.Name == name && at.Unit != "" {
					out += " " + at.Unit
				}
			}
			return out
		}
		// Asked for a value: the attribute whose name or label is in the question.
		names := make([]string, 0, len(attrs))
		for k := range attrs {
			names = append(names, k)
		}
		sort.Slice(names, func(i, j int) bool { return len(attrLabel(names[i])) > len(attrLabel(names[j])) })
		for _, k := range names {
			if secretAttr.MatchString(k) || fmt.Sprint(attrs[k]) == "" {
				continue
			}
			for _, w := range []string{normalize(attrLabel(k)), normalize(strings.ReplaceAll(k, "_", " "))} {
				if len(w) >= 2 && strings.Contains(q, " "+w+" ") || (w == "ip adresse" && strings.Contains(q, " ip ")) {
					res.Answer = fmt.Sprintf(tx["attr"], attrLabel(k), e.Name, value(k))
					return res, nil
				}
			}
		}
		// Asked where: follow located_in (and part_of) up to three steps.
		for _, w := range whereWords {
			if !strings.Contains(q, " "+w+" ") {
				continue
			}
			edges, err := s.DB.EdgesOfProject(ctx, p.ID)
			if err != nil {
				return res, err
			}
			var chain []string
			cur := e.ID
			for range 3 {
				next := ""
				for _, x := range edges {
					if x.From == cur && (x.Rel == "located_in" || x.Rel == "part_of" || x.Rel == "mounted_in") {
						chain = append(chain, x.ToName)
						next = x.To
						break
					}
				}
				if next == "" {
					break
				}
				cur = next
			}
			if len(chain) == 0 {
				res.Answer = fmt.Sprintf(tx["nowhere"], e.Name)
			} else {
				res.Answer = fmt.Sprintf(tx["where"], e.Name, strings.Join(chain, ", "))
			}
			return res, nil
		}
		// Otherwise a short summary with up to three values.
		var parts []string
		for _, at := range kind.Attrs {
			if v, ok := attrs[at.Name]; ok && fmt.Sprint(v) != "" && !secretAttr.MatchString(at.Name) && len(parts) < 3 {
				parts = append(parts, attrLabel(at.Name)+": "+value(at.Name))
			}
		}
		kindName := kind.Name
		if kindName == "" {
			kindName = label("kind", e.Kind)
		}
		if len(parts) == 0 {
			res.Answer = fmt.Sprintf(tx["summary"], e.Name, kindName)
		} else {
			res.Answer = fmt.Sprintf(tx["summary_values"], e.Name, kindName, strings.Join(parts, ", "))
		}
		return res, nil
	}

	// No entry named: the best page of the full-text search.
	hits, err := s.Search(ctx, a, question, ref, 5)
	if err != nil {
		return AnswerResult{}, err
	}
	for _, h := range hits {
		if h.Type != "document" || h.Snippet == "" {
			continue
		}
		snippet := strings.Join(strings.Fields(tagRe.ReplaceAllString(h.Snippet, "")), " ")
		if len([]rune(snippet)) > 220 {
			snippet = string([]rune(snippet)[:217]) + "…"
		}
		return AnswerResult{Found: true, Answer: fmt.Sprintf(tx["page"], h.Title, snippet), Source: "/p/" + p.Slug + "/d/" + h.Slug, SourceTitle: h.Title}, nil
	}
	return AnswerResult{Answer: tx["none"]}, nil
}

// normalize lowercases and keeps letters, digits and single spaces.
func normalize(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return ' '
	}, s)
	return strings.Join(strings.Fields(s), " ")
}
