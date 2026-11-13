package secret

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSealOpen(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "master.key")
	b, err := Load("", file)
	if err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(file); err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("key file: %v %v", err, st.Mode())
	}
	s, err := b.Seal([]byte("JBSWY3DPEHPK3PXP"), "totp:u1")
	if err != nil || !strings.HasPrefix(s, "v1:") || strings.Contains(s, "JBSWY3DP") {
		t.Fatalf("seal: %v %q", err, s)
	}
	if p, err := b.Open(s, "totp:u1"); err != nil || string(p) != "JBSWY3DPEHPK3PXP" {
		t.Fatalf("open: %v", err)
	}
	if _, err := b.Open(s, "totp:u2"); err != ErrDecrypt {
		t.Fatal("ciphertext must be bound to its context")
	}
	tampered := s[:len(s)-2] + "AA"
	if _, err := b.Open(tampered, "totp:u1"); err != ErrDecrypt {
		t.Fatal("tampering not detected")
	}
	// the same file gives the same key
	b2, err := Load("", file)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b2.Open(s, "totp:u1"); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if _, err := Load("bm90LTMyLWJ5dGVz", file); err == nil {
		t.Fatal("short env key accepted")
	}
}
