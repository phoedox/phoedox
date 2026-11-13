package auth

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/phoedox/phoedox/internal/store"
)

func TestPassword(t *testing.T) {
	h, err := HashPassword("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword(h, "correct horse battery") || VerifyPassword(h, "wrong horse battery") {
		t.Fatal("verify")
	}
	if _, err := NewPasswordHash("short"); err == nil {
		t.Fatal("short password accepted")
	}
}

func TestLoginAndTokens(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, store.SQLite, filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	a := &Auth{DB: db}
	u, err := a.CreateUser(ctx, "admin@coph.dev", "Admin", "sehr-geheimes-passwort", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.Login(ctx, "admin@coph.dev", "falsch-falsch-falsch"); err != ErrBadLogin {
		t.Fatalf("wrong password: %v", err)
	}
	c, _, err := a.Login(ctx, "ADMIN@coph.dev", "sehr-geheimes-passwort")
	if err != nil || !c.HttpOnly {
		t.Fatalf("login: %v", err)
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(c)
	if actor, cookie, err := a.FromRequest(r, "token"); err != nil || !cookie || actor.User.ID != u.ID {
		t.Fatalf("session: %v", err)
	}

	_, plain, err := a.CreateToken(ctx, u.ID, "Claude")
	if err != nil || !strings.HasPrefix(plain, "phx_") {
		t.Fatalf("token: %v %q", err, plain)
	}
	r = httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Authorization", "Bearer "+plain)
	if actor, cookie, err := a.FromRequest(r, "mcp"); err != nil || cookie || actor.Via != "mcp" {
		t.Fatalf("bearer: %v %+v", err, actor)
	}
	r.Header.Set("Authorization", "Bearer phx_wrong")
	if _, _, err := a.FromRequest(r, "mcp"); err == nil {
		t.Fatal("wrong token accepted")
	}
}
