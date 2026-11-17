package core

import (
	"context"
	"strings"
	"testing"

	"github.com/phoedox/phoedox/internal/plugins/plugintest"
	"github.com/phoedox/phoedox/internal/store"
)

func TestUsedMovedKinds(t *testing.T) {
	page := []byte(`{"markdown":"Text\n\n` + "```gantt" + `\nA: 2026-01-01, 2d\n` + "```" + `\n"}`)
	board := []byte(`{"version":1,"elements":[{"type":"block","kind":"gantt","source":"A: 2026-01-01"},{"type":"block","kind":"poll"}]}`)
	other := []byte(`{"markdown":"the word gantt alone is no block"}`)
	if !usedMovedKinds(page)["gantt"] || !usedMovedKinds(board)["gantt"] || len(usedMovedKinds(other)) != 0 {
		t.Fatal(usedMovedKinds(page), usedMovedKinds(board), usedMovedKinds(other))
	}
}

// ganttLike is the test plugin under the id and block kind of a moved block.
func ganttLike(t *testing.T) []byte {
	m := strings.ReplaceAll(plugintest.Manifest, "de.example.ups", "com.phoedox.gantt")
	m = strings.Replace(m, `"id": "timeline"`, `"id": "gantt"`, 1)
	return plugintest.Package(t, m)
}

func TestMovedBlocks(t *testing.T) {
	ctx := context.Background()
	s, owner, _, _, priv := pluginSetup(t)
	admin := owner
	admin.User.IsAdmin = true
	gantt := "```gantt\nA: 2026-01-01, 2d\n```"
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Bad"})
	quiet, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Leer"})
	if _, err := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "markdown", Title: "Plan", Markdown: gantt}); err != nil {
		t.Fatal(err)
	}
	missing := func(slug string) *MissingPlugin {
		list, err := s.PluginBlocks(ctx, owner, slug)
		if err != nil {
			t.Fatal(err)
		}
		for _, b := range list {
			if b.Kind == "gantt" {
				return b.Missing
			}
		}
		t.Fatalf("gantt not listed: %+v", list)
		return nil
	}

	// Without a store: the placeholder (C).
	if m := missing(p.Slug); m == nil || m.Plugin != "com.phoedox.gantt" || m.Installed || m.Store {
		t.Fatalf("missing: %+v", m)
	}
	r, err := s.RenderPluginBlock(ctx, owner, p.Slug, "gantt", "A: 2026-01-01, 2d", "de", 600)
	if err != nil || r.Missing == nil || r.SVG != "" || len(r.Problems) != 1 || !strings.Contains(r.Problems[0].Message, "com.phoedox.gantt") {
		t.Fatalf("render without plugin: %v %+v", err, r)
	}
	ps, err := s.ValidateBlocks(ctx, owner, p.Slug, "", &gantt)
	if err != nil || len(ps) != 1 || ps[0].Severity != "warning" {
		t.Fatalf("an AI may still write it: %v %+v", err, ps)
	}
	if err := s.InstallMovedBlockPlugins(ctx); err != nil {
		t.Fatal(err)
	}

	// With a store: installed and enabled where used (B).
	data := ganttLike(t)
	s.Plugins.Store = &fakeStore{priv: priv, catalog: []CatalogEntry{{ID: "com.phoedox.gantt", Name: "Gantt", Version: "1.2.0"}},
		pkgs: map[string][]byte{"com.phoedox.gantt@1.2.0": data}}
	if m := missing(p.Slug); m == nil || !m.Store || m.Installed {
		t.Fatalf("admins can install it: %+v", m)
	}
	if err := s.InstallMovedBlockPlugins(ctx); err != nil {
		t.Fatal(err)
	}
	row, err := s.DB.GetPlugin(ctx, "com.phoedox.gantt")
	if err != nil || row.Source != "store" || row.SignedBy != "store-1" {
		t.Fatalf("installed: %v %+v", err, row)
	}
	if m := missing(p.Slug); m != nil {
		t.Fatalf("active now: %+v", m)
	}
	if r, err := s.RenderPluginBlock(ctx, owner, p.Slug, "gantt", "x", "de", 600); err != nil || !strings.Contains(r.SVG, "gantt:de") {
		t.Fatalf("drawn by the plugin: %v %+v", err, r)
	}
	if m := missing(quiet.Slug); m == nil || !m.Installed {
		t.Fatalf("only where used; owners elsewhere can enable it: %+v", m)
	}
	evs, _ := s.DB.Events(ctx, store.EventFilter{Kind: "plugin.project"})
	if len(evs) == 0 || evs[0].Actor != "system" {
		t.Fatalf("logged as system: %+v", evs)
	}

	// An owner's choice stays.
	if err := s.SetProjectPlugin(ctx, owner, p.Slug, "com.phoedox.gantt", false); err != nil {
		t.Fatal(err)
	}
	if err := s.InstallMovedBlockPlugins(ctx); err != nil {
		t.Fatal(err)
	}
	if m := missing(p.Slug); m == nil {
		t.Fatal("turned on again after the owner turned it off")
	}

	// So does an admin's: no reinstall, not even for new content.
	if err := s.UninstallPlugin(ctx, admin, "com.phoedox.gantt"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateDocument(ctx, owner, quiet.Slug, DocumentInput{Type: "markdown", Title: "Plan", Markdown: gantt}); err != nil {
		t.Fatal(err)
	}
	if err := s.InstallMovedBlockPlugins(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.GetPlugin(ctx, "com.phoedox.gantt"); err == nil {
		t.Fatal("installed again after the admin removed it")
	}
}
