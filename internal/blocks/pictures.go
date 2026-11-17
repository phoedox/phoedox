package blocks

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Mind maps, schedules (Gantt), before/after images and annotated images
// (ADR-0085), read like web/src/blocks/{mindmap,gantt,beforeafter,annotate}.ts.

var (
	ganttLine   = regexp.MustCompile(`^(.+?)(?:\s*\[([A-Za-z0-9-]+)\])?\s*:\s*(.+)$`)
	ganttRange  = regexp.MustCompile(`\s+(?:–|—|-|\.\.|bis|to)\s+(\d{4}-)`)
	ganttDur    = regexp.MustCompile(`(?i)^\d+(?:[.,]\d+)?\s*[dtwm]$`)
	ganttPct    = regexp.MustCompile(`^\d{1,3}\s*%$`)
	ganttDone   = regexp.MustCompile(`(?i)^(done|fertig|erledigt)$`)
	ganttAfter  = regexp.MustCompile(`(?i)^(?:after|nach)\s+(.+)$`)
	ganttSec    = regexp.MustCompile(`(?i)^(?:section|abschnitt)\s+(.+)$`)
	imageFile   = regexp.MustCompile(`^/api/v1/files/[A-Za-z0-9]+(/[^\s"'<>]*)?$`)
	imageID     = regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{26}$`)
	imageHTTPS  = regexp.MustCompile(`^https://[^\s"'<>]+$`)
	mdImage     = regexp.MustCompile(`^!\[([^\]]*)\]\(([^)\s]+)\)$`)
	markerLine  = regexp.MustCompile(`(?i)^[-*+]?\s*(\d+(?:[.,]\d+)?)\s*%?\s*[,;]\s*(\d+(?:[.,]\d+)?)\s*%?(?:\s+([a-z]+))?\s*:\s*(.*)$`)
	ganttHead   = regexp.MustCompile(`(?m)^\s*##\s+`)
	blockColors = map[string]bool{"gray": true, "red": true, "orange": true, "yellow": true, "green": true, "blue": true, "purple": true, "pink": true}
)

// --- mind map ---------------------------------------------------------------------

func mindmapItems(src []byte) []string {
	var out []string
	for _, l := range strings.Split(strings.ReplaceAll(string(src), "\r\n", "\n"), "\n") {
		t := strings.TrimSpace(l)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		out = append(out, strings.TrimSpace(listLine.ReplaceAllString(t, "$1")))
	}
	return out
}

func checkMindmap(src []byte) []Problem {
	items := mindmapItems(src)
	if len(items) == 0 {
		return []Problem{problem("the mind map is empty")}
	}
	if len(items) > 400 {
		return []Problem{problem("at most 400 items")}
	}
	return nil
}

// --- schedule ----------------------------------------------------------------------

// ganttLines reads "## Section" as "section Section" (lines() drops # comments).
func ganttLines(src []byte) []string {
	return lines(ganttHead.ReplaceAll(src, []byte("section ")))
}

func ganttDay(s string) bool {
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}

func checkGantt(src []byte) []Problem {
	var ps []Problem
	type item struct {
		name  string
		after []string
	}
	var items []item
	ids := map[string]bool{}
	board := false
	for _, l := range ganttLines(src) {
		if k, _, ok := key(l, "title", "titel", "board"); ok {
			board = board || k == "board"
			continue
		}
		if strings.EqualFold(l, "today") || strings.EqualFold(l, "heute") || ganttSec.MatchString(l) {
			continue
		}
		m := ganttLine.FindStringSubmatch(l)
		if m == nil {
			ps = append(ps, problem("unknown line %q (expected \"Name: start, duration\")", l))
			continue
		}
		it := item{name: strings.TrimSpace(m[1])}
		if m[2] != "" {
			ids[m[2]] = true
		}
		parts := strings.Split(ganttRange.ReplaceAllString(m[3], ", $1"), ",")
		first := strings.TrimSpace(parts[0])
		if a := ganttAfter.FindStringSubmatch(first); a != nil {
			it.after = strings.Fields(a[1])
		} else if !ganttDay(first) {
			ps = append(ps, problem("%q: %q is neither a date (YYYY-MM-DD) nor \"after id\"", it.name, first))
			continue
		}
		end := ""
		for _, p := range parts[1:] {
			p = strings.TrimSpace(p)
			switch {
			case p == "":
			case ganttDay(p):
				end = p
			case ganttDur.MatchString(p), ganttPct.MatchString(p), ganttDone.MatchString(p), blockColors[strings.ToLower(p)]:
			default:
				ps = append(ps, problem("%q: %q is not a date, a duration (3d, 2w), done, a percentage or a colour", it.name, p))
			}
		}
		if end != "" && it.after == nil && end < first {
			ps = append(ps, problem("%q ends before it starts", it.name))
		}
		items = append(items, it)
	}
	for _, it := range items {
		for _, a := range it.after {
			if !ids[a] {
				ps = append(ps, problem("%q follows %q, but there is no such id", it.name, a))
			}
		}
	}
	if len(items) == 0 && !board && len(ps) == 0 {
		ps = append(ps, problem("the schedule has no items"))
	}
	return ps
}

// --- images ---------------------------------------------------------------------------

func imageOK(v string) bool {
	f := strings.Fields(v)
	if len(f) == 0 {
		return true // not chosen yet
	}
	return imageFile.MatchString(f[0]) || imageID.MatchString(f[0]) || imageHTTPS.MatchString(f[0])
}

func checkBeforeAfter(src []byte) []Problem {
	var ps []Problem
	for _, l := range lines(src) {
		if k, v, ok := key(l, "title", "titel", "before", "vorher", "after", "nachher", "start"); ok {
			if k != "title" && k != "titel" && k != "start" && !imageOK(v) {
				ps = append(ps, problem("%q is not an image: a file of the project (/api/v1/files/…) or an https address", v))
			}
			continue
		}
		if m := mdImage.FindStringSubmatch(l); m != nil {
			if !imageOK(m[2]) {
				ps = append(ps, problem("%q is not an image: a file of the project (/api/v1/files/…) or an https address", m[2]))
			}
			continue
		}
		ps = append(ps, problem("unknown line %q", l))
	}
	return ps
}

func parseAnnotate(src []byte) (title string, texts []string, ps []Problem) {
	image := false
	for _, l := range lines(src) {
		if k, v, ok := key(l, "title", "titel", "image", "bild"); ok {
			if k == "title" || k == "titel" {
				title = v
			} else if v != "" {
				image = true
				if !imageOK(v) {
					ps = append(ps, problem("%q is not an image: a file of the project (/api/v1/files/…) or an https address", v))
				}
			}
			continue
		}
		m := markerLine.FindStringSubmatch(l)
		if m == nil {
			ps = append(ps, problem("unknown line %q (expected \"- x, y: text\")", l))
			continue
		}
		x, _ := strconv.ParseFloat(strings.Replace(m[1], ",", ".", 1), 64)
		y, _ := strconv.ParseFloat(strings.Replace(m[2], ",", ".", 1), 64)
		if x > 100 || y > 100 {
			ps = append(ps, problem("positions are percent from 0 to 100: %q", l))
			continue
		}
		if c := strings.ToLower(m[3]); c != "" && !blockColors[c] {
			ps = append(ps, problem("unknown colour %q", c))
			continue
		}
		texts = append(texts, strings.TrimSpace(m[4]))
	}
	if !image && len(texts) == 0 && len(ps) == 0 {
		ps = append(ps, problem("an image is missing (image: …)"))
	}
	return title, texts, ps
}

func init() {
	Register(Def{Kind: "mindmap", Board: true, Check: checkMindmap,
		Text: func(src []byte) string { return strings.Join(mindmapItems(src), " ") }})
	Register(Def{Kind: "gantt", Board: true, Check: checkGantt,
		Text: func(src []byte) string {
			var out []string
			for _, l := range ganttLines(src) {
				if k, v, ok := key(l, "title", "titel", "board"); ok {
					if k != "board" {
						out = append(out, v)
					}
				} else if s := ganttSec.FindStringSubmatch(l); s != nil {
					out = append(out, s[1])
				} else if m := ganttLine.FindStringSubmatch(l); m != nil {
					out = append(out, strings.TrimSpace(m[1]))
				}
			}
			return strings.Join(out, " ")
		}})
	Register(Def{Kind: "beforeafter", Board: true, Check: checkBeforeAfter,
		Text: func(src []byte) string {
			var out []string
			for _, l := range lines(src) {
				if k, v, ok := key(l, "title", "titel", "before", "vorher", "after", "nachher"); ok {
					if k == "title" || k == "titel" {
						out = append(out, v)
					} else if f := strings.Fields(v); len(f) > 1 {
						out = append(out, f[1:]...)
					}
				}
			}
			return strings.Join(out, " ")
		}})
	Register(Def{Kind: "annotate", Board: true,
		Check: func(src []byte) []Problem { _, _, ps := parseAnnotate(src); return ps },
		Text: func(src []byte) string {
			t, texts, _ := parseAnnotate(src)
			return strings.Join(append([]string{t}, texts...), " ")
		}})
}
