package plugins

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"slices"
	"strings"
)

// ManifestFile is the name of the manifest at the root of a package.
const ManifestFile = "phoedox-plugin.json"

// Limits of a package.
const (
	MaxPackage  = 32 << 20 // compressed
	maxUnpacked = 96 << 20
	maxFiles    = 1000
)

// Package is a plugin package (.phxp): a tar.gz with the manifest at the
// root, the backend WASM, the frontend module and schemas.
type Package struct {
	Manifest Manifest
	Files    map[string][]byte
	SHA256   string // of the compressed package
	Size     int64
}

// Wasm returns the backend module (nil without backend).
func (p *Package) Wasm() []byte {
	if p.Manifest.Backend == nil {
		return nil
	}
	return p.Files[clean(p.Manifest.Backend.Wasm)]
}

// File returns a file of the package by its path.
func (p *Package) File(name string) ([]byte, bool) {
	b, ok := p.Files[clean(name)]
	return b, ok
}

func clean(name string) string {
	return strings.TrimPrefix(path.Clean("/"+name), "/")
}

// ErrPackage marks a malformed package.
var ErrPackage = errors.New("invalid plugin package")

func bad(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrPackage, fmt.Sprintf(format, args...))
}

// ReadPackage reads and checks a package. Nothing in it is executed.
func ReadPackage(data []byte) (*Package, error) {
	if len(data) > MaxPackage {
		return nil, bad("larger than %d MB", MaxPackage>>20)
	}
	sum := sha256.Sum256(data)
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, bad("not gzip: %v", err)
	}
	tr := tar.NewReader(zr)
	p := &Package{Files: map[string][]byte{}, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(data))}
	var total int64
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, bad("tar: %v", err)
		}
		switch h.Typeflag {
		case tar.TypeDir:
			continue
		case tar.TypeReg:
		default:
			return nil, bad("%s: only regular files are allowed", h.Name)
		}
		name := strings.TrimPrefix(h.Name, "./")
		if name == "" || strings.HasPrefix(name, "/") || strings.Contains(name, "\\") || clean(name) != name {
			return nil, bad("unsafe path %q", h.Name)
		}
		if _, dup := p.Files[name]; dup {
			return nil, bad("duplicate file %s", name)
		}
		if len(p.Files) >= maxFiles {
			return nil, bad("more than %d files", maxFiles)
		}
		total += h.Size
		if h.Size < 0 || total > maxUnpacked {
			return nil, bad("unpacked larger than %d MB", maxUnpacked>>20)
		}
		b, err := io.ReadAll(io.LimitReader(tr, h.Size+1))
		if err != nil || int64(len(b)) != h.Size {
			return nil, bad("%s: truncated", name)
		}
		p.Files[name] = b
	}
	raw, ok := p.Files[ManifestFile]
	if !ok {
		return nil, bad("%s is missing", ManifestFile)
	}
	if p.Manifest, err = ParseManifest(raw); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPackage, err)
	}
	m := p.Manifest
	if m.Backend != nil {
		if w, ok := p.File(m.Backend.Wasm); !ok || !bytes.HasPrefix(w, []byte("\x00asm")) {
			return nil, bad("backend %s is missing or not WebAssembly", m.Backend.Wasm)
		}
	}
	if m.Frontend != nil {
		if !strings.HasSuffix(m.Frontend.Module, ".js") {
			return nil, bad("frontend module must be a .js file")
		}
		if _, ok := p.File(m.Frontend.Module); !ok {
			return nil, bad("frontend module %s is missing", m.Frontend.Module)
		}
	}
	for _, k := range m.Contributes.Kinds {
		if k.Schema == "" {
			continue
		}
		b, ok := p.File(k.Schema)
		if !ok || !json.Valid(b) {
			return nil, bad("schema %s of kind %s is missing or not JSON", k.Schema, k.ID)
		}
	}
	for _, t := range m.Contributes.MCPTools {
		if t.Input == "" {
			continue
		}
		if b, ok := p.File(t.Input); !ok || !json.Valid(b) {
			return nil, bad("input schema %s of tool %s is missing or not JSON", t.Input, t.Name)
		}
	}
	return p, nil
}

// BuildPackage writes files into a package (for tests and `phx plugin pack`).
func BuildPackage(files map[string][]byte) ([]byte, error) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	slices.Sort(names)
	for _, n := range names {
		if err := tw.WriteHeader(&tar.Header{Name: n, Mode: 0o644, Size: int64(len(files[n])), Typeflag: tar.TypeReg}); err != nil {
			return nil, err
		}
		if _, err := tw.Write(files[n]); err != nil {
			return nil, err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Signature is the store's (or an author's) signature of a package.
type Signature struct {
	KeyID string `json:"keyId"`
	Sig   string `json:"sig"` // base64 ed25519
}

// SignedMessage is what a package signature covers. The store signs the same
// bytes (phoedox-store: internal/sign).
func SignedMessage(id, version, sha256hex string) []byte {
	return []byte("phoedox-plugin-v1\n" + id + "\n" + version + "\n" + sha256hex)
}

// Sign signs a package (used by tests; the store signs real packages).
func Sign(key ed25519.PrivateKey, keyID, id, version, sha256hex string) Signature {
	return Signature{KeyID: keyID, Sig: base64.StdEncoding.EncodeToString(ed25519.Sign(key, SignedMessage(id, version, sha256hex)))}
}

// ErrSignature means the signature is missing, unknown or wrong.
var ErrSignature = errors.New("plugin signature is not valid")

// Verify checks a signature against the trusted keys.
func Verify(keys map[string]ed25519.PublicKey, id, version, sha256hex string, s Signature) error {
	pub, ok := keys[s.KeyID]
	if !ok {
		return fmt.Errorf("%w: unknown key %q", ErrSignature, s.KeyID)
	}
	sig, err := base64.StdEncoding.DecodeString(s.Sig)
	if err != nil || !ed25519.Verify(pub, SignedMessage(id, version, sha256hex), sig) {
		return ErrSignature
	}
	return nil
}

// ParseKeys reads trusted public keys "keyid:base64,keyid2:base64".
func ParseKeys(s string) (map[string]ed25519.PublicKey, error) {
	keys := map[string]ed25519.PublicKey{}
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, b64, ok := strings.Cut(part, ":")
		raw, err := base64.StdEncoding.DecodeString(b64)
		if !ok || id == "" || err != nil || len(raw) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("store key %q: want keyid:base64(ed25519 public key)", part)
		}
		keys[id] = ed25519.PublicKey(raw)
	}
	return keys, nil
}
