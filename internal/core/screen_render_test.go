package core

import (
	"bytes"
	"context"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/phoedox/phoedox/internal/blob"
	"github.com/phoedox/phoedox/internal/render"
	"github.com/phoedox/phoedox/internal/store"
)

func TestScreenHTMLDoc(t *testing.T) {
	doc := screenHTMLDoc(`<p data-show="on">Hi</p>`, true, []WBVar{{Name: "on", Type: "bool", Value: []byte("true")}})
	if !strings.Contains(doc, `data-theme="dark"`) || !strings.Contains(doc, `{"on":true}`) || !strings.Contains(doc, "--surface") {
		t.Fatalf("doc: %.300s", doc)
	}
	full := screenHTMLDoc(`<!doctype html><html lang="en"><head><title>x</title></head><body><p>x</p></body></html>`, false, nil)
	if !strings.Contains(full, `<html data-theme="light" lang="en">`) || strings.Index(full, "--surface") > strings.Index(full, "<title>") || !strings.Contains(full, "</script></body>") {
		t.Fatalf("full doc: %s", full)
	}
}

func TestRendererSettingsAndScreens(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, _ := setup(t)
	admin := owner
	admin.User.IsAdmin = true
	// Without a service, it cannot be switched on.
	s.Renderer = render.New("", 1)
	if _, err := s.SetAdminRenderer(ctx, admin, RendererSettings{Enabled: true}); err == nil {
		t.Fatal("switched on without a service")
	}
	if _, err := s.AdminRenderer(ctx, reader); err == nil {
		t.Fatal("a non-admin read the renderer settings")
	}
	u := os.Getenv("PHOEDOX_TEST_RENDERER")
	if u == "" {
		t.Skip("PHOEDOX_TEST_RENDERER not set")
	}
	s.Renderer = render.New(u, 2)
	defer s.Renderer.Close()
	v, err := s.SetAdminRenderer(ctx, admin, RendererSettings{Enabled: true, Off: []string{RenderPDF, "bogus"}})
	if err != nil || !v.Status.Reachable || len(v.Off) != 1 {
		t.Fatalf("settings %+v %v", v, err)
	}
	if s.RendererOn(ctx, RenderPDF) || !s.RendererOn(ctx, RenderScreens) {
		t.Fatal("uses")
	}
	dir := t.TempDir()
	s.Blobs, s.TempDir = blob.Local{Dir: filepath.Join(dir, "files")}, filepath.Join(dir, "tmp")
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Shots"})
	_ = s.SetMember(ctx, owner, p.Slug, reader.User.Email, store.RoleReader)
	res, err := s.DrawScreens(ctx, owner, p.Slug, "", "", "", ScreensInput{Prototype: "App", Device: "phone",
		Vars:    []WBVar{{Name: "n", Type: "number", Value: []byte("7")}},
		Screens: []ScreenInput{{Name: "Home", HTML: `<div style="height:100vh;background:var(--accent)"><b data-text="n">0</b></div>`}}})
	if err != nil {
		t.Fatal(err)
	}
	// A reader asks: the server draws the missing picture.
	n, err := s.MakeScreenPictures(ctx, reader, p.Slug, res.Document.ID, "Home", false)
	if err != nil || n != 1 {
		t.Fatalf("made %d %v", n, err)
	}
	bp, _ := s.WhiteboardForPicture(ctx, reader, p.Slug, res.Document.ID)
	if m := bp.Missing("Home", false); len(m) != 0 {
		t.Fatalf("still missing %v", m)
	}
	var home WBElement
	for _, e := range bp.Board.Elements {
		if e.Screen != nil {
			home = e
		}
	}
	img, ok := bp.Screen(home, false)
	if !ok || img.Bounds().Dx() != 390 || img.Bounds().Dy() != 844 {
		t.Fatalf("picture %v %v", ok, img)
	}
	// The accent colour of the tokens filled the screen.
	r, g, b, _ := img.At(200, 600).RGBA()
	if r>>8 < 150 || b>>8 > 100 {
		t.Fatalf("not the accent: %d,%d,%d", r>>8, g>>8, b>>8)
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	_ = os.WriteFile(filepath.Join(os.TempDir(), "phoedox-screen-test.png"), buf.Bytes(), 0o644)
	// Again: nothing to do.
	if n, _ := s.MakeScreenPictures(ctx, reader, p.Slug, res.Document.ID, "", false); n != 0 {
		t.Fatalf("drew again: %d", n)
	}
}
