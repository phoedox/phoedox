package core

import (
	"context"
	"testing"

	"github.com/phoedox/phoedox/internal/store"
)

func TestQueriesAndBulk(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, _ := setup(t)
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Lab"})
	_ = s.SetMember(ctx, owner, p.Slug, "reader@x.de", store.RoleReader)

	// Bulk create with one bad row.
	res, err := s.Bulk(ctx, owner, p.Slug, BulkInput{Upserts: []EntityInput{
		{Kind: "server", Name: "Web", Attrs: map[string]any{"ip": "10.0.0.2", "os": "Debian 12", "height_units": 1}},
		{Kind: "server", Name: "NAS", Attrs: map[string]any{"ip": "10.0.0.3", "os": "TrueNAS", "height_units": 2}},
		{Kind: "server", Name: "Old", Attrs: map[string]any{"os": "Debian 9"}},
		{Kind: "nope", Name: "Bad"},
		{Kind: "room", Name: "Cellar"},
	}})
	if err != nil || res.Created != 4 || len(res.Errors) != 1 || res.Errors[0].Row != 3 || res.Errors[0].Key != "validation.kind_unknown" {
		t.Fatalf("bulk: %+v %v", res, err)
	}
	if _, err := s.Bulk(ctx, reader, p.Slug, BulkInput{Delete: []string{"web"}}); err == nil {
		t.Fatal("reader changed entries")
	}
	_, _ = s.Link(ctx, owner, p.ID, EdgeInput{From: "web", Rel: "located_in", To: "cellar"})

	q := GraphQuery{Kind: "server", Where: []Cond{{Field: "os", Op: "contains", Value: "debian"}}, Sort: "name"}
	r, err := s.RunQuery(ctx, reader, p.Slug, q)
	if err != nil || r.Total != 2 || r.Rows[0].Name != "Old" || r.Rows[1].Name != "Web" || len(r.Columns) == 0 {
		t.Fatalf("contains: %+v %v", r, err)
	}
	// Servers without a location.
	r, _ = s.RunQuery(ctx, reader, p.Slug, GraphQuery{Kind: "server", Rels: []RelCond{{Rel: "located_in", Missing: true}}})
	if r.Total != 2 || r.Rows[0].Name != "NAS" {
		t.Fatalf("missing rel: %+v", r.Rows)
	}
	r, _ = s.RunQuery(ctx, reader, p.Slug, GraphQuery{Kind: "server", Rels: []RelCond{{Rel: "located_in", To: "cellar"}}})
	if r.Total != 1 || r.Rows[0].Slug != "web" {
		t.Fatalf("rel to: %+v", r.Rows)
	}
	r, _ = s.RunQuery(ctx, reader, p.Slug, GraphQuery{Kind: "server", Where: []Cond{{Field: "height_units", Op: "gt", Value: 1}}})
	if r.Total != 1 || r.Rows[0].Slug != "nas" {
		t.Fatalf("gt: %+v", r.Rows)
	}
	r, _ = s.RunQuery(ctx, reader, p.Slug, GraphQuery{Kind: "server", Where: []Cond{{Field: "ip", Op: "empty"}}, Sort: "-name"})
	if r.Total != 1 || r.Rows[0].Slug != "old" {
		t.Fatalf("empty: %+v", r.Rows)
	}
	if _, err := s.RunQuery(ctx, reader, p.Slug, GraphQuery{Kind: "server", Where: []Cond{{Field: "colour", Op: "eq"}}}); invalidKey(err) != "validation.rule_field" {
		t.Fatalf("unknown field: %v", err)
	}
	if _, err := s.RunQuery(ctx, reader, p.Slug, GraphQuery{Kind: "server", Where: []Cond{{Field: "os", Op: "like"}}}); invalidKey(err) != "validation.query_op" {
		t.Fatalf("unknown op: %v", err)
	}

	// Saved queries.
	if _, err := s.SaveQuery(ctx, reader, p.Slug, "", SaveQueryInput{Name: "Debian", Query: q}); err == nil {
		t.Fatal("reader saved a query")
	}
	sq, err := s.SaveQuery(ctx, owner, p.Slug, "", SaveQueryInput{Name: "Debian servers", Query: q})
	if err != nil || sq.Slug != "debian-servers" {
		t.Fatalf("save: %+v %v", sq, err)
	}
	if _, err := s.SaveQuery(ctx, owner, p.Slug, "", SaveQueryInput{Name: "Debian servers", Query: q}); invalidKey(err) != "validation.query_name_taken" {
		t.Fatalf("duplicate: %v", err)
	}
	if r, err = s.RunSavedQuery(ctx, reader, p.Slug, "debian-servers"); err != nil || r.Total != 2 {
		t.Fatalf("run saved: %+v %v", r, err)
	}
	if list, _ := s.SavedQueries(ctx, reader, p.Slug); len(list) != 1 || list[0].Def.Kind != "server" {
		t.Fatalf("list: %+v", list)
	}

	// Bulk edit: set a field on two entries, remove it on one, delete one.
	res, err = s.Bulk(ctx, owner, p.Slug, BulkInput{Set: &struct {
		Slugs []string       `json:"slugs"`
		Attrs map[string]any `json:"attrs"`
	}{Slugs: []string{"web", "nas", "ghost"}, Attrs: map[string]any{"os": "Debian 13"}}, Delete: []string{"old"}})
	if err != nil || res.Updated != 2 || res.Deleted != 1 || len(res.Errors) != 1 || res.Errors[0].Ref != "ghost" {
		t.Fatalf("bulk edit: %+v %v", res, err)
	}
	if r, _ = s.RunSavedQuery(ctx, reader, p.Slug, sq.ID); r.Total != 2 {
		t.Fatalf("after edit: %+v", r.Rows)
	}
	if err := s.DeleteQuery(ctx, owner, p.Slug, "debian-servers"); err != nil {
		t.Fatal(err)
	}
}
