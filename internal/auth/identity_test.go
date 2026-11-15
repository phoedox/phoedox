package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net/url"
	"path/filepath"
	"testing"

	"github.com/phoedox/phoedox/internal/core"
	"github.com/phoedox/phoedox/internal/store"
)

func TestIdentity(t *testing.T) {
	if _, err := ParseIdentityClients("store|Store|http://evil.example/cb"); err == nil {
		t.Fatal("plain http redirect accepted")
	}
	clients, err := ParseIdentityClients(" phoedox-store|Phoedox Store|https://store.example/auth/callback ")
	if err != nil || len(clients) != 1 {
		t.Fatalf("parse: %v %+v", err, clients)
	}
	ctx := context.Background()
	db, err := store.Open(ctx, store.SQLite, filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	a := &Auth{DB: db, IdentityClients: clients}
	u, _ := a.CreateUser(ctx, "jan@example.org", "Jan", "sehr-geheimes-passwort", false)
	web := core.Actor{User: u, Via: "web"}
	verifier := "a-verifier-that-is-long-enough-for-pkce-0123456789"
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	cb := "https://store.example/auth/callback"

	if _, err := a.ApproveIdentity(ctx, web, "phoedox-store", "https://evil.example/cb", "s", challenge); keyOf(err) != "validation.identity_client" {
		t.Fatalf("other redirect: %v", err)
	}
	if _, err := a.ApproveIdentity(ctx, core.Actor{User: u, Via: "mcp"}, "phoedox-store", cb, "s", challenge); err == nil {
		t.Fatal("AI clients must not approve")
	}
	redirect, err := a.ApproveIdentity(ctx, web, "phoedox-store", cb, "xyz", challenge)
	if err != nil {
		t.Fatal(err)
	}
	ru, _ := url.Parse(redirect)
	code := ru.Query().Get("code")
	if ru.Query().Get("state") != "xyz" || code == "" || ru.Host != "store.example" {
		t.Fatalf("redirect: %s", redirect)
	}
	if _, err := a.RedeemIdentity("phoedox-store", cb, code, "wrong-verifier-wrong-verifier-wrong-verifier"); err == nil {
		t.Fatal("wrong verifier accepted")
	}
	// A failed attempt burns the code.
	if _, err := a.RedeemIdentity("phoedox-store", cb, code, verifier); err == nil {
		t.Fatal("code reused after a failed attempt")
	}
	redirect, _ = a.ApproveIdentity(ctx, web, "phoedox-store", cb, "xyz", challenge)
	ru, _ = url.Parse(redirect)
	id, err := a.RedeemIdentity("phoedox-store", cb, ru.Query().Get("code"), verifier)
	if err != nil || id.Subject != u.ID || id.Email != "jan@example.org" || id.Name != "Jan" {
		t.Fatalf("identity: %+v %v", id, err)
	}
	if _, err := a.RedeemIdentity("phoedox-store", cb, ru.Query().Get("code"), verifier); err == nil {
		t.Fatal("code used twice")
	}
}

func TestAppSignIn(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, store.SQLite, filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	a := &Auth{DB: db} // no configured services: the app client is built in
	u, _ := a.CreateUser(ctx, "jan@example.org", "Jan", "sehr-geheimes-passwort", false)
	web := core.Actor{User: u, Via: "web"}
	verifier := "app-verifier-that-is-long-enough-for-pkce-0123456789"
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])

	if c, err := a.IdentityRequest("phoedox-app", "phoedox://signin"); err != nil || !c.App {
		t.Fatalf("app client: %+v %v", c, err)
	}
	if _, err := a.IdentityRequest("phoedox-app", "https://evil.example/cb"); err == nil {
		t.Fatal("app client with another redirect")
	}
	redirect, err := a.ApproveIdentity(ctx, web, "phoedox-app", "phoedox://signin", "st", challenge)
	if err != nil {
		t.Fatal(err)
	}
	ru, _ := url.Parse(redirect)
	code := ru.Query().Get("code")
	if ru.Scheme != "phoedox" || ru.Query().Get("state") != "st" || code == "" {
		t.Fatalf("redirect: %s", redirect)
	}
	token, id, err := a.RedeemApp(ctx, code, verifier, "Pixel 9")
	if err != nil || token == "" || id.Email != "jan@example.org" {
		t.Fatalf("redeem: %q %+v %v", token, id, err)
	}
	// The token works for the REST API and is listed under the device's name.
	if _, tok, err := db.UserByToken(ctx, HashSecret(token)); err != nil || tok.Name != "Phoedox app (Pixel 9)" || tok.Access != "write" {
		t.Fatalf("token: %+v %v", tok, err)
	}
	if _, _, err := a.RedeemApp(ctx, code, verifier, "x"); err == nil {
		t.Fatal("app code reused")
	}
}
