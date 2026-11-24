package publish

import (
	"image/color"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/phoedox/phoedox/web"
)

// theme holds the colour tokens of one theme (web/src/styles/tokens.css):
// bg, surface, fg, muted, line, accent … and the whiteboard palette as
// tc-<name> (lines, text) and tb-<name> (fills).
type theme map[string]color.RGBA

var (
	themesOnce            sync.Once
	lightTheme, darkTheme theme
	tokenRe               = regexp.MustCompile(`--([a-z0-9-]+):\s*(#[0-9a-fA-F]{3,8})\b`)
)

// themes reads the light theme from the plain ":root {" blocks and the dark
// one from the ':root[data-theme="dark"] {' blocks, on top of the light one.
func themes() (theme, theme) {
	themesOnce.Do(func() {
		lightTheme, darkTheme = parseTokens(web.Tokens)
	})
	return lightTheme, darkTheme
}

func parseTokens(css string) (theme, theme) {
	light, dark := theme{}, theme{}
	for _, part := range strings.Split(css, "}") {
		i := strings.LastIndex(part, "{")
		if i < 0 {
			continue
		}
		sel := strings.TrimSpace(part[:i])
		if j := strings.LastIndex(sel, "\n"); j >= 0 {
			sel = strings.TrimSpace(sel[j+1:])
		}
		var into theme
		switch sel {
		case ":root":
			into = light
		case `:root[data-theme="dark"]`:
			into = dark
		default:
			continue
		}
		for _, m := range tokenRe.FindAllStringSubmatch(part[i+1:], -1) {
			if c, ok := parseHex(m[2]); ok {
				into[m[1]] = c
			}
		}
	}
	for k, v := range light {
		if _, ok := dark[k]; !ok {
			dark[k] = v
		}
	}
	return light, dark
}

// parseHex reads #rgb or #rrggbb.
func parseHex(s string) (color.RGBA, bool) {
	s = strings.TrimPrefix(s, "#")
	if len(s) == 3 {
		s = string([]byte{s[0], s[0], s[1], s[1], s[2], s[2]})
	}
	if len(s) != 6 {
		return color.RGBA{}, false
	}
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return color.RGBA{}, false
	}
	return color.RGBA{uint8(v >> 16), uint8(v >> 8), uint8(v), 0xff}, true
}

// get returns a token, or the fallback token, or black.
func (t theme) get(name string, fallback ...string) color.RGBA {
	if c, ok := t[name]; ok {
		return c
	}
	for _, f := range fallback {
		if c, ok := t[f]; ok {
			return c
		}
	}
	return color.RGBA{A: 0xff}
}

// stroke is a whiteboard line/text colour: a palette name or #rrggbb.
func (t theme) stroke(c string) color.RGBA {
	if v, ok := parseHex(c); ok && strings.HasPrefix(c, "#") {
		return v
	}
	if c == "" {
		c = "gray"
	}
	return t.get("tc-"+c, "tc-gray")
}

// fill is a whiteboard fill colour: a palette name or #rrggbb.
func (t theme) fill(c string) color.RGBA {
	if v, ok := parseHex(c); ok && strings.HasPrefix(c, "#") {
		return v
	}
	if c == "" {
		c = "gray"
	}
	return t.get("tb-"+c, "tb-gray")
}
