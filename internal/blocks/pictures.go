package blocks

import (
	"regexp"
	"strconv"
	"strings"
)

// Mind maps, before/after images and annotated images (ADR-0085), read like
// web/src/blocks/{mindmap,beforeafter,annotate}.ts. Schedules (gantt) are a
// plugin (com.phoedox.gantt, ADR-0086).

var (
	imageFile   = regexp.MustCompile(`^/api/v1/files/[A-Za-z0-9]+(/[^\s"'<>]*)?$`)
	imageID     = regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{26}$`)
	imageHTTPS  = regexp.MustCompile(`^https://[^\s"'<>]+$`)
	mdImage     = regexp.MustCompile(`^!\[([^\]]*)\]\(([^)\s]+)\)$`)
	markerLine  = regexp.MustCompile(`(?i)^[-*+]?\s*(\d+(?:[.,]\d+)?)\s*%?\s*[,;]\s*(\d+(?:[.,]\d+)?)\s*%?(?:\s+([a-z]+))?\s*:\s*(.*)$`)
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
