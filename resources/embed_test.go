package resources

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPromptEmbeddedAndOverride(t *testing.T) {
	if !strings.Contains(Prompt("mcp-instructions"), "Phoedox") {
		t.Fatal("embedded prompt missing")
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "mcp-instructions.md"), []byte("Eigene Anleitung\n"), 0o644)
	t.Setenv("PHOEDOX_PROMPTS_DIR", dir)
	mu.Lock()
	delete(cache, "mcp-instructions")
	mu.Unlock()
	if got := Prompt("mcp-instructions"); got != "Eigene Anleitung" {
		t.Fatalf("override not used: %q", got)
	}
}

func TestMail(t *testing.T) {
	data := map[string]any{"Name": "Jan", "Inviter": "Admin", "Instance": "docs.example.org", "Link": "https://docs.example.org/invite/abc", "Hours": 168}
	for _, name := range []string{"invite", "reset"} {
		for _, loc := range []string{"de", "en", "fr"} {
			subj, body, err := Mail(name, loc, data)
			if err != nil || subj == "" || !strings.Contains(body, "https://docs.example.org/invite/abc") || strings.Contains(body, "Subject:") {
				t.Fatalf("%s.%s: %v %q %q", name, loc, err, subj, body)
			}
		}
	}
	if _, _, err := Mail("invite", "de", map[string]any{}); err == nil {
		t.Fatal("missing fields must fail")
	}
}
