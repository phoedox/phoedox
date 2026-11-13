package backup

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/phoedox/phoedox/internal/blob"
	"github.com/phoedox/phoedox/internal/core"
	"github.com/phoedox/phoedox/internal/graph"
	"github.com/phoedox/phoedox/internal/store"
)

func openDB(t *testing.T, dialect store.Dialect) *store.DB {
	t.Helper()
	ctx := context.Background()
	if dialect == store.SQLite {
		d, err := store.Open(ctx, store.SQLite, filepath.Join(t.TempDir(), "b.db"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { d.Close() })
		return d
	}
	dsn := os.Getenv("PHOEDOX_TEST_PG")
	admin, err := store.Open(ctx, store.Postgres, dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := "bk_" + strings.ToLower(string(graph.NewID()))
	if _, err := admin.SQL.ExecContext(ctx, `CREATE SCHEMA "`+schema+`"`); err != nil {
		t.Fatal(err)
	}
	d, err := store.Open(ctx, store.Postgres, dsn+"&search_path="+schema)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		d.Close()
		admin.SQL.ExecContext(context.Background(), `DROP SCHEMA "`+schema+`" CASCADE`)
		admin.Close()
	})
	return d
}

// fill makes some data in every important area.
func fill(t *testing.T, d *store.DB, blobs blob.Store, tmp string) {
	t.Helper()
	ctx := context.Background()
	s := core.New(d)
	s.Blobs, s.TempDir = blobs, tmp
	u, _ := d.CreateUser(ctx, store.User{ID: string(graph.NewID()), Email: "admin@example.org", Name: "Admin", IsAdmin: true})
	a := core.Actor{User: u, Via: "web"}
	org, err := s.CreateOrg(ctx, a, core.OrgInput{Name: "Firma"})
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.CreateProject(ctx, a, core.ProjectInput{Name: "Haus", AIWriteMode: "direct", Org: &org.Slug})
	if err != nil {
		t.Fatal(err)
	}
	doc, _ := s.CreateDocument(ctx, a, p.Slug, core.DocumentInput{Type: "markdown", Title: "Netz", Markdown: "Der @router steht im Flur."})
	_, _ = s.UpdateDocument(ctx, a, p.Slug, doc.Document.Slug, core.DocumentPatch{Markdown: new("Der @router steht im Keller.")})
	_, _ = s.SetRestricted(ctx, a, p.Slug, doc.Document.Slug, true)
	if _, err := s.UpsertEntity(ctx, a, p.Slug, core.EntityInput{Kind: "router", Name: "Router", Attrs: map[string]any{"ip": "10.0.0.1", "uplink": "fiber"}}); err != nil {
		t.Fatal(err)
	}
	_, _ = s.UpsertEntity(ctx, a, p.Slug, core.EntityInput{Kind: "room", Name: "Keller"})
	if _, err := s.Link(ctx, a, p.Slug, core.EdgeInput{From: "router", Rel: "located_in", To: "keller"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UploadFile(ctx, a, p.Slug, core.FileInput{Name: "plan.txt", Document: doc.Document.Slug}, strings.NewReader("Grundriss Keller")); err != nil {
		t.Fatal(err)
	}
	_ = d.AppendDocUpdate(ctx, doc.Document.ID, 1, []byte{0, 1, 2, 250})
}

func counts(t *testing.T, d *store.DB) map[string]int {
	t.Helper()
	out := map[string]int{}
	for _, tb := range Tables {
		var n int
		if err := d.QueryRow(store.System(context.Background()), `SELECT COUNT(*) FROM `+tb).Scan(&n); err != nil {
			t.Fatal(err)
		}
		out[tb] = n
	}
	return out
}

func TestRoundTrip(t *testing.T) {
	dialects := []store.Dialect{store.SQLite}
	if os.Getenv("PHOEDOX_TEST_PG") != "" {
		dialects = append(dialects, store.Postgres)
	}
	for _, from := range dialects {
		for _, to := range dialects {
			t.Run(string(from)+"_to_"+string(to), func(t *testing.T) {
				ctx := context.Background()
				dir := t.TempDir()
				src, dst := openDB(t, from), openDB(t, to)
				srcBlobs, dstBlobs := blob.Local{Dir: filepath.Join(dir, "a")}, blob.Local{Dir: filepath.Join(dir, "b")}
				fill(t, src, srcBlobs, dir)
				f, _ := os.Create(filepath.Join(dir, Name(timeNow())))
				m, err := Export(ctx, src, srcBlobs, true, "test", "k1", f)
				f.Close()
				if err != nil || m.Blobs < 1 || m.Rows["documents"] != 1 {
					t.Fatalf("export: %v %+v", err, m)
				}
				// the target has its own data, which the restore replaces
				_, _ = dst.CreateUser(ctx, store.User{ID: "x", Email: "old@example.org", Name: "Alt"})
				if _, err := Restore(ctx, dst, dstBlobs, dir, f.Name()); err != nil {
					t.Fatal(err)
				}
				if a, b := counts(t, src), counts(t, dst); !mapsEqual(a, b) {
					t.Fatalf("rows differ:\n%v\n%v", a, b)
				}
				svc := core.New(dst)
				svc.Blobs, svc.TempDir = dstBlobs, dir
				u, err := dst.UserByEmail(ctx, "admin@example.org")
				if err != nil || !u.IsAdmin {
					t.Fatalf("user: %v %+v", err, u)
				}
				a := core.Actor{User: u, Via: "web"}
				d, err := svc.Document(ctx, a, "haus", "netz")
				if err != nil || !d.Restricted || d.Version != 2 || !strings.Contains(string(d.Content), "Keller") {
					t.Fatalf("document: %v %+v", err, d)
				}
				e, err := svc.Entity(ctx, a, "haus", "router")
				if err != nil || len(e.Edges) != 1 || !strings.Contains(string(e.Entity.Attrs), "fiber") {
					t.Fatalf("entity: %v %+v", err, e)
				}
				fs, _ := svc.Files(ctx, a, "haus", core.FileFilter{})
				fc, err := svc.OpenFile(ctx, a, fs[0].ID, 0, false)
				if err != nil {
					t.Fatal(err)
				}
				b, _ := io.ReadAll(fc.Reader)
				fc.Reader.Close()
				if string(b) != "Grundriss Keller" {
					t.Fatalf("file: %q", b)
				}
				ups, _ := dst.DocUpdates(ctx, d.ID)
				if len(ups) != 1 || ups[0].Data[3] != 250 {
					t.Fatalf("binary data: %+v", ups)
				}
				// times stay comparable after the move (trash, expiry)
				if ps, _ := svc.Projects(ctx, a); len(ps) != 1 || ps[0].OrgName != "Firma" {
					t.Fatalf("projects: %+v", ps)
				}
				// new rows still get ids (Postgres sequences)
				if err := dst.AddEvent(ctx, "x", "test", "", nil); err != nil {
					t.Fatalf("event after restore: %v", err)
				}
			})
		}
	}
}

// Every table of the schema is backed up or skipped on purpose.
func TestTablesComplete(t *testing.T) {
	d := openDB(t, store.SQLite)
	rows, err := d.SQL.Query(`SELECT name FROM sqlite_master WHERE type = 'table'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var n string
		_ = rows.Scan(&n)
		if strings.HasPrefix(n, "search_index_") {
			continue // FTS shadow tables
		}
		if !slices.Contains(Tables, n) && !slices.Contains(Skipped, n) {
			t.Errorf("table %s is neither backed up nor skipped", n)
		}
	}
}

func TestNames(t *testing.T) {
	if Name(time.Unix(0, 0)) == Name(time.Unix(0, int64(time.Millisecond))) {
		t.Fatal("names of the same second must differ")
	}
	if !IsName("phoedox-20261113-180000.000.tar.gz") || IsName("../phoedox-1.tar.gz") || IsName("x.tar.gz") {
		t.Fatal("IsName")
	}
}

func mapsEqual(a, b map[string]int) bool {
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return len(a) == len(b)
}

func timeNow() time.Time { return time.Now() }
