package auth

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/phoedox/phoedox/internal/mail"
	"github.com/phoedox/phoedox/internal/store"
)

func TestSignup(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, store.SQLite, filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	box := &mail.Capture{}
	a := &Auth{DB: db, Mail: box, DefaultLocale: "de"}

	if err := a.Signup(ctx, SignupInput{Email: "neu@example.org", Locale: "de"}, base, true); keyOf(err) != "validation.terms_required" {
		t.Fatalf("terms: %v", err)
	}
	if err := a.Signup(ctx, SignupInput{Email: "kein-mail", Locale: "de", Accept: true}, base, true); err == nil {
		t.Fatal("bad address accepted")
	}
	if err := a.Signup(ctx, SignupInput{Email: "Neu@Example.org", Name: "Neu", Locale: "en", Accept: true}, base, true); err != nil {
		t.Fatal(err)
	}
	m, ok := box.Last()
	if !ok || m.To != "neu@example.org" || !strings.Contains(m.Subject, "Confirm your Phoedox account") {
		t.Fatalf("confirmation mail: %+v", m)
	}
	// Without the link there is no password, so nobody can sign in yet.
	u, err := db.UserByEmail(ctx, "neu@example.org")
	if err != nil || u.HasPassword || u.IsAdmin {
		t.Fatalf("pending account: %+v %v", u, err)
	}
	if _, err := a.Login(ctx, "neu@example.org", "irgendwas-langes"); err == nil {
		t.Fatal("login before confirmation")
	}
	tok := tokenFrom(t, m.Body)
	if _, u, err := a.CompleteLink(ctx, tok, "ein-langes-passwort"); err != nil || u.Email != "neu@example.org" {
		t.Fatalf("confirm: %+v %v", u, err)
	}

	// Signing up again with a known address sends a reset link instead and
	// answers the same.
	n := len(box.Sent)
	if err := a.Signup(ctx, SignupInput{Email: "neu@example.org", Locale: "de", Accept: true}, base, true); err != nil {
		t.Fatal(err)
	}
	if all := box.Sent; len(all) != n+1 || !strings.Contains(all[n].Body, base+"/reset/") {
		t.Fatalf("known address: %+v", box.Sent)
	}
}
