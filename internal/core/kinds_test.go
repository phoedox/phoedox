package core

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/phoedox/phoedox/internal/store"
)

func TestKinds(t *testing.T) {
	ctx := context.Background()
	s, owner, editor, reader := setup(t)
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Haus", AIWriteMode: "suggest"})
	_ = s.SetMember(ctx, owner, p.Slug, editor.User.Email, store.RoleEditor)
	_ = s.SetMember(ctx, owner, p.Slug, reader.User.Email, store.RoleReader)

	m, err := s.DataModel(ctx, reader, p.Slug)
	if err != nil || len(m.Kinds) < 20 {
		t.Fatalf("model: %v %d", err, len(m.Kinds))
	}
	rack, _ := m.Find("rack")
	if rack.Source != "core" || rack.Icon == "" || len(rack.Attrs) == 0 {
		t.Fatalf("rack: %+v", rack)
	}

	// unknown kinds and wrong values are rejected with keys
	if _, err := s.UpsertEntity(ctx, editor, p.Slug, EntityInput{Kind: "spaceship", Name: "X"}); invalidKey(err) != "validation.kind_unknown" {
		t.Fatalf("unknown kind: %v", err)
	}
	if _, err := s.UpsertEntity(ctx, editor, p.Slug, EntityInput{Kind: "rack", Name: "Rack"}); invalidKey(err) != "validation.attr_required" {
		t.Fatalf("required: %v", err)
	}
	bad := map[string]map[string]any{
		"validation.attr_type":   {"ip": "192.168.300.1"},
		"validation.attr_option": {"uplink": "carrier-pigeon"},
	}
	for key, attrs := range bad {
		if _, err := s.UpsertEntity(ctx, editor, p.Slug, EntityInput{Kind: "router", Name: "R", Attrs: attrs}); invalidKey(err) != key {
			t.Fatalf("%s: %v", key, err)
		}
	}
	// values are normalized: numbers from strings, empty values dropped, unknown attributes kept
	res, err := s.UpsertEntity(ctx, editor, p.Slug, EntityInput{Kind: "rack", Name: "Rack Keller", Attrs: map[string]any{"height_units": "12", "depth": "", "note": "alt"}})
	if err != nil || !strings.Contains(string(res.Entity.Attrs), `"height_units":12`) || strings.Contains(string(res.Entity.Attrs), "depth") || !strings.Contains(string(res.Entity.Attrs), "note") {
		t.Fatalf("normalize: %v %s", err, res.Entity.Attrs)
	}
	_, _ = s.UpsertEntity(ctx, editor, p.Slug, EntityInput{Kind: "server", Name: "NAS", Attrs: map[string]any{"ip": "10.0.0.2"}})
	_, _ = s.UpsertEntity(ctx, editor, p.Slug, EntityInput{Kind: "room", Name: "Keller"})

	// relations follow the rules; generic ones are always allowed
	if _, err := s.Link(ctx, editor, p.Slug, EdgeInput{From: "nas", Rel: "uplink", To: "rack-keller"}); invalidKey(err) != "validation.rel_not_allowed" {
		t.Fatalf("rel not allowed: %v", err)
	}
	for _, rel := range []string{"mounted_in", "related_to"} {
		if _, err := s.Link(ctx, editor, p.Slug, EdgeInput{From: "nas", Rel: rel, To: "rack-keller"}); err != nil {
			t.Fatalf("%s: %v", rel, err)
		}
	}

	// own kinds and extensions of core kinds
	if _, err := s.DefineKind(ctx, reader, p.Slug, KindInput{ID: "solar", Name: "Wechselrichter"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("reader define: %v", err)
	}
	if _, err := s.DefineKind(ctx, editor, p.Slug, KindInput{ID: "rack", Attrs: []Attr{{Name: "depth", Type: "number"}}}); invalidKey(err) != "validation.attr_duplicate" {
		t.Fatalf("duplicate core attr: %v", err)
	}
	if _, err := s.DefineKind(ctx, editor, p.Slug, KindInput{ID: "solar", Name: "Wechselrichter", Attrs: []Attr{{Name: "mode", Type: "select"}}}); invalidKey(err) != "validation.attr_options" {
		t.Fatalf("select without options: %v", err)
	}
	if _, err := s.DefineKind(ctx, editor, p.Slug, KindInput{ID: "solar", Name: "Wechselrichter", Relations: []Relation{{Rel: "feeds", To: []string{"spaceship"}}}}); invalidKey(err) != "validation.kind_unknown" {
		t.Fatalf("relation to unknown kind: %v", err)
	}
	solar := KindInput{ID: "solar", Name: "Wechselrichter", Icon: "PlugZap",
		Attrs:     []Attr{{Name: "peak_kw", Label: "Leistung", Type: "number", Unit: "kWp", Required: true}},
		Relations: []Relation{{Rel: "feeds", To: []string{"outlet"}}, {Rel: "located_in", To: []string{"room"}}}}
	if r, err := s.DefineKind(ctx, editor, p.Slug, solar); err != nil || !r.Applied || r.Kind.Source != "project" || r.Kind.Group != "custom" {
		t.Fatalf("define: %v %+v", err, r)
	}
	if _, err := s.DefineKind(ctx, editor, p.Slug, KindInput{ID: "rack", Attrs: []Attr{{Name: "asset_tag", Type: "text"}}}); err != nil {
		t.Fatal(err)
	}
	m, _ = s.DataModel(ctx, reader, p.Slug)
	rack, _ = m.Find("rack")
	if last := rack.Attrs[len(rack.Attrs)-1]; last.Name != "asset_tag" || !last.Project || rack.Count != 1 {
		t.Fatalf("extension: %+v", rack)
	}
	if _, err := s.UpsertEntity(ctx, editor, p.Slug, EntityInput{Kind: "solar", Name: "Balkon", Attrs: map[string]any{"peak_kw": 0.8}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Link(ctx, editor, p.Slug, EdgeInput{From: "balkon", Rel: "located_in", To: "keller"}); err != nil {
		t.Fatalf("own relation: %v", err)
	}
	if err := s.DeleteKind(ctx, editor, p.Slug, "solar"); invalidKey(err) != "validation.kind_in_use" {
		t.Fatalf("delete used kind: %v", err)
	}
	if err := s.DeleteKind(ctx, editor, p.Slug, "rack"); err != nil {
		t.Fatalf("remove extension: %v", err)
	}

	// AI clients in suggest mode propose kinds
	ai := owner
	ai.Via = "mcp"
	if r, err := s.DefineKind(ctx, ai, p.Slug, KindInput{ID: "boiler", Name: "Heizkessel"}); err != nil || r.Applied || r.Suggestion == nil {
		t.Fatalf("ai define: %v %+v", err, r)
	} else if _, err := s.DecideSuggestion(ctx, owner, r.Suggestion.ID, true); err != nil {
		t.Fatal(err)
	}
	m, _ = s.DataModel(ctx, reader, p.Slug)
	if k, ok := m.Find("boiler"); !ok || k.Icon != "Shapes" {
		t.Fatalf("accepted kind: %+v", k)
	}
	if schema := rack.JSONSchema(); schema["required"].([]string)[0] != "height_units" {
		t.Fatalf("schema: %+v", schema)
	}
}
