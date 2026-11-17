// Package resources holds texts that should be easy to change without
// touching code: system prompts for AI clients and email templates. Files
// are embedded at compile time; PHOEDOX_PROMPTS_DIR and PHOEDOX_MAIL_DIR can
// override single files at startup.
package resources

import (
	"archive/zip"
	"bytes"
	"embed"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"text/template"
)

//go:embed prompts/*.md
var prompts embed.FS

//go:embed mail/*.md mail/layout.html
var mails embed.FS

//go:embed legal/*.md
var legal embed.FS

//go:embed tracker/*.md
var trackerTexts embed.FS

// TrackerText is the text of an issue or comment Phoedox writes into an
// external tracker (resources/tracker/<name>.<lang>.md, English as
// fallback), a text/template.
func TrackerText(name, lang string, data any) (string, error) {
	src, err := trackerTexts.ReadFile("tracker/" + name + "." + lang + ".md")
	if err != nil {
		if src, err = trackerTexts.ReadFile("tracker/" + name + ".en.md"); err != nil {
			return "", fmt.Errorf("resources: unknown tracker text %s", name)
		}
	}
	tpl, err := template.New(name).Option("missingkey=error").Parse(string(src))
	if err != nil {
		return "", err
	}
	var out bytes.Buffer
	if err := tpl.Execute(&out, data); err != nil {
		return "", err
	}
	return out.String(), nil
}

// Legal returns a legal text (resources/legal/<name>.<lang>.md, English as
// fallback), filled with data when it is a template. PHOEDOX_MAIL_DIR does
// not apply; operators publish their own texts in the administration.
func Legal(name, lang string, data any) (string, error) {
	src, err := legal.ReadFile("legal/" + name + "." + lang + ".md")
	if err != nil {
		if src, err = legal.ReadFile("legal/" + name + ".en.md"); err != nil {
			return "", fmt.Errorf("resources: unknown legal text %s", name)
		}
	}
	if data == nil {
		return string(src), nil
	}
	tpl, err := template.New(name).Option("missingkey=error").Parse(string(src))
	if err != nil {
		return "", err
	}
	var out bytes.Buffer
	if err := tpl.Execute(&out, data); err != nil {
		return "", err
	}
	return out.String(), nil
}

// MailLayout is the HTML frame of e-mails (resources/mail/layout.html, or
// PHOEDOX_MAIL_DIR/layout.html), a html/template.
func MailLayout() string {
	if dir := os.Getenv("PHOEDOX_MAIL_DIR"); dir != "" {
		if b, err := os.ReadFile(filepath.Join(dir, "layout.html")); err == nil {
			return string(b)
		}
	}
	b, _ := mails.ReadFile("mail/layout.html")
	return string(b)
}

//go:embed texts/*.txt
var texts embed.FS

// Text returns a fixed text file shipped with Phoedox (resources/texts),
// e.g. the README of personal data exports.
func Text(name string) (string, error) {
	b, err := texts.ReadFile("texts/" + name)
	return string(b), err
}

//go:embed publish/*.json
var publishTexts embed.FS

// PublishTexts returns the interface texts of published sites
// (resources/publish/<locale>.json; "en" if the locale is unknown).
// PublishLabels returns the names of kinds, attributes, relations and
// choices (copied from the web app by internal/publish/labelsgen).
func PublishLabels(locale string) []byte {
	b, err := publishTexts.ReadFile("publish/labels." + locale + ".json")
	if err != nil {
		b, _ = publishTexts.ReadFile("publish/labels.en.json")
	}
	return b
}

func PublishTexts(locale string) []byte {
	b, err := publishTexts.ReadFile("publish/" + locale + ".json")
	if err != nil {
		b, _ = publishTexts.ReadFile("publish/en.json")
	}
	return b
}

//go:embed templates/*.json
var templates embed.FS

// Template returns resources/templates/<id>.<locale>.json (English if the
// locale is missing) or nil for an unknown template.
func Template(id, locale string) []byte {
	b, err := templates.ReadFile("templates/" + id + "." + locale + ".json")
	if err != nil {
		b, err = templates.ReadFile("templates/" + id + ".en.json")
	}
	if err != nil {
		return nil
	}
	return b
}

// TemplateIDs lists the project templates.
func TemplateIDs() []string {
	entries, _ := templates.ReadDir("templates")
	var ids []string
	seen := map[string]bool{}
	for _, e := range entries {
		id, _, _ := strings.Cut(e.Name(), ".")
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids
}

//go:embed apps
var appStarters embed.FS

// AppStarter returns the files of a built-in app kind (resources/apps/<name>)
// and its manifest.json; ok is false for an unknown kind.
func AppStarter(name string) (files map[string]string, manifest []byte, ok bool) {
	if name == "" || strings.ContainsAny(name, "/.\\") {
		return nil, nil, false
	}
	entries, err := appStarters.ReadDir("apps/" + name)
	if err != nil {
		return nil, nil, false
	}
	files = map[string]string{}
	for _, e := range entries {
		b, err := appStarters.ReadFile("apps/" + name + "/" + e.Name())
		if err != nil {
			return nil, nil, false
		}
		if e.Name() == "manifest.json" {
			manifest = b
			continue
		}
		files[e.Name()] = string(b)
	}
	return files, manifest, true
}

// AppStarters lists the built-in app kinds.
func AppStarters() []string {
	entries, _ := appStarters.ReadDir("apps")
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	return out
}

//go:embed kinds/core.json
var coreKinds []byte

// CoreKinds returns the definition of the kinds that ship with Phoedox
// (resources/kinds/core.json). Labels live in the web i18n tables.
func CoreKinds() []byte { return coreKinds }

var (
	mu    sync.Mutex
	cache = map[string]string{}
)

// Prompt returns resources/prompts/<name>.md. If PHOEDOX_PROMPTS_DIR is set
// and contains <name>.md, that file wins (read once, at first use).
func Prompt(name string) string {
	mu.Lock()
	defer mu.Unlock()
	if p, ok := cache[name]; ok {
		return p
	}
	var text string
	if dir := os.Getenv("PHOEDOX_PROMPTS_DIR"); dir != "" {
		if b, err := os.ReadFile(filepath.Join(dir, name+".md")); err == nil {
			text = string(b)
			slog.Info("prompt override loaded", "name", name, "dir", dir)
		}
	}
	if text == "" {
		b, err := prompts.ReadFile("prompts/" + name + ".md")
		if err != nil {
			panic("resources: unknown prompt " + name)
		}
		text = string(b)
	}
	text = strings.TrimSpace(text)
	cache[name] = text
	return text
}

// Mail renders resources/mail/<name>.<locale>.md (falling back to German)
// with data. The first line is "Subject: …", followed by a blank line and
// the plain-text body. PHOEDOX_MAIL_DIR can override single templates.
func Mail(name, locale string, data any) (subject, body string, err error) {
	if locale != "en" {
		locale = "de"
	}
	file := name + "." + locale + ".md"
	var src []byte
	if dir := os.Getenv("PHOEDOX_MAIL_DIR"); dir != "" {
		src, _ = os.ReadFile(filepath.Join(dir, file))
	}
	if src == nil {
		if src, err = mails.ReadFile("mail/" + file); err != nil {
			return "", "", fmt.Errorf("resources: unknown mail %s", file)
		}
	}
	tpl, err := template.New(file).Option("missingkey=error").Parse(string(src))
	if err != nil {
		return "", "", err
	}
	var out bytes.Buffer
	if err := tpl.Execute(&out, data); err != nil {
		return "", "", err
	}
	head, rest, _ := strings.Cut(out.String(), "\n")
	subject, ok := strings.CutPrefix(strings.TrimSpace(head), "Subject:")
	if !ok {
		return "", "", fmt.Errorf("resources: %s must start with a Subject line", file)
	}
	return strings.TrimSpace(subject), strings.TrimSpace(rest) + "\n", nil
}

//go:embed skills
var skills embed.FS

// SkillText returns all files of the Phoedox skill joined (tests check that
// every MCP tool is explained).
func SkillText() string {
	var b strings.Builder
	_ = fs.WalkDir(skills, "skills", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			c, _ := skills.ReadFile(p)
			b.Write(c)
		}
		return nil
	})
	return b.String()
}

// SkillFile is one file of the Phoedox skill, Path relative to the skills
// folder (phoedox/SKILL.md, phoedox/references/pages.md …).
type SkillFile struct {
	Path    string
	Content []byte
}

// SkillFiles lists the files of the Phoedox skill (served over MCP).
func SkillFiles() []SkillFile {
	var out []SkillFile
	_ = fs.WalkDir(skills, "skills", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			c, _ := skills.ReadFile(p)
			out = append(out, SkillFile{Path: strings.TrimPrefix(p, "skills/"), Content: c})
		}
		return nil
	})
	return out
}

// WriteSkill writes the Phoedox skill for AI clients (resources/skills/phoedox)
// as a ZIP with the folder phoedox/ (SKILL.md and references).
func WriteSkill(w io.Writer) error {
	z := zip.NewWriter(w)
	err := fs.WalkDir(skills, "skills", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := skills.ReadFile(p)
		if err != nil {
			return err
		}
		f, err := z.Create(strings.TrimPrefix(p, "skills/"))
		if err != nil {
			return err
		}
		_, err = f.Write(b)
		return err
	})
	if err != nil {
		return err
	}
	return z.Close()
}
