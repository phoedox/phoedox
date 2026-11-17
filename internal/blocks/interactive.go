package blocks

import (
	"fmt"
	"regexp"
	"strings"
)

// Helpers to read block sources line by line. The interactive blocks
// (polls, quizzes, decision matrices, calculators) are plugins (ADR-0087).

var (
	keyLine = regexp.MustCompile(`^([A-Za-z]+)\s*:\s*(.*)$`)
)

func lines(src []byte) []string {
	var out []string
	for _, l := range strings.Split(strings.ReplaceAll(string(src), "\r\n", "\n"), "\n") {
		l = strings.TrimSpace(l)
		if l != "" && !strings.HasPrefix(l, "#") {
			out = append(out, l)
		}
	}
	return out
}

func key(l string, keys ...string) (string, string, bool) {
	m := keyLine.FindStringSubmatch(l)
	if m == nil {
		return "", "", false
	}
	k := strings.ToLower(m[1])
	for _, want := range keys {
		if k == want {
			return k, strings.TrimSpace(m[2]), true
		}
	}
	return "", "", false
}

func problem(f string, a ...any) Problem {
	return Problem{Line: 1, Severity: "error", Message: fmt.Sprintf(f, a...)}
}

// --- poll ---------------------------------------------------------------------
