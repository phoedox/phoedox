package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	// The search index is filled by the core service; see TestSearchIndex.
	if hits, err := d.Search(ctx, []string{p.ID}, "router", 10); err != nil || len(hits) != 0 {
		t.Fatalf("search without index: %v %+v", err, hits)
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

func TestSearchIndex(t *testing.T) {
	for name, d := range testDBs(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			u, _ := d.CreateUser(ctx, User{ID: id(), Email: "s@y.de", Name: "S"})
			p1, _ := d.CreateProject(ctx, Project{ID: id(), Slug: "haus", Name: "Haus", Short: "HA", PrimaryColor: "#F26A1B", AIWriteMode: "suggest", OwnerID: u.ID})
			p2, _ := d.CreateProject(ctx, Project{ID: id(), Slug: "firma", Name: "Firma", Short: "FI", PrimaryColor: "#F26A1B", AIWriteMode: "suggest", OwnerID: u.ID})
			put := func(e IndexEntry) {
				t.Helper()
				if err := d.IndexPut(ctx, e); err != nil {
					t.Fatal(err)
				}
			}
			put(IndexEntry{RefID: "d1", Kind: "document", ProjectID: p1.ID, Slug: "netz", Subtype: "markdown", Title: "Hauptrouter im Keller", Body: "Der Router hängt am Glasfaser-Modem neben dem Sicherungskasten."})
			put(IndexEntry{RefID: "d2", Kind: "document", ProjectID: p1.ID, Slug: "architektur", Subtype: "markdown", Title: "Architektur", Body: "Der Go-Server liefert API, MCP und die Web-App aus. Ein Router verteilt die Pfade."})
			put(IndexEntry{RefID: "e1", Kind: "entity", ProjectID: p1.ID, Slug: "fritzbox", Subtype: "router", Title: "Fritz!Box 7590", Body: "router\nfritzbox\nip: 192.168.178.1"})
			put(IndexEntry{RefID: "x1", Kind: "document", ProjectID: p2.ID, Slug: "router", Subtype: "markdown", Title: "Router der Firma", Body: "Geheim"})
			// replacing an entry keeps one row
			put(IndexEntry{RefID: "d2", Kind: "document", ProjectID: p1.ID, Slug: "architektur", Subtype: "markdown", Title: "Architektur", Body: "Der Go-Server liefert API, MCP und die Web-App aus. Ein Router verteilt die Pfade."})
			if n, _ := d.IndexCount(ctx); n != 4 {
				t.Fatalf("count: %d", n)
			}
			search := func(q string) []Hit {
				t.Helper()
				hits, err := d.Search(ctx, []string{p1.ID}, q, 10)
				if err != nil {
					t.Fatalf("%q: %v", q, err)
				}
				return hits
			}
			ids := func(hs []Hit) string {
				var s []string
				for _, h := range hs {
					s = append(s, h.ID)
				}
				return strings.Join(s, ",")
			}
			// part of a word, title match first, other project hidden
			hs := search("router")
			if len(hs) != 3 || hs[0].ID != "d1" || strings.Contains(ids(hs), "x1") {
				t.Fatalf("router: %s", ids(hs))
			}
			if hs[0].ProjectSlug != "haus" || hs[0].Kind != "markdown" || hs[0].Slug != "netz" {
				t.Fatalf("hit fields: %+v", hs[0])
			}
			// all words must match; snippet marks them
			hs = search("glasfaser modem")
			if ids(hs) != "d1" || !strings.Contains(hs[0].Snippet, MarkStart+"Glasfaser"+MarkEnd) || !strings.Contains(hs[0].Snippet, MarkStart+"Modem"+MarkEnd) {
				t.Fatalf("glasfaser: %s %+v", ids(hs), hs)
			}
			if hs := search("glasfaser kaffee"); len(hs) != 0 {
				t.Fatalf("all words: %s", ids(hs))
			}
			if hs := search("hängt"); ids(hs) != "d1" {
				t.Fatalf("umlaut: %s", ids(hs))
			}
			if hs := search("go-server"); ids(hs) != "d2" {
				t.Fatalf("hyphen: %s", ids(hs))
			}
			if hs := search("192.168.178"); ids(hs) != "e1" {
				t.Fatalf("ip: %s", ids(hs))
			}
			if hs := search("Ar"); !strings.Contains(ids(hs), "d2") {
				t.Fatalf("short word: %s", ids(hs))
			}
			if hs := search(`"; DROP TABLE x; --`); len(hs) != 0 {
				t.Fatalf("junk: %s", ids(hs))
			}
			if err := d.IndexDelete(ctx, "d1"); err != nil {
				t.Fatal(err)
			}
			if hs := search("glasfaser"); len(hs) != 0 {
				t.Fatalf("deleted entry found: %s", ids(hs))
			}
		})
	}
}

func TestSnippet(t *testing.T) {
	text := strings.Repeat("Füllwort ", 40) + "Der Router hängt am Modem. " + strings.Repeat("Ende ", 40)
	s := Snippet(text, []string{"router", "modem"}, 80)
	if !strings.HasPrefix(s, "…") || !strings.HasSuffix(s, "…") || !strings.Contains(s, MarkStart+"Router"+MarkEnd) || len([]rune(s)) > 100 {
		t.Fatalf("snippet: %q", s)
	}
	if Snippet("nichts", []string{"x"}, 80) != "" {
		t.Fatal("no match must give no snippet")
	}
	if s := Snippet("Über Ärger", []string{"arger"}, 80); !strings.Contains(s, MarkStart+"Ärger"+MarkEnd) {
		t.Fatalf("diacritics: %q", s)
	}
}

func TestTrashFavoritesStore(t *testing.T) {
	for name, d := range testDBs(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			u, _ := d.CreateUser(ctx, User{ID: id(), Email: "t@y.de", Name: "Tina"})
			p, _ := d.CreateProject(ctx, Project{ID: id(), Slug: "p", Name: "P", Short: "P", PrimaryColor: "#F26A1B", AIWriteMode: "suggest", OwnerID: u.ID})
			doc, _ := d.CreateDocument(ctx, Document{ID: id(), ProjectID: p.ID, Type: DocMarkdown, Slug: "a", Title: "A", Content: JSON(`{"markdown":"1"}`), CreatedBy: u.ID}, "user:"+u.ID)
			doc2, _ := d.CreateDocument(ctx, Document{ID: id(), ProjectID: p.ID, Type: DocMarkdown, Slug: "b", Title: "B", Content: JSON(`{}`), CreatedBy: u.ID}, "user:"+u.ID)
			if v, err := d.GetVersion(ctx, doc.ID, 1); err != nil || strings.ReplaceAll(string(v.Content), " ", "") != `{"markdown":"1"}` {
				t.Fatalf("version: %v %+v", err, v)
			}
			if names, _ := d.UserNames(ctx, []string{u.ID, "x"}); names[u.ID] != "Tina" || len(names) != 1 {
				t.Fatalf("names: %v", names)
			}
			// favorites
			_ = d.AddFavorite(ctx, id(), p.ID, doc.ID, "")
			_ = d.AddFavorite(ctx, id(), p.ID, doc2.ID, "")
			_ = d.AddFavorite(ctx, id(), p.ID, doc.ID, u.ID)
			if err := d.AddFavorite(ctx, id(), p.ID, doc.ID, ""); err != nil {
				t.Fatalf("duplicate favorite: %v", err)
			}
			_ = d.ReorderFavorites(ctx, p.ID, "", []string{doc2.ID, doc.ID})
			fs, err := d.Favorites(ctx, p.ID, u.ID)
			if err != nil || len(fs) != 3 || fs[0].DocumentID != doc2.ID || fs[0].Personal || !fs[2].Personal {
				t.Fatalf("favorites: %v %+v", err, fs)
			}
			_ = d.RemoveFavorite(ctx, p.ID, doc.ID, u.ID)
			if fs, _ := d.Favorites(ctx, p.ID, u.ID); len(fs) != 2 {
				t.Fatalf("remove: %+v", fs)
			}
			// trash with entity + edge restore
			a, _, _ := d.UpsertEntity(ctx, Entity{ID: id(), ProjectID: p.ID, Kind: "router", Slug: "r", Name: "R", Attrs: JSON(`{}`), Source: "manual"})
			b, _, _ := d.UpsertEntity(ctx, Entity{ID: id(), ProjectID: p.ID, Kind: "server", Slug: "s", Name: "S", Attrs: JSON(`{}`), Source: "manual"})
			_, _ = d.LinkEntities(ctx, Edge{ID: id(), From: b.ID, Rel: "uplink", To: a.ID, Attrs: JSON(`{}`), Source: "manual"})
			_ = d.DeleteDocument(ctx, doc.ID)
			_ = d.DeleteEntity(ctx, a.ID)
			items, err := d.Trash(ctx, p.ID)
			if err != nil || len(items) != 2 || items[0].Kind == items[1].Kind {
				t.Fatalf("trash: %v %+v", err, items)
			}
			if _, err := d.RestoreEntity(ctx, p.ID, a.ID); err != nil {
				t.Fatal(err)
			}
			if es, _ := d.EdgesOf(ctx, a.ID); len(es) != 1 {
				t.Fatal("edge not restored")
			}
			if _, err := d.RestoreDocument(ctx, p.ID, doc.ID); err != nil {
				t.Fatal(err)
			}
			_ = d.DeleteDocument(ctx, doc.ID)
			if err := d.Purge(ctx, p.ID, doc.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := d.GetVersion(ctx, doc.ID, 1); !errors.Is(err, ErrNotFound) {
				t.Fatal("versions must go with the document")
			}
			_ = d.DeleteEntity(ctx, b.ID)
			later := Now()
			later.Time = later.Time.Add(1e9)
			if n, err := d.PurgeBefore(ctx, later); err != nil || n != 1 {
				t.Fatalf("purge before: %v %d", err, n)
			}
		})
	}
}

func TestLiveStore(t *testing.T) {
	for name, d := range testDBs(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			u, _ := d.CreateUser(ctx, User{ID: id(), Email: "l@y.de", Name: "L"})
			p, _ := d.CreateProject(ctx, Project{ID: id(), Slug: "p", Name: "P", Short: "P", PrimaryColor: "#F26A1B", AIWriteMode: "suggest", OwnerID: u.ID})
			doc, _ := d.CreateDocument(ctx, Document{ID: id(), ProjectID: p.ID, Type: DocMarkdown, Slug: "a", Title: "A", Content: JSON(`{"markdown":"1"}`), CreatedBy: u.ID}, "user:"+u.ID)
			bin := []byte{0, 1, 2, 255, 0x80}
			for i, b := range [][]byte{bin, {9, 9, 9}} {
				if err := d.AppendDocUpdate(ctx, doc.ID, int64(i+1), b); err != nil {
					t.Fatal(err)
				}
			}
			if ups, err := d.DocUpdates(ctx, doc.ID); err != nil || len(ups) != 2 || !bytes.Equal(ups[0].Data, bin) {
				t.Fatalf("updates: %v %+v", err, ups)
			}
			if err := d.CompactDocUpdates(ctx, doc.ID, 2, []byte("snap")); err != nil {
				t.Fatal(err)
			}
			if ups, _ := d.DocUpdates(ctx, doc.ID); len(ups) != 1 || string(ups[0].Data) != "snap" {
				t.Fatalf("compact: %+v", ups)
			}
			doc.Content = JSON(`{"markdown":"2"}`)
			s1, created, err := d.SaveLive(ctx, doc, "user:"+u.ID, time.Hour)
			if err != nil || !created || s1.Version != 2 {
				t.Fatalf("live 1: %v %v %d", err, created, s1.Version)
			}
			s1.Content = JSON(`{"markdown":"3"}`)
			s2, created, err := d.SaveLive(ctx, s1, "user:"+u.ID, time.Hour)
			if err != nil || created || s2.Version != 2 {
				t.Fatalf("live 2 must merge: %v %v %d", err, created, s2.Version)
			}
			if v, _ := d.GetVersion(ctx, doc.ID, 2); !strings.Contains(string(v.Content), "3") {
				t.Fatalf("merged content: %s", v.Content)
			}
			if err := d.ClearDocUpdates(ctx, doc.ID); err != nil {
				t.Fatal(err)
			}
		})
	}
}
