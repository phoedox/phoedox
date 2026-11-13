// Package auth handles passwords, sessions and API tokens.
//
// MVP: password login with an HttpOnly session cookie and personal API tokens
// (Bearer) for REST, MCP and CLI. TOTP, passkeys, OIDC, magic links, the
// short-lived JWT and OAuth 2.1 for MCP follow (see docs/decisions.md).
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"

	"github.com/phoedox/phoedox/internal/core"
	"github.com/phoedox/phoedox/internal/graph"
	"github.com/phoedox/phoedox/internal/mail"
	"github.com/phoedox/phoedox/internal/secret"
	"github.com/phoedox/phoedox/internal/store"
)

// CookieName is the session cookie.
const CookieName = "phx_session"

// SessionTTL is how long a browser login lasts.
const SessionTTL = 30 * 24 * time.Hour

// ErrBadLogin hides whether the email or the password was wrong.
var ErrBadLogin = errors.New("email or password is wrong")

const (
	argonTime    = 2
	argonMemory  = 64 * 1024
	argonThreads = 2
	argonKeyLen  = 32
)

// HashPassword returns an argon2id hash in PHC string format. Length rules
// are checked by passwordHash.
func HashPassword(pw string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(pw), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

// VerifyPassword checks a password against a hash from HashPassword.
func VerifyPassword(hash, pw string) bool {
	parts := strings.Split(hash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var m, t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return false
	}
	salt, err1 := base64.RawStdEncoding.DecodeString(parts[4])
	want, err2 := base64.RawStdEncoding.DecodeString(parts[5])
	if err1 != nil || err2 != nil {
		return false
	}
	got := argon2.IDKey([]byte(pw), salt, t, m, p, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// NewSecret returns a random secret with a prefix and its stored hash.
func NewSecret(prefix string) (plain, hash string) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	plain = prefix + base64.RawURLEncoding.EncodeToString(b)
	return plain, HashSecret(plain)
}

// HashSecret hashes a session id or token for storage.
func HashSecret(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}

// Auth bundles login, accounts and token handling.
type Auth struct {
	DB *store.DB
	// SecureCookies sets the Secure flag (true behind HTTPS).
	SecureCookies bool
	// Mail sends invitations and reset links; nil or disabled means links
	// are only shown to admins.
	Mail mail.Sender
	// DefaultLocale is used for mails to users without a language.
	DefaultLocale string
	// Box encrypts TOTP seeds with the instance master key.
	Box *secret.Box
}

// Login checks the password. Without two-factor authentication it starts a
// session; otherwise it returns a challenge for the second step.
func (a *Auth) Login(ctx context.Context, email, password string) (LoginStep, error) {
	u, err := a.DB.UserByEmail(ctx, email)
	if err != nil || u.PasswordHash == "" || !VerifyPassword(u.PasswordHash, password) {
		// Spend similar time for unknown users to avoid user enumeration.
		if err != nil {
			VerifyPassword("$argon2id$v=19$m=65536,t=2,p=2$c2FsdHNhbHRzYWx0c2FsdA$aGFzaGhhc2hoYXNoaGFzaGhhc2hoYXNoaGFzaGhhc2g", password)
		}
		return LoginStep{}, ErrBadLogin
	}
	return a.passwordStep(ctx, u)
}

// startSession creates a session for u and returns its cookie.
func (a *Auth) startSession(ctx context.Context, u store.User) (*http.Cookie, error) {
	plain, hash := NewSecret("")
	exp := store.Now()
	exp.Time = exp.Time.Add(SessionTTL)
	if err := a.DB.CreateSession(ctx, hash, u.ID, exp); err != nil {
		return nil, err
	}
	_ = a.DB.TouchLogin(ctx, u.ID)
	return a.cookie(plain, exp.Time), nil
}

func (a *Auth) cookie(value string, exp time.Time) *http.Cookie {
	return &http.Cookie{Name: CookieName, Value: value, Path: "/", Expires: exp, HttpOnly: true, Secure: a.SecureCookies, SameSite: http.SameSiteLaxMode}
}

// Logout ends the session of the request and returns a clearing cookie.
func (a *Auth) Logout(r *http.Request) *http.Cookie {
	if c, err := r.Cookie(CookieName); err == nil {
		_ = a.DB.DeleteSession(r.Context(), HashSecret(c.Value))
	}
	return &http.Cookie{Name: CookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: a.SecureCookies, SameSite: http.SameSiteLaxMode}
}

// FromRequest resolves the actor from a Bearer token or the session cookie.
// via is used for token requests ("token" for REST, "mcp" for MCP).
func (a *Auth) FromRequest(r *http.Request, via string) (core.Actor, bool, error) {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		u, t, err := a.DB.UserByToken(r.Context(), HashSecret(strings.TrimSpace(h[7:])))
		if err != nil {
			return core.Actor{}, false, err
		}
		return core.Actor{User: u, Via: via, TokenID: t.ID}, false, nil
	}
	c, err := r.Cookie(CookieName)
	if err != nil {
		return core.Actor{}, false, store.ErrNotFound
	}
	u, err := a.DB.UserBySession(r.Context(), HashSecret(c.Value))
	if err != nil {
		return core.Actor{}, false, err
	}
	return core.Actor{User: u, Via: "web"}, true, nil
}

// CreateToken creates a personal API token and returns the plain value once.
func (a *Auth) CreateToken(ctx context.Context, userID, name string) (store.Token, string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "Token"
	}
	plain, hash := NewSecret("phx_")
	t, err := a.DB.CreateToken(ctx, store.Token{ID: string(graph.NewID()), UserID: userID, Name: name, Prefix: plain[:10]}, hash)
	if err == nil {
		_ = a.DB.AddEvent(ctx, "user:"+userID, "token.create", t.ID, map[string]string{"name": name})
	}
	return t, plain, err
}

// CreateUser creates an account with a password (CLI, first admin).
func (a *Auth) CreateUser(ctx context.Context, email, name, password string, admin bool) (store.User, error) {
	email, err := normalizeEmail(email)
	if err != nil {
		return store.User{}, err
	}
	hash, err := NewPasswordHash(password)
	if err != nil {
		return store.User{}, err
	}
	u, err := a.DB.CreateUser(ctx, store.User{ID: string(graph.NewID()), Email: email, Name: name, IsAdmin: admin, PasswordHash: hash})
	if errors.Is(err, store.ErrConflict) {
		return u, core.Invalid("validation.email_taken", nil, "a user with this email exists")
	}
	return u, err
}
