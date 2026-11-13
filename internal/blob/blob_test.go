package blob

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocal(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path, hash, size, head, err := Spool(filepath.Join(dir, "tmp"), strings.NewReader("hallo welt"), 100)
	if err != nil || size != 10 || string(head) != "hallo welt" {
		t.Fatalf("spool: %v %d %q", err, size, head)
	}
	defer os.Remove(path)
	if len(hash) != 64 {
		t.Fatalf("hash %q", hash)
	}
	s := Local{Dir: filepath.Join(dir, "blobs")}
	for range 2 { // idempotent
		if err := s.Put(ctx, hash, path, size); err != nil {
			t.Fatal(err)
		}
	}
	r, err := s.Open(ctx, hash)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(r)
	r.Close()
	if string(b) != "hallo welt" {
		t.Fatalf("content %q", b)
	}
	if err := s.Delete(ctx, hash); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Open(ctx, hash); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after delete: %v", err)
	}
	if _, err := s.Open(ctx, "../../etc/passwd"); err == nil {
		t.Fatal("path traversal must fail")
	}
	if _, _, _, _, err := Spool(dir, strings.NewReader(strings.Repeat("x", 11)), 10); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("limit: %v", err)
	}
}
