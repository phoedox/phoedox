package core

import (
	"context"
	"strings"
	"testing"

	"github.com/phoedox/phoedox/internal/store"
	"github.com/phoedox/phoedox/resources"
)

func TestBuiltInAppKinds(t *testing.T) {
	ctx := context.Background()
	s, owner, _, _ := setup(t)
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Haus"})
	kinds := resources.AppStarters()
	if len(kinds) != 4 {
		t.Fatalf("kinds: %v", kinds)
	}
	for _, k := range kinds {
		res, err := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: store.DocApp, Title: k, App: &App{Starter: k}})
		if err != nil {
			t.Fatalf("%s: %v", k, err)
		}
		app := appOf(*res.Document)
		if _, ok := app.Files["index.html"]; !ok || app.Starter != "" {
			t.Fatalf("%s: %+v", k, app)
		}
		if app.Build != nil && len(app.Build.Errors) > 0 {
			t.Fatalf("%s: build errors %v", k, app.Build.Errors)
		}
		if strings.Contains(strings.Join(mapValues(app.Files), ""), "https://") {
			t.Fatalf("%s loads something from the network", k)
		}
		switch k {
		case "slides":
			if !app.SelfEdit || app.Files["slides.md"] == "" {
				t.Fatalf("slides: %+v", app)
			}
		case "dashboard":
			if app.Level != 1 || len(app.Sources) != 1 || app.Sources[0].Kind != "checks" {
				t.Fatalf("dashboard: %+v", app)
			}
		case "sheet":
			if app.Level != 2 || app.Files["formula.js"] == "" {
				t.Fatalf("sheet: %+v", app)
			}
		}
	}
	// Own files replace the kind's; unknown kinds are refused.
	res, err := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: store.DocApp, Title: "Talk", App: &App{Starter: "slides", Files: map[string]string{"slides.md": "# Hi"}}})
	if err != nil || appOf(*res.Document).Files["slides.md"] != "# Hi" || appOf(*res.Document).Files["index.html"] == "" {
		t.Fatalf("own files: %v", err)
	}
	if _, err := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: store.DocApp, Title: "x", App: &App{Starter: "../x"}}); invalidKey(err) != "validation.app_starter" {
		t.Fatalf("unknown: %v", err)
	}
}

func mapValues(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	return out
}
