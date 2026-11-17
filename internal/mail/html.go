package mail

import (
	"bytes"
	"html"
	"html/template"
	"net/url"
	"regexp"
	"strings"

	"github.com/phoedox/phoedox/resources"
)

// Brand is how an e-mail looks: the name, logo and accent colour of the
// effective theme (organization or instance).
type Brand struct {
	Name string
	// Logo is an absolute https address of the logo (or empty).
	Logo string
	// Accent is a #rrggbb colour.
	Accent string
	// Site is the instance's address shown in the footer.
	Site string
}

var (
	hexColorRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
	urlRe      = regexp.MustCompile(`https?://[^\s<>"]+[^\s<>".,;:!?)\]]`)
	layoutTpl  = template.Must(template.New("layout").Parse(resources.MailLayout()))
)

func (b Brand) normalized() Brand {
	if b.Name == "" {
		b.Name = "Phoedox"
	}
	if !hexColorRe.MatchString(b.Accent) {
		b.Accent = "#b84a0c"
	}
	if u, err := url.Parse(b.Logo); err != nil || u.Scheme != "https" {
		b.Logo = ""
	}
	if u, err := url.Parse(b.Site); err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		b.Site = ""
	}
	return b
}

// linkify escapes text and turns addresses into links.
func linkify(s, accent string) string {
	var out strings.Builder
	last := 0
	for _, m := range urlRe.FindAllStringIndex(s, -1) {
		out.WriteString(html.EscapeString(s[last:m[0]]))
		u := s[m[0]:m[1]]
		out.WriteString(`<a href="` + html.EscapeString(u) + `" style="color:` + accent + `;word-break:break-all;">` + html.EscapeString(u) + `</a>`)
		last = m[1]
	}
	out.WriteString(html.EscapeString(s[last:]))
	return out.String()
}

// TextToHTML turns the plain-text body of a mail into simple HTML:
// paragraphs, lists ("• " or "- " lines with indented continuation lines),
// line breaks and links.
func TextToHTML(text, accent string) string {
	text = strings.ReplaceAll(strings.TrimSpace(text), "\r\n", "\n")
	var b strings.Builder
	for _, block := range regexp.MustCompile(`\n\s*\n`).Split(text, -1) {
		lines := strings.Split(strings.Trim(block, "\n"), "\n")
		if len(lines) == 0 || strings.TrimSpace(block) == "" {
			continue
		}
		var para []string
		var items [][]string
		flushPara := func() {
			if len(para) > 0 {
				parts := make([]string, len(para))
				for i, l := range para {
					parts[i] = linkify(l, accent)
				}
				b.WriteString(`<p style="margin:0 0 14px;">` + strings.Join(parts, "<br>") + "</p>\n")
				para = nil
			}
		}
		flushItems := func() {
			if len(items) > 0 {
				b.WriteString(`<ul style="margin:0 0 14px;padding-left:20px;">`)
				for _, it := range items {
					parts := make([]string, len(it))
					for i, l := range it {
						parts[i] = linkify(l, accent)
					}
					b.WriteString(`<li style="margin:0 0 8px;">` + strings.Join(parts, "<br>") + "</li>")
				}
				b.WriteString("</ul>\n")
				items = nil
			}
		}
		for _, l := range lines {
			t := strings.TrimSpace(l)
			switch {
			case strings.HasPrefix(t, "• ") || strings.HasPrefix(t, "- "):
				flushPara()
				items = append(items, []string{strings.TrimSpace(t[strings.Index(t, " ")+1:])})
			case len(items) > 0 && strings.HasPrefix(l, "  ") && t != "":
				items[len(items)-1] = append(items[len(items)-1], t)
			default:
				flushItems()
				para = append(para, t)
			}
		}
		flushPara()
		flushItems()
	}
	return b.String()
}

// RenderHTML puts a message into the HTML frame of the brand.
func RenderHTML(m Message, brand Brand) string {
	brand = brand.normalized()
	body := m.HTML
	if body == "" {
		body = TextToHTML(m.Body, brand.Accent)
	}
	host := ""
	if u, err := url.Parse(brand.Site); err == nil {
		host = u.Host
	}
	var out bytes.Buffer
	err := layoutTpl.Execute(&out, map[string]any{"Lang": m.Lang, "Subject": m.Subject, "Name": brand.Name, "Logo": brand.Logo, "Accent": template.CSS(brand.Accent),
		"Body": template.HTML(body), "Site": brand.Site, "SiteHost": host})
	if err != nil {
		return ""
	}
	return out.String()
}
