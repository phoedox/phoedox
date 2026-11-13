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
