package core

import (
	"context"
	"testing"
	"time"

	"github.com/phoedox/phoedox/internal/store"
)

func TestDocumentationRules(t *testing.T) {
	ctx := context.Background()
	s, owner, editor, _ := setup(t)
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Lab"})
	_ = s.SetMember(ctx, owner, p.Slug, "reader@x.de", store.RoleEditor)
	_, _ = s.UpsertEntity(ctx, owner, p.ID, EntityInput{Kind: "server", Name: "NAS"})
	_, _ = s.UpsertEntity(ctx, owner, p.ID, EntityInput{Kind: "server", Name: "Web", Attrs: map[string]any{"ip": "10.0.0.2", "os": "Debian"}})
	_, _ = s.UpsertEntity(ctx, owner, p.ID, EntityInput{Kind: "room", Name: "Cellar"})
	_, _ = s.Link(ctx, owner, p.ID, EdgeInput{From: "web", Rel: "located_in", To: "cellar"})
	_, _ = s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "markdown", Title: "Web", Markdown: "The @web server runs the website."})

	if _, err := s.SetKindRules(ctx, editor, p.Slug, "server", KindRules{Location: true}); err == nil {
		t.Fatal("editor set rules")
	}
	if _, err := s.SetKindRules(ctx, owner, p.Slug, "server", KindRules{Fields: []string{"colour"}}); invalidKey(err) != "validation.rule_field" {
		t.Fatalf("unknown field: %v", err)
	}
	k, err := s.SetKindRules(ctx, owner, p.Slug, "server", KindRules{Location: true, Page: true, Owner: true, Fields: []string{"ip", "os", "ip"}, ReviewDays: 180})
	if err != nil || k.Rules == nil || len(k.Rules.Fields) != 2 {
		t.Fatalf("rules: %+v %v", k.Rules, err)
	}
	if m, _ := s.DataModel(ctx, editor, p.Slug); func() bool {
		for _, x := range m.Kinds {
			if x.ID == "server" {
				return x.Rules == nil || x.Rules.ReviewDays != 180
			}
		}
		return true
	}() {
		t.Fatal("rules missing from the data model")
	}

	nas, err := s.EntityMeta(ctx, editor, p.Slug, "nas")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"location", "page", "owner", "fields"}
	if len(nas.Violations.Rules) != 4 || nas.Violations.Rules[0] != want[0] || nas.Violations.Rules[3] != want[3] || len(nas.Violations.Missing) != 2 || nas.KindReview != 180 || nas.ReviewDue == "" {
		t.Fatalf("nas: %+v", nas)
	}
	// Web: located, mentioned, filled; the owner is missing until set.
	web, _ := s.EntityMeta(ctx, editor, p.Slug, "web")
	if len(web.Violations.Rules) != 1 || web.Violations.Rules[0] != "owner" {
		t.Fatalf("web: %+v", web.Violations)
	}
	if _, err := s.SetEntityMeta(ctx, editor, p.Slug, "web", EntityMetaInput{Owner: strp("stranger@x.de")}); err == nil {
		t.Fatal("non-member became responsible")
	}
	web, err = s.SetEntityMeta(ctx, owner, p.Slug, "web", EntityMetaInput{Owner: strp("reader@x.de")})
	if err != nil || web.Owner == nil || len(web.Violations.Rules) != 0 {
		t.Fatalf("owner: %+v %v", web, err)
	}
	if list, _ := s.Notifications(ctx, editor, false, 0); len(list.Items) == 0 || list.Items[0].Kind != "entity.owner" {
		t.Fatalf("owner not notified: %+v", list.Items)
	}
	// A person entry responsible for NAS also counts.
	_, _ = s.UpsertEntity(ctx, owner, p.ID, EntityInput{Kind: "person", Name: "Kim"})
	_, _ = s.Link(ctx, owner, p.ID, EdgeInput{From: "kim", Rel: "responsible_for", To: "nas"})
	if nas, _ = s.EntityMeta(ctx, editor, p.Slug, "nas"); !nas.PersonOwner || len(nas.Violations.Rules) != 3 {
		t.Fatalf("person owner: %+v", nas)
	}
	gaps, _ := s.DocumentationGaps(ctx, editor, p.Slug, 0)
	if gaps.Counts["rule_violation"] != 1 {
		t.Fatalf("gaps: %v", gaps.Counts)
	}

	// Review: an own interval wins over the kind's; reminders go to the owner once.
	days := 30
	web, _ = s.SetEntityMeta(ctx, editor, p.Slug, "web", EntityMetaInput{ReviewDays: &days})
	if web.ReviewDays != 30 || web.Overdue {
		t.Fatalf("review: %+v", web)
	}
	later := time.Now().AddDate(0, 0, 40)
	s.RemindEntityReviews(ctx, later)
	s.RemindEntityReviews(ctx, later.Add(2*time.Hour))
	n := 0
	list, _ := s.Notifications(ctx, editor, false, 0)
	for _, it := range list.Items {
		if it.Kind == "entity.review_due" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("entity reminders: %d", n)
	}
	if web, _ = s.MarkEntityReviewed(ctx, editor, p.Slug, "web"); web.ReviewedAt.IsZero() {
		t.Fatal("not reviewed")
	}
	if _, err := s.SetKindRules(ctx, owner, p.Slug, "server", KindRules{}); err != nil {
		t.Fatal(err)
	}
	if nas, _ = s.EntityMeta(ctx, editor, p.Slug, "nas"); nas.Rules != nil || len(nas.Violations.Rules) != 0 {
		t.Fatalf("rules not removed: %+v", nas)
	}
}

func TestLocations(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, _ := setup(t)
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Campus"})
	_ = s.SetMember(ctx, owner, p.Slug, "reader@x.de", store.RoleReader)
	for _, in := range []EntityInput{
		{Kind: "site", Name: "Main campus", Attrs: map[string]any{"lat": 52.52, "lng": 13.40}},
		{Kind: "building", Name: "House A"}, {Kind: "floor", Name: "Upper", Attrs: map[string]any{"level": 1}},
		{Kind: "floor", Name: "Ground", Attrs: map[string]any{"level": 0}}, {Kind: "room", Name: "Server room"},
		{Kind: "room", Name: "Shed"}, {Kind: "rack", Name: "R1", Attrs: map[string]any{"height_units": 12}}, {Kind: "server", Name: "NAS"}, {Kind: "device", Name: "Printer"},
	} {
		if _, err := s.UpsertEntity(ctx, owner, p.ID, in); err != nil {
			t.Fatal(err)
		}
	}
	for _, e := range []EdgeInput{{From: "house-a", Rel: "part_of", To: "main-campus"}, {From: "upper", Rel: "part_of", To: "house-a"},
		{From: "ground", Rel: "part_of", To: "house-a"}, {From: "server-room", Rel: "part_of", To: "ground"},
		{From: "r1", Rel: "located_in", To: "server-room"}, {From: "nas", Rel: "mounted_in", To: "r1"}, {From: "printer", Rel: "located_in", To: "server-room"}} {
		if _, err := s.Link(ctx, owner, p.ID, e); err != nil {
			t.Fatal(e, err)
		}
	}
	roots, err := s.Locations(ctx, reader, p.Slug)
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != 2 || roots[0].Kind != "site" || roots[1].Name != "Shed" {
		t.Fatalf("roots: %+v", roots)
	}
	house := roots[0].Children[0]
	if house.Name != "House A" || len(house.Children) != 2 || house.Children[0].Name != "Ground" {
		t.Fatalf("floors in level order: %+v", house.Children)
	}
	room := house.Children[0].Children[0]
	if room.Name != "Server room" || room.Devices != 3 {
		t.Fatalf("room: %+v", room)
	}
	if roots[0].Attrs["lat"] != 52.52 {
		t.Fatalf("site attrs: %+v", roots[0].Attrs)
	}
}
