package store

import (
	"context"
	"encoding/json"
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

func TestTimeJSONZeroIsNull(t *testing.T) {
	b, err := json.Marshal(struct{ A Time }{})
	if err != nil || string(b) != `{"A":null}` {
		t.Fatalf("got %s, %v", b, err)
	}
}

func TestDecisionNumbers(t *testing.T) {
	for name, d := range testDBs(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			u, err := d.CreateUser(ctx, User{ID: id(), Email: "a@b.de", Name: "A"})
			if err != nil {
				t.Fatal(err)
			}
			p, err := d.CreateProject(ctx, Project{ID: id(), Slug: "p", Name: "P", Short: "P", PrimaryColor: "#F26A1B", AIWriteMode: "suggest", OwnerID: u.ID})
			if err != nil {
				t.Fatal(err)
			}
			mk := func(typ, slug string) Document {
				doc, err := d.CreateDocument(ctx, Document{ID: id(), ProjectID: p.ID, Type: typ, Slug: slug, Title: slug, Content: JSON(`{}`), CreatedBy: u.ID}, "test")
				if err != nil {
					t.Fatal(err)
				}
				return doc
			}
			a, page, b := mk(DocADR, "a"), mk(DocMarkdown, "page"), mk(DocADR, "b")
			if a.Number != 1 || page.Number != 0 || b.Number != 2 {
				t.Fatalf("numbers: %d %d %d", a.Number, page.Number, b.Number)
			}
			if got, err := d.GetDecision(ctx, p.ID, 2); err != nil || got.ID != b.ID || got.Number != 2 {
				t.Fatalf("GetDecision: %v %+v", err, got)
			}
			if err := d.DeleteDocument(ctx, b.ID); err != nil {
				t.Fatal(err)
			}
			if c := mk(DocADR, "c"); c.Number != 3 {
				t.Fatalf("number reused: %d", c.Number)
			}

			// a stale related document rolls back the whole change
			a.Title = "a2"
			stale := page
			stale.Version = 7
			if _, err := d.UpdateDocument(ctx, a, 0, "test", stale); !errors.Is(err, ErrConflict) {
				t.Fatalf("stale related: %v", err)
			}
			if got, _ := d.GetDocument(ctx, p.ID, a.ID); got.Title != "a" || got.Version != 1 {
				t.Fatalf("not rolled back: %+v", got)
			}
			if _, err := d.UpdateDocument(ctx, a, 0, "test", page); err != nil {
				t.Fatal(err)
			}
			if got, _ := d.GetDocument(ctx, p.ID, page.ID); got.Version != 2 {
				t.Fatalf("related not saved: v%d", got.Version)
			}
		})
	}
}

func TestAccountStore(t *testing.T) {
	for name, d := range testDBs(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			u, err := d.CreateUser(ctx, User{ID: id(), Email: "x@y.de", Name: "X", IsAdmin: true, Locale: "en"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := d.CreateUser(ctx, User{ID: id(), Email: "X@y.de", Name: "X2"}); !errors.Is(err, ErrConflict) {
				t.Fatalf("duplicate: %v", err)
			}
			if n, err := d.CountActiveAdmins(ctx); err != nil || n != 1 {
				t.Fatalf("admins: %v %d", err, n)
			}
			exp := Now()
			exp.Time = exp.Time.Add(3600e9)
			if err := d.CreateSession(ctx, "s", u.ID, exp); err != nil {
				t.Fatal(err)
			}
			u.Disabled = true
			if err := d.UpdateUser(ctx, u); err != nil {
				t.Fatal(err)
			}
			got, err := d.UserByID(ctx, u.ID)
			if err != nil || !got.Disabled || got.Locale != "en" {
				t.Fatalf("disabled: %v %+v", err, got)
			}
			if _, err := d.UserBySession(ctx, "s"); !errors.Is(err, ErrNotFound) {
				t.Fatalf("session of disabled user: %v", err)
			}
			if n, _ := d.CountActiveAdmins(ctx); n != 0 {
				t.Fatalf("disabled admin counted: %d", n)
			}
			u.Disabled = false
			if err := d.UpdateUser(ctx, u); err != nil {
				t.Fatal(err)
			}
			if got, _ := d.UserByEmail(ctx, "x@y.de"); got.Disabled || got.ID != u.ID {
				t.Fatalf("enabled: %+v", got)
			}
			if us, err := d.ListUsers(ctx); err != nil || len(us) != 1 {
				t.Fatalf("list: %v %d", err, len(us))
			}

			// links: newer replaces older, use once, sets password, ends sessions
			if _, err := d.CreateLink(ctx, AccountLink{ID: "h1", UserID: u.ID, Purpose: LinkReset, CreatedBy: "t", ExpiresAt: exp}); err != nil {
				t.Fatal(err)
			}
			if _, err := d.CreateLink(ctx, AccountLink{ID: "h2", UserID: u.ID, Purpose: LinkReset, CreatedBy: "t", ExpiresAt: exp}); err != nil {
				t.Fatal(err)
			}
			if _, err := d.ValidLink(ctx, "h1"); !errors.Is(err, ErrNotFound) {
				t.Fatalf("old link valid: %v", err)
			}
			_ = d.CreateSession(ctx, "s2", u.ID, exp)
			if l, err := d.UseLink(ctx, "h2", "hash"); err != nil || l.UserID != u.ID {
				t.Fatalf("use: %v", err)
			}
			if _, err := d.UseLink(ctx, "h2", "hash2"); !errors.Is(err, ErrNotFound) {
				t.Fatalf("reuse: %v", err)
			}
			if got, _ := d.UserByID(ctx, u.ID); !got.HasPassword {
				t.Fatal("password not set")
			}
			if _, err := d.UserBySession(ctx, "s2"); !errors.Is(err, ErrNotFound) {
				t.Fatal("session survived reset")
			}
		})
	}
}

func TestFactorStore(t *testing.T) {
	for name, d := range testDBs(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			u, _ := d.CreateUser(ctx, User{ID: id(), Email: "f@y.de", Name: "F"})
			if err := d.SetTOTP(ctx, u.ID, "enc"); err != nil {
				t.Fatal(err)
			}
			if err := d.UseTOTPStep(ctx, u.ID, 100, true); err != nil {
				t.Fatal(err)
			}
			if err := d.UseTOTPStep(ctx, u.ID, 100, false); !errors.Is(err, ErrConflict) {
				t.Fatalf("step reuse: %v", err)
			}
			if tt, err := d.GetTOTP(ctx, u.ID); err != nil || !tt.Confirmed || tt.LastStep != 100 {
				t.Fatalf("totp: %v %+v", err, tt)
			}
			if us, _ := d.ListUsers(ctx); len(us) != 1 || !us[0].SecondFactor {
				t.Fatalf("second factor flag: %+v", us)
			}
			if err := d.ReplaceRecoveryCodes(ctx, u.ID, []string{"a", "b"}); err != nil {
				t.Fatal(err)
			}
			if err := d.UseRecoveryCode(ctx, u.ID, "a"); err != nil {
				t.Fatal(err)
			}
			if err := d.UseRecoveryCode(ctx, u.ID, "a"); !errors.Is(err, ErrNotFound) {
				t.Fatalf("code reuse: %v", err)
			}
			if n, _ := d.CountRecoveryCodes(ctx, u.ID); n != 1 {
				t.Fatalf("codes left: %d", n)
			}
			if _, err := d.AddPasskey(ctx, Passkey{ID: "cred1", UserID: u.ID, Name: "Laptop", Credential: "{}"}); err != nil {
				t.Fatal(err)
			}
			if err := d.RenamePasskey(ctx, u.ID, "cred1", "MacBook"); err != nil {
				t.Fatal(err)
			}
			if err := d.UpdatePasskeyUse(ctx, "cred1", `{"x":1}`); err != nil {
				t.Fatal(err)
			}
			if p, err := d.PasskeyByID(ctx, "cred1"); err != nil || p.Name != "MacBook" || p.LastUsedAt.IsZero() {
				t.Fatalf("passkey: %v %+v", err, p)
			}
			exp := Now()
			exp.Time = exp.Time.Add(60e9)
			if err := d.CreateChallenge(ctx, Challenge{ID: "c1", UserID: u.ID, Kind: "k", Data: "d"}, exp); err != nil {
				t.Fatal(err)
			}
			if err := d.CreateChallenge(ctx, Challenge{ID: "c2", Kind: "k"}, exp); err != nil {
				t.Fatal(err)
			}
			_, _ = d.GetChallenge(ctx, "c1", "k")
			if c, err := d.GetChallenge(ctx, "c1", "k"); err != nil || c.Attempts != 1 || c.Data != "d" || c.UserID != u.ID {
				t.Fatalf("challenge: %v %+v", err, c)
			}
			if _, err := d.GetChallenge(ctx, "c1", "other"); !errors.Is(err, ErrNotFound) {
				t.Fatalf("kind mismatch: %v", err)
			}
			if err := d.ResetSecondFactors(ctx, u.ID); err != nil {
				t.Fatal(err)
			}
			if pks, _ := d.Passkeys(ctx, u.ID); len(pks) != 0 {
				t.Fatal("passkeys survived reset")
			}
		})
	}
}

func TestOAuthStore(t *testing.T) {
	for name, d := range testDBs(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			u, _ := d.CreateUser(ctx, User{ID: id(), Email: "o@y.de", Name: "O"})
			if _, err := d.CreateOAuthClient(ctx, OAuthClient{ID: "c1", Name: "Claude", RedirectURIs: []string{"https://claude.ai/cb"}}); err != nil {
				t.Fatal(err)
			}
			c, err := d.OAuthClientByID(ctx, "c1")
			if err != nil || c.RedirectURIs[0] != "https://claude.ai/cb" || c.SecretHash != "" {
				t.Fatalf("client: %v %+v", err, c)
			}
			g, err := d.UpsertGrant(ctx, OAuthGrant{ID: "g1", ClientID: "c1", UserID: u.ID, Scope: "mcp", Resource: "r"})
			if err != nil {
				t.Fatal(err)
			}
			if g2, _ := d.UpsertGrant(ctx, OAuthGrant{ID: "g2", ClientID: "c1", UserID: u.ID, Scope: "mcp", Resource: "r2"}); g2.ID != "g1" {
				t.Fatalf("grant not reused: %+v", g2)
			}
			exp := Now()
			exp.Time = exp.Time.Add(60e9)
			if err := d.CreateOAuthCode(ctx, "code", g.ID, "https://claude.ai/cb", "ch", exp); err != nil {
				t.Fatal(err)
			}
			oc, err := d.UseOAuthCode(ctx, "code")
			if err != nil || oc.Used || oc.Expired || oc.Grant.ClientName != "Claude" || oc.Grant.Resource != "r2" {
				t.Fatalf("code: %v %+v", err, oc)
			}
			if oc, _ := d.UseOAuthCode(ctx, "code"); !oc.Used {
				t.Fatal("second use not detected")
			}
			if err := d.CreateOAuthToken(ctx, "rt", g.ID, "refresh", exp); err != nil {
				t.Fatal(err)
			}
			if err := d.UseRefreshToken(ctx, "rt"); err != nil {
				t.Fatal(err)
			}
			if err := d.UseRefreshToken(ctx, "rt"); !errors.Is(err, ErrConflict) {
				t.Fatalf("refresh reuse: %v", err)
			}
			d.TouchGrant(ctx, g)
			if gs, err := d.Grants(ctx, u.ID); err != nil || len(gs) != 1 || gs[0].LastUsedAt.IsZero() {
				t.Fatalf("grants: %v %+v", err, gs)
			}
			if err := d.RevokeGrant(ctx, u.ID, g.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := d.OAuthTokenByHash(ctx, "rt"); !errors.Is(err, ErrNotFound) {
				t.Fatalf("token of revoked grant: %v", err)
			}
		})
	}
}
