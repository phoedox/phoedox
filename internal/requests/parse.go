// Package requests runs HTTP request blocks from documents (like Swagger's
// "try it out"): parse, fill {{variables}}, send with safety limits, check
// expectations and mask secrets in the result.
//
// Block format (Markdown code fence with the info string "request"):
//
//	GET {{base_url}}/api/status
//	Authorization: Bearer {{token}}
//
//	{"optional": "body"}
//
//	expect status == 200
//	expect $.state >= -70
package requests

import (
	"errors"
	"regexp"
	"strings"
)

// Request is a parsed block.
type Request struct {
	Method  string
	URL     string
	Headers [][2]string
	Body    string
	Expects []string
}

var (
	methodRe = regexp.MustCompile(`^(GET|HEAD|POST|PUT|PATCH|DELETE|OPTIONS)\s+(\S+)$`)
	headerRe = regexp.MustCompile(`^([A-Za-z0-9-]+):\s*(.*)$`)
	fenceRe  = regexp.MustCompile("(?m)^(`{3,}|~{3,})[ \t]*(request|http)\\b[^\n]*\n")
)

// ErrSyntax is returned for blocks that cannot be parsed.
var ErrSyntax = errors.New("requests: invalid request block")

// Parse reads a request block.
func Parse(src string) (Request, error) {
	lines := strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n")
	i := 0
	for i < len(lines) && (strings.TrimSpace(lines[i]) == "" || strings.HasPrefix(strings.TrimSpace(lines[i]), "#")) {
		i++
	}
	if i == len(lines) {
		return Request{}, ErrSyntax
	}
	m := methodRe.FindStringSubmatch(strings.TrimSpace(lines[i]))
	if m == nil {
		return Request{}, ErrSyntax
	}
	r := Request{Method: m[1], URL: m[2]}
	i++
	for ; i < len(lines); i++ {
		l := strings.TrimSpace(lines[i])
		if l == "" || strings.HasPrefix(l, "expect ") {
			break
		}
		h := headerRe.FindStringSubmatch(l)
		if h == nil {
			return Request{}, ErrSyntax
		}
		r.Headers = append(r.Headers, [2]string{h[1], h[2]})
	}
	var body []string
	for ; i < len(lines); i++ {
		l := strings.TrimSpace(lines[i])
		if strings.HasPrefix(l, "expect ") {
			r.Expects = append(r.Expects, strings.TrimSpace(strings.TrimPrefix(l, "expect ")))
			continue
		}
		if len(r.Expects) == 0 {
			body = append(body, lines[i])
		}
	}
	r.Body = strings.TrimSpace(strings.Join(body, "\n"))
	return r, nil
}

// Blocks returns the sources of all request blocks in Markdown, in order.
func Blocks(markdown string) []string {
	var out []string
	rest := markdown
	for {
		loc := fenceRe.FindStringSubmatchIndex(rest)
		if loc == nil {
			return out
		}
		fence := rest[loc[2]:loc[3]]
		body := rest[loc[1]:]
		end := strings.Index(body, "\n"+fence)
		if end < 0 {
			if strings.HasPrefix(body, fence) {
				end = 0
			} else {
				out = append(out, body)
				return out
			}
		}
		out = append(out, body[:end])
		rest = body[end+len(fence)+1:]
	}
}

var varRe = regexp.MustCompile(`\{\{\s*([A-Za-z_][A-Za-z0-9_]*)\s*\}\}`)

// Variables lists the {{names}} used in a request.
func (r Request) Variables() []string {
	seen := map[string]bool{}
	var out []string
	add := func(s string) {
		for _, m := range varRe.FindAllStringSubmatch(s, -1) {
			if !seen[m[1]] {
				seen[m[1]] = true
				out = append(out, m[1])
			}
		}
	}
	add(r.URL)
	for _, h := range r.Headers {
		add(h[1])
	}
	add(r.Body)
	return out
}

// Fill replaces {{names}} with values; unknown names are reported.
func (r Request) Fill(vars map[string]string) (Request, []string) {
	var missing []string
	sub := func(s string) string {
		return varRe.ReplaceAllStringFunc(s, func(m string) string {
			name := varRe.FindStringSubmatch(m)[1]
			v, ok := vars[name]
			if !ok {
				missing = append(missing, name)
				return m
			}
			return v
		})
	}
	out := r
	out.URL = sub(r.URL)
	out.Headers = make([][2]string, len(r.Headers))
	for i, h := range r.Headers {
		out.Headers[i] = [2]string{h[0], sub(h[1])}
	}
	out.Body = sub(r.Body)
	return out, missing
}
