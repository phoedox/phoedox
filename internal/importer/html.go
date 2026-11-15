package importer

import (
	"strings"
	"unicode"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// HTMLToMarkdown converts the HTML of an exported page (Confluence,
// BookStack) to Markdown: headings, paragraphs, emphasis, links, images,
// lists, quotes, code and simple tables. link rewrites href and src values
// (archive-relative paths to page:/asset: references); it may return the
// value unchanged.
func HTMLToMarkdown(src string, link func(url string, image bool) string) string {
	doc, err := html.Parse(strings.NewReader(src))
	if err != nil {
		return ""
	}
	c := &conv{link: link}
	c.block(doc)
	return tidy(c.b.String())
}

type conv struct {
	b    strings.Builder
	link func(string, bool) string
	// list nesting: one entry per open list, true = ordered, with counters
	lists []listState
}

type listState struct {
	ordered bool
	n       int
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func hasClass(n *html.Node, class string) bool {
	for _, c := range strings.Fields(attr(n, "class")) {
		if c == class {
			return true
		}
	}
	return false
}

// skip leaves out navigation, scripts and Confluence page chrome.
func skip(n *html.Node) bool {
	switch n.DataAtom {
	case atom.Script, atom.Style, atom.Head, atom.Nav, atom.Noscript, atom.Button, atom.Form:
		return true
	}
	id := attr(n, "id")
	return id == "breadcrumb-section" || id == "footer" || id == "page-metadata-banner" || hasClass(n, "pageSection") && strings.Contains(attr(n, "class"), "group")
}

func (c *conv) nl(k int) {
	s := c.b.String()
	have := len(s) - len(strings.TrimRight(s, "\n"))
	if len(s) == 0 {
		return
	}
	for i := have; i < k; i++ {
		c.b.WriteByte('\n')
	}
}

func (c *conv) block(n *html.Node) {
	for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
		c.node(ch)
	}
}

func (c *conv) node(n *html.Node) {
	switch n.Type {
	case html.TextNode:
		t := collapse(n.Data)
		if strings.TrimSpace(t) == "" {
			if c.b.Len() > 0 && !strings.HasSuffix(c.b.String(), " ") && !strings.HasSuffix(c.b.String(), "\n") {
				c.b.WriteByte(' ')
			}
			return
		}
		c.b.WriteString(escapeMD(t))
		return
	case html.ElementNode:
	default:
		c.block(n)
		return
	}
	if skip(n) {
		return
	}
	switch n.DataAtom {
	case atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
		c.nl(2)
		c.b.WriteString(strings.Repeat("#", int(n.Data[1]-'0')) + " ")
		c.b.WriteString(strings.TrimSpace(c.inline(n)))
		c.nl(2)
	case atom.P, atom.Div, atom.Section, atom.Article, atom.Main, atom.Body, atom.Html:
		if n.DataAtom == atom.P {
			c.nl(2)
		} else {
			c.nl(1)
		}
		c.block(n)
		if n.DataAtom == atom.P {
			c.nl(2)
		} else {
			c.nl(1)
		}
	case atom.Br:
		c.b.WriteString("  \n")
	case atom.Hr:
		c.nl(2)
		c.b.WriteString("---")
		c.nl(2)
	case atom.Ul, atom.Ol:
		c.nl(1)
		if len(c.lists) == 0 {
			c.nl(2)
		}
		c.lists = append(c.lists, listState{ordered: n.DataAtom == atom.Ol})
		c.block(n)
		c.lists = c.lists[:len(c.lists)-1]
		c.nl(1)
		if len(c.lists) == 0 {
			c.nl(2)
		}
	case atom.Li:
		c.nl(1)
		depth := len(c.lists)
		marker := "- "
		if depth > 0 {
			l := &c.lists[depth-1]
			if l.ordered {
				l.n++
				marker = itoa(l.n) + ". "
			}
		}
		if depth > 1 {
			c.b.WriteString(strings.Repeat("  ", depth-1))
		}
		c.b.WriteString(marker)
		c.block(n)
		c.nl(1)
	case atom.Blockquote:
		c.nl(2)
		inner := (&conv{link: c.link}).sub(n)
		for _, line := range strings.Split(strings.TrimSpace(inner), "\n") {
			c.b.WriteString("> " + line + "\n")
		}
		c.nl(2)
	case atom.Pre:
		c.nl(2)
		lang := attr(n, "data-language")
		if lang == "" {
			if p := attr(n, "data-syntaxhighlighter-params"); strings.Contains(p, "brush:") {
				lang = strings.TrimSpace(strings.Split(strings.SplitN(p, "brush:", 2)[1], ";")[0])
			}
		}
		c.b.WriteString("```" + lang + "\n" + strings.TrimRight(textOf(n), "\n") + "\n```")
		c.nl(2)
	case atom.Table:
		c.nl(2)
		c.table(n)
		c.nl(2)
	case atom.Img:
		c.b.WriteString(c.image(n))
	default:
		c.b.WriteString(c.inlineNode(n))
	}
}

// sub converts the children of n into a separate buffer (block level).
func (c *conv) sub(n *html.Node) string {
	c.block(n)
	return tidy(c.b.String())
}

func (c *conv) image(n *html.Node) string {
	src := attr(n, "src")
	if src == "" {
		return ""
	}
	if c.link != nil {
		src = c.link(src, true)
	}
	alt := attr(n, "alt")
	if alt == "" {
		alt = attr(n, "title")
	}
	return "![" + strings.NewReplacer("[", "", "]", "").Replace(alt) + "](" + strings.ReplaceAll(src, " ", "%20") + ")"
}

// inline converts the children of n to inline Markdown.
func (c *conv) inline(n *html.Node) string {
	var b strings.Builder
	for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
		b.WriteString(c.inlineNode(ch))
	}
	return b.String()
}

func (c *conv) inlineNode(n *html.Node) string {
	switch n.Type {
	case html.TextNode:
		return escapeMD(collapse(n.Data))
	case html.ElementNode:
	default:
		return c.inline(n)
	}
	if skip(n) {
		return ""
	}
	wrap := func(mark string) string {
		in := c.inline(n)
		t := strings.TrimSpace(in)
		if t == "" {
			return in
		}
		lead := in[:len(in)-len(strings.TrimLeft(in, " "))]
		trail := in[len(strings.TrimRight(in, " ")):]
		return lead + mark + t + mark + trail
	}
	switch n.DataAtom {
	case atom.Strong, atom.B:
		return wrap("**")
	case atom.Em, atom.I:
		return wrap("_")
	case atom.S, atom.Del, atom.Strike:
		return wrap("~~")
	case atom.Code, atom.Kbd, atom.Tt:
		return "`" + strings.ReplaceAll(textOf(n), "`", "'") + "`"
	case atom.Br:
		return "  \n"
	case atom.Img:
		return c.image(n)
	case atom.A:
		href := attr(n, "href")
		text := strings.TrimSpace(c.inline(n))
		if href == "" || strings.HasPrefix(href, "#") || strings.HasPrefix(strings.ToLower(href), "javascript:") {
			return text
		}
		if c.link != nil {
			href = c.link(href, false)
		}
		if text == "" {
			text = href
		}
		return "[" + text + "](" + strings.ReplaceAll(href, " ", "%20") + ")"
	}
	// Block elements inside inline context (e.g. a <p> in a table cell).
	switch n.DataAtom {
	case atom.P, atom.Div, atom.Li, atom.Ul, atom.Ol:
		return " " + strings.TrimSpace(c.inline(n)) + " "
	}
	return c.inline(n)
}

// table writes a pipe table; the first row is the header.
func (c *conv) table(n *html.Node) {
	var rows [][]string
	var walk func(*html.Node)
	walk = func(x *html.Node) {
		for ch := x.FirstChild; ch != nil; ch = ch.NextSibling {
			if ch.Type != html.ElementNode {
				continue
			}
			if ch.DataAtom == atom.Tr {
				var row []string
				for td := ch.FirstChild; td != nil; td = td.NextSibling {
					if td.Type == html.ElementNode && (td.DataAtom == atom.Td || td.DataAtom == atom.Th) {
						cell := strings.TrimSpace(collapse(c.inline(td)))
						row = append(row, strings.ReplaceAll(cell, "|", `\|`))
					}
				}
				rows = append(rows, row)
				continue
			}
			if ch.DataAtom != atom.Table {
				walk(ch)
			}
		}
	}
	walk(n)
	if len(rows) == 0 {
		return
	}
	cols := 0
	for _, r := range rows {
		cols = max(cols, len(r))
	}
	line := func(r []string) {
		for len(r) < cols {
			r = append(r, "")
		}
		c.b.WriteString("| " + strings.Join(r, " | ") + " |\n")
	}
	line(rows[0])
	sep := make([]string, cols)
	for i := range sep {
		sep[i] = "---"
	}
	line(sep)
	for _, r := range rows[1:] {
		line(r)
	}
}

func textOf(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(x *html.Node) {
		if x.Type == html.TextNode {
			b.WriteString(x.Data)
		}
		if x.Type == html.ElementNode && x.DataAtom == atom.Br {
			b.WriteByte('\n')
		}
		for ch := x.FirstChild; ch != nil; ch = ch.NextSibling {
			walk(ch)
		}
	}
	walk(n)
	return b.String()
}

func collapse(s string) string {
	var b strings.Builder
	space := false
	for _, r := range s {
		if unicode.IsSpace(r) || r == '\u00a0' {
			if !space {
				b.WriteByte(' ')
				space = true
			}
			continue
		}
		space = false
		b.WriteRune(r)
	}
	return b.String()
}

// escapeMD escapes characters that would start Markdown syntax in text.
func escapeMD(s string) string {
	return strings.NewReplacer(`\`, `\\`, "*", `\*`, "_", `\_`, "`", "\\`", "[", `\[`, "]", `\]`).Replace(s)
}

// tidy trims spaces at line ends (keeping hard breaks) and squeezes blank lines.
func tidy(s string) string {
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	blank := 0
	for _, l := range lines {
		if !strings.HasSuffix(l, "  ") || strings.TrimSpace(l) == "" {
			l = strings.TrimRight(l, " ")
		}
		if strings.TrimSpace(l) == "" {
			blank++
			if blank > 1 {
				continue
			}
			l = ""
		} else {
			blank = 0
		}
		out = append(out, l)
	}
	return strings.TrimSpace(strings.Join(out, "\n")) + "\n"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
