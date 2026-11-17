package blocks

import (
	"fmt"
	"regexp"
	"strings"
)

// The interactive blocks (ADR-0085): polls, decision matrices, calculators
// and quizzes. Their line formats are read like web/src/blocks/*.ts does.

var (
	keyLine    = regexp.MustCompile(`^([A-Za-z]+)\s*:\s*(.*)$`)
	listLine   = regexp.MustCompile(`^[-*+]\s+(.+)$`)
	calcNumRe  = regexp.MustCompile(`^-?\d[\d ]*(?:[.,]\d+)?$`)
	calcEntRe  = regexp.MustCompile(`^@([a-z0-9-]+)\.([A-Za-z0-9_]+)$`)
	calcUnitRe = regexp.MustCompile(`^(.*?)\s*\(([^)]*)\)\s*$`)
	nonNameRe  = regexp.MustCompile(`[^\p{L}\p{N}_]+`)
	tokenRe    = regexp.MustCompile(`^(?:\s+|\d+(?:[.,]\d+)?|"[^"]*"|[\p{L}_][\p{L}\p{N}_]*|<=|>=|<>|[-+*/^&(),;=<>%])`)
	nameRe     = regexp.MustCompile(`^[\p{L}_][\p{L}\p{N}_]*$`)
	quizLine   = regexp.MustCompile(`^([?+\-=>])\s*(.*)$`)
	formulaFns = map[string]bool{"SUM": true, "AVERAGE": true, "AVG": true, "MIN": true, "MAX": true, "ROUND": true, "CEIL": true, "FLOOR": true, "ABS": true, "SQRT": true, "IF": true}
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

func parsePoll(src []byte) (question string, options []string, ps []Problem) {
	for _, l := range lines(src) {
		if k, v, ok := key(l, "question", "frage", "multiple", "closed", "results", "id"); ok {
			if k == "question" || k == "frage" {
				question = v
			}
			continue
		}
		if f := strings.ToLower(l); f == "multiple" || f == "closed" {
			continue // a flag on its own line
		}
		if m := listLine.FindStringSubmatch(l); m != nil {
			options = append(options, strings.TrimSpace(m[1]))
		} else if question == "" {
			question = l
		}
	}
	if question == "" {
		ps = append(ps, problem("the question is missing (question: …)"))
	}
	if len(options) < 2 {
		ps = append(ps, problem("a poll needs at least two answers (- …)"))
	}
	if len(options) > 30 {
		ps = append(ps, problem("at most 30 answers"))
	}
	seen := map[string]bool{}
	for _, o := range options {
		if seen[o] {
			ps = append(ps, problem("the answer %q is there twice", o))
			break
		}
		seen[o] = true
	}
	return
}

// --- calc -------------------------------------------------------------------------

// CalcName turns a label into the name formulas use: "Kosten pro Jahr (€)" → Kosten_pro_Jahr.
func CalcName(label string) string {
	if m := calcUnitRe.FindStringSubmatch(label); m != nil {
		label = m[1]
	}
	return strings.Trim(nonNameRe.ReplaceAllString(strings.TrimSpace(label), "_"), "_")
}

// formulaNames returns the names a formula uses, or an error when it cannot be read.
func formulaNames(f string) ([]string, error) {
	var names []string
	depth := 0
	for rest := f; rest != ""; {
		m := tokenRe.FindString(rest)
		if m == "" {
			return nil, fmt.Errorf("unexpected %q", rest[:1])
		}
		rest = rest[len(m):]
		switch {
		case m == "(":
			depth++
		case m == ")":
			depth--
			if depth < 0 {
				return nil, fmt.Errorf("unbalanced brackets")
			}
		case nameRe.MatchString(m):
			if formulaFns[strings.ToUpper(m)] && strings.HasPrefix(strings.TrimLeft(rest, " "), "(") {
				continue
			}
			names = append(names, m)
		}
	}
	if depth != 0 {
		return nil, fmt.Errorf("unbalanced brackets")
	}
	return names, nil
}

func checkCalc(src []byte) (names []string, ps []Problem) {
	deps := map[string][]string{}
	for _, l := range lines(src) {
		if _, _, ok := key(l, "title", "titel"); ok {
			continue
		}
		i := strings.Index(l, "=")
		if i <= 0 {
			ps = append(ps, problem("%q: a line needs label = value or formula", l))
			continue
		}
		name, expr := CalcName(l[:i]), strings.TrimSpace(l[i+1:])
		if name == "" || expr == "" {
			ps = append(ps, problem("%q: a line needs label = value or formula", l))
			continue
		}
		if _, dup := deps[name]; dup {
			ps = append(ps, problem("the name %q is there twice", name))
		}
		names = append(names, name)
		deps[name] = nil
		if calcNumRe.MatchString(expr) || calcEntRe.MatchString(expr) {
			continue
		}
		used, err := formulaNames(expr)
		if err != nil {
			ps = append(ps, problem("the formula of %q cannot be read: %v", name, err))
			continue
		}
		deps[name] = used
	}
	for _, n := range names {
		for _, u := range deps[n] {
			if _, ok := deps[u]; !ok {
				ps = append(ps, problem("%q uses %q, which does not exist (names are labels with _ for spaces, without the unit)", n, u))
			}
		}
	}
	if len(names) == 0 {
		ps = append(ps, problem("the calculator is empty"))
	}
	if len(ps) == 0 {
		// Cycles: a formula that (through others) uses itself.
		state := map[string]int{}
		var visit func(string) bool
		visit = func(n string) bool {
			if state[n] == 1 {
				return true
			}
			if state[n] == 2 {
				return false
			}
			state[n] = 1
			for _, u := range deps[n] {
				if visit(u) {
					return true
				}
			}
			state[n] = 2
			return false
		}
		for _, n := range names {
			if visit(n) {
				ps = append(ps, problem("%q depends on itself", n))
				break
			}
		}
	}
	return
}

// --- quiz -------------------------------------------------------------------------

func parseQuiz(src []byte) (questions []string, ps []Problem) {
	type q struct {
		text           string
		options, right int
		answer         bool
	}
	var qs []*q
	var cur *q
	for _, l := range lines(src) {
		if cur == nil {
			if _, _, ok := key(l, "title", "titel", "id"); ok {
				continue
			}
		}
		m := quizLine.FindStringSubmatch(l)
		if m == nil {
			ps = append(ps, problem("%q: lines start with ?, +, -, = or >", l))
			continue
		}
		if m[1] == "?" {
			cur = &q{text: m[2]}
			qs = append(qs, cur)
			continue
		}
		if cur == nil {
			ps = append(ps, problem("%q comes before the first question (? …)", l))
			continue
		}
		switch m[1] {
		case "+":
			cur.options++
			cur.right++
		case "-":
			cur.options++
		case "=":
			cur.answer = true
		}
	}
	if len(qs) == 0 {
		ps = append(ps, problem("a question is missing (? …)"))
	}
	for i, x := range qs {
		questions = append(questions, x.text)
		switch {
		case x.options == 0 && !x.answer:
			ps = append(ps, problem("question %d needs answers (+ right, - wrong) or a solution (= …)", i+1))
		case x.options > 0 && x.right == 0:
			ps = append(ps, problem("question %d has no right answer (+ …)", i+1))
		}
	}
	return
}

func init() {
	Register(Def{Kind: "poll", Board: true,
		Check: func(src []byte) []Problem { _, _, ps := parsePoll(src); return ps },
		Text: func(src []byte) string {
			q, o, _ := parsePoll(src)
			return strings.Join(append([]string{q}, o...), " ")
		},
	})
	Register(Def{Kind: "calc", Board: true,
		Check: func(src []byte) []Problem { _, ps := checkCalc(src); return ps },
		Text:  func(src []byte) string { return strings.Join(lines(src), " ") },
	})
	Register(Def{Kind: "quiz",
		Check: func(src []byte) []Problem { _, ps := parseQuiz(src); return ps },
		Text: func(src []byte) string {
			q, _ := parseQuiz(src)
			return strings.Join(q, " ")
		},
	})
}
