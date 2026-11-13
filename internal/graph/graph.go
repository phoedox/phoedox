// Package graph contains the core knowledge graph types: entities and the
// typed edges between them. Every document type writes into this graph.
package graph

import (
	"crypto/rand"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
)

// ID is a ULID: stable, sortable and independent of the human readable slug.
type ID string

// NewID returns a new ULID based on the current time.
func NewID() ID {
	return ID(ulid.MustNew(ulid.Timestamp(time.Now()), rand.Reader).String())
}

// Source records where an edge or entity came from.
type Source string

const (
	SourceManual    Source = "manual"
	SourceText      Source = "text"  // extracted from a mention in a document
	SourceAgent     Source = "agent" // reported by phoedox-agent
	SourceHA        Source = "homeassistant"
	SourcePlugin    Source = "plugin"
	SourceAISuggest Source = "ai_suggestion" // unconfirmed until a human accepts it
)

// Entity is a node in the graph, e.g. a room, router, VM or table.
type Entity struct {
	ID        ID             `json:"id"`
	ProjectID ID             `json:"projectId"`
	Kind      string         `json:"kind"`
	Slug      string         `json:"slug"`
	Name      string         `json:"name"`
	Attrs     map[string]any `json:"attrs,omitempty"`
	Source    Source         `json:"source"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
	DeletedAt *time.Time     `json:"deletedAt,omitempty"` // trash, see retention policy
}

// Edge is a typed, directed relation such as located_in or uplink.
type Edge struct {
	ID     ID             `json:"id"`
	From   ID             `json:"from"`
	Rel    string         `json:"rel"`
	To     ID             `json:"to"`
	Attrs  map[string]any `json:"attrs,omitempty"` // e.g. {"pos": 4, "height": 2} for mounted_in
	Source Source         `json:"source"`
}

var slugRe = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// ErrInvalidSlug is returned for slugs that cannot be used in @mentions.
var ErrInvalidSlug = errors.New("graph: slug must be lowercase letters, digits and single hyphens")

// ValidateSlug checks that a slug can be used in mentions like @router-flur.
func ValidateSlug(s string) error {
	if !slugRe.MatchString(s) || len(s) > 64 {
		return ErrInvalidSlug
	}
	return nil
}

var umlauts = strings.NewReplacer("ä", "ae", "ö", "oe", "ü", "ue", "ß", "ss", "Ä", "ae", "Ö", "oe", "Ü", "ue")

// Slugify derives a slug from a display name, e.g. "Router Flur" -> "router-flur".
func Slugify(name string) string {
	name = strings.ToLower(umlauts.Replace(name))
	var b strings.Builder
	dash := false
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	s := strings.TrimSuffix(b.String(), "-")
	if len(s) > 64 {
		s = strings.TrimSuffix(s[:64], "-")
	}
	return s
}

// Mention is a parsed reference like @router-flur or @lindenstrasse/router-flur.
type Mention struct {
	Project string // empty means the current project
	Slug    string
}

// The mention must start the text or follow a character that cannot be part
// of an e-mail address, so "a@b.de" is not a mention.
var mentionRe = regexp.MustCompile(`(?:^|[^\w@./-])@(?:([a-z0-9-]+)/)?([a-z0-9]+(?:-[a-z0-9]+)*)`)

// ParseMentions finds all mentions in a text block. Fenced code blocks and
// inline code spans are skipped: "@x" in code is literal text.
func ParseMentions(text string) []Mention {
	var out []Mention
	for _, m := range mentionRe.FindAllStringSubmatch(StripCode(text), -1) {
		out = append(out, Mention{Project: m[1], Slug: m[2]})
	}
	return out
}

// StripCode replaces fenced code blocks and inline code spans with spaces.
func StripCode(text string) string {
	lines := strings.Split(text, "\n")
	fence := ""
	for i, l := range lines {
		t := strings.TrimLeft(l, " ")
		if fence != "" {
			if strings.HasPrefix(t, fence) && strings.TrimSpace(strings.TrimLeft(t, fence[:1])) == "" {
				fence = ""
			}
			lines[i] = ""
			continue
		}
		if len(l)-len(t) < 4 {
			for _, f := range []string{"```", "~~~"} {
				if strings.HasPrefix(t, f) {
					n := len(t) - len(strings.TrimLeft(t, f[:1]))
					fence = strings.Repeat(f[:1], n)
				}
			}
			if fence != "" {
				lines[i] = ""
				continue
			}
		}
		lines[i] = stripSpans(l)
	}
	return strings.Join(lines, "\n")
}

// stripSpans blanks inline code spans (a run of backticks up to the next
// run of the same length).
func stripSpans(l string) string {
	b := []byte(l)
	for i := 0; i < len(b); {
		if b[i] != '`' {
			i++
			continue
		}
		n := 0
		for i+n < len(b) && b[i+n] == '`' {
			n++
		}
		end := -1
		for j := i + n; j < len(b); {
			if b[j] != '`' {
				j++
				continue
			}
			m := 0
			for j+m < len(b) && b[j+m] == '`' {
				m++
			}
			if m == n {
				end = j + m
				break
			}
			j += m
		}
		if end < 0 {
			i += n
			continue
		}
		for k := i; k < end; k++ {
			b[k] = ' '
		}
		i = end
	}
	return string(b)
}
