package publish

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/phoedox/phoedox/internal/core"
	"github.com/phoedox/phoedox/internal/store"
)

// The offline emergency copy: a project as plain HTML files in a ZIP. Every
// page links with relative addresses, so the copy opens from a USB stick or
// a network share without a server and without JavaScript.

var offlineTmpl = template.Must(template.New("offline").Parse(`<!doctype html>
<html lang="{{.Lang}}"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}}</title>
<style>
:root{--bg:#f6f4f2;--surface:#fff;--fg:#1f1a17;--muted:#6b6159;--line:#ddd5ce;--accent:#b84a0c;--warn:#fdf0d8;color-scheme:light dark}
@media (prefers-color-scheme:dark){:root{--bg:#14110f;--surface:#1c1815;--fg:#eee7e1;--muted:#a69b92;--line:#3a312a;--accent:#f26a1b;--warn:#382a12}}
*{box-sizing:border-box}body{margin:0;background:var(--bg);color:var(--fg);font:16px/1.6 system-ui,sans-serif}
a{color:var(--accent)}header{border-bottom:1px solid var(--line);background:var(--surface);padding:10px 16px;display:flex;gap:16px;align-items:center;flex-wrap:wrap}
header b{font-size:18px}header nav{display:flex;gap:14px;flex-wrap:wrap}main{max-width:900px;margin:0 auto;padding:20px 16px 60px}
.note{background:var(--warn);border-radius:8px;padding:8px 12px;font-size:14px;margin:0 0 18px}
h1{font-size:30px;line-height:1.15;margin:8px 0 14px}h2{font-size:21px;margin:26px 0 8px}h3{font-size:16px;margin:18px 0 6px}
.muted{color:var(--muted)}.chip{display:inline-block;border:1px solid var(--line);border-radius:99px;padding:0 9px;font-size:13px;margin-right:6px}
table{border-collapse:collapse;width:100%;margin:8px 0}td,th{border-bottom:1px solid var(--line);padding:6px 8px 6px 0;text-align:left;vertical-align:top}
pre{background:var(--surface);border:1px solid var(--line);border-radius:8px;padding:10px;overflow:auto;font-size:13px}code{font-size:.92em}
img{max-width:100%;height:auto}ul.idx{padding-left:20px}.board{display:flex;gap:10px;overflow-x:auto}.board .col{flex:0 0 220px;background:var(--surface);border:1px solid var(--line);border-radius:10px;padding:8px}
.board .card{border:1px solid var(--line);border-radius:8px;padding:6px 8px;margin:6px 0;font-size:14px}.board .card small{display:block;color:var(--muted)}.board .done{opacity:.6;text-decoration:line-through}
.callout{border-left:3px solid var(--accent);padding:4px 12px;margin:10px 0;background:var(--surface)}
@media print{header,.note{display:none}body{background:#fff;color:#000}}
</style></head><body>
<header><b>{{.Project}}</b><nav><a href="index.html">{{.T.offline_home}}</a><a href="entries.html">{{.T.offline_entries}}</a></nav></header>
<main><p class="note">{{.Note}}</p>{{.Body}}</main></body></html>`))

type offlinePage struct {
	Lang, Title, Project, Note string
	T                          texts
	Body                       template.HTML
}

var apiFile = regexp.MustCompile(`/api/v1/files/([A-Za-z0-9]+)(/[^)\s"'<>]*)?`)

// WriteOffline writes the offline copy of a project as a ZIP.
func WriteOffline(ctx context.Context, w io.Writer, d core.OfflineData, open func(context.Context, store.File) (io.ReadCloser, error), locale string, now time.Time) error {
	t := textsFor(locale)
	lb := labelsFor(locale)
	lang := d.Project.MainLocale
	if lang == "" {
		lang = locale
	}
	z := zip.NewWriter(w)
	note := fmt.Sprintf(t["offline_note"], now.Format("2006-01-02 15:04"))
	write := func(name, title string, body string) error {
		f, err := z.Create(name)
		if err != nil {
			return err
		}
		return offlineTmpl.Execute(f, offlinePage{Lang: lang, Title: title + " – " + d.Project.Name, Project: d.Project.Name, Note: note, T: t, Body: template.HTML(body)})
	}

	// Files keep their name so browsers know the type when opened from disk.
	filePath := map[string]string{}
	for _, f := range d.Files {
		name := cleanFileName(f.Name)
		filePath[f.ID] = "files/" + f.ID + "/" + name
	}
	fixFiles := func(md string) string {
		return apiFile.ReplaceAllStringFunc(md, func(m string) string {
			id := apiFile.FindStringSubmatch(m)[1]
			if p, ok := filePath[id]; ok {
				return "/api/v1/files/" + strings.TrimPrefix(p, "files/")
			}
			return m
		})
	}
	page := func(slug string) string { return "p-" + slug + ".html" }
	links := Links{
		File: "files/",
		Page: func(project, doc string) string {
			for _, x := range d.Docs {
				if x.Slug == doc {
					return page(doc)
				}
			}
			return ""
		},
		Whiteboard: func(ref string) string {
			for _, x := range d.Docs {
				if x.Type == store.DocWhiteboard && (x.Slug == ref || x.ID == ref) {
					return whiteboardSVG(x, "files/")
				}
			}
			return ""
		},
		PageRef: func(ref string) string {
			if _, r, ok := strings.Cut(ref, "/"); ok {
				ref = r
			}
			if x, ok := matchDoc(d.Docs, ref); ok {
				return page(x.Slug)
			}
			return ""
		},
	}

	// Pages.
	for _, doc := range d.Docs {
		c := map[string]any{}
		_ = json.Unmarshal(doc.Content, &c)
		str := func(k string) string { v, _ := c[k].(string); return v }
		md := func(s string) string { return Markdown(fixFiles(s), links) }
		var b strings.Builder
		fmt.Fprintf(&b, `<h1>%s</h1>`, template.HTMLEscapeString(doc.Title))
		switch doc.Type {
		case store.DocMarkdown:
			b.WriteString(md(withoutTitle(str("markdown"), doc.Title)))
		case store.DocADR:
			fmt.Fprintf(&b, `<p><span class="chip">%s</span><span class="chip">%s</span><span class="chip">%s</span></p>`,
				core.DecisionRef(doc.Number), template.HTMLEscapeString(t["status_"+str("status")]), template.HTMLEscapeString(str("date")))
			for _, k := range []string{"context", "decision", "consequences", "alternatives"} {
				if v := str(k); v != "" {
					fmt.Fprintf(&b, `<h2>%s</h2>%s`, template.HTMLEscapeString(t[k]), md(v))
				}
			}
			if v := core.OpenOptions(doc.Content); v != "" {
				fmt.Fprintf(&b, `<h2>%s</h2>%s`, template.HTMLEscapeString(t["options"]), md(v))
			}
		case store.DocLink:
			fmt.Fprintf(&b, `<p><a href="%s">%s</a></p>%s`, template.HTMLEscapeString(str("url")), template.HTMLEscapeString(str("url")), md(str("description")))
		case store.DocBoard:
			b.WriteString(md(str("description")))
			b.WriteString(offlineBoard(doc, d.Tasks[doc.ID], t))
		case store.DocFloorplan:
			b.WriteString(md(str("description")))
			b.WriteString(floorplanSVG(doc, "files/", ""))
		case store.DocWhiteboard:
			b.WriteString(whiteboardSVG(doc, "files/"))
		case store.DocApp:
			fmt.Fprintf(&b, `<p>%s</p>`, template.HTMLEscapeString(t["app_not_public"]))
		case store.DocCalendar:
			fmt.Fprintf(&b, `<p>%s</p>`, template.HTMLEscapeString(t["calendar_not_public"]))
		}
		if err := write(page(doc.Slug), doc.Title, b.String()); err != nil {
			return err
		}
	}

	// Index: pages by category, decisions apart.
	var idx strings.Builder
	fmt.Fprintf(&idx, `<h1>%s</h1>`, template.HTMLEscapeString(d.Project.Name))
	if d.Project.Description != "" {
		fmt.Fprintf(&idx, `<p class="muted">%s</p>`, template.HTMLEscapeString(d.Project.Description))
	}
	cats := map[string][]store.Document{}
	var adrs []store.Document
	for _, doc := range d.Docs {
		if doc.Type == store.DocADR {
			adrs = append(adrs, doc)
			continue
		}
		cats[doc.Category] = append(cats[doc.Category], doc)
	}
	names := make([]string, 0, len(cats))
	for k := range cats {
		names = append(names, k)
	}
	sort.Slice(names, func(i, j int) bool {
		return names[i] != "" && (names[j] == "" || strings.ToLower(names[i]) < strings.ToLower(names[j]))
	})
	for _, cat := range names {
		title := cat
		if title == "" {
			title = t["uncategorized"]
		}
		fmt.Fprintf(&idx, `<h2>%s</h2><ul class="idx">`, template.HTMLEscapeString(title))
		docs := cats[cat]
		sort.Slice(docs, func(i, j int) bool {
			return docs[i].Position < docs[j].Position || (docs[i].Position == docs[j].Position && docs[i].Title < docs[j].Title)
		})
		for _, doc := range docs {
			fmt.Fprintf(&idx, `<li><a href="%s">%s</a></li>`, page(doc.Slug), template.HTMLEscapeString(doc.Title))
		}
		idx.WriteString(`</ul>`)
	}
	if len(adrs) > 0 {
		sort.Slice(adrs, func(i, j int) bool { return adrs[i].Number < adrs[j].Number })
		fmt.Fprintf(&idx, `<h2>%s</h2><ul class="idx">`, template.HTMLEscapeString(t["decisions"]))
		for _, doc := range adrs {
			fmt.Fprintf(&idx, `<li><a href="%s">%s · %s</a></li>`, page(doc.Slug), core.DecisionRef(doc.Number), template.HTMLEscapeString(doc.Title))
		}
		idx.WriteString(`</ul>`)
	}
	if len(d.Entities) > 0 {
		fmt.Fprintf(&idx, `<p><a href="entries.html">%s (%d)</a></p>`, template.HTMLEscapeString(t["offline_entries"]), len(d.Entities))
	}
	if err := write("index.html", d.Project.Name, idx.String()); err != nil {
		return err
	}

	// Entries: one list by kind and one page per entry with values and relations.
	kindName := func(id string) string {
		if k, ok := d.Model.Find(id); ok && k.Name != "" {
			return k.Name
		}
		return lb.get("kind", id)
	}
	byKind := map[string][]store.Entity{}
	for _, e := range d.Entities {
		byKind[e.Kind] = append(byKind[e.Kind], e)
	}
	kinds := make([]string, 0, len(byKind))
	for k := range byKind {
		kinds = append(kinds, k)
	}
	sort.Slice(kinds, func(i, j int) bool { return strings.ToLower(kindName(kinds[i])) < strings.ToLower(kindName(kinds[j])) })
	var list strings.Builder
	fmt.Fprintf(&list, `<h1>%s</h1>`, template.HTMLEscapeString(t["offline_entries"]))
	for _, k := range kinds {
		es := byKind[k]
		sort.Slice(es, func(i, j int) bool { return strings.ToLower(es[i].Name) < strings.ToLower(es[j].Name) })
		fmt.Fprintf(&list, `<h2>%s</h2><ul class="idx">`, template.HTMLEscapeString(kindName(k)))
		for _, e := range es {
			fmt.Fprintf(&list, `<li><a href="e-%s.html">%s</a></li>`, e.Slug, template.HTMLEscapeString(e.Name))
		}
		list.WriteString(`</ul>`)
	}
	if err := write("entries.html", t["offline_entries"], list.String()); err != nil {
		return err
	}
	for _, e := range d.Entities {
		var b strings.Builder
		fmt.Fprintf(&b, `<h1>%s</h1><p><span class="chip">%s</span></p>`, template.HTMLEscapeString(e.Name), template.HTMLEscapeString(kindName(e.Kind)))
		attrs := map[string]any{}
		_ = json.Unmarshal(e.Attrs, &attrs)
		if len(attrs) > 0 {
			labels := map[string]string{}
			var order []string
			if k, ok := d.Model.Find(e.Kind); ok {
				for _, a := range k.Attrs {
					labels[a.Name] = a.Label
					if a.Label == "" {
						labels[a.Name] = lb.get("attr", a.Name)
					}
					if a.Unit != "" {
						labels[a.Name] += " (" + a.Unit + ")"
					}
					order = append(order, a.Name)
				}
			}
			keys := make([]string, 0, len(attrs))
			for k := range attrs {
				keys = append(keys, k)
			}
			sort.SliceStable(keys, func(i, j int) bool { return pos(order, keys[i]) < pos(order, keys[j]) })
			b.WriteString(`<table>`)
			for _, k := range keys {
				label := labels[k]
				if label == "" {
					label = lb.get("attr", k)
				}
				v := attrs[k]
				s, ok := v.(string)
				if !ok {
					raw, _ := json.Marshal(v)
					s = string(raw)
				}
				fmt.Fprintf(&b, `<tr><th>%s</th><td>%s</td></tr>`, template.HTMLEscapeString(label), template.HTMLEscapeString(s))
			}
			b.WriteString(`</table>`)
		}
		var rels []string
		for _, x := range d.Edges {
			switch e.ID {
			case x.From:
				rels = append(rels, fmt.Sprintf(`<li>%s → <a href="e-%s.html">%s</a></li>`, template.HTMLEscapeString(lb.get("rel", x.Rel)), x.ToSlug, template.HTMLEscapeString(x.ToName)))
			case x.To:
				rels = append(rels, fmt.Sprintf(`<li><a href="e-%s.html">%s</a> → %s</li>`, x.FromSlug, template.HTMLEscapeString(x.FromName), template.HTMLEscapeString(lb.get("rel", x.Rel))))
			}
		}
		if len(rels) > 0 {
			fmt.Fprintf(&b, `<h2>%s</h2><ul class="idx">%s</ul>`, template.HTMLEscapeString(t["offline_relations"]), strings.Join(rels, ""))
		}
		var files []string
		for _, f := range d.Files {
			if f.EntityID == e.ID {
				files = append(files, fmt.Sprintf(`<li><a href="%s">%s</a></li>`, filePath[f.ID], template.HTMLEscapeString(f.Name)))
			}
		}
		if len(files) > 0 {
			fmt.Fprintf(&b, `<h2>%s</h2><ul class="idx">%s</ul>`, template.HTMLEscapeString(t["offline_files"]), strings.Join(files, ""))
		}
		if err := write("e-"+e.Slug+".html", e.Name, b.String()); err != nil {
			return err
		}
	}

	// Attachments.
	for _, f := range d.Files {
		rc, err := open(ctx, f)
		if err != nil {
			continue // a missing blob must not stop the copy
		}
		zf, err := z.CreateHeader(&zip.FileHeader{Name: filePath[f.ID], Method: zip.Store, Modified: now})
		if err == nil {
			_, err = io.Copy(zf, rc)
		}
		rc.Close()
		if err != nil {
			return err
		}
	}
	readme, _ := z.Create("README.txt")
	_, _ = io.WriteString(readme, fmt.Sprintf(t["offline_readme"], d.Project.Name, now.Format("2006-01-02 15:04")))
	return z.Close()
}

func pos(order []string, k string) int {
	for i, x := range order {
		if x == k {
			return i
		}
	}
	return len(order)
}

// cleanFileName keeps a name that is safe as a path inside the ZIP.
func cleanFileName(name string) string {
	name = path.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`"<>:|?*/`, r) {
			return '_'
		}
		return r
	}, name)
	if name == "" || name == "." || name == ".." {
		name = "file"
	}
	return name
}

func offlineBoard(doc store.Document, tasks []store.Task, t texts) string {
	var b struct {
		Columns []core.BoardColumn `json:"columns"`
	}
	_ = json.Unmarshal(doc.Content, &b)
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
			meta := append([]string{}, x.Labels...)
			if x.Due != "" {
				meta = append(meta, x.Due)
			}
			if x.AssigneeName != "" {
				meta = append(meta, x.AssigneeName)
			} else if x.AssigneeText != "" {
				meta = append(meta, x.AssigneeText)
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
