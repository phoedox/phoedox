package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/phoedox/phoedox/internal/store"
)

func openDB(t *testing.T) *store.DB {
	t.Helper()
	ctx := context.Background()
	if dsn := os.Getenv("PHOEDOX_TEST_PG"); dsn != "" {
		db, err := store.Open(ctx, store.Postgres, dsn)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = db.Exec(ctx, `DELETE FROM jobs`)
		t.Cleanup(func() { db.Close() })
		return db
	}
	db, err := store.Open(ctx, store.SQLite, filepath.Join(t.TempDir(), "j.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestQueue(t *testing.T) {
	ctx := context.Background()
	q := &Queue{DB: openDB(t)}
	var got []string
	fails := 1
	q.Handle("echo", func(_ context.Context, p json.RawMessage) error {
		var s string
		_ = json.Unmarshal(p, &s)
		if s == "flaky" && fails > 0 {
			fails--
			return errors.New("try again")
		}
		if s == "broken" {
			return Permanent(errors.New("never works"))
		}
		got = append(got, s)
		return nil
	})
	for _, s := range []string{"a", "flaky", "broken"} {
		if err := q.Add(ctx, "echo", s); err != nil {
			t.Fatal(err)
		}
	}
	_ = q.Add(ctx, "unknown", 1)
	for q.RunOne(ctx) {
	}
	if len(got) != 1 || got[0] != "a" {
		t.Fatalf("first round: %v", got)
	}
	counts, _ := q.DB.JobCounts(ctx)
	if counts["queued"] != 1 || counts["failed"] != 2 || counts["done"] != 1 {
		t.Fatalf("counts: %v", counts)
	}
	// the flaky job waits for its retry time
	_, _ = q.DB.Exec(ctx, `UPDATE jobs SET run_at = ? WHERE status = 'queued'`, store.Now())
	for q.RunOne(ctx) {
	}
	if len(got) != 2 || got[1] != "flaky" {
		t.Fatalf("retry: %v", got)
	}
}
