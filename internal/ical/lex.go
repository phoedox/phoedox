package ical

import (
	"bytes"
	"strings"
)

// property is one content line: NAME;PARAM=a,b;PARAM2="x:y":value.
type property struct {
	name   string
	params map[string][]string
	value  string
}

// param returns the first value of the parameter name (upper case) or "".
func (p property) param(name string) string {
	if v := p.params[name]; len(v) > 0 {
		return v[0]
	}
	return ""
}

// unfold splits data into logical content lines. It accepts CRLF and bare
// LF line ends; a line starting with a space or a tab continues the
// previous one (RFC 5545 section 3.1).
func unfold(data []byte) []string {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")) // UTF-8 BOM
	var lines []string
	var cur []byte
	has := false
	for len(data) > 0 {
		var raw []byte
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			raw, data = data[:i], data[i+1:]
		} else {
			raw, data = data, nil
		}
		raw = bytes.TrimSuffix(raw, []byte("\r"))
		if len(raw) > 0 && (raw[0] == ' ' || raw[0] == '\t') {
			if has {
				cur = append(cur, raw[1:]...)
			}
			continue
		}
		if has {
			lines = append(lines, string(cur))
		}
		cur = append(cur[:0], raw...)
		has = true
	}
	if has {
		lines = append(lines, string(cur))
	}
	return lines
}

// parseLine parses one unfolded content line. Parameter values may be
// quoted (and then contain ";", ":" and ","); unquoted parameter values
// are split at ",".
func parseLine(line string) (property, bool) {
	i := strings.IndexAny(line, ";:")
	if i <= 0 {
		return property{}, false
	}
	p := property{name: strings.ToUpper(strings.TrimSpace(line[:i]))}
	rest := line[i:]
	for len(rest) > 0 && rest[0] == ';' {
		rest = rest[1:]
		eq := strings.IndexAny(rest, "=;:")
		if eq < 0 {
			return property{}, false
		}
		if rest[eq] != '=' { // parameter without a value: tolerate and skip
			rest = rest[eq:]
			continue
		}
		name := strings.ToUpper(strings.TrimSpace(rest[:eq]))
		rest = rest[eq+1:]
		for {
			var v string
			if len(rest) > 0 && rest[0] == '"' {
				end := strings.IndexByte(rest[1:], '"')
				if end < 0 {
					return property{}, false
				}
				v, rest = rest[1:1+end], rest[2+end:]
			} else {
				end := strings.IndexAny(rest, ",;:")
				if end < 0 {
					return property{}, false
				}
				v, rest = rest[:end], rest[end:]
			}
			if p.params == nil {
				p.params = map[string][]string{}
			}
			p.params[name] = append(p.params[name], v)
			if len(rest) > 0 && rest[0] == ',' {
				rest = rest[1:]
				continue
			}
			break
		}
	}
	if len(rest) == 0 || rest[0] != ':' {
		return property{}, false
	}
	p.value = rest[1:]
	return p, true
}

// unescapeText reverses TEXT escaping: \n and \N become a line feed, \\ \;
// \, become the character itself; any other escaped character is kept.
func unescapeText(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\\' && i+1 < len(s) {
			i++
			if s[i] == 'n' || s[i] == 'N' {
				b.WriteByte('\n')
			} else {
				b.WriteByte(s[i])
			}
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

// escapeText applies TEXT escaping for Encode.
func escapeText(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '\\':
			b.WriteString(`\\`)
		case ';':
			b.WriteString(`\;`)
		case ',':
			b.WriteString(`\,`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			if i+1 < len(s) && s[i+1] == '\n' {
				continue // CRLF counts as one line break
			}
			b.WriteString(`\n`)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// splitList splits a comma separated value list (EXDATE, RDATE, BYDAY …).
func splitList(v string) []string {
	parts := strings.Split(v, ",")
	out := parts[:0]
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
