package store

import (
	"context"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/phoedox/phoedox/internal/graph"
)

// TestRowLevelSecurity runs as a role without superuser rights, so that
// Postgres really filters by tenant.
func TestRowLevelSecurity(t *testing.T) {
	dsn := os.Getenv("PHOEDOX_TEST_PG")
	if dsn == "" {
		t.Skip("PHOEDOX_TEST_PG not set")
	}
	ctx := context.Background()
	admin, err := Open(ctx, Postgres, dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := "rls_" + strings.ToLower(string(graph.NewID()))
	for _, q := range []string{
		`DO $$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'phx_rls') THEN CREATE ROLE phx_rls LOGIN PASSWORD 'phx_rls' NOSUPERUSER NOBYPASSRLS; END IF; END $$`,
		`CREATE SCHEMA "` + schema + `" AUTHORIZATION phx_rls`,
		`GRANT CONNECT ON DATABASE ` + strings.TrimPrefix(mustURL(t, dsn).Path, "/") + ` TO phx_rls`,
	} {
		if _, err := admin.SQL.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		admin.SQL.ExecContext(context.Background(), `DROP SCHEMA "`+schema+`" CASCADE`)
		admin.Close()
	})
	u := mustURL(t, dsn)
	u.User = url.UserPassword("phx_rls", "phx_rls")
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()

	d, err := Open(ctx, Postgres, u.String(), WithMultiTenant())
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if bypass, err := d.RLSBypassed(ctx); err != nil || bypass {
		t.Fatalf("test role must not bypass RLS: %v %v", bypass, err)
	}
	for _, tn := range []Tenant{{ID: "t1", Name: "Kunde 1", Domains: []string{"eins.example.org"}}, {ID: "t2", Name: "Kunde 2", Domains: []string{"zwei.example.org"}}} {
		if err := d.CreateTenant(ctx, tn); err != nil {
			t.Fatal(err)
		}
	}
	if id, err := d.TenantByDomain(ctx, "Zwei.Example.org"); err != nil || id != "t2" {
		t.Fatalf("domain: %v %q", err, id)
	}
	c1, c2 := WithTenant(ctx, "t1"), WithTenant(ctx, "t2")
	u1, err := d.CreateUser(c1, User{ID: id(), Email: "jan@example.org", Name: "Jan (1)"})
	if err != nil {
		t.Fatal(err)
	}
	u2, err := d.CreateUser(c2, User{ID: id(), Email: "jan@example.org", Name: "Jan (2)"})
	if err != nil {
		t.Fatalf("same email in another tenant: %v", err)
	}
	p1, err := d.CreateProject(c1, Project{ID: id(), Slug: "netz", Name: "Netz", Short: "NE", PrimaryColor: "#F26A1B", AIWriteMode: "suggest", OwnerID: u1.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.CreateProject(c2, Project{ID: id(), Slug: "netz", Name: "Netz 2", Short: "NE", PrimaryColor: "#F26A1B", AIWriteMode: "suggest", OwnerID: u2.ID}); err != nil {
		t.Fatalf("same slug in another tenant: %v", err)
	}
	// Queries by id alone stay inside the tenant.
	if _, err := d.UserByID(c2, u1.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other tenant's user by id: %v", err)
	}
	if got, err := d.UserByEmail(c1, "jan@example.org"); err != nil || got.ID != u1.ID {
		t.Fatalf("own user: %v %+v", err, got)
	}
	if got, err := d.ProjectFor(c1, "netz", u1.ID); err != nil || got.ID != p1.ID || got.Role != RoleOwner {
		t.Fatalf("own project: %v %+v", err, got)
	}
	var n int
	if err := d.QueryRow(c2, `SELECT COUNT(*) FROM project_members`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("members visible to t2: %v %d", err, n)
	}
	// Writing into another tenant is refused by the policy.
	if _, err := d.Exec(c2, `UPDATE users SET name = 'x' WHERE id = ?`, u1.ID); err != nil {
		t.Fatal(err)
	} else if got, _ := d.UserByID(c1, u1.ID); got.Name != "Jan (1)" {
		t.Fatal("update crossed tenants")
	}
	if _, err := d.Exec(c2, `INSERT INTO projects (id, tenant_id, owner_id, slug, name, short, created_at) VALUES (?, 't1', ?, 'x', 'x', 'X', ?)`, id(), u1.ID, Now()); err == nil {
		t.Fatal("insert into another tenant must fail")
	}
	// Without a tenant nothing is visible.
	if err := d.QueryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("no tenant: %v %d", err, n)
	}
	if err := d.QueryRow(System(ctx), `SELECT COUNT(*) FROM users`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("system: %v %d", err, n)
	}
	// Rows from a pooled connection never leak into the next statement's tenant.
	for range 20 {
		if err := d.QueryRow(c2, `SELECT COUNT(*) FROM projects`).Scan(&n); err != nil || n != 1 {
			t.Fatalf("pooled: %v %d", err, n)
		}
	}
}

func mustURL(t *testing.T, s string) *url.URL {
	t.Helper()
	u, err := url.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return u
}
