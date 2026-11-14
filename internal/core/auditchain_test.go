package core

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// TestAuditChain: events form a hash chain; editing or deleting one shows.
func TestAuditChain(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, _ := setup(t)
	admin := owner
	admin.User.IsAdmin = true
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Haus"})
	for i := 0; i < 5; i++ {
		if _, err := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "markdown", Title: fmt.Sprintf("Seite %d", i), Markdown: "x"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.VerifyAudit(ctx, reader); !errors.Is(err, ErrForbidden) {
		t.Fatalf("reader: %v", err)
	}
	r, err := s.VerifyAudit(ctx, admin)
	if err != nil || !r.OK || r.Checked < 6 || r.Head == "" || r.HeadSeq != r.Checked {
		t.Fatalf("intact: %v %+v", err, r)
	}
	// Someone edits the database by hand: the chain shows where.
	if _, err := s.DB.Exec(ctx, `UPDATE events SET actor = 'user:someone-else' WHERE seq = 3`); err != nil {
		t.Fatal(err)
	}
	r, _ = s.DB.VerifyEventChain(ctx)
	if r.OK || r.Broken != 3 || r.Reason != "changed" {
		t.Fatalf("changed: %+v", r)
	}
	if _, err := s.DB.Exec(ctx, `DELETE FROM events WHERE seq = 3`); err != nil {
		t.Fatal(err)
	}
	r, _ = s.DB.VerifyEventChain(ctx)
	if r.OK || r.Broken != 3 || r.Reason != "missing" {
		t.Fatalf("deleted: %+v", r)
	}
}

// TestSecretLinks: ciphertext only, opened once, expires.
func TestSecretLinks(t *testing.T) {
	ctx := context.Background()
	s, owner, _, _ := setup(t)
	if _, err := s.CreateSecretLink(ctx, Actor{User: owner.User, Via: "mcp"}, SecretLinkInput{Ciphertext: "x", TTLHours: 1}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("mcp: %v", err)
	}
	if _, err := s.CreateSecretLink(ctx, owner, SecretLinkInput{Ciphertext: "x", TTLHours: 5}); invalidKey(err) != "validation.secret_link_ttl" {
		t.Fatalf("ttl: %v", err)
	}
	l, err := s.CreateSecretLink(ctx, owner, SecretLinkInput{Ciphertext: "eyJ2IjoxfQ", Passphrase: true, TTLHours: 24})
	if err != nil || len(l.ID) != 32 {
		t.Fatalf("create: %v %+v", err, l)
	}
	got, err := s.OpenSecretLink(ctx, l.ID)
	if err != nil || got.Ciphertext != "eyJ2IjoxfQ" || !got.Passphrase {
		t.Fatalf("open: %v %+v", err, got)
	}
	if _, err := s.OpenSecretLink(ctx, l.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second open: %v", err)
	}
	old, _ := s.CreateSecretLink(ctx, owner, SecretLinkInput{Ciphertext: "y", TTLHours: 1})
	s.PurgeSecretLinks(ctx, time.Now().Add(2*time.Hour))
	if _, err := s.OpenSecretLink(ctx, old.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired: %v", err)
	}
	if _, err := s.OpenSecretLink(ctx, "../etc"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("bad id: %v", err)
	}
}
