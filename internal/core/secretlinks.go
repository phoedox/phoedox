package core

import (
	"context"
	"errors"
	"regexp"
	"slices"
	"time"

	"github.com/phoedox/phoedox/internal/store"
)

// One-time secret links (end-to-end encrypted): the browser encrypts the
// secret with a random key and sends only the ciphertext; the key travels in
// the link's #fragment, which browsers never send to the server. Opening the
// link deletes it. An optional passphrase adds a second factor.

const maxSecretCiphertext = 64 << 10

var (
	secretLinkID   = regexp.MustCompile(`^[a-z0-9]{20,64}$`)
	secretLinkTTLs = []int{1, 24, 168} // hours
)

// SecretLinkInput creates a link.
type SecretLinkInput struct {
	Ciphertext string `json:"ciphertext"`
	Passphrase bool   `json:"passphrase"`
	TTLHours   int    `json:"ttlHours"`
}

// SecretLinkCreated is the stored link (without its key).
type SecretLinkCreated struct {
	ID        string     `json:"id"`
	ExpiresAt store.Time `json:"expiresAt"`
}

// CreateSecretLink stores an encrypted secret (signed-in people in the browser).
func (s *Service) CreateSecretLink(ctx context.Context, a Actor, in SecretLinkInput) (SecretLinkCreated, error) {
	if a.User.ID == "" || a.Via != "web" || a.Restricted() {
		return SecretLinkCreated{}, ErrForbidden
	}
	if in.Ciphertext == "" || len(in.Ciphertext) > maxSecretCiphertext {
		return SecretLinkCreated{}, invalid("validation.secret_link_size", map[string]any{"max": maxSecretCiphertext >> 10}, "the secret is empty or too large")
	}
	if !slices.Contains(secretLinkTTLs, in.TTLHours) {
		return SecretLinkCreated{}, invalid("validation.secret_link_ttl", nil, "unknown lifetime")
	}
	now := store.Now()
	l := store.SecretLink{ID: newSecretID(), Ciphertext: in.Ciphertext, Passphrase: in.Passphrase,
		ExpiresAt: store.Time{Time: now.Add(time.Duration(in.TTLHours) * time.Hour)}, CreatedBy: a.User.ID, CreatedAt: now}
	if err := s.DB.CreateSecretLink(ctx, l); err != nil {
		return SecretLinkCreated{}, err
	}
	s.event(ctx, a, "secret_link.create", l.ID, map[string]any{"ttlHours": in.TTLHours, "passphrase": in.Passphrase})
	return SecretLinkCreated{ID: l.ID, ExpiresAt: l.ExpiresAt}, nil
}

// OpenSecretLink hands out the ciphertext once and deletes it.
func (s *Service) OpenSecretLink(ctx context.Context, id string) (store.SecretLink, error) {
	if !secretLinkID.MatchString(id) {
		return store.SecretLink{}, ErrNotFound
	}
	l, err := s.DB.TakeSecretLink(store.System(ctx), id, store.Now())
	if errors.Is(err, store.ErrNotFound) {
		return l, ErrNotFound
	}
	if err != nil {
		return l, err
	}
	// The opener is anonymous; the event records that the link was used.
	_ = s.DB.AddEvent(store.WithTenant(ctx, l.TenantID), "link:"+l.ID, "secret_link.open", l.ID, map[string]any{"by": l.CreatedBy})
	return l, nil
}

// PurgeSecretLinks removes expired links (all tenants).
func (s *Service) PurgeSecretLinks(ctx context.Context, now time.Time) {
	_, _ = s.DB.PurgeSecretLinks(store.System(ctx), store.Time{Time: now})
}

func newSecretID() string {
	// 160 random bits, lowercase: not guessable, safe in URLs.
	return randomToken("")[:32]
}
