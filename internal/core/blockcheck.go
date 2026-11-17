package core

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/phoedox/phoedox/internal/store"
)

// Checks for diagram blocks before people open the page: dbmodel blocks
// are parsed like the web does; Mermaid is checked for common mistakes
// (unknown diagram type, keywords as names, unbalanced blocks or brackets).
// The Mermaid check is not the real parser – the page view still shows
// what it misses.

// BlockProblem is one finding in a fenced block.
type BlockProblem struct {
	Block    int    `json:"block"` // 1-based number of the fenced block on the page
	Kind     string `json:"kind"`  // mermaid | dbmodel | chart
	Line     int    `json:"line"`  // line in the page (1-based)
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

// fencedBlock is a ```lang … ``` block of a Markdown text.
type fencedBlock struct {
	Lang string
	Body string
	Line int // line of the opening fence (1-based)
}

func fencedBlocks(md string) []fencedBlock {
	var out []fencedBlock
	lines := strings.Split(md, "\n")
	for i := 0; i < len(lines); i++ {
		t := strings.TrimLeft(lines[i], " ")
		if !strings.HasPrefix(t, "```") && !strings.HasPrefix(t, "~~~") {
			continue
		}
		fence := t[:3]
		n := len(t) - len(strings.TrimLeft(t, fence[:1]))
		lang := strings.Fields(strings.TrimSpace(t[n:]) + " ")
		b := fencedBlock{Line: i + 1}
		if len(lang) > 0 {
			b.Lang = strings.ToLower(lang[0])
		}
		var body []string
		j := i + 1
		for ; j < len(lines); j++ {
			c := strings.TrimLeft(lines[j], " ")
			if strings.HasPrefix(c, strings.Repeat(fence[:1], n)) && strings.TrimSpace(strings.TrimLeft(c, fence[:1])) == "" {
				break
			}
			body = append(body, lines[j])
		}
		b.Body = strings.Join(body, "\n")
		out = append(out, b)
		i = j
	}
	return out
}

// DDL parsing (the same rules as web/src/erd.ts).
var (
	ddlComment = regexp.MustCompile(`--[^\n]*|/\*[\s\S]*?\*/`)
	ddlTable   = regexp.MustCompile("(?i)create\\s+(?:temporary\\s+|temp\\s+)?table\\s+(?:if\\s+not\\s+exists\\s+)?([`\"\\[\\]\\w.]+)\\s*\\(")
	ddlRef     = regexp.MustCompile("(?i)\\breferences\\s+([`\"\\[\\]\\w.]+)")
	ddlShow    = regexp.MustCompile(`(?im)^\s*--\s*tables?\s*:\s*(.+)$`)
)

func ddlIdent(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndex(s, "."); i >= 0 {
		s = s[i+1:]
	}
	return strings.ToLower(strings.Trim(s, "`\"[]"))
}

// ddlTables returns the tables of SQL text with the tables they reference.
func ddlTables(sql string) map[string][]string {
	clean := ddlComment.ReplaceAllString(sql, "")
	out := map[string][]string{}
	locs := ddlTable.FindAllStringSubmatchIndex(clean, -1)
	for _, l := range locs {
		name := ddlIdent(clean[l[2]:l[3]])
		depth, i := 1, l[1]
		for ; i < len(clean) && depth > 0; i++ {
			switch clean[i] {
			case '(':
				depth++
			case ')':
				depth--
			}
		}
		refs := []string{}
		for _, m := range ddlRef.FindAllStringSubmatch(clean[l[1]:i], -1) {
			refs = append(refs, ddlIdent(m[1]))
		}
		out[name] = refs
	}
	return out
}

// dbShown are the tables a block takes from the project schema
// ("-- tables: users, sessions").
func dbShown(sql string) []string {
	var out []string
	for _, m := range ddlShow.FindAllStringSubmatch(sql, -1) {
		for _, n := range strings.Split(m[1], ",") {
			if n = ddlIdent(n); n != "" {
				out = append(out, n)
			}
		}
	}
	return out
}

var mermaidTypes = []string{"flowchart", "graph", "sequencediagram", "classdiagram", "statediagram", "statediagram-v2", "erdiagram", "journey", "gantt", "pie",
	"quadrantchart", "requirementdiagram", "gitgraph", "c4context", "c4container", "c4component", "c4dynamic", "c4deployment", "mindmap", "timeline",
	"sankey-beta", "sankey", "xychart-beta", "xychart", "block-beta", "block", "packet-beta", "packet", "kanban", "architecture-beta", "radar-beta", "zenuml", "treemap-beta"}

var (
	seqParticipant = regexp.MustCompile(`(?i)^\s*(?:participant|actor)\s+([^\s]+)`)
	seqMessage     = regexp.MustCompile(`^\s*([^\s:>-][^:>-]*?)\s*-{1,2}[>x)]{1,2}[+-]?\s*([^:]+?)\s*:`)
	seqKeywords    = map[string]bool{"end": true, "loop": true, "alt": true, "else": true, "opt": true, "par": true, "and": true, "rect": true, "critical": true,
		"break": true, "note": true, "activate": true, "deactivate": true, "autonumber": true, "box": true, "link": true, "links": true, "create": true, "destroy": true,
		"participant": true, "actor": true, "title": true, "off": true, "option": true}
	seqOpen    = regexp.MustCompile(`(?i)^\s*(loop|alt|opt|par|critical|break|rect|box)\b`)
	flowOpen   = regexp.MustCompile(`(?i)^\s*subgraph\b`)
	blockEnd   = regexp.MustCompile(`(?i)^\s*end\s*$`)
	flowEndID  = regexp.MustCompile(`(?:^|[\s;&>-])end(?:[\[({\s;&-]|$)`)
	mermaidCmt = regexp.MustCompile(`^\s*%%`)
	flowAsym   = regexp.MustCompile(`\w>[^\]]*\]`)
)

// checkMermaid returns problems of one Mermaid block (line numbers relative
// to the block body, 1-based).
func checkMermaid(body string) []BlockProblem {
	var out []BlockProblem
	add := func(line int, sev, msg string, args ...any) {
		out = append(out, BlockProblem{Kind: "mermaid", Line: line, Severity: sev, Message: fmt.Sprintf(msg, args...)})
	}
	lines := strings.Split(body, "\n")
	typ, first := "", 0
	inFront := false
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if t == "---" {
			inFront = !inFront
			continue
		}
		if inFront || t == "" || mermaidCmt.MatchString(t) {
			continue
		}
		typ, first = strings.ToLower(strings.Fields(t)[0]), i+1
		break
	}
	if typ == "" {
		add(1, "error", "the block is empty")
		return out
	}
	known := false
	for _, k := range mermaidTypes {
		if typ == k {
			known = true
		}
	}
	if !known {
		add(first, "error", "unknown diagram type %q; start with e.g. flowchart TD, sequenceDiagram, erDiagram, classDiagram, stateDiagram-v2, gantt, pie", typ)
		return out
	}
	depth := 0
	openAt := []int{}
	for i, l := range lines[first:] {
		n := first + i + 1
		t := strings.TrimSpace(l)
		if t == "" || mermaidCmt.MatchString(t) {
			continue
		}
		if strings.Count(t, `"`)%2 == 1 {
			add(n, "error", "unbalanced quotes")
		}
		switch typ {
		case "sequencediagram":
			if m := seqParticipant.FindStringSubmatch(t); m != nil && seqKeywords[strings.ToLower(m[1])] {
				add(n, "error", "%q is a Mermaid keyword and cannot be a participant name; use another name or an alias (participant P as %s)", m[1], m[1])
			}
			if m := seqMessage.FindStringSubmatch(t); m != nil {
				for _, p := range []string{m[1], m[2]} {
					if seqKeywords[strings.ToLower(strings.TrimSpace(p))] {
						add(n, "error", "%q is a Mermaid keyword and cannot be a participant name", strings.TrimSpace(p))
					}
				}
			}
			if seqOpen.MatchString(t) {
				depth++
				openAt = append(openAt, n)
			}
		case "flowchart", "graph":
			if flowOpen.MatchString(t) {
				depth++
				openAt = append(openAt, n)
			} else if !blockEnd.MatchString(t) && flowEndID.MatchString(t) && !strings.Contains(t, `"`) {
				add(n, "warning", "a node named \"end\" (lower case) breaks flowcharts; write End or put the text in quotes")
			}
			for _, pair := range [][2]string{{"[", "]"}, {"(", ")"}, {"{", "}"}} {
				open, closed := strings.Count(t, pair[0]), strings.Count(t, pair[1])
				if pair[0] == "[" && closed == open+1 && flowAsym.MatchString(t) {
					continue // asymmetric shape A>text]
				}
				if open != closed {
					add(n, "error", "unbalanced %s %s", pair[0], pair[1])
				}
			}
		}
		if blockEnd.MatchString(t) && (typ == "sequencediagram" || typ == "flowchart" || typ == "graph") {
			if depth == 0 {
				add(n, "error", "\"end\" without an open block")
			} else {
				depth--
				openAt = openAt[:len(openAt)-1]
			}
		}
	}
	for _, at := range openAt {
		add(at, "error", "block opened here is never closed with \"end\"")
	}
	return out
}

var repoFromLine = regexp.MustCompile(`(?i)^\s*from\s+[\w.-]+/[\w.-]+\s+\S+\s*$`)

func firstLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) != "" {
			return l
		}
	}
	return ""
}

// checkDBModel returns problems of one dbmodel block; schema are the tables
// of the whole project (all dbmodel blocks).
func checkDBModel(body string, schema map[string][]string) []BlockProblem {
	var out []BlockProblem
	// "from owner/repo path" reads the schema from a repository's migrations.
	if repoFromLine.MatchString(firstLine(body)) {
		return nil
	}
	own := ddlTables(body)
	shown := dbShown(body)
	if len(own) == 0 && len(shown) == 0 {
		return []BlockProblem{{Kind: "dbmodel", Line: 1, Severity: "error", Message: "no CREATE TABLE statement and no \"-- tables:\" line found"}}
	}
	for _, n := range shown {
		if _, ok := schema[n]; !ok {
			out = append(out, BlockProblem{Kind: "dbmodel", Line: 1, Severity: "error", Message: fmt.Sprintf("table %q is not defined in any dbmodel block of the project", n)})
		}
	}
	for t, refs := range own {
		for _, r := range refs {
			if _, ok := own[r]; ok {
				continue
			}
			if _, ok := schema[r]; !ok {
				out = append(out, BlockProblem{Kind: "dbmodel", Line: 1, Severity: "warning", Message: fmt.Sprintf("%s references %q, which no dbmodel block of the project defines; the relation is not drawn", t, r)})
			}
		}
	}
	return out
}

// CheckBlocks checks the diagram blocks of a Markdown text against the
// project's database schema.
func CheckBlocks(md string, schema map[string][]string) []BlockProblem {
	out := []BlockProblem{}
	for i, b := range fencedBlocks(md) {
		var ps []BlockProblem
		switch b.Lang {
		case "mermaid":
			ps = checkMermaid(b.Body)
		case "dbmodel":
			ps = checkDBModel(b.Body, schema)
		case "chart":
			ps = checkChart(b.Body)
		}
		for _, p := range ps {
			p.Block, p.Line = i+1, b.Line+p.Line
			out = append(out, p)
		}
	}
	return out
}

// DBSchemaBlock is one dbmodel block of a project.
type DBSchemaBlock struct {
	DocumentID string   `json:"documentId"`
	Slug       string   `json:"slug"`
	Title      string   `json:"title"`
	SQL        string   `json:"sql"`
	Tables     []string `json:"tables"`
}

// DBSchema returns all dbmodel blocks of the project's pages the caller
// can read: together they are the project's database schema, so a block
// can show tables defined on other pages.
func (s *Service) DBSchema(ctx context.Context, a Actor, ref string) ([]DBSchemaBlock, error) {
	docs, err := s.Documents(ctx, a, ref, store.DocFilter{Type: store.DocMarkdown})
	if err != nil {
		return nil, err
	}
	p, err := s.project(ctx, a, ref, store.RoleDoc)
	if err != nil {
		return nil, err
	}
	out := []DBSchemaBlock{}
	for _, d := range docs {
		full, err := s.DB.GetDocument(ctx, p.ID, d.ID)
		if err != nil {
			continue
		}
		var c struct {
			Markdown string `json:"markdown"`
		}
		_ = json.Unmarshal(full.Content, &c)
		if !strings.Contains(c.Markdown, "dbmodel") {
			continue
		}
		for _, b := range fencedBlocks(c.Markdown) {
			if b.Lang != "dbmodel" {
				continue
			}
			ts := ddlTables(b.Body)
			if len(ts) == 0 {
				continue
			}
			names := make([]string, 0, len(ts))
			for n := range ts {
				names = append(names, n)
			}
			out = append(out, DBSchemaBlock{DocumentID: d.ID, Slug: d.Slug, Title: d.Title, SQL: b.Body, Tables: names})
		}
	}
	return out, nil
}

// schemaTables merges the tables of all dbmodel blocks.
func schemaTables(blocks []DBSchemaBlock) map[string][]string {
	out := map[string][]string{}
	for _, b := range blocks {
		for t, refs := range ddlTables(b.SQL) {
			if len(refs) >= len(out[t]) {
				out[t] = refs
			}
		}
	}
	return out
}

// ValidateBlocks checks the diagram blocks of a text, or of a page (its
// open draft if there is one) when markdown is nil.
func (s *Service) ValidateBlocks(ctx context.Context, a Actor, ref, docRef string, markdown *string) ([]BlockProblem, error) {
	if markdown == nil {
		_, doc, err := s.docAccess(ctx, a, ref, docRef, store.RoleReader)
		if err != nil {
			return nil, err
		}
		var c struct {
			Markdown string `json:"markdown"`
		}
		_ = json.Unmarshal(doc.Content, &c)
		if d, err := s.DB.GetDraft(ctx, doc.ID); err == nil {
			c.Markdown = d.Markdown
		}
		markdown = &c.Markdown
	}
	if !strings.Contains(*markdown, "mermaid") && !strings.Contains(*markdown, "dbmodel") && !strings.Contains(*markdown, "chart") {
		return []BlockProblem{}, nil
	}
	var schema map[string][]string
	if strings.Contains(*markdown, "dbmodel") {
		blocks, err := s.DBSchema(ctx, a, ref)
		if err != nil {
			return nil, err
		}
		schema = schemaTables(blocks)
	}
	return CheckBlocks(*markdown, schema), nil
}
