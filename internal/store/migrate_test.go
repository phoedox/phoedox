package store

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/phoedox/phoedox/internal/graph"
)

// TestEntitySlugMigration checks that rebuilding the entities table
// (migration 9) keeps edges and document links, and frees slugs of
// deleted entities.
func TestEntitySlugMigration(t *testing.T) {
	ctx := context.Background()
	dsns := map[string]struct {
		dialect Dialect
		dsn     string
	}{"sqlite": {SQLite, filepath.Join(t.TempDir(), "m.db")}}
	if pg := os.Getenv("PHOEDOX_TEST_PG"); pg != "" {
		admin, err := Open(ctx, Postgres, pg)
		if err != nil {
			t.Fatal(err)
		}
		schema := "m_" + strings.ToLower(string(graph.NewID()))
		if _, err := admin.SQL.ExecContext(ctx, `CREATE SCHEMA "`+schema+`"`); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { admin.SQL.ExecContext(context.Background(), `DROP SCHEMA "`+schema+`" CASCADE`); admin.Close() })
		dsns["postgres"] = struct {
			dialect Dialect
			dsn     string
		}{Postgres, pg + "&search_path=" + schema}
	}
	for name, c := range dsns {
		t.Run(name, func(t *testing.T) {
			old, err := openUpTo(ctx, c.dialect, c.dsn, 8)
			if err != nil {
				t.Fatal(err)
			}
			u, _ := old.CreateUser(ctx, User{ID: id(), Email: "m@y.de", Name: "M"})
			// Columns as of migration 8 (the current CreateProject writes newer ones).
			p := Project{ID: id(), Slug: "p", Name: "P", Short: "P", PrimaryColor: "#F26A1B", AIWriteMode: "suggest", OwnerID: u.ID}
			if _, err := old.Exec(ctx, `INSERT INTO projects (id, tenant_id, owner_id, slug, name, short, description, primary_color, ai_write_mode) VALUES (?, ?, ?, ?, ?, ?, '', ?, ?)`,
				p.ID, TenantOf(ctx), p.OwnerID, p.Slug, p.Name, p.Short, p.PrimaryColor, p.AIWriteMode); err != nil {
				t.Fatal(err)
			}
			a, _, _ := old.UpsertEntity(ctx, Entity{ID: id(), ProjectID: p.ID, Kind: "router", Slug: "router", Name: "Router", Attrs: JSON(`{}`), Source: "manual"})
			b, _, _ := old.UpsertEntity(ctx, Entity{ID: id(), ProjectID: p.ID, Kind: "server", Slug: "nas", Name: "NAS", Attrs: JSON(`{}`), Source: "manual"})
			if _, err := old.LinkEntities(ctx, Edge{ID: id(), From: b.ID, Rel: "uplink", To: a.ID, Attrs: JSON(`{}`), Source: "manual"}); err != nil {
				t.Fatal(err)
			}
			// Also as of migration 8 (CreateDocument writes newer columns).
			doc := Document{ID: id()}
			if _, err := old.Exec(ctx, `INSERT INTO documents (id, tenant_id, project_id, doc_type, slug, title, content, created_by) VALUES (?, ?, ?, ?, 'netz', 'Netz', '{}', ?)`,
				doc.ID, TenantOf(ctx), p.ID, DocMarkdown, u.ID); err != nil {
				t.Fatal(err)
			}
			if err := old.SetDocumentLinks(ctx, doc.ID, []string{a.ID}); err != nil {
				t.Fatal(err)
			}
			old.Close()

			d, err := openUpTo(ctx, c.dialect, c.dsn, 0)
			if err != nil {
				t.Fatalf("migrate: %v", err)
			}
			defer d.Close()
			if edges, err := d.EdgesOf(ctx, a.ID); err != nil || len(edges) != 1 {
				t.Fatalf("edges lost: %v %d", err, len(edges))
			}
			if ds, _ := d.DocumentsMentioning(ctx, a.ID); len(ds) != 1 {
				t.Fatal("document links lost")
			}
			// the slug of a deleted entity can be used again
			if err := d.DeleteEntity(ctx, a.ID); err != nil {
				t.Fatal(err)
			}
			if _, created, err := d.UpsertEntity(ctx, Entity{ID: id(), ProjectID: p.ID, Kind: "router", Slug: "router", Name: "Neuer Router", Attrs: JSON(`{}`), Source: "manual"}); err != nil || !created {
				t.Fatalf("slug reuse: %v %v", err, created)
			}
			var fk int
			if c.dialect == SQLite {
				_ = d.QueryRow(ctx, `PRAGMA foreign_keys`).Scan(&fk)
				if fk != 1 {
					t.Fatal("foreign keys must be on again")
				}
			}
		})
	}
}
