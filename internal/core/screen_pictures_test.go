package core

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"path/filepath"
	"testing"

	"github.com/phoedox/phoedox/internal/blob"
	"github.com/phoedox/phoedox/internal/store"
)

func TestScreenPictureNameMatchesTheBrowser(t *testing.T) {
	e := WBElement{ID: "screen-Home", W: 390.5, H: 844, Screen: &WBScreen{HTML: "<p>Hi</p>"}}
	// web/src/wb-html.ts screenPictureName computes the same.
	if got := screenPictureName(e, true); got != "screen-2ff2268a-f1937f08727c1567-dark.png" {
		t.Fatalf("name %s", got)
	}
}

func TestScreenPictures(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, _ := setup(t)
	dir := t.TempDir()
	s.Blobs, s.TempDir = blob.Local{Dir: filepath.Join(dir, "files")}, filepath.Join(dir, "tmp")
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Pics"})
	_ = s.SetMember(ctx, owner, p.Slug, reader.User.Email, store.RoleReader)
	res, err := s.DrawScreens(ctx, owner, p.Slug, "", "", "", ScreensInput{Prototype: "App", Screens: []ScreenInput{{Name: "Home", HTML: "<h1>Hi</h1>"}}})
	if err != nil {
		t.Fatal(err)
	}
	doc := res.Document
	frame := res.Screens["Home"]
	img := image.NewRGBA(image.Rect(0, 0, 39, 84))
	img.Set(1, 1, color.RGBA{200, 60, 10, 255})
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)

	bp, _ := s.WhiteboardForPicture(ctx, owner, p.Slug, doc.ID)
	if m := bp.Missing("", false); len(m) != 1 {
		t.Fatalf("missing before: %v", m)
	}
	if err := s.SetScreenPicture(ctx, reader, p.Slug, doc.ID, frame, "light", buf.Bytes()); err == nil {
		t.Fatal("a reader stored a picture")
	}
	if err := s.SetScreenPicture(ctx, owner, p.Slug, doc.ID, frame, "light", []byte("nope")); err == nil {
		t.Fatal("not a PNG accepted")
	}
	if err := s.SetScreenPicture(ctx, owner, p.Slug, doc.ID, frame, "light", buf.Bytes()); err != nil {
		t.Fatal(err)
	}
	names, _ := s.ScreenPictures(ctx, owner, p.Slug, doc.ID)
	if len(names) != 1 {
		t.Fatalf("pictures: %v", names)
	}
	bp, _ = s.WhiteboardForPicture(ctx, owner, p.Slug, doc.ID)
	var home WBElement
	for _, e := range bp.Board.Elements {
		if e.ID == frame {
			home = e
		}
	}
	if got, ok := bp.Screen(home, false); !ok || got.Bounds().Dx() != 39 {
		t.Fatalf("loaded: %v %v", ok, got)
	}
	if _, ok := bp.Screen(home, true); !ok {
		t.Fatal("no light picture in place of a missing dark one")
	}
	if m := bp.Missing("Home", false); len(m) != 0 {
		t.Fatalf("missing after: %v", m)
	}
	// A changed screen needs a new picture; the old one is replaced.
	if _, err := s.DrawScreens(ctx, owner, p.Slug, doc.ID, "", "", ScreensInput{Prototype: "App", Screens: []ScreenInput{{Name: "Home", HTML: "<h1>Hello</h1>"}}}); err != nil {
		t.Fatal(err)
	}
	if names, _ := s.ScreenPictures(ctx, owner, p.Slug, doc.ID); len(names) != 0 {
		t.Fatalf("stale picture listed: %v", names)
	}
	if err := s.SetScreenPicture(ctx, owner, p.Slug, doc.ID, frame, "light", buf.Bytes()); err != nil {
		t.Fatal(err)
	}
	all := s.screenPictureFiles(ctx, p.ID, doc.ID)
	if len(all) != 1 {
		t.Fatalf("files: %d", len(all))
	}
}
