package core

import (
	"regexp"
	"strings"
)

var (
	headingRe  = regexp.MustCompile(`^(#{1,6})\s+(.*?)\s*#*\s*$`)
	headingNum = regexp.MustCompile(`^([0-9][0-9.]*[.:)]?)\s+`)
)

// Section returns the part of a Markdown text under one heading, up to the
// next heading of the same or a higher level. ref matches the heading's
// text (case-insensitive), its leading number ("6.4", "§6.4") or the start
// of its text. Without a match it returns the outline (all headings).
func Section(md, ref string) (string, []string, bool) {
	ref = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(ref), "§")))
	lines := strings.Split(md, "\n")
	var outline []string
	start, level := -1, 0
	fence := false
	best := 0 // 3 exact, 2 number, 1 prefix
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			fence = !fence
		}
		if fence {
			continue
		}
		m := headingRe.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		text := m[2]
		outline = append(outline, m[1]+" "+text)
		low := strings.ToLower(strings.TrimPrefix(text, "§"))
		num, name := "", low
		if m := headingNum.FindStringSubmatch(low); m != nil {
			num, name = strings.TrimRight(m[1], ".:)"), strings.TrimSpace(low[len(m[0]):])
		}
		score := 0
		switch {
		case low == ref || name == ref:
			score = 3
		case num != "" && num == ref:
			score = 2
		case strings.HasPrefix(name, ref) || strings.HasPrefix(low, ref):
			score = 1
		}
		if score > best {
			best, start, level = score, i, len(m[1])
		}
	}
	if start < 0 {
		return "", outline, false
	}
	end := len(lines)
	fence = false
	for i := start + 1; i < len(lines); i++ {
		t := strings.TrimSpace(lines[i])
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			fence = !fence
		}
		if fence {
			continue
		}
		if m := headingRe.FindStringSubmatch(lines[i]); m != nil && len(m[1]) <= level {
			end = i
			break
		}
	}
	return strings.TrimRight(strings.Join(lines[start:end], "\n"), "\n") + "\n", outline, true
}
