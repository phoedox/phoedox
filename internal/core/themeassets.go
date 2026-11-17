package core

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"regexp"
	"strings"

	"github.com/phoedox/phoedox/internal/store"
)

// Theme assets: woff2 fonts (with a licence note in the theme) and logos
// (PNG, JPEG, WebP). They are stored by content hash and served publicly
// under /api/v1/theme-assets/<sha256>.<ext>, because themes also style
// sign-in pages, publications and e-mails.

const (
	maxThemeFont = 2 << 20
	maxThemeLogo = 512 << 10
)

// ThemeAssetInfo is an uploaded asset.
type ThemeAssetInfo struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Kind   string `json:"kind"`
	Mime   string `json:"mime"`
}

var assetExt = map[string]string{"font/woff2": "woff2", "image/png": "png", "image/jpeg": "jpg", "image/webp": "webp"}

// sniffThemeAsset checks the file type by its first bytes.
func sniffThemeAsset(kind string, data []byte) (string, bool) {
	switch kind {
	case "font":
		return "font/woff2", bytes.HasPrefix(data, []byte("wOF2"))
	case "logo":
		switch {
		case bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")):
			return "image/png", true
		case bytes.HasPrefix(data, []byte{0xff, 0xd8, 0xff}):
			return "image/jpeg", true
		case len(data) > 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP":
			return "image/webp", true
		}
	}
	return "", false
}

// UploadThemeAsset stores a font or logo for themes.
func (s *Service) UploadThemeAsset(ctx context.Context, a Actor, kind, name string, data []byte) (ThemeAssetInfo, error) {
	if err := s.fullUser(a); err != nil {
		return ThemeAssetInfo{}, err
	}
	limit := maxThemeLogo
	if kind == "font" {
		limit = maxThemeFont
	}
	mime, ok := sniffThemeAsset(kind, data)
	if !ok {
		return ThemeAssetInfo{}, invalid("validation.theme_asset_type", nil, "fonts must be woff2, logos PNG, JPEG or WebP")
	}
	if len(data) > limit {
		return ThemeAssetInfo{}, invalid("validation.theme_asset_size", map[string]any{"kb": limit >> 10}, "the file is larger than %d KB", limit>>10)
	}
	sum := sha256.Sum256(data)
	sha := hex.EncodeToString(sum[:])
	if err := s.putBlob(ctx, sha, data); err != nil {
		return ThemeAssetInfo{}, err
	}
	if err := s.DB.AddThemeAsset(ctx, store.ThemeAsset{SHA256: sha, Kind: kind, Mime: mime, Name: excerpt(strings.TrimSpace(name), 120), Size: int64(len(data))}, a.User.ID); err != nil {
		return ThemeAssetInfo{}, err
	}
	s.event(ctx, a, "theme.asset", sha, map[string]any{"kind": kind, "size": len(data)})
	return ThemeAssetInfo{URL: "/api/v1/theme-assets/" + sha + "." + assetExt[mime], SHA256: sha, Kind: kind, Mime: mime}, nil
}

var assetFileRe = regexp.MustCompile(`^([0-9a-f]{64})\.(woff2|png|jpg|webp)$`)

// ThemeAsset returns an asset's content (no sign-in needed).
func (s *Service) ThemeAsset(ctx context.Context, file string) ([]byte, string, error) {
	m := assetFileRe.FindStringSubmatch(file)
	if m == nil {
		return nil, "", ErrNotFound
	}
	rec, err := s.DB.GetThemeAsset(ctx, m[1])
	if err != nil || assetExt[rec.Mime] != m[2] {
		return nil, "", ErrNotFound
	}
	r, err := s.Blobs.Open(ctx, rec.SHA256)
	if err != nil {
		return nil, "", ErrNotFound
	}
	defer r.Close()
	data, err := io.ReadAll(io.LimitReader(r, maxThemeFont+1))
	return data, rec.Mime, err
}
