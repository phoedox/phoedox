package publish

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/phoedox/phoedox/internal/core"
	"github.com/phoedox/phoedox/internal/store"
	"github.com/phoedox/phoedox/resources"
)

//go:embed layout.html
var layoutHTML string

var layout = template.Must(template.New("layout").Parse(layoutHTML))

// Site serves publications.
type Site struct {
	Core *core.Service
	// Web is the built frontend (web/dist): islands under pub/ and assets/.
	Web fs.FS
	// Version busts caches of the islands.
	Version string
	// AppURL is the instance (for links back), e.g. https://app.example.org.
	AppURL string
	// Member reports whether the request comes from a signed-in user of the
	// instance (access "members"); nil means nobody is.
	Member func(r *http.Request) bool
}

type texts map[string]string

func textsFor(locale string) texts {
	t := texts{}
	_ = json.Unmarshal(resources.PublishTexts(locale), &t)
	return t
}

// uiLocale picks de or en from ?lang, the cookie or Accept-Language.
func uiLocale(r *http.Request) string {
	for _, l := range []string{r.URL.Query().Get("lang"), cookie(r, "phx_lang")} {
		if l == "de" || l == "en" {
			return l
		}
	}
	if strings.HasPrefix(strings.ToLower(r.Header.Get("Accept-Language")), "de") {
		return "de"
	}
	return "en"
}

func cookie(r *http.Request, name string) string {
	if c, err := r.Cookie(name); err == nil {
		return c.Value
	}
	return ""
}

// islands returns the script and styles of web/src/pub from the Vite manifest.
func (s *Site) islands() (js string, css []string) {
	b, err := fs.ReadFile(s.Web, ".vite/manifest.json")
	if err != nil {
		return "", nil
	}
	var m map[string]struct {
		File    string   `json:"file"`
		CSS     []string `json:"css"`
		Imports []string `json:"imports"`
	}
	if json.Unmarshal(b, &m) != nil {
		return "", nil
	}
	seen := map[string]bool{}
	var walk func(key string)
	walk = func(key string) {
		e, ok := m[key]
		if !ok || seen[key] {
			return
		}
		seen[key] = true
		for _, i := range e.Imports {
			walk(i)
		}
		css = append(css, e.CSS...)
	}
	walk("src/pub/main.ts")
	return m["src/pub/main.ts"].File, css
}

// page is the data of the layout template.
type releaseKey struct{}

type releaseInfo struct {
	Release store.PublicationRelease
	Live    string // base of the live publication
}

type page struct {
	T           texts
	Base        string
	Pub         store.Publication
	Site        []core.PubProject
	Project     *core.PubProject
	Doc         *store.Document
	Title       string
	Description string
	Canonical   string
	Body        template.HTML
	Nav         []navGroup
	Langs       []langLink
	Search      string
	Note        string
	// NoteLink adds a link to the note (e.g. to the current version).
	NoteLink     string
	NoteLinkText string
	JS           string
	CSS          []string
	Version      string
	Accent       template.CSS
	Custom       template.CSS
	Lang         string
	Status       int
	Login        bool
	LoginError   bool
	// Plain: no page menu (status pages).
	Plain bool
	// Embed: only the content, for iframes on other sites; links open on top.
	Embed bool
	// Source is the full page of an embed.
	Source string
}

type navGroup struct {
	Name  string
	Depth int
	Links []navLink
}

type navLink struct {
	Title, URL string
	Current    bool
}

type langLink struct {
	Code, URL string
	Current   bool
}

// Serve handles a request for publication p, mounted at base ("" for an own domain).
func (s *Site) Serve(w http.ResponseWriter, r *http.Request, p store.Publication, base string) {
	ctx := store.WithTenant(r.Context(), p.TenantID)
	r = r.WithContext(ctx)
	rel := strings.TrimPrefix(r.URL.Path, base)
	if rel == "" {
		rel = "/"
	}
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
	// Islands load from here; embeds and iframes from https only.
	// Only the /_embed/ views may be framed by other sites (public publications).
	frame := "'self'"
	if strings.HasPrefix(rel, "/_embed/") && p.Access == "public" {
		frame = "*"
	}
	h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data: https:; style-src 'self' 'unsafe-inline'; script-src 'self'; frame-src https:; object-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors "+frame)

	switch {
	case strings.HasPrefix(rel, "/pub/") || strings.HasPrefix(rel, "/assets/"):
		s.asset(w, r, strings.TrimPrefix(rel, "/"))
		return
	case rel == "/robots.txt":
		h.Set("Content-Type", "text/plain; charset=utf-8")
		if p.NoIndex || p.Access != "public" {
			_, _ = io.WriteString(w, "User-agent: *\nDisallow: /\n")
		} else {
			_, _ = fmt.Fprintf(w, "User-agent: *\nAllow: /\nSitemap: %s\n", s.absolute(r, base+"/sitemap.xml"))
		}
		return
	}

	t := textsFor(uiLocale(r))
	// Embeds and the public MCP endpoint exist for public publications only.
	if (strings.HasPrefix(rel, "/_embed/") || rel == "/mcp") && p.Access != "public" {
		http.NotFound(w, r)
		return
	}
	if p.Access == "members" && (s.Member == nil || !s.Member(r)) {
		// The app's session cookie only exists on the app's own domain.
		target := base + rel
		if !strings.HasPrefix(base, "/pub/") {
			target = "/pub/" + p.Slug + rel
		}
		h.Set("Cache-Control", "private, no-store")
		http.Redirect(w, r, strings.TrimRight(s.AppURL, "/")+"/login?next="+url.QueryEscape(target), http.StatusFound)
		return
	}
	if p.Access == "password" && !s.unlocked(r, p) {
		if rel == "/_login" && r.Method == http.MethodPost {
			if s.Core.VerifyPassword != nil && s.Core.VerifyPassword(p.PasswordHash, r.FormValue("password")) {
				http.SetCookie(w, &http.Cookie{Name: "phx_pub_" + p.ID, Value: s.token(p), Path: base + "/", HttpOnly: true, Secure: r.TLS != nil || s.secure(r), SameSite: http.SameSiteLaxMode, MaxAge: 30 * 24 * 3600})
				http.Redirect(w, r, base+"/", http.StatusSeeOther)
				return
			}
			s.render(w, r, http.StatusUnauthorized, &page{T: t, Base: base, Pub: p, Title: t["password_title"], Login: true, LoginError: true})
			return
		}
		s.render(w, r, http.StatusUnauthorized, &page{T: t, Base: base, Pub: p, Title: t["password_title"], Login: true})
		return
	}
	if p.Access != "public" {
		h.Set("Cache-Control", "private, no-store")
		h.Set("X-Robots-Tag", "noindex")
	}

	// Fixed states: /v/<n>/… shows the publication as it was at release n.
	parts := strings.Split(strings.Trim(rel, "/"), "/")
	var release *store.PublicationRelease
	if len(parts) >= 2 && parts[0] == "v" {
		n, err := strconv.Atoi(parts[1])
		rl, err2 := s.Core.DB.Release(r.Context(), p.ID, n)
		if err != nil || err2 != nil {
			http.NotFound(w, r)
			return
		}
		release = &rl
		r = r.WithContext(context.WithValue(r.Context(), releaseKey{}, releaseInfo{Release: rl, Live: base}))
		base += "/v/" + parts[1]
		rel = "/" + strings.Join(parts[2:], "/")
		parts = strings.Split(strings.Trim(rel, "/"), "/")
		h.Set("X-Robots-Tag", "noindex") // the live pages are the ones to find
	}
	site, err := s.Core.SiteAt(r.Context(), p, release)
	if err != nil {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	switch {
	case rel == "/mcp":
		s.mcp(w, r, p, base, site)
	case rel == "/_embed/status":
		s.pubStatusPage(w, r, p, base, site, t, true)
	case len(parts) == 3 && parts[0] == "_embed":
		s.docPage(w, r, p, base, site, parts[1], parts[2], t, true)
	case rel == "/sitemap.xml":
		s.sitemap(w, r, p, base, site)
	case rel == "/llms.txt":
		s.llms(w, r, p, base, site)
	case len(parts) == 2 && strings.HasSuffix(parts[1], ".md"):
		s.markdown(w, r, site, parts[0], strings.TrimSuffix(parts[1], ".md"))
	case rel == "/status":
		s.pubStatusPage(w, r, p, base, site, t, false)
	case rel == "/search":
		s.search(w, r, p, base, site, t)
	case parts[0] == "_files" && len(parts) >= 2:
		s.file(w, r, site, parts[1], r.URL.Query().Get("preview") != "")
	case rel == "/":
		if len(site) == 0 {
			s.notFound(w, r, p, base, site, t)
			return
		}
		{
			if d := firstDoc(site[0]); d != "" {
				http.Redirect(w, r, base+"/"+site[0].Project.Slug+"/"+d+keepLang(r), http.StatusFound)
				return
			}
		}
		s.notFound(w, r, p, base, site, t)
	case len(parts) == 1:
		for _, pp := range site {
			if pp.Project.Slug == parts[0] {
				if d := firstDoc(pp); d != "" {
					http.Redirect(w, r, base+"/"+pp.Project.Slug+"/"+d+keepLang(r), http.StatusFound)
					return
				}
			}
		}
		s.notFound(w, r, p, base, site, t)
	case len(parts) == 2:
		s.docPage(w, r, p, base, site, parts[0], parts[1], t, false)
	default:
		s.notFound(w, r, p, base, site, t)
	}
}

func keepLang(r *http.Request) string {
	if l := r.URL.Query().Get("lang"); l != "" {
		return "?lang=" + url.QueryEscape(l)
	}
	return ""
}

// firstDoc is the landing page of a project: the first text page in menu
// order, else the first page.
func firstDoc(pp core.PubProject) string {
	t := textsFor("en")
	for _, g := range nav("", pp, "", t, "") {
		for _, l := range g.Links {
			slug := l.URL[strings.LastIndex(l.URL, "/")+1:]
			for _, d := range pp.Docs {
				if d.Slug == slug && d.Type == store.DocMarkdown {
					return d.Slug
				}
			}
		}
	}
	if len(pp.Docs) > 0 {
		return pp.Docs[0].Slug
	}
	return ""
}

func (s *Site) secure(r *http.Request) bool { return strings.HasPrefix(s.AppURL, "https://") }

func (s *Site) absolute(r *http.Request, p string) string {
	scheme := "https"
	if r.TLS == nil && !s.secure(r) {
		scheme = "http"
	}
	return scheme + "://" + r.Host + p
}

// token is the cookie value of an unlocked protected publication; it
// changes with the password.
func (s *Site) token(p store.Publication) string {
	m := hmac.New(sha256.New, []byte(p.PasswordHash))
	m.Write([]byte("phoedox-publication:" + p.ID))
	return hex.EncodeToString(m.Sum(nil))
}

func (s *Site) unlocked(r *http.Request, p store.Publication) bool {
	return p.PasswordHash != "" && hmac.Equal([]byte(cookie(r, "phx_pub_"+p.ID)), []byte(s.token(p)))
}

func (s *Site) asset(w http.ResponseWriter, r *http.Request, name string) {
	b, err := fs.ReadFile(s.Web, path.Clean(name))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", mime.TypeByExtension(path.Ext(name)))
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(b)
}

func (s *Site) file(w http.ResponseWriter, r *http.Request, site []core.PubProject, id string, preview bool) {
	fc, err := s.Core.PubFile(r.Context(), site, id, preview)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer fc.Reader.Close()
	h := w.Header()
	if fc.Mime != "" {
		h.Set("Content-Type", fc.Mime)
	}
	h.Set("Content-Security-Policy", "default-src 'none'; img-src 'self' data:; style-src 'unsafe-inline'; media-src 'self'; sandbox")
	h.Set("Cache-Control", "public, max-age=86400")
	if !strings.HasPrefix(fc.Mime, "image/") && fc.Mime != "application/pdf" {
		h.Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(fc.File.Name))
	}
	http.ServeContent(w, r, "", fc.File.CreatedAt.Time, fc.Reader)
}

// nav builds the sidebar of a project: categories as folders, without decisions.
func nav(base string, pp core.PubProject, current string, t texts, lang string) []navGroup {
	q := ""
	if lang != "" {
		q = "?lang=" + url.QueryEscape(lang)
	}
	groups := map[string]*navGroup{}
	var order []string
	var decisions []navLink
	for _, d := range pp.Docs {
		l := navLink{Title: d.Title, URL: base + "/" + pp.Project.Slug + "/" + d.Slug + q, Current: d.Slug == current}
		if d.Type == store.DocADR {
			l.Title = core.DecisionRef(d.Number) + " " + d.Title
			decisions = append(decisions, l)
			continue
		}
		cat := strings.Trim(d.Category, "/ ")
		g, ok := groups[cat]
		if !ok {
			name := strings.ReplaceAll(cat, "/", " › ")
			if cat == "" {
				name = t["uncategorized"]
			}
			g = &navGroup{Name: name}
			groups[cat] = g
			order = append(order, cat)
		}
		g.Links = append(g.Links, l)
	}
	sort.SliceStable(order, func(i, j int) bool {
		if order[i] == "" || order[j] == "" {
			return order[j] == ""
		}
		return order[i] < order[j]
	})
	out := []navGroup{}
	for _, c := range order {
		out = append(out, *groups[c])
	}
	if len(decisions) > 0 {
		out = append(out, navGroup{Name: t["decisions"], Links: decisions})
	}
	return out
}

func (s *Site) docPage(w http.ResponseWriter, r *http.Request, p store.Publication, base string, site []core.PubProject, projectSlug, docSlug string, t texts, embed bool) {
	lang := r.URL.Query().Get("lang")
	pp, d, err := s.Core.PubDocument(r.Context(), site, projectSlug, docSlug, lang)
	if err != nil {
		s.notFound(w, r, p, base, site, t)
		return
	}
	links := s.links(r, base, site, pp)
	c := map[string]any{}
	_ = json.Unmarshal(d.Content, &c)
	str := func(k string) string { v, _ := c[k].(string); return v }
	var body strings.Builder
	switch d.Type {
	case store.DocMarkdown:
		body.WriteString(Markdown(withoutTitle(str("markdown"), d.Title), links))
	case store.DocADR:
		status := str("status")
		fmt.Fprintf(&body, `<p class="meta"><span class="chip">%s</span> <span class="chip">%s</span> <span class="chip">%s</span></p>`,
			core.DecisionRef(d.Number), template.HTMLEscapeString(t["status_"+status]), template.HTMLEscapeString(str("date")))
		for _, k := range []string{"context", "decision", "consequences", "alternatives"} {
			if v := str(k); v != "" {
				fmt.Fprintf(&body, `<section><h2>%s</h2>%s</section>`, template.HTMLEscapeString(t[k]), Markdown(v, links))
			}
		}
	case store.DocLink:
		u := str("url")
		fmt.Fprintf(&body, `<p><a href="%s" rel="noopener external">%s</a></p>%s`, template.HTMLEscapeString(u), template.HTMLEscapeString(t["open_link"]), Markdown(str("description"), links))
	case store.DocBoard:
		body.WriteString(Markdown(str("description"), links))
		body.WriteString(s.board(r, pp, d, t))
	case store.DocFloorplan:
		body.WriteString(Markdown(str("description"), links))
		body.WriteString(floorplanSVG(d, links.File))
	}
	pg := &page{T: t, Base: base, Pub: p, Site: site, Project: &pp, Doc: &d, Title: d.Title + " – " + p.Title, Body: template.HTML(body.String()),
		Nav: nav(base, pp, d.Slug, t, lang), Canonical: s.absolute(r, base+"/"+pp.Project.Slug+"/"+d.Slug), Lang: pp.Project.MainLocale}
	pg.Description = summary(str("markdown") + str("decision") + str("description"))
	if embed {
		pg.Embed, pg.Plain, pg.Source = true, true, base+"/"+pp.Project.Slug+"/"+d.Slug
	}
	if ri, ok := r.Context().Value(releaseKey{}).(releaseInfo); ok {
		pg.Note = fmt.Sprintf(t["release_note"], ri.Release.Number, ri.Release.CreatedAt.Format(time.DateOnly))
		if ri.Release.Note != "" {
			pg.Note += " – " + ri.Release.Note
		}
		live := ri.Live + "/" + pp.Project.Slug + "/" + d.Slug
		pg.NoteLink, pg.NoteLinkText = live, t["release_current"]
		pg.Canonical = s.absolute(r, live)
	} else if pp.Project.MainLocale != "" {
		pg.Lang = pp.Project.MainLocale
		if d.Translation != nil {
			pg.Lang = d.Locale
			if d.Translation.SourceVersion < d.Version {
				pg.Note = t["outdated"]
			}
		} else if lang != "" && lang != pp.Project.MainLocale {
			pg.Note = t["not_translated"]
		}
		for _, l := range append([]string{pp.Project.MainLocale}, pp.Project.Locales...) {
			pg.Langs = append(pg.Langs, langLink{Code: l, URL: base + "/" + pp.Project.Slug + "/" + d.Slug + "?lang=" + l, Current: l == pg.Lang})
		}
	}
	s.render(w, r, http.StatusOK, pg)
}

// withoutTitle drops a leading "# Heading" equal to the title.
func withoutTitle(md, title string) string {
	s := strings.TrimLeft(md, " \n")
	if !strings.HasPrefix(s, "# ") {
		return md
	}
	line, rest, _ := strings.Cut(s, "\n")
	if strings.EqualFold(strings.TrimSpace(strings.Trim(strings.TrimPrefix(line, "# "), "#*_` ")), strings.TrimSpace(title)) {
		return rest
	}
	return md
}

func summary(md string) string {
	s := strings.Join(strings.Fields(strings.NewReplacer("#", "", "*", "", "`", "", ">", "", "|", " ").Replace(md)), " ")
	if len([]rune(s)) > 160 {
		s = string([]rune(s)[:157]) + "…"
	}
	return s
}

func (s *Site) links(r *http.Request, base string, site []core.PubProject, pp core.PubProject) Links {
	return Links{
		File: base + "/_files/",
		Page: func(project, doc string) string {
			for _, x := range site {
				if x.Project.Slug == project {
					for _, d := range x.Docs {
						if d.Slug == doc {
							return base + "/" + project + "/" + doc + keepLang(r)
						}
					}
				}
			}
			return ""
		},
		Include: func(ref string) string {
			_, d, err := s.Core.PubDocument(r.Context(), site, pp.Project.Slug, ref, r.URL.Query().Get("lang"))
			if err != nil || d.Type != store.DocMarkdown {
				return ""
			}
			c := map[string]any{}
			_ = json.Unmarshal(d.Content, &c)
			md, _ := c["markdown"].(string)
			inner := s.links(r, base, site, pp)
			inner.Include = nil // one level only
			return `<div class="include">` + Markdown(withoutTitle(md, d.Title), inner) + `</div>`
		},
		Board: func(ref string) string {
			for _, d := range pp.Docs {
				if d.Type == store.DocBoard && (d.Slug == ref || d.ID == ref) {
					return s.board(r, pp, d, textsFor(uiLocale(r)))
				}
			}
			return ""
		},
	}
}

// board renders a board read-only.
// floorplanSVG draws a plan as static SVG: background, rooms with names and
// devices as dots.
func floorplanSVG(d store.Document, files string) string {
	var fp core.Floorplan
	if json.Unmarshal(d.Content, &fp) != nil || fp.Width <= 0 || fp.Height <= 0 {
		return ""
	}
	esc := template.HTMLEscapeString
	num := func(v float64) string { return strconv.FormatFloat(v, 'f', 0, 64) }
	var out strings.Builder
	fmt.Fprintf(&out, `<figure class="floorplan"><svg viewBox="0 0 %s %s" role="img" aria-label="%s">`, num(fp.Width), num(fp.Height), esc(d.Title))
	if fp.Image != "" && fp.ImageWidth > 0 {
		fmt.Fprintf(&out, `<image href="%s%s" x="0" y="0" width="%s" opacity=".55"/>`, esc(files), esc(url.PathEscape(fp.Image)), num(fp.ImageWidth))
	}
	font := max(fp.Width, fp.Height) / 60
	for _, r := range fp.Rooms {
		pts := make([]string, len(r.Points))
		cx, cy := 0.0, 0.0
		for i, p := range r.Points {
			pts[i] = num(p[0]) + "," + num(p[1])
			cx += p[0] / float64(len(r.Points))
			cy += p[1] / float64(len(r.Points))
		}
		color := r.Color
		if color == "" {
			color = "gray"
		}
		fmt.Fprintf(&out, `<polygon class="room c-%s" points="%s"/>`, esc(color), strings.Join(pts, " "))
		fmt.Fprintf(&out, `<text x="%s" y="%s" font-size="%s" text-anchor="middle">%s</text>`, num(cx), num(cy), num(font), esc(r.Name))
	}
	for _, dv := range fp.Devices {
		fmt.Fprintf(&out, `<circle class="dev" cx="%s" cy="%s" r="%s"><title>%s</title></circle>`, num(dv.X), num(dv.Y), num(font/2), esc(dv.Entity))
	}
	out.WriteString(`</svg></figure>`)
	return out.String()
}

func (s *Site) board(r *http.Request, pp core.PubProject, d store.Document, t texts) string {
	var b struct {
		Columns []core.BoardColumn `json:"columns"`
	}
	_ = json.Unmarshal(d.Content, &b)
	tasks, _ := s.Core.PubTasks(r.Context(), pp, d)
	var out strings.Builder
	out.WriteString(`<div class="board">`)
	for _, c := range b.Columns {
		fmt.Fprintf(&out, `<section class="col"><h3>%s</h3>`, template.HTMLEscapeString(c.Name))
		n := 0
		for _, x := range tasks {
			if x.Column != c.ID {
				continue
			}
			n++
			cls := "card"
			if c.Done {
				cls += " done"
			}
			fmt.Fprintf(&out, `<div class="%s">%s`, cls, template.HTMLEscapeString(x.Title))
			var meta []string
			meta = append(meta, x.Labels...)
			if x.Due != "" {
				meta = append(meta, x.Due)
			}
			if len(meta) > 0 {
				fmt.Fprintf(&out, `<small>%s</small>`, template.HTMLEscapeString(strings.Join(meta, " · ")))
			}
			out.WriteString(`</div>`)
		}
		if n == 0 {
			fmt.Fprintf(&out, `<small class="muted">%s</small>`, template.HTMLEscapeString(t["board_empty"]))
		}
		out.WriteString(`</section>`)
	}
	out.WriteString(`</div>`)
	return out.String()
}

func (s *Site) search(w http.ResponseWriter, r *http.Request, p store.Publication, base string, site []core.PubProject, t texts) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	var body strings.Builder
	fmt.Fprintf(&body, `<h1>%s</h1>`, template.HTMLEscapeString(fmt.Sprintf(t["search_results"], q)))
	hits := core.PubSearch(site, q)
	if len(hits) == 0 {
		fmt.Fprintf(&body, `<p>%s</p>`, template.HTMLEscapeString(t["search_none"]))
	} else {
		body.WriteString(`<ul class="hits">`)
		for _, h := range hits {
			fmt.Fprintf(&body, `<li><a href="%s/%s/%s">%s</a><small>%s · %s</small></li>`, base, h.Project.Project.Slug, h.Doc.Slug,
				template.HTMLEscapeString(h.Doc.Title), template.HTMLEscapeString(h.Project.Project.Name), template.HTMLEscapeString(summary(h.Snippet)))
		}
		body.WriteString(`</ul>`)
	}
	pg := &page{T: t, Base: base, Pub: p, Site: site, Title: t["search"] + " – " + p.Title, Body: template.HTML(body.String()), Search: q}
	if len(site) > 0 {
		pg.Nav = nav(base, site[0], "", t, "")
	}
	w.Header().Set("X-Robots-Tag", "noindex")
	s.render(w, r, http.StatusOK, pg)
}

func (s *Site) notFound(w http.ResponseWriter, r *http.Request, p store.Publication, base string, site []core.PubProject, t texts) {
	pg := &page{T: t, Base: base, Pub: p, Site: site, Title: t["not_found"], Body: template.HTML("<h1>404</h1><p>" + template.HTMLEscapeString(t["not_found"]) + "</p>")}
	s.render(w, r, http.StatusNotFound, pg)
}

type urlset struct {
	XMLName xml.Name `xml:"http://www.sitemaps.org/schemas/sitemap/0.9 urlset"`
	URLs    []struct {
		Loc     string `xml:"loc"`
		LastMod string `xml:"lastmod,omitempty"`
	} `xml:"url"`
}

// llms.txt (llmstxt.org): an index for AI readers with links to the
// Markdown of every page.
func (s *Site) llms(w http.ResponseWriter, r *http.Request, p store.Publication, base string, site []core.PubProject) {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", p.Title)
	if p.Description != "" {
		fmt.Fprintf(&b, "> %s\n\n", strings.ReplaceAll(p.Description, "\n", " "))
	}
	for _, pp := range site {
		fmt.Fprintf(&b, "## %s\n\n", pp.Project.Name)
		for _, d := range pp.Docs {
			fmt.Fprintf(&b, "- [%s](%s)\n", strings.ReplaceAll(d.Title, "]", ")"), s.absolute(r, base+"/"+pp.Project.Slug+"/"+d.Slug+".md"))
		}
		b.WriteString("\n")
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, b.String())
}

// markdown serves the source of a page for AI readers (…/<project>/<page>.md).
func (s *Site) markdown(w http.ResponseWriter, r *http.Request, site []core.PubProject, projectSlug, docSlug string) {
	_, d, err := s.Core.PubDocument(r.Context(), site, projectSlug, docSlug, r.URL.Query().Get("lang"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	_, _ = io.WriteString(w, docMarkdown(d))
}

// docMarkdown is the Markdown source of a published page.
func docMarkdown(d store.Document) string {
	c := map[string]any{}
	_ = json.Unmarshal(d.Content, &c)
	str := func(k string) string { v, _ := c[k].(string); return v }
	var b strings.Builder
	switch d.Type {
	case store.DocADR:
		fmt.Fprintf(&b, "# %s\n\n%s · %s\n\n", d.Title, core.DecisionRef(d.Number), str("status"))
		for _, k := range []string{"context", "decision", "consequences", "alternatives"} {
			if v := str(k); v != "" {
				fmt.Fprintf(&b, "## %s\n\n%s\n\n", strings.ToUpper(k[:1])+k[1:], v)
			}
		}
	case store.DocMarkdown:
		md := str("markdown")
		if !strings.HasPrefix(strings.TrimSpace(md), "# ") {
			fmt.Fprintf(&b, "# %s\n\n", d.Title)
		}
		b.WriteString(md)
	default:
		fmt.Fprintf(&b, "# %s\n\n%s\n", d.Title, str("description"))
	}
	return b.String()
}

func (s *Site) sitemap(w http.ResponseWriter, r *http.Request, p store.Publication, base string, site []core.PubProject) {
	var u urlset
	for _, pp := range site {
		for _, d := range pp.Docs {
			u.URLs = append(u.URLs, struct {
				Loc     string `xml:"loc"`
				LastMod string `xml:"lastmod,omitempty"`
			}{s.absolute(r, base+"/"+pp.Project.Slug+"/"+d.Slug), d.UpdatedAt.Format(time.DateOnly)})
		}
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	_, _ = io.WriteString(w, xml.Header)
	_ = xml.NewEncoder(w).Encode(u)
}

func (s *Site) render(w http.ResponseWriter, r *http.Request, status int, pg *page) {
	pg.JS, pg.CSS = s.islands()
	pg.Version = s.Version
	if pg.Pub.Color != "" {
		pg.Accent = template.CSS("--accent:" + pg.Pub.Color + ";")
	}
	pg.Custom = template.CSS(pg.Pub.CSS)
	if pg.Lang == "" {
		pg.Lang = uiLocale(r)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := layout.Execute(w, pg); err != nil {
		_, _ = io.WriteString(w, "render error")
	}
}
