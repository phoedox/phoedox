// Package resources holds texts that should be easy to change without
// touching code: system prompts for AI clients and email templates. Files
// are embedded at compile time; PHOEDOX_PROMPTS_DIR and PHOEDOX_MAIL_DIR can
// override single files at startup.
package resources

import (
	"bytes"
	"embed"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"text/template"
)

//go:embed prompts/*.md
var prompts embed.FS

//go:embed mail/*.md
var mails embed.FS

//go:embed publish/*.json
var publishTexts embed.FS

// PublishTexts returns the interface texts of published sites
// (resources/publish/<locale>.json; "en" if the locale is unknown).
func PublishTexts(locale string) []byte {
	b, err := publishTexts.ReadFile("publish/" + locale + ".json")
	if err != nil {
		b, _ = publishTexts.ReadFile("publish/en.json")
	}
	return b
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
