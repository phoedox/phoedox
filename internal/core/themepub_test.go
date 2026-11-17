package core

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/phoedox/phoedox/internal/blob"
)

func TestThemeAssetsAndPublications(t *testing.T) {
	ctx := context.Background()
	s, owner, _, _ := setup(t)
	dir := t.TempDir()
	s.Blobs, s.TempDir = blob.Local{Dir: filepath.Join(dir, "files")}, filepath.Join(dir, "tmp")
	_ = os.MkdirAll(s.TempDir, 0o700)
	s.PublicURL = "https://app.example"

	if _, err := s.UploadThemeAsset(ctx, owner, "font", "x.woff2", []byte("not a font")); invalidKey(err) != "validation.theme_asset_type" {
		t.Fatalf("type: %v", err)
	}
	font, err := s.UploadThemeAsset(ctx, owner, "font", "Brand.woff2", append([]byte("wOF2"), make([]byte, 100)...))
	if err != nil || !strings.HasSuffix(font.URL, ".woff2") {
		t.Fatalf("font: %v %+v", err, font)
	}
	logo, err := s.UploadThemeAsset(ctx, owner, "logo", "logo.png", append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 20)...))
	if err != nil {
		t.Fatal(err)
	}
	if data, mime, err := s.ThemeAsset(ctx, strings.TrimPrefix(font.URL, "/api/v1/theme-assets/")); err != nil || mime != "font/woff2" || len(data) != 104 {
		t.Fatalf("serve: %v %s %d", err, mime, len(data))
	}
	if _, _, err := s.ThemeAsset(ctx, strings.Repeat("0", 64)+".png"); err == nil {
		t.Fatal("unknown asset served")
	}

	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Docs"})
	th, err := s.CreateTheme(ctx, owner, ThemeInput{Scope: "project", Owner: p.Slug, Name: "Brand", Data: ThemeData{
		Light: map[string]string{"accent": "#2c6e49"}, Dark: map[string]string{},
		Body: ThemeFont{Family: "Brand Sans", URL: font.URL, License: "OFL"}, Logo: logo.URL, Radius: 6}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetProjectTheme(ctx, owner, p.Slug, th.ID); err != nil {
		t.Fatal(err)
	}
	pub, err := s.CreatePublication(ctx, owner, PublicationInput{Title: "Docs site", Projects: []string{p.Slug}})
	if err != nil {
		t.Fatal(err)
	}
	css, l := s.PubTheme(ctx, pub.Publication)
	if !strings.Contains(css, "--accent:#2c6e49") || !strings.Contains(css, `src:url("https://app.example/api/v1/theme-assets/`) ||
		!strings.Contains(css, "prefers-color-scheme: dark") || !strings.Contains(css, `--font-body:"Brand Sans"`) || l != "https://app.example"+logo.URL {
		t.Fatalf("pub theme: %s | %s", css, l)
	}
}
