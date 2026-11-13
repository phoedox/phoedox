package core

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"testing"

	"github.com/phoedox/phoedox/internal/store"
)

func pngOf(w, h int) []byte {
	var b bytes.Buffer
	_ = png.Encode(&b, image.NewRGBA(image.Rect(0, 0, w, h)))
	return b.Bytes()
}

func TestImages(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, _ := setup(t)
	v, err := s.SetAvatar(ctx, owner, pngOf(64, 64))
	if err != nil || v == "" {
		t.Fatalf("avatar: %v", err)
	}
	if u, _ := s.DB.UserByID(ctx, owner.User.ID); u.Avatar != v {
		t.Fatalf("version: %q", u.Avatar)
	}
	if img, err := s.UserImage(ctx, reader, owner.User.ID); err != nil || img.Mime != "image/png" {
		t.Fatalf("read: %v %s", err, img.Mime)
	}
	if _, err := s.SetAvatar(ctx, owner, []byte("<svg onload=alert(1)>")); invalidKey(err) != "validation.image" {
		t.Fatalf("no svg: %v", err)
	}
	if _, err := s.SetAvatar(ctx, owner, pngOf(2000, 10)); invalidKey(err) != "validation.image" {
		t.Fatalf("too large: %v", err)
	}
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Bild"})
	if _, err := s.SetProjectImage(ctx, reader, p.Slug, pngOf(8, 8)); err == nil {
		t.Fatal("only owners set project images")
	}
	if _, err := s.SetProjectImage(ctx, owner, p.Slug, pngOf(8, 8)); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Project(ctx, owner, p.Slug); got.Image == "" {
		t.Fatal("project image version")
	}
	if err := s.DeleteAvatar(ctx, owner); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UserImage(ctx, owner, owner.User.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("deleted: %v", err)
	}
}
