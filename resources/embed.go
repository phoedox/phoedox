// Package resources holds texts that should be easy to change without
// touching code, e.g. system prompts for AI clients. Files are embedded at
// compile time; PHOEDOX_PROMPTS_DIR can override single prompts at startup.
package resources

import (
	"embed"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

//go:embed prompts/*.md
var prompts embed.FS

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
