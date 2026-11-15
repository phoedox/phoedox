package auth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/phoedox/phoedox/internal/core"
)

// "Sign in with Phoedox" for our own services such as the plugin store
// (ADR-0058). Unlike the OAuth server for MCP it hands out no access to
// content: a service only learns who the person is (id, name, e-mail).
// Clients are fixed in the configuration with their exact redirect address;
// codes are single use, short-lived and bound to a PKCE challenge.

// IdentityClient is a service allowed to ask who someone is.
type IdentityClient struct {
	ID, Name, Redirect string
}

// ParseIdentityClients reads PHOEDOX_IDENTITY_CLIENTS:
// "id|Name|https://host/callback", several separated by commas.
func ParseIdentityClients(s string) ([]IdentityClient, error) {
	var out []IdentityClient
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		f := strings.Split(part, "|")
		if len(f) != 3 || f[0] == "" || f[1] == "" {
			return nil, fmt.Errorf("identity client %q: use id|Name|https://host/callback", part)
		}
		u, err := url.Parse(f[2])
		if err != nil || u.Host == "" || (u.Scheme != "https" && !(u.Scheme == "http" && isLoopback(u.Hostname()))) || u.Fragment != "" {
			return nil, fmt.Errorf("identity client %q: the redirect must be an https address", f[0])
		}
		out = append(out, IdentityClient{ID: f[0], Name: f[1], Redirect: f[2]})
	}
	return out, nil
}

// Identity is what a service learns about the person.
type Identity struct {
	Subject string `json:"sub"`
	Email   string `json:"email"`
	Name    string `json:"name"`
}

type identityCode struct {
	client, redirect, challenge string
	id                          Identity
	expires                     time.Time
}

// IdentityCodes holds the codes until a service redeems them (in memory:
// they live two minutes).
type IdentityCodes struct {
	mu    sync.Mutex
	codes map[string]identityCode
}

const identityCodeTTL = 2 * time.Minute

func (a *Auth) identityClient(id, redirect string) (IdentityClient, error) {
	for _, c := range a.IdentityClients {
		if c.ID == id && c.Redirect == redirect {
			return c, nil
		}
	}
	return IdentityClient{}, core.Invalid("validation.identity_client", nil, "unknown service or redirect address")
}

// IdentityRequest checks a request and returns the service's name.
func (a *Auth) IdentityRequest(clientID, redirect string) (IdentityClient, error) {
	return a.identityClient(clientID, redirect)
}

// ApproveIdentity issues a code for the signed-in person and returns the
// address to send the browser to.
func (a *Auth) ApproveIdentity(ctx context.Context, actor core.Actor, clientID, redirect, state, challenge string) (string, error) {
	if actor.Via != "web" || actor.User.ID == "" {
		return "", core.ErrForbidden
	}
	c, err := a.identityClient(clientID, redirect)
	if err != nil {
		return "", err
	}
	if len(challenge) < 43 || len(challenge) > 128 || state == "" || len(state) > 200 {
		return "", core.Invalid("validation.identity_request", nil, "state and a S256 code challenge are required")
	}
	plain, hash := NewSecret("phxi_")
	a.Identity.put(hash, identityCode{client: c.ID, redirect: c.Redirect, challenge: challenge,
		id: Identity{Subject: actor.User.ID, Email: actor.User.Email, Name: actor.User.Name}, expires: time.Now().Add(identityCodeTTL)})
	_ = a.DB.AddEvent(ctx, actor.String(), "identity.grant", actor.User.ID, map[string]string{"client": c.ID})
	q := url.Values{"code": {plain}, "state": {state}}
	sep := "?"
	if strings.Contains(c.Redirect, "?") {
		sep = "&"
	}
	return c.Redirect + sep + q.Encode(), nil
}

// RedeemIdentity exchanges a code for the identity (once).
func (a *Auth) RedeemIdentity(clientID, redirect, code, verifier string) (Identity, error) {
	bad := core.Invalid("validation.identity_code", nil, "the code is invalid, expired or was already used")
	c, ok := a.Identity.take(HashSecret(code))
	if !ok || time.Now().After(c.expires) || c.client != clientID || c.redirect != redirect {
		return Identity{}, bad
	}
	sum := sha256.Sum256([]byte(verifier))
	if subtle.ConstantTimeCompare([]byte(base64.RawURLEncoding.EncodeToString(sum[:])), []byte(c.challenge)) != 1 {
		return Identity{}, bad
	}
	return c.id, nil
}

func (s *IdentityCodes) put(hash string, c identityCode) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.codes == nil {
		s.codes = map[string]identityCode{}
	}
	now := time.Now()
	for k, v := range s.codes {
		if now.After(v.expires) {
			delete(s.codes, k)
		}
	}
	s.codes[hash] = c
}

func (s *IdentityCodes) take(hash string) (identityCode, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.codes[hash]
	delete(s.codes, hash)
	return c, ok
}
