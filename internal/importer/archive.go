package importer

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Archive is the content of an uploaded ZIP: pages and the files they use.
// Page Markdown refers to other pages of the archive as "page:<path>" and
// to files as "asset:<path>"; the caller replaces them once the pages and
// files exist (see ReplaceRefs).
type Archive struct {
	// Format is markdown (also Obsidian), notion, confluence or bookstack.
	Format  string
	Pages   []Page
	Assets  []Asset
	Skipped []string
}

// Asset is an image or document a page uses.
type Asset struct {
	Path string
	Name string
	Data []byte
}

// Limits for files that come with pages.
const (
	MaxAssets      = 1000
	MaxAssetSize   = 20 << 20
	MaxAssetsTotal = 300 << 20
)

// assetExt lists the file types kept as attachments. SVG is left out: it
// can carry scripts.
var assetExt = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true, ".pdf": true}

var (
	notionIDRe  = regexp.MustCompile(`\s+[0-9a-f]{32}$`)
	mdLinkRe    = regexp.MustCompile(`(!?)\[([^\]]*)\]\(<?([^)>\s]+)>?(\s+"[^"]*")?\)`)
	refRe       = regexp.MustCompile(`\((page|asset):([^)\s]+)\)`)
	bsRefRe     = regexp.MustCompile(`\[\[bsexport:(page|image|attachment|chapter|book):(\d+)\]\]`)
	confTitleRe = regexp.MustCompile(`(?s)<title>(.*?)</title>`)
)

type entry struct {
	name string
	f    *zip.File
}

// ReadArchive reads a ZIP of Markdown files (also Obsidian vaults and
// Notion exports), a Confluence HTML space export or a BookStack ZIP export.
func ReadArchive(data []byte) (Archive, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return Archive{}, ErrNotZip
	}
	files := map[string]*zip.File{}
	var names []string
	for _, f := range zr.File {
		name := path.Clean(strings.ReplaceAll(f.Name, "\\", "/"))
		if f.FileInfo().IsDir() || strings.HasPrefix(name, "../") || strings.HasPrefix(name, "/") || name == "." {
			continue
		}
		hidden := false
		for _, part := range strings.Split(name, "/") {
			if strings.HasPrefix(part, ".") || part == "__MACOSX" {
				hidden = true
			}
		}
		if !hidden {
			files[name] = f
			names = append(names, name)
		}
	}
	sort.Strings(names)
	r := &reader{files: files, names: names}
	switch {
	case r.find("data.json") != "":
		return r.bookstack()
	case r.confluence():
		return r.readConfluence()
	default:
		return r.markdown()
	}
}

type reader struct {
	files    map[string]*zip.File
	names    []string
	assets   []Asset
	assetSet map[string]bool
	total    int
	skipped  []string
}

// find returns the archive path of a file at the root or in a single top folder.
func (r *reader) find(base string) string {
	for _, n := range r.names {
		if n == base || (strings.Count(n, "/") == 1 && path.Base(n) == base) {
			return n
		}
	}
	return ""
}

func (r *reader) read(name string, limit int) ([]byte, error) {
	f := r.files[name]
	if f == nil {
		return nil, fmt.Errorf("importer: %s missing", name)
	}
	if f.UncompressedSize64 > uint64(limit) {
		return nil, fmt.Errorf("importer: %s too large", name)
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	b, err := io.ReadAll(io.LimitReader(rc, int64(limit)+1))
	if err == nil && len(b) > limit {
		err = fmt.Errorf("importer: %s too large", name)
	}
	return b, err
}

// asset keeps a file a page refers to; it returns false if it cannot be kept.
func (r *reader) asset(p string) bool {
	if r.assetSet == nil {
		r.assetSet = map[string]bool{}
	}
	if r.assetSet[p] {
		return true
	}
	if r.files[p] == nil || !assetExt[strings.ToLower(path.Ext(p))] || len(r.assets) >= MaxAssets {
		return false
	}
	b, err := r.read(p, MaxAssetSize)
	if err != nil || r.total+len(b) > MaxAssetsTotal {
		r.skipped = append(r.skipped, p)
		return false
	}
	r.total += len(b)
	r.assetSet[p] = true
	r.assets = append(r.assets, Asset{Path: p, Name: path.Base(p), Data: b})
	return true
}

// resolve turns a link in the page at from into an archive path ("" for
// external links and anchors).
func resolve(from, target string) string {
	if target == "" || strings.HasPrefix(target, "#") || strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") || strings.HasPrefix(target, "/") {
		return ""
	}
	target, _, _ = strings.Cut(target, "#")
	target, _, _ = strings.Cut(target, "?")
	if u, err := url.PathUnescape(target); err == nil {
		target = u
	}
	p := path.Clean(path.Join(path.Dir(from), target))
	if strings.HasPrefix(p, "../") {
		return ""
	}
	return p
}

// ---- Markdown, Obsidian, Notion ----

func (r *reader) markdown() (Archive, error) {
	a := Archive{Format: "markdown"}
	var mds []string
	notion := false
	for _, n := range r.names {
		ext := strings.ToLower(path.Ext(n))
		if ext == ".md" || ext == ".markdown" {
			mds = append(mds, n)
			if notionIDRe.MatchString(strings.TrimSuffix(path.Base(n), path.Ext(n))) {
				notion = true
			}
		}
	}
	if notion {
		a.Format = "notion"
	}
	if len(mds) > MaxFiles {
		return a, fmt.Errorf("importer: more than %d Markdown files", MaxFiles)
	}
	byBase := map[string]string{}
	for _, n := range r.names {
		if _, ok := byBase[strings.ToLower(path.Base(n))]; !ok {
			byBase[strings.ToLower(path.Base(n))] = n
		}
	}
	prefix := commonTop(mds)
	isMD := map[string]bool{}
	for _, n := range mds {
		isMD[n] = true
	}
	used := map[string]bool{}
	for _, n := range mds {
		b, err := r.read(n, MaxFileSize)
		if err != nil {
			r.skipped = append(r.skipped, n)
			continue
		}
		used[n] = true
		rel := strings.TrimPrefix(n, prefix)
		dir := path.Dir(rel)
		if dir == "." {
			dir = ""
		}
		key := strings.TrimSuffix(path.Base(rel), path.Ext(rel))
		if notion {
			key = notionIDRe.ReplaceAllString(key, "")
			parts := strings.Split(dir, "/")
			for i := range parts {
				parts[i] = notionIDRe.ReplaceAllString(parts[i], "")
			}
			dir = strings.Join(parts, "/")
		}
		md, meta := frontMatter(strings.ReplaceAll(string(b), "\r\n", "\n"))
		title := meta["title"]
		if title == "" {
			if m := h1Re.FindStringSubmatch(md); m != nil {
				title = m[1]
				if notion {
					// Notion repeats the title as the first heading.
					md = strings.TrimLeft(strings.Replace(md, m[0], "", 1), "\n")
				}
			} else {
				title = key
			}
		}
		// Relative links and images.
		md = mdLinkRe.ReplaceAllStringFunc(md, func(m string) string {
			p := mdLinkRe.FindStringSubmatch(m)
			target := resolve(n, p[3])
			switch {
			case target == "":
				return m
			case isMD[target]:
				return p[1] + "[" + p[2] + "](page:" + refPath(target) + ")"
			case r.asset(target):
				used[target] = true
				return p[1] + "[" + p[2] + "](asset:" + refPath(target) + ")"
			}
			return m
		})
		// Obsidian embeds ![[file.png]] of kept files.
		md = wikiRe.ReplaceAllStringFunc(md, func(m string) string {
			p := wikiRe.FindStringSubmatch(m)
			if p[1] != "!" {
				return m
			}
			target := byBase[strings.ToLower(path.Base(strings.TrimSpace(p[2])))]
			if target != "" && r.asset(target) {
				used[target] = true
				return "![" + path.Base(target) + "](asset:" + refPath(target) + ")"
			}
			return m
		})
		a.Pages = append(a.Pages, Page{Path: n, Category: dir, Title: strings.TrimSpace(title), Key: key, Markdown: md})
	}
	for _, n := range r.names {
		if !used[n] && !r.assetSet[n] {
			r.skipped = append(r.skipped, n)
		}
	}
	return r.done(a), nil
}

// commonTop is a single top folder shared by all paths ("" if none).
func commonTop(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	first, _, ok := strings.Cut(paths[0], "/")
	if !ok {
		return ""
	}
	prefix := first + "/"
	for _, p := range paths {
		if !strings.HasPrefix(p, prefix) {
			return ""
		}
	}
	return prefix
}

// refPath escapes a path for use inside a Markdown link.
func refPath(p string) string {
	return strings.NewReplacer(" ", "%20", "(", "%28", ")", "%29").Replace(p)
}

func (r *reader) done(a Archive) Archive {
	sort.SliceStable(a.Pages, func(i, j int) bool {
		return a.Pages[i].Category+"/"+a.Pages[i].Title < a.Pages[j].Category+"/"+a.Pages[j].Title
	})
	a.Assets = r.assets
	a.Skipped = append(a.Skipped, r.skipped...)
	return a
}

// ---- Confluence (space export as HTML) ----

func (r *reader) confluence() bool {
	for _, n := range r.names {
		if strings.HasSuffix(n, ".html") && path.Base(n) != "index.html" {
			b, err := r.read(n, MaxFileSize)
			if err != nil {
				continue
			}
			s := string(b)
			return strings.Contains(s, `id="main-content"`) && (strings.Contains(s, "Confluence") || strings.Contains(s, `id="title-text"`))
		}
	}
	return false
}

func (r *reader) readConfluence() (Archive, error) {
	a := Archive{Format: "confluence"}
	isPage := map[string]bool{}
	for _, n := range r.names {
		if strings.HasSuffix(n, ".html") && path.Base(n) != "index.html" {
			isPage[n] = true
		}
	}
	if len(isPage) > MaxFiles {
		return a, fmt.Errorf("importer: more than %d pages", MaxFiles)
	}
	for _, n := range r.names {
		if !isPage[n] {
			continue
		}
		b, err := r.read(n, MaxFileSize)
		if err != nil {
			r.skipped = append(r.skipped, n)
			continue
		}
		src := string(b)
		title := strings.TrimSuffix(path.Base(n), ".html")
		if m := confTitleRe.FindStringSubmatch(src); m != nil {
			t := strings.TrimSpace(htmlText(m[1]))
			if _, after, ok := strings.Cut(t, " : "); ok {
				t = after
			}
			if t != "" {
				title = t
			}
		}
		crumbs := confluenceCrumbs(src)
		main := between(src, `id="main-content"`, `id="footer"`)
		if main == "" {
			main = src
		} else if i := strings.Index(main, ">"); i >= 0 {
			main = "<div " + main[:i+1] + main[i+1:]
		}
		md := HTMLToMarkdown(main, func(u string, image bool) string {
			target := resolve(n, u)
			switch {
			case target == "":
				return u
			case isPage[target] && !image:
				return "page:" + target
			case r.asset(target):
				return "asset:" + target
			}
			return u
		})
		a.Pages = append(a.Pages, Page{Path: n, Category: strings.Join(crumbs, "/"), Title: title, Key: title, Markdown: md})
	}
	return r.done(a), nil
}

// confluenceCrumbs reads the breadcrumb trail without the space home.
func confluenceCrumbs(src string) []string {
	sec := between(src, `id="breadcrumbs"`, "</ol>")
	if sec == "" {
		return nil
	}
	var out []string
	for _, m := range regexp.MustCompile(`(?s)<a[^>]*>(.*?)</a>`).FindAllStringSubmatch(sec, -1) {
		if t := strings.TrimSpace(htmlText(m[1])); t != "" {
			out = append(out, strings.ReplaceAll(t, "/", "-"))
		}
	}
	if len(out) > 0 {
		out = out[1:] // the space itself
	}
	return out
}

func between(s, start, end string) string {
	i := strings.Index(s, start)
	if i < 0 {
		return ""
	}
	rest := s[i+len(start):]
	if j := strings.Index(rest, end); j >= 0 {
		rest = rest[:j]
	}
	return rest
}

func htmlText(s string) string {
	return strings.TrimSpace(HTMLToMarkdownText(s))
}

// HTMLToMarkdownText returns the plain text of an HTML fragment.
func HTMLToMarkdownText(s string) string {
	md := HTMLToMarkdown("<p>"+s+"</p>", nil)
	return strings.NewReplacer(`\*`, "*", `\_`, "_", `\[`, "[", `\]`, "]", `\\`, `\`).Replace(strings.TrimSpace(md))
}

// ---- BookStack (portable ZIP export) ----

type bsImage struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	File string `json:"file"`
}

type bsPage struct {
	ID          int       `json:"id"`
	Name        string    `json:"name"`
	HTML        string    `json:"html"`
	Markdown    string    `json:"markdown"`
	Images      []bsImage `json:"images"`
	Attachments []bsImage `json:"attachments"`
}

type bsChapter struct {
	ID    int      `json:"id"`
	Name  string   `json:"name"`
	Pages []bsPage `json:"pages"`
}

type bsBook struct {
	Name     string      `json:"name"`
	Chapters []bsChapter `json:"chapters"`
	Pages    []bsPage    `json:"pages"`
}

func (r *reader) bookstack() (Archive, error) {
	a := Archive{Format: "bookstack"}
	dataPath := r.find("data.json")
	b, err := r.read(dataPath, 64<<20)
	if err != nil {
		return a, err
	}
	var d struct {
		Book    *bsBook    `json:"book"`
		Chapter *bsChapter `json:"chapter"`
		Page    *bsPage    `json:"page"`
	}
	if err := json.Unmarshal(b, &d); err != nil {
		return a, fmt.Errorf("importer: data.json: %w", err)
	}
	base := path.Dir(dataPath)
	if base == "." {
		base = ""
	} else {
		base += "/"
	}
	type item struct {
		page     bsPage
		category string
	}
	var items []item
	switch {
	case d.Book != nil:
		for _, p := range d.Book.Pages {
			items = append(items, item{p, ""})
		}
		for _, c := range d.Book.Chapters {
			for _, p := range c.Pages {
				items = append(items, item{p, c.Name})
			}
		}
	case d.Chapter != nil:
		for _, p := range d.Chapter.Pages {
			items = append(items, item{p, ""})
		}
	case d.Page != nil:
		items = append(items, item{*d.Page, ""})
	default:
		return a, fmt.Errorf("importer: data.json has no book, chapter or page")
	}
	if len(items) > MaxFiles {
		return a, fmt.Errorf("importer: more than %d pages", MaxFiles)
	}
	files := map[string]string{} // "image:5" → archive path
	for _, it := range items {
		for _, im := range it.page.Images {
			files["image:"+strconv.Itoa(im.ID)] = base + "files/" + im.File
		}
		for _, at := range it.page.Attachments {
			files["attachment:"+strconv.Itoa(at.ID)] = base + "files/" + at.File
		}
	}
	ref := func(kind, id string) string {
		switch kind {
		case "page":
			return "page:bookstack/" + id
		case "image", "attachment":
			if p := files[kind+":"+id]; p != "" && r.asset(p) {
				return "asset:" + refPath(p)
			}
		}
		return ""
	}
	for _, it := range items {
		md := it.page.Markdown
		if strings.TrimSpace(md) == "" {
			md = HTMLToMarkdown(bsRefRe.ReplaceAllStringFunc(it.page.HTML, func(m string) string {
				p := bsRefRe.FindStringSubmatch(m)
				return ref(p[1], p[2])
			}), nil)
		}
		md = bsRefRe.ReplaceAllStringFunc(md, func(m string) string {
			p := bsRefRe.FindStringSubmatch(m)
			return ref(p[1], p[2])
		})
		a.Pages = append(a.Pages, Page{Path: "bookstack/" + strconv.Itoa(it.page.ID), Category: it.category, Title: strings.TrimSpace(it.page.Name), Key: it.page.Name, Markdown: md})
	}
	return r.done(a), nil
}

// ReplaceRefs turns page: and asset: references into links: page(path)
// and asset(path) return the URL ("" leaves the plain text for pages and
// drops images that could not be stored).
func ReplaceRefs(md string, page, asset func(path string) string) string {
	return refRe.ReplaceAllStringFunc(md, func(m string) string {
		p := refRe.FindStringSubmatch(m)
		target, _ := url.PathUnescape(p[2])
		var u string
		if p[1] == "page" {
			u = page(target)
		} else {
			u = asset(target)
		}
		if u == "" {
			u = "#"
		}
		return "(" + u + ")"
	})
}
