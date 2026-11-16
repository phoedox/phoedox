package core

import (
	"strings"
	"testing"
)

func TestSection(t *testing.T) {
	md := "# Title\n\nIntro\n\n## 6 Backups\n\nText\n\n### 6.4 Restore\n\nSteps\n```sh\n# not a heading\n```\n\n### 6.41 Other\n\nX\n\n## 7 Next\n"
	if s, _, ok := Section(md, "6.4"); !ok || !strings.HasPrefix(s, "### 6.4 Restore") || !strings.Contains(s, "# not a heading") || strings.Contains(s, "6.41") {
		t.Fatalf("6.4: %q", s)
	}
	if s, _, ok := Section(md, "§6"); !ok || !strings.Contains(s, "6.41 Other") || strings.Contains(s, "7 Next") {
		t.Fatalf("6: %q", s)
	}
	if s, _, ok := Section(md, "restore"); !ok || !strings.HasPrefix(s, "### 6.4 Restore") {
		t.Fatalf("by text: %q", s)
	}
	if _, outline, ok := Section(md, "missing"); ok || len(outline) != 5 {
		t.Fatalf("outline: %v", outline)
	}
}
