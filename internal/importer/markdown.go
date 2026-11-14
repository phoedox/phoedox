// Package importer turns other documentation into Phoedox pages: folders of
// Markdown files (also Obsidian vaults) and OpenAPI descriptions. It only
// parses; creating pages is the caller's job.
package importer

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"sort"
	"strings"
)

// Page is a page to create.
type Page struct {
	Category string // folder path, "A/B"
	Title    string
	Key      string // file name without extension, for wiki links
	Markdown string
}

// Limits keep imports small enough for one request.
const (
	MaxFiles    = 2000
	MaxFileSize = 2 << 20
)

// ErrNotZip means the upload is not a ZIP archive.
var ErrNotZip = errors.New("importer: not a ZIP archive")

var (
	h1Re   = regexp.MustCompile(`(?m)^#\s+(.+?)\s*#*\s*$`)
	wikiRe = regexp.MustCompile(`(!?)\[\[([^\]|#]+)(#[^\]|]*)?(?:\|([^\]]+))?\]\]`)
)

// MarkdownZip reads the Markdown files of a ZIP archive. Hidden folders
// (.obsidian, .git) and other files are skipped and listed.
func MarkdownZip(data []byte) (pages []Page, skipped []string, err error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, nil, ErrNotZip
	}
	type file struct {
		name string
		f    *zip.File
	}
	var files []file
	for _, f := range zr.File {
		name := path.Clean(strings.ReplaceAll(f.Name, "\\", "/"))
		if f.FileInfo().IsDir() || strings.HasPrefix(name, "../") || strings.HasPrefix(name, "/") {
			continue
		}
		hidden := false
		for _, part := range strings.Split(name, "/") {
			if strings.HasPrefix(part, ".") || part == "__MACOSX" {
				hidden = true
			}
		}
		ext := strings.ToLower(path.Ext(name))
		if hidden {
			continue
		}
		if ext != ".md" && ext != ".markdown" {
			skipped = append(skipped, name)
			continue
		}
		if len(files) >= MaxFiles {
			return nil, nil, fmt.Errorf("importer: more than %d Markdown files", MaxFiles)
		}
		files = append(files, file{name, f})
	}
	// A single top folder (the vault or repository name) is not a category.
	prefix := ""
	if len(files) > 0 {
		first, _, ok := strings.Cut(files[0].name, "/")
		if ok {
			prefix = first + "/"
			for _, f := range files {
				if !strings.HasPrefix(f.name, prefix) {
					prefix = ""
					break
				}
			}
		}
	}
	for _, f := range files {
		if f.f.UncompressedSize64 > MaxFileSize {
			skipped = append(skipped, f.name)
			continue
		}
		rc, err := f.f.Open()
		if err != nil {
			return nil, nil, err
		}
		b, err := io.ReadAll(io.LimitReader(rc, MaxFileSize+1))
		rc.Close()
		if err != nil {
			return nil, nil, err
		}
		rel := strings.TrimPrefix(f.name, prefix)
		dir := path.Dir(rel)
		if dir == "." {
			dir = ""
		}
		key := strings.TrimSuffix(path.Base(rel), path.Ext(rel))
		md, meta := frontMatter(strings.ReplaceAll(string(b), "\r\n", "\n"))
		title := meta["title"]
		if title == "" {
			if m := h1Re.FindStringSubmatch(md); m != nil {
				title = m[1]
			} else {
				title = key
			}
		}
		pages = append(pages, Page{Category: dir, Title: strings.TrimSpace(title), Key: key, Markdown: md})
	}
	sort.SliceStable(pages, func(i, j int) bool {
		return pages[i].Category+"/"+pages[i].Title < pages[j].Category+"/"+pages[j].Title
	})
	return pages, skipped, nil
}

// frontMatter removes a leading YAML block and returns its simple key: value pairs.
func frontMatter(md string) (string, map[string]string) {
	meta := map[string]string{}
	if !strings.HasPrefix(md, "---\n") {
		return md, meta
	}
	end := strings.Index(md[4:], "\n---")
	if end < 0 {
		return md, meta
	}
	for _, line := range strings.Split(md[4:4+end], "\n") {
		if k, v, ok := strings.Cut(line, ":"); ok {
			meta[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `"'`)
		}
	}
	rest := md[4+end+4:]
	return strings.TrimLeft(rest, "\n"), meta
}

// ResolveWikiLinks turns [[Page]], [[Page|Text]] and [[Page#Part]] into
// Markdown links with url(key) ("" keeps the text). ![[file]] embeds become
// plain text.
func ResolveWikiLinks(md string, url func(key string) string) string {
	return wikiRe.ReplaceAllStringFunc(md, func(m string) string {
		p := wikiRe.FindStringSubmatch(m)
		embed, target, text := p[1] == "!", strings.TrimSpace(p[2]), strings.TrimSpace(p[4])
		if text == "" {
			text = target
		}
		if embed {
			return text
		}
		if u := url(target); u != "" {
			return "[" + text + "](" + u + ")"
		}
		return text
	})
}
