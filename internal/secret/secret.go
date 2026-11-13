// Package secret encrypts small secrets at rest (TOTP seeds, project
// environment secrets) with AES-256-GCM under an instance master key.
//
// The key comes from PHOEDOX_MASTER_KEY (32 bytes, base64) or, if unset,
// from <data dir>/master.key, which is created on first start. Several
// server replicas must share the same key. Ciphertexts carry the key id so
// keys can be rotated later.
package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// ErrDecrypt is returned for tampered data or an unknown key.
var ErrDecrypt = errors.New("secret: cannot decrypt")

// Box seals and opens secrets.
type Box struct {
	keys    map[string]cipher.AEAD
	current string
}

// New builds a box from a 32-byte key.
func New(key []byte) (*Box, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("secret: master key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(key)
	kid := hex.EncodeToString(sum[:4])
	return &Box{keys: map[string]cipher.AEAD{kid: aead}, current: kid}, nil
}

// Load reads the key from envKey (base64) or from keyFile, creating the
// file with a random key if neither exists.
func Load(envKey, keyFile string) (*Box, error) {
	if envKey != "" {
		key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(envKey))
		if err != nil {
			return nil, fmt.Errorf("PHOEDOX_MASTER_KEY: %w", err)
		}
		return New(key)
	}
	if b, err := os.ReadFile(keyFile); err == nil {
		key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(b)))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", keyFile, err)
		}
		return New(key)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(keyFile), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(keyFile, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create master key: %w", err)
	}
	defer f.Close()
	if _, err := f.WriteString(base64.StdEncoding.EncodeToString(key) + "\n"); err != nil {
		return nil, err
	}
	slog.Warn("created a new master key – back it up together with the database", "file", keyFile)
	return New(key)
}

// Seal encrypts plain; context binds the ciphertext to a purpose (e.g.
// "totp:<user id>") so it cannot be moved to another record.
func (b *Box) Seal(plain []byte, context string) (string, error) {
	aead := b.keys[b.current]
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	ct := aead.Seal(nonce, nonce, plain, []byte(context))
	return "v1:" + b.current + ":" + base64.RawStdEncoding.EncodeToString(ct), nil
}

// Open decrypts a value from Seal with the same context.
func (b *Box) Open(sealed, context string) ([]byte, error) {
	parts := strings.SplitN(sealed, ":", 3)
	if len(parts) != 3 || parts[0] != "v1" {
		return nil, ErrDecrypt
	}
	aead, ok := b.keys[parts[1]]
	if !ok {
		return nil, ErrDecrypt
	}
	raw, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil || len(raw) < aead.NonceSize() {
		return nil, ErrDecrypt
	}
	plain, err := aead.Open(nil, raw[:aead.NonceSize()], raw[aead.NonceSize():], []byte(context))
	if err != nil {
		return nil, ErrDecrypt
	}
	return plain, nil
}
