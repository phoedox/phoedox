package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/phoedox/phoedox/internal/core"
	"github.com/phoedox/phoedox/internal/mail"
	"github.com/phoedox/phoedox/internal/store"
)

const base = "https://docs.example.org"

var linkRe = regexp.MustCompile(`https://docs\.example\.org/(invite|reset)/([A-Za-z0-9_-]+)`)

func keyOf(err error) string {
	var ie *core.InvalidError
	if errors.As(err, &ie) {
		return ie.Key
	}
	return ""
}

func tokenFrom(t *testing.T, link string) string {
	t.Helper()
	m := linkRe.FindStringSubmatch(link)
	if m == nil {
		t.Fatalf("no link in %q", link)
	}
	return m[2]
}

func cookieRequest(c *http.Cookie) *http.Request {
	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(c)
	return r
}

func TestAccounts(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, store.SQLite, filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	box := &mail.Capture{}
	a := &Auth{DB: db, Mail: box, DefaultLocale: "de"}
	adminUser, err := a.CreateUser(ctx, "admin@example.org", "Admin", "sehr-geheimes-passwort", true)
	if err != nil {
		t.Fatal(err)
	}
	admin := core.Actor{User: adminUser, Via: "web"}

	// invitations: admins only, valid input, no duplicates
	if _, err := a.Invite(ctx, core.Actor{User: store.User{ID: "x"}, Via: "web"}, InviteInput{Email: "a@b.de"}, base); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("non-admin invite: %v", err)
	}
	if _, err := a.Invite(ctx, core.Actor{User: adminUser, Via: "mcp"}, InviteInput{Email: "a@b.de"}, base); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("AI client must not manage users: %v", err)
	}
	if _, err := a.Invite(ctx, admin, InviteInput{Email: "Jan <jan@example.org>"}, base); keyOf(err) != "validation.email" {
		t.Fatalf("invalid email: %v", err)
	}
	inv, err := a.Invite(ctx, admin, InviteInput{Email: " Jan@Example.org ", Name: "Jan", Locale: "en"}, base)
	if err != nil || !inv.Mailed || inv.User.HasPassword || inv.User.Email != "jan@example.org" {
		t.Fatalf("invite: %v %+v", err, inv)
	}
	msg, _ := box.Last()
	if msg.To != "jan@example.org" || !strings.Contains(msg.Subject, "Invitation") || !strings.Contains(msg.Body, inv.Link) || !strings.Contains(msg.Body, "Admin invited you") {
		t.Fatalf("invite mail: %+v", msg)
	}
	if _, err := a.Invite(ctx, admin, InviteInput{Email: "jan@example.org"}, base); keyOf(err) != "validation.email_taken" {
		t.Fatalf("duplicate: %v", err)
	}
	if _, err := a.Login(ctx, "jan@example.org", ""); err != ErrBadLogin {
		t.Fatalf("invited user without password must not log in: %v", err)
	}

	// a new link replaces the old one
	first := tokenFrom(t, inv.Link)
	again, err := a.NewAccountLink(ctx, admin, inv.User.ID, base)
	if err != nil || again.Purpose != store.LinkInvite {
		t.Fatalf("new link: %v %+v", err, again)
	}
	if _, err := a.Link(ctx, first); keyOf(err) != "validation.link_invalid" {
		t.Fatalf("old link must stop working: %v", err)
	}
	tok := tokenFrom(t, again.Link)
	if info, err := a.Link(ctx, tok); err != nil || info.Email != "jan@example.org" || info.Purpose != "invite" {
		t.Fatalf("link info: %v %+v", err, info)
	}
	if _, _, err := a.CompleteLink(ctx, tok, "kurz"); keyOf(err) != "validation.password_short" {
		t.Fatalf("short password: %v", err)
	}
	c1, jan, err := a.CompleteLink(ctx, tok, "jans-langes-passwort")
	if err != nil || !jan.HasPassword {
		t.Fatalf("complete: %v", err)
	}
	if _, _, err := a.CompleteLink(ctx, tok, "noch-ein-passwort"); keyOf(err) != "validation.link_invalid" {
		t.Fatalf("link reuse: %v", err)
	}
	if _, _, err := a.FromRequest(cookieRequest(c1), "token"); err != nil {
		t.Fatalf("session after invite: %v", err)
	}

	// self-service reset: mail only for known users, ends old sessions
	sent := len(box.Sent)
	if err := a.RequestReset(ctx, "unknown@example.org", base); err != nil || len(box.Sent) != sent {
		t.Fatalf("unknown address must not send: %v", err)
	}
	if err := a.RequestReset(ctx, "JAN@example.org", base); err != nil || len(box.Sent) != sent+1 {
		t.Fatalf("reset request: %v", err)
	}
	msg, _ = box.Last()
	if !strings.Contains(msg.Body, base+"/reset/") || !strings.Contains(msg.Subject, "Reset") {
		t.Fatalf("reset mail: %+v", msg)
	}
	c2, _, err := a.CompleteLink(ctx, tokenFrom(t, msg.Body), "neues-passwort-123")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.FromRequest(cookieRequest(c1), "token"); err == nil {
		t.Fatal("old session must end after reset")
	}

	// change password: wrong current, then ok; other sessions end, current stays
	janActor := core.Actor{User: jan, Via: "web"}
	if err := a.ChangePassword(ctx, janActor, "falsch", "noch-ein-neues-passwort", ""); keyOf(err) != "validation.password_wrong" {
		t.Fatalf("wrong current: %v", err)
	}
	step3, _ := a.Login(ctx, "jan@example.org", "neues-passwort-123")
	c3 := step3.Cookie
	if err := a.ChangePassword(ctx, janActor, "neues-passwort-123", "noch-ein-neues-passwort", SessionID(cookieRequest(c3))); err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.FromRequest(cookieRequest(c2), "token"); err == nil {
		t.Fatal("other session must end")
	}
	if _, _, err := a.FromRequest(cookieRequest(c3), "token"); err != nil {
		t.Fatalf("current session must stay: %v", err)
	}

	// profile
	if _, err := a.UpdateProfile(ctx, janActor, ProfilePatch{Locale: strp("fr")}); keyOf(err) != "validation.locale" {
		t.Fatalf("locale: %v", err)
	}
	if u, err := a.UpdateProfile(ctx, janActor, ProfilePatch{Name: strp("Jan M."), Locale: strp("en")}); err != nil || u.Name != "Jan M." || u.Locale != "en" {
		t.Fatalf("profile: %v %+v", err, u)
	}

	// disabling ends sessions and tokens; admins cannot lock themselves out
	_, plain, _ := a.CreateToken(ctx, jan.ID, "t", "write", nil)
	if _, err := a.UpdateUser(ctx, admin, jan.ID, UserPatch{Disabled: boolp(true)}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.FromRequest(cookieRequest(c3), "token"); err == nil {
		t.Fatal("disabled user keeps session")
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Authorization", "Bearer "+plain)
	if _, _, err := a.FromRequest(r, "token"); err == nil {
		t.Fatal("disabled user keeps token")
	}
	if _, err := a.Login(ctx, "jan@example.org", "noch-ein-neues-passwort"); err != ErrBadLogin {
		t.Fatalf("disabled login: %v", err)
	}
	if err := a.RequestReset(ctx, "jan@example.org", base); err != nil {
		t.Fatal(err)
	}
	if _, err := a.UpdateUser(ctx, admin, adminUser.ID, UserPatch{Admin: boolp(false)}); keyOf(err) != "validation.self_lockout" {
		t.Fatalf("self demote: %v", err)
	}
	if _, err := a.UpdateUser(ctx, admin, jan.ID, UserPatch{Disabled: boolp(false), Admin: boolp(true)}); err != nil {
		t.Fatal(err)
	}
	janAdmin := core.Actor{User: store.User{ID: jan.ID, IsAdmin: true}, Via: "web"}
	if _, err := a.UpdateUser(ctx, janAdmin, adminUser.ID, UserPatch{Disabled: boolp(true)}); err != nil {
		t.Fatalf("disable another admin: %v", err)
	}
	if _, err := a.UpdateUser(ctx, admin, jan.ID, UserPatch{Admin: boolp(false)}); keyOf(err) != "validation.last_admin" && !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("last admin: %v", err)
	}
	users, err := a.Users(ctx, janAdmin)
	if err != nil || len(users) != 2 {
		t.Fatalf("users: %v %d", err, len(users))
	}
}

func TestLastAdmin(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, store.SQLite, filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	a := &Auth{DB: db}
	one, _ := a.CreateUser(ctx, "one@example.org", "One", "sehr-geheimes-passwort", true)
	two, _ := a.CreateUser(ctx, "two@example.org", "Two", "sehr-geheimes-passwort", true)
	act := core.Actor{User: one, Via: "web"}
	if _, err := a.UpdateUser(ctx, act, two.ID, UserPatch{Admin: boolp(false)}); err != nil {
		t.Fatal(err)
	}
	// two is no admin any more; one is the last one and cannot be demoted by a CLI actor either
	cli := core.Actor{User: store.User{ID: "cli", IsAdmin: true}, Via: "cli"}
	if _, err := a.UpdateUser(ctx, cli, one.ID, UserPatch{Disabled: boolp(true)}); keyOf(err) != "validation.last_admin" {
		t.Fatalf("last admin: %v", err)
	}
	// without mail, links are only returned
	res, err := a.NewAccountLink(ctx, cli, two.ID, base)
	if err != nil || res.Mailed || res.Purpose != store.LinkReset || !strings.HasPrefix(res.Link, base+"/reset/") {
		t.Fatalf("admin reset link: %v %+v", err, res)
	}
}

func strp(s string) *string { return &s }
func boolp(b bool) *bool    { return &b }
