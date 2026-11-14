package resources

import (
	"archive/zip"
	"bytes"
	"io"
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

func TestWriteSkill(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteSkill(&buf); err != nil {
		t.Fatal(err)
	}
	z, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]string{}
	for _, f := range z.File {
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		names[f.Name] = string(b)
	}
	md := names["phoedox/SKILL.md"]
	if !strings.HasPrefix(md, "---\nname: phoedox\ndescription: ") || names["phoedox/references/workflows.md"] == "" || names["phoedox/references/page-syntax.md"] == "" {
		t.Fatalf("zip: %v", len(names))
	}
	// The front matter follows the SKILL.md rules.
	desc := strings.SplitN(strings.SplitN(md, "description: ", 2)[1], "\n", 2)[0]
	if len(desc) > 1024 || strings.ContainsAny(desc, "<>") {
		t.Fatalf("description: %d", len(desc))
	}
}
