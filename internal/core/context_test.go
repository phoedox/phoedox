package core

import (
	"context"
	"strings"
	"testing"
)

func TestEntityContext(t *testing.T) {
	ctx := context.Background()
	s, owner, _, stranger := setup(t)
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Heim"})
	_, _ = s.UpsertEntity(ctx, owner, p.Slug, EntityInput{Kind: "router", Name: "Fritzbox", Slug: "router", Attrs: map[string]any{"ip": "192.168.178.1"}})
	_, _ = s.UpsertEntity(ctx, owner, p.Slug, EntityInput{Kind: "repeater", Name: "Repeater", Slug: "rep", Attrs: map[string]any{"status": "offline"}})
	_, _ = s.Link(ctx, owner, p.Slug, EdgeInput{From: "rep", Rel: "uplink", To: "router"})
	_, _ = s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "markdown", Title: "WLAN", Markdown: "# WLAN\n\nDer @rep hängt am Router.\n\n```\ncode\n```"})
	pack, err := s.EntityContext(ctx, owner, p.Slug, "rep")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# Repeater (repeater, @rep)", "Status: down (state_down)", "Network path: Repeater [down] –uplink→ Fritzbox", "- uplink → Fritzbox (router, @router)", "WLAN (markdown, slug wlan): WLAN Der @rep hängt am Router."} {
		if !strings.Contains(pack, want) {
			t.Errorf("missing %q in:\n%s", want, pack)
		}
	}
	if strings.Contains(pack, "code") {
		t.Errorf("code blocks stay out of snippets:\n%s", pack)
	}
	if _, err := s.EntityContext(ctx, stranger, p.Slug, "rep"); err == nil {
		t.Fatal("stranger")
	}
}
