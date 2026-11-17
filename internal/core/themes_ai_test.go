package core

import (
	"context"
	"testing"
)

func TestGenerateTheme(t *testing.T) {
	ctx := context.Background()
	s, owner, _, _ := setup(t)
	s.AI = &fakeAI{text: `Here: {"name":"Forest","light":{"accent":"#2E7D32","fg":"#cccccc","bg":"#f7faf7","evil":"red"},"dark":{"bg":"#0b120c","fg":"#333333"},"body":"Inter","display":"Comic Sans","radius":40,"density":"wild"}`}
	if _, err := s.GenerateTheme(ctx, owner, ThemeIdea{}); invalidKey(err) != "validation.theme_idea" {
		t.Fatalf("empty: %v", err)
	}
	p, err := s.GenerateTheme(ctx, owner, ThemeIdea{Description: "calm green forest"})
	if err != nil {
		t.Fatal(err)
	}
	d := p.Data
	if p.Name != "Forest" || d.Light["accent"] != "#2e7d32" || d.Light["evil"] != "" || d.Body.Family != "Inter" || d.Display.Family != "" || d.Radius != 24 || d.Density != "" {
		t.Fatalf("proposal: %+v", p)
	}
	if probs := CheckThemeContrast(d); len(probs) != 0 {
		t.Fatalf("not readable: %+v", probs)
	}
	if err := validTheme(&d, ""); err != nil {
		t.Fatalf("invalid: %v", err)
	}
}
