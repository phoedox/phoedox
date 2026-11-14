// Package publish renders publications (ADR-0006) as plain HTML pages: no
// JavaScript needed to read them; formulas, dates and embeds are small
// islands (web/src/pub) that improve the page when scripts run.
package publish

import (
	"bytes"
	"html"
	"regexp"
	"strings"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/renderer"
	ghtml "github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
)

// Links tells the renderer how page and file URLs look on the site.
type Links struct {
	// Page returns the URL of a page of a project of this publication, or ""
	// when the page is not part of it.
	Page func(project, doc string) string
	// File is the URL prefix for attachments, e.g. "/pub/docs/_files/".
	File string
	// Board renders an embedded board (```board) or returns "".
	Board func(ref string) string
}

var (
	calloutStart = regexp.MustCompile(`^> \[!([a-z]+)((?: [a-z]+=[A-Za-z0-9-]+)*)\]\s*$`)
	colorSpan    = regexp.MustCompile(`\[((?:\\.|[^\]\\\n])+)\]\{((?:color|bg)=[a-z]+(?: (?:color|bg)=[a-z]+)?)\}`)
	underline    = regexp.MustCompile(`\+\+(\S(?:[^+\n]*?\S)?)\+\+`)
	inlineMath   = regexp.MustCompile(`(^|[^\\$])\$([^\s$](?:[^$\n]*?[^\s\\])?)\$([^\d]|$)`)
	codeSpan     = regexp.MustCompile("(`+)[^`]*?(`+)")
	fenceLine    = regexp.MustCompile("^\\s{0,3}(```+|~~~+)")
	colors       = map[string]bool{"gray": true, "red": true, "orange": true, "yellow": true, "green": true, "blue": true, "purple": true, "pink": true}
	presets      = map[string][2]string{"note": {"Info", "blue"}, "tip": {"Lightbulb", "green"}, "important": {"MessageSquareWarning", "purple"},
		"warning": {"TriangleAlert", "orange"}, "caution": {"OctagonAlert", "red"}, "callout": {"Info", "gray"}}
	iconName = regexp.MustCompile(`^[A-Z][A-Za-z0-9]{1,40}$`)
)

// inline applies the inline extensions outside code spans.
func inline(line string) string {
	var out strings.Builder
	last := 0
	for _, m := range codeSpan.FindAllStringIndex(line, -1) {
		out.WriteString(inlineText(line[last:m[0]]))
		out.WriteString(line[m[0]:m[1]])
		last = m[1]
	}
	out.WriteString(inlineText(line[last:]))
	return out.String()
}

func inlineText(s string) string {
	s = colorSpan.ReplaceAllStringFunc(s, func(m string) string {
		p := colorSpan.FindStringSubmatch(m)
		var cls []string
		for _, kv := range strings.Fields(p[2]) {
			k, v, _ := strings.Cut(kv, "=")
			if colors[v] {
				cls = append(cls, map[string]string{"color": "tc-", "bg": "tb-"}[k]+v)
			}
		}
		return `<span class="` + strings.Join(cls, " ") + `">` + p[1] + `</span>`
	})
	s = underline.ReplaceAllString(s, "<u>$1</u>")
	s = inlineMath.ReplaceAllStringFunc(s, func(m string) string {
		p := inlineMath.FindStringSubmatch(m)
		tex := p[2]
		return p[1] + `<phx-math data-tex="` + html.EscapeString(tex) + `">` + "`" + strings.ReplaceAll(tex, "`", "'") + "`" + `</phx-math>` + p[3]
	})
	return s
}

// preprocess turns callouts and inline extensions into HTML that goldmark
// passes through; fenced code stays untouched.
func preprocess(md string) string {
	lines := strings.Split(strings.ReplaceAll(md, "\r\n", "\n"), "\n")
	var out []string
	fence := ""
	for i := 0; i < len(lines); i++ {
		l := lines[i]
		if m := fenceLine.FindStringSubmatch(l); m != nil {
			if fence == "" {
				fence = m[1][:1]
			} else if strings.HasPrefix(strings.TrimSpace(l), strings.Repeat(fence, 3)) {
				fence = ""
			}
			out = append(out, l)
			continue
		}
		if fence != "" {
			out = append(out, l)
			continue
		}
		if m := calloutStart.FindStringSubmatch(l); m != nil {
			preset, ok := presets[m[1]]
			if ok {
				icon, color, iconColor := preset[0], preset[1], ""
				for _, kv := range strings.Fields(m[2]) {
					k, v, _ := strings.Cut(kv, "=")
					switch {
					case k == "icon" && iconName.MatchString(v):
						icon = v
					case k == "color" && colors[v]:
						color = v
					case k == "iconcolor" && colors[v]:
						iconColor = " ic-" + v
					}
				}
				var body []string
				for i+1 < len(lines) && (lines[i+1] == ">" || strings.HasPrefix(lines[i+1], "> ")) {
					i++
					body = append(body, strings.TrimPrefix(strings.TrimPrefix(lines[i], ">"), " "))
				}
				out = append(out, `<div class="callout cb-`+color+`" data-kind="`+m[1]+`"><span class="ci`+iconColor+`" data-icon="`+icon+`"></span><div class="cc">`, "")
				out = append(out, strings.Split(preprocess(strings.Join(body, "\n")), "\n")...)
				out = append(out, "", `</div></div>`, "")
				continue
			}
		}
		out = append(out, inline(l))
	}
	return strings.Join(out, "\n")
}

// fenceRenderer renders our fenced blocks; other code blocks stay code.
type fenceRenderer struct{ links Links }

func (r fenceRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindFencedCodeBlock, r.render)
}

func (r fenceRenderer) render(w util.BufWriter, src []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	b := n.(*ast.FencedCodeBlock)
	lang := string(b.Language(src))
	var body bytes.Buffer
	for i := 0; i < b.Lines().Len(); i++ {
		seg := b.Lines().At(i)
		body.Write(seg.Value(src))
	}
	text := strings.TrimSpace(body.String())
	esc := html.EscapeString(text)
	switch lang {
	case "math":
		_, _ = w.WriteString(`<phx-math data-tex="` + esc + `" data-display="1"><pre>` + esc + `</pre></phx-math>`)
	case "embed", "iframe":
		_, _ = w.WriteString(`<phx-embed data-url="` + esc + `" data-kind="` + lang + `"><a href="` + esc + `" rel="noopener external">` + esc + `</a></phx-embed>`)
	case "mermaid":
		_, _ = w.WriteString(`<phx-mermaid><pre>` + esc + `</pre></phx-mermaid>`)
	case "board":
		if r.links.Board != nil {
			_, _ = w.WriteString(r.links.Board(text))
		}
	default:
		cls := ""
		if lang != "" {
			cls = ` class="language-` + html.EscapeString(lang) + `"`
		}
		_, _ = w.WriteString(`<pre><code` + cls + `>` + html.EscapeString(body.String()) + `</code></pre>`)
	}
	return ast.WalkSkipChildren, nil
}

var policy = func() *bluemonday.Policy {
	p := bluemonday.UGCPolicy()
	p.AllowAttrs("class").OnElements("span", "div", "code", "pre", "li", "input", "table", "td", "th", "section", "small", "h3")
	p.AllowAttrs("data-kind", "data-icon").OnElements("div", "span")
	p.AllowElements("u", "phx-math", "phx-embed", "phx-mermaid", "time", "section", "article", "h2", "h3")
	p.AllowNoAttrs().OnElements("phx-mermaid") // the source is the <pre> inside
	p.AllowAttrs("data-tex", "data-display").OnElements("phx-math")
	p.AllowAttrs("data-url", "data-kind").OnElements("phx-embed")
	p.AllowAttrs("datetime", "data-kind").OnElements("time")
	p.AllowAttrs("type", "checked", "disabled").OnElements("input")
	p.AllowAttrs("aria-label", "role").Globally()
	p.RequireNoReferrerOnLinks(false)
	return p
}()

var pageLink = regexp.MustCompile(`href="/p/([a-z0-9-]+)/d/([a-z0-9-]+)"`)
var fileLink = regexp.MustCompile(`(src|href)="/api/v1/files/([A-Za-z0-9]+)(/[^"]*)?"`)

// Markdown renders a page for the site: our extensions, safe HTML, links
// to pages and files of the publication.
func Markdown(md string, links Links) string {
	g := goldmark.New(
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithRendererOptions(ghtml.WithUnsafe(), renderer.WithNodeRenderers(util.Prioritized(fenceRenderer{links}, 100))),
	)
	var buf bytes.Buffer
	if err := g.Convert([]byte(preprocess(md)), &buf); err != nil {
		return "<p>" + html.EscapeString(md) + "</p>"
	}
	out := policy.Sanitize(buf.String())
	out = pageLink.ReplaceAllStringFunc(out, func(m string) string {
		p := pageLink.FindStringSubmatch(m)
		if links.Page != nil {
			if u := links.Page(p[1], p[2]); u != "" {
				return `href="` + u + `"`
			}
		}
		return `href="#"`
	})
	out = fileLink.ReplaceAllString(out, `$1="`+links.File+`$2$3"`)
	return out
}
