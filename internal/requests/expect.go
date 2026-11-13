package requests

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Check is the result of one expectation.
type Check struct {
	Expr   string `json:"expr"`
	OK     bool   `json:"ok"`
	Actual string `json:"actual,omitempty"`
	Error  string `json:"error,omitempty"` // invalid expectation
}

var exprRe = regexp.MustCompile(`^(status|time|header\s+[A-Za-z0-9-]+|\$[^\s]*)\s+(==|!=|<=|>=|<|>|contains|!exists|exists)\s*(.*)$`)

// Evaluate checks expectations against a response.
func Evaluate(exprs []string, status int, ms int64, header func(string) string, body []byte) []Check {
	var parsed any
	jsonOK := json.Unmarshal(body, &parsed) == nil
	out := make([]Check, 0, len(exprs))
	for _, e := range exprs {
		c := Check{Expr: e}
		m := exprRe.FindStringSubmatch(e)
		if m == nil {
			c.Error = "syntax"
			out = append(out, c)
			continue
		}
		subject, op, want := m[1], m[2], strings.TrimSpace(m[3])
		var actual any
		found := true
		switch {
		case subject == "status":
			actual = float64(status)
		case subject == "time":
			actual = float64(ms)
		case strings.HasPrefix(subject, "header"):
			name := strings.TrimSpace(strings.TrimPrefix(subject, "header"))
			v := header(name)
			found = v != ""
			actual = v
		default:
			if !jsonOK {
				found = false
			} else {
				actual, found = jsonPath(parsed, subject)
			}
		}
		if found {
			c.Actual = show(actual)
		}
		switch op {
		case "exists":
			c.OK = found
		case "!exists":
			c.OK = !found
		default:
			c.OK = found && compare(actual, op, want)
		}
		out = append(out, c)
	}
	return out
}

func show(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	default:
		b, _ := json.Marshal(x)
		s := string(b)
		if len(s) > 200 {
			s = s[:200] + "…"
		}
		return s
	}
}

// literal parses the right side: number, "string", true/false/null or bare text.
func literal(s string) any {
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f
	}
	if u, err := strconv.Unquote(s); err == nil && strings.HasPrefix(s, `"`) {
		return u
	}
	switch s {
	case "true":
		return true
	case "false":
		return false
	case "null":
		return nil
	}
	return s
}

func compare(actual any, op, raw string) bool {
	want := literal(raw)
	if op == "contains" {
		ws := fmt.Sprint(want)
		switch a := actual.(type) {
		case string:
			return strings.Contains(a, ws)
		case []any:
			for _, x := range a {
				if fmt.Sprint(x) == ws {
					return true
				}
			}
		}
		return false
	}
	af, aNum := actual.(float64)
	wf, wNum := want.(float64)
	if aNum && wNum {
		switch op {
		case "==":
			return af == wf
		case "!=":
			return af != wf
		case "<":
			return af < wf
		case "<=":
			return af <= wf
		case ">":
			return af > wf
		case ">=":
			return af >= wf
		}
	}
	eq := fmt.Sprint(actual) == fmt.Sprint(want)
	switch op {
	case "==":
		return eq
	case "!=":
		return !eq
	}
	return false
}

var pathPart = regexp.MustCompile(`\.([A-Za-z0-9_-]+)|\[(\d+)\]|\.(length)$`)

// jsonPath supports $.a.b[0].c and .length of arrays, strings and objects.
func jsonPath(root any, path string) (any, bool) {
	if !strings.HasPrefix(path, "$") {
		return nil, false
	}
	rest := path[1:]
	cur := root
	for rest != "" {
		m := pathPart.FindStringSubmatchIndex(rest)
		if m == nil || m[0] != 0 {
			return nil, false
		}
		switch {
		case m[2] >= 0:
			key := rest[m[2]:m[3]]
			if key == "length" && m[1] == len(rest) {
				switch c := cur.(type) {
				case []any:
					return float64(len(c)), true
				case string:
					return float64(len([]rune(c))), true
				case map[string]any:
					if v, ok := c["length"]; ok {
						return v, true
					}
					return float64(len(c)), true
				}
			}
			obj, ok := cur.(map[string]any)
			if !ok {
				return nil, false
			}
			if cur, ok = obj[key]; !ok {
				return nil, false
			}
		case m[4] >= 0:
			idx, _ := strconv.Atoi(rest[m[4]:m[5]])
			arr, ok := cur.([]any)
			if !ok || idx >= len(arr) {
				return nil, false
			}
			cur = arr[idx]
		}
		rest = rest[m[1]:]
	}
	return cur, true
}
