package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/phoedox/phoedox/internal/graph"
)

// testDBs returns a fresh SQLite database and, if PHOEDOX_TEST_PG is set,
// a fresh Postgres schema. Every test runs against both.
func testDBs(t *testing.T) map[string]*DB {
	t.Helper()
	ctx := context.Background()
	out := map[string]*DB{}
	lite, err := Open(ctx, SQLite, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lite.Close() })
	out["sqlite"] = lite
	if dsn := os.Getenv("PHOEDOX_TEST_PG"); dsn != "" {
		admin, err := Open(ctx, Postgres, dsn)
		if err != nil {
			t.Fatal(err)
		}
		schema := "t_" + strings.ToLower(string(graph.NewID()))
		if _, err := admin.SQL.ExecContext(ctx, `CREATE SCHEMA "`+schema+`"`); err != nil {
			t.Fatal(err)
		}
		admin.Close()
		pg, err := Open(ctx, Postgres, dsn+"&search_path="+schema)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			pg.SQL.ExecContext(context.Background(), `DROP SCHEMA "`+schema+`" CASCADE`)
			pg.Close()
		})
		out["postgres"] = pg
	}
	return out
}

func id() string { return string(graph.NewID()) }

func TestStore(t *testing.T) {
	for name, d := range testDBs(t) {
		t.Run(name, func(t *testing.T) { runStore(t, d) })
	}
}

func runStore(t *testing.T, d *DB) {
	ctx := context.Background()
	u, err := d.CreateUser(ctx, User{ID: id(), Email: "Jan@Example.org", Name: "Jan", PasswordHash: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := d.UserByEmail(ctx, "jan@example.org"); err != nil || got.ID != u.ID {
		t.Fatalf("UserByEmail: %v %+v", err, got)
	}
	if _, err := d.CreateUser(ctx, User{ID: id(), Email: "jan@example.org", Name: "Dup"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate email: want ErrConflict, got %v", err)
	}

	// tokens
	tok, err := d.CreateToken(ctx, Token{ID: id(), UserID: u.ID, Name: "mcp", Prefix: "phx_abc"}, "hash1")
	if err != nil {
		t.Fatal(err)
	}
	if gu, gt, err := d.UserByToken(ctx, "hash1"); err != nil || gu.ID != u.ID || gt.ID != tok.ID {
		t.Fatalf("UserByToken: %v", err)
	}
	if err := d.RevokeToken(ctx, u.ID, tok.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := d.UserByToken(ctx, "hash1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoked token still valid: %v", err)
	}

	// sessions
	exp := Now()
	exp.Time = exp.Time.Add(3600e9)
	if err := d.CreateSession(ctx, "s1", u.ID, exp); err != nil {
		t.Fatal(err)
	}
	if su, err := d.UserBySession(ctx, "s1"); err != nil || su.ID != u.ID {
		t.Fatalf("UserBySession: %v", err)
	}

	// projects
	p, err := d.CreateProject(ctx, Project{ID: id(), Slug: "phoedox", Name: "Phoedox", Short: "PH", PrimaryColor: "#F26A1B", AIWriteMode: "suggest", OwnerID: u.ID})
	if err != nil {
		t.Fatal(err)
	}
	got, err := d.ProjectFor(ctx, "phoedox", u.ID)
	if err != nil || got.ID != p.ID || got.Role != RoleOwner {
		t.Fatalf("ProjectFor: %v %+v", err, got)
	}
	other, _ := d.CreateUser(ctx, User{ID: id(), Email: "lena@example.org", Name: "Lena"})
	if ps, _ := d.ListProjects(ctx, other.ID, false); len(ps) != 0 {
		t.Fatalf("non-member sees projects: %+v", ps)
	}
	if err := d.SetMember(ctx, p.ID, other.ID, RoleReader); err != nil {
		t.Fatal(err)
	}
	if ps, _ := d.ListProjects(ctx, other.ID, false); len(ps) != 1 || ps[0].Role != RoleReader {
		t.Fatalf("member projects: %+v", ps)
	}

	// documents with versions
	doc, err := d.CreateDocument(ctx, Document{ID: id(), ProjectID: p.ID, Type: DocMarkdown, Slug: "netz", Title: "Netz", Category: "Grundlagen", Content: JSON(`{"markdown":"Router @router-flur"}`), CreatedBy: u.ID}, "user:"+u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.UpdateDocument(ctx, Document{ID: doc.ID, Slug: "netz", Title: "Netzwerk", Category: "Grundlagen", Content: JSON(`{"markdown":"neu"}`)}, 1, "user:"+u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.UpdateDocument(ctx, Document{ID: doc.ID, Slug: "netz", Title: "x", Content: JSON(`{}`)}, 1, "user:"+u.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale version: want ErrConflict, got %v", err)
	}
	full, err := d.GetDocument(ctx, p.ID, "netz")
	if err != nil || full.Version != 2 || full.Title != "Netzwerk" || string(full.Content) == "" {
		t.Fatalf("GetDocument: %v %+v", err, full)
	}
	if vs, _ := d.ListVersions(ctx, doc.ID); len(vs) != 2 {
		t.Fatalf("versions: %+v", vs)
	}
	if _, err := d.CreateDocument(ctx, Document{ID: id(), ProjectID: p.ID, Type: DocMarkdown, Slug: "netz", Title: "Dup", Content: JSON(`{}`)}, "x"); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate slug: %v", err)
	}

	// entities, edges, traversal
	mk := func(slug, kind string) Entity {
		e, _, err := d.UpsertEntity(ctx, Entity{ID: id(), ProjectID: p.ID, Kind: kind, Slug: slug, Name: slug, Attrs: JSON(`{}`), Source: "manual"})
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	laptop, rep, router, inet := mk("laptop", "client"), mk("repeater-az", "repeater"), mk("router-flur", "router"), mk("internet", "wan")
	if again, created, _ := d.UpsertEntity(ctx, Entity{ID: id(), ProjectID: p.ID, Kind: "router", Slug: "router-flur", Name: "Router Flur", Attrs: JSON(`{"ip":"192.168.178.1"}`), Source: "manual"}); created || again.ID != router.ID {
		t.Fatal("upsert by slug must update")
	}
	for _, e := range []Edge{{From: laptop.ID, Rel: "uplink", To: rep.ID}, {From: rep.ID, Rel: "uplink", To: router.ID}, {From: router.ID, Rel: "uplink", To: inet.ID}, {From: inet.ID, Rel: "uplink", To: laptop.ID}} {
		e.ID, e.Attrs, e.Source = id(), JSON(`{}`), "manual"
		if _, err := d.LinkEntities(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	hops, err := d.Traverse(ctx, laptop.ID, "uplink", "out", 6)
	if err != nil {
		t.Fatal(err)
	}
	if len(hops) != 3 || hops[0].Entity.Slug != "repeater-az" || hops[2].Entity.Slug != "internet" || hops[2].Depth != 3 {
		t.Fatalf("traverse: %+v", hops)
	}
	if in, _ := d.Traverse(ctx, router.ID, "", "in", 1); len(in) != 1 || in[0].Entity.ID != rep.ID {
		t.Fatalf("traverse in: %+v", in)
	}
	if both, _ := d.Traverse(ctx, router.ID, "", "both", 1); len(both) != 2 {
		t.Fatalf("traverse both: %+v", both)
	}
	if edges, _ := d.EdgesOf(ctx, router.ID); len(edges) != 2 {
		t.Fatalf("edges: %+v", edges)
	}

	// document links + search
	if err := d.SetDocumentLinks(ctx, doc.ID, []string{router.ID, router.ID}); err != nil {
		t.Fatal(err)
	}
	if ds, _ := d.DocumentsMentioning(ctx, router.ID); len(ds) != 1 {
		t.Fatalf("mentions: %+v", ds)
	}
	hits, err := d.Search(ctx, []string{p.ID}, "router 192.168", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Slug != "router-flur" {
		t.Fatalf("search: %+v", hits)
	}
	if hits, _ := d.Search(ctx, []string{p.ID}, "NETZWERK", 10); len(hits) != 1 || hits[0].Type != "document" {
		t.Fatalf("search doc: %+v", hits)
	}

	// suggestions + events
	s, err := d.CreateSuggestion(ctx, Suggestion{ID: id(), ProjectID: p.ID, Actor: "mcp:x", Op: "document.create", Summary: "Neue Seite", Payload: JSON(`{"title":"x"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.DecideSuggestion(ctx, s.ID, "accepted", u.ID); err != nil {
		t.Fatal(err)
	}
	if err := d.DecideSuggestion(ctx, s.ID, "rejected", u.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("decided suggestion must not change again")
	}
	if err := d.AddEvent(ctx, "user:"+u.ID, "document.update", doc.ID, map[string]int{"version": 2}); err != nil {
		t.Fatal(err)
	}
	if err := d.DeleteEntity(ctx, rep.ID); err != nil {
		t.Fatal(err)
	}
	if hops, _ := d.Traverse(ctx, laptop.ID, "uplink", "out", 6); len(hops) != 0 {
		t.Fatalf("deleted entity edges must be gone: %+v", hops)
	}
}
