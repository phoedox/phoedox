package core

import (
	"context"
	"strings"
	"testing"

	"github.com/phoedox/phoedox/internal/store"
)

func TestTemplates(t *testing.T) {
	ctx := context.Background()
	s, owner, _, _ := setup(t)
	list := Templates("de")
	if len(list) != 4 || list[0].Name == "" || list[0].Pages != nil {
		t.Fatalf("templates: %+v", list)
	}
	if _, err := s.CreateProject(ctx, owner, ProjectInput{Name: "X", Template: "nope"}); invalidKey(err) != "validation.template" {
		t.Fatalf("unknown template: %v", err)
	}
	p, err := s.CreateProject(ctx, owner, ProjectInput{Name: "Zuhause", Template: "home", TemplateLocale: "de"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(p.Features, ","), "graph") {
		t.Fatalf("features from template: %v", p.Features)
	}
	docs, _ := s.Documents(ctx, owner, p.Slug, store.DocFilter{})
	titles := []string{}
	for _, d := range docs {
		titles = append(titles, d.Title)
	}
	if got := strings.Join(titles, "|"); !strings.Contains(got, "Notfallmappe") || !strings.Contains(got, "Haushalt") {
		t.Fatalf("pages: %s", got)
	}
	en, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Lab", Template: "homelab", TemplateLocale: "fr"})
	docs, _ = s.Documents(ctx, owner, en.Slug, store.DocFilter{})
	if len(docs) != 5 || docs[0].Title == "Überblick" {
		t.Fatalf("falls back to English: %+v", docs)
	}
}
