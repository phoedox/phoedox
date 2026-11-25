package core

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io/fs"
	"math"
	"regexp"
	"strings"
	"sync"

	"github.com/phoedox/phoedox/internal/store"
	"github.com/phoedox/phoedox/web"
)

// HTML screens drawn by the server-side browser (ADR-0095): the screen's
// HTML in the app's tokens and fonts (inlined, as the browser has no
// network), with the prototype's variables at their start values.

var (
	tokensOnce   sync.Once
	tokensInline string
	fontURLRe    = regexp.MustCompile(`url\((/fonts/[^)"']+)\)`)
)

// inlineTokens is tokens.css with its fonts as data: URLs.
func inlineTokens() string {
	tokensOnce.Do(func() {
		dist := web.Dist()
		tokensInline = fontURLRe.ReplaceAllStringFunc(web.Tokens, func(m string) string {
			path := fontURLRe.FindStringSubmatch(m)[1]
			if dist == nil {
				return "url()"
			}
			b, err := fs.ReadFile(dist, strings.TrimPrefix(path, "/"))
			if err != nil {
				return "url()"
			}
			return "url(data:font/woff2;base64," + base64.StdEncoding.EncodeToString(b) + ")"
		})
	})
	return tokensInline
}

// screenShowScript shows the variables' start values (data-show, data-text,
// data-bind) as the player does before anyone clicks.
const screenShowScript = `(function(S){function t(v){return Array.isArray(v)?v.length>0:!!v&&v!=="false"&&v!=="0"}
function h(c){if(!c)return true;var n=c.charAt(0)==="!";if(n)c=c.slice(1);var i=c.indexOf("=");var r=i<0?t(S[c]):String(S[c.slice(0,i)]==null?"":S[c.slice(0,i)])===c.slice(i+1);return n?!r:r}
document.querySelectorAll("[data-show]").forEach(function(e){e.hidden=!h(e.getAttribute("data-show"))});
document.querySelectorAll("[data-text]").forEach(function(e){var v=S[e.getAttribute("data-text")];if(v!==undefined)e.textContent=Array.isArray(v)?v.join(", "):String(v)});
document.querySelectorAll("[data-bind]").forEach(function(e){var v=S[e.getAttribute("data-bind")];if(v===undefined)return;if(e.type==="checkbox")e.checked=t(v);else e.value=String(v)})})`

// screenHTMLDoc is the page the server-side browser draws for a screen.
func screenHTMLDoc(html string, dark bool, vars []WBVar) string {
	theme := "light"
	if dark {
		theme = "dark"
	}
	state := map[string]any{}
	for _, v := range vars {
		var val any
		_ = json.Unmarshal(v.Value, &val)
		state[v.Name] = val
	}
	sb, _ := json.Marshal(state)
	head := `<meta charset="utf-8"><style>` + inlineTokens() + `</style><style>html,body{margin:0}body{font-family:var(--font-body,system-ui,sans-serif);color:var(--fg);background:var(--surface);font-size:15px;line-height:1.45}[hidden]{display:none!important}</style>`
	script := `<script>` + screenShowScript + `(` + strings.ReplaceAll(string(sb), "</", `<\/`) + `)</script>`
	if regexp.MustCompile(`(?i)<html[\s>]`).MatchString(html) {
		out := html
		if loc := regexp.MustCompile(`(?i)<head[^>]*>`).FindStringIndex(out); loc != nil {
			out = out[:loc[1]] + head + out[loc[1]:]
		} else if loc := regexp.MustCompile(`(?i)<html[^>]*>`).FindStringIndex(out); loc != nil {
			out = out[:loc[1]] + "<head>" + head + "</head>" + out[loc[1]:]
		}
		out = regexp.MustCompile(`(?i)<html`).ReplaceAllString(out, `<html data-theme="`+theme+`"`)
		if i := strings.LastIndex(strings.ToLower(out), "</body>"); i >= 0 {
			return out[:i] + script + out[i:]
		}
		return out + script
	}
	return `<!doctype html><html data-theme="` + theme + `"><head>` + head + `</head><body>` + html + script + `</body></html>`
}

// MakeScreenPictures draws the HTML screens of a board that have no
// picture in this theme yet with the server-side browser (frame: one screen
// by id or name, "" all). It returns how many it drew; with the browser off
// it does nothing.
func (s *Service) MakeScreenPictures(ctx context.Context, a Actor, ref, docRef, frame string, dark bool) (int, error) {
	r := s.renderer(ctx, RenderScreens)
	if r == nil || s.Blobs == nil {
		return 0, nil
	}
	p, doc, wb, err := s.whiteboardDoc(ctx, a, ref, docRef, store.RoleReader)
	if err != nil {
		return 0, err
	}
	have := s.screenPictureFiles(ctx, p.ID, doc.ID)
	theme := "light"
	if dark {
		theme = "dark"
	}
	made := 0
	for _, e := range wb.Elements {
		if e.Screen == nil || e.Screen.HTML == "" || e.W <= 0 || e.H <= 0 || (frame != "" && frame != e.ID && !strings.EqualFold(frame, e.Text)) {
			continue
		}
		if _, ok := have[screenPictureName(e, dark)]; ok {
			continue
		}
		var vars []WBVar
		if pr, ok := wb.prototype(e.Screen.Proto); ok {
			vars = pr.Vars
		}
		png, err := r.HTML(ctx, screenHTMLDoc(e.Screen.HTML, dark, vars), int(math.Round(e.W)), int(math.Round(e.H)), 1)
		if err != nil {
			return made, err
		}
		if err := s.storeScreenPicture(ctx, a, p, doc, e, theme, png); err != nil {
			return made, err
		}
		made++
	}
	return made, nil
}
