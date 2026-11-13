package core

import (
	"bytes"
	"context"
	"image"
	_ "image/jpeg" // decoders for validation
	_ "image/png"

	"github.com/phoedox/phoedox/internal/store"
)

// Profile pictures and project images. The browser resizes them to at most
// 512 px; the server only checks type, size and dimensions.

const maxImageBytes = 512 << 10

// imageMime checks the bytes (not the declared type) and returns the type.
func imageMime(data []byte) (string, error) {
	bad := invalid("validation.image", nil, "the picture must be a PNG, JPEG or WebP of at most 512 KB and 1024×1024 px")
	if len(data) == 0 || len(data) > maxImageBytes {
		return "", bad
	}
	if len(data) > 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP" {
		return "image/webp", nil
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width > 1024 || cfg.Height > 1024 || (format != "png" && format != "jpeg") {
		return "", bad
	}
	return "image/" + format, nil
}

// SetAvatar stores the actor's profile picture.
func (s *Service) SetAvatar(ctx context.Context, a Actor, data []byte) (string, error) {
	if a.Via == "mcp" {
		return "", ErrForbidden
	}
	mime, err := imageMime(data)
	if err != nil {
		return "", err
	}
	v, err := s.DB.SetImage(ctx, store.ImageUser, a.User.ID, mime, data)
	if err == nil {
		s.event(ctx, a, "user.avatar", a.User.ID, map[string]string{"version": v})
	}
	return v, err
}

// DeleteAvatar removes the actor's profile picture.
func (s *Service) DeleteAvatar(ctx context.Context, a Actor) error {
	if err := s.DB.DeleteImage(ctx, store.ImageUser, a.User.ID); err != nil {
		return err
	}
	s.event(ctx, a, "user.avatar_delete", a.User.ID, nil)
	return nil
}

// SetProjectImage stores the image of a project (owners only).
func (s *Service) SetProjectImage(ctx context.Context, a Actor, ref string, data []byte) (string, error) {
	p, err := s.project(ctx, a, ref, store.RoleOwner)
	if err != nil {
		return "", err
	}
	mime, err := imageMime(data)
	if err != nil {
		return "", err
	}
	v, err := s.DB.SetImage(ctx, store.ImageProject, p.ID, mime, data)
	if err == nil {
		s.event(ctx, a, "project.image", p.ID, map[string]string{"version": v})
	}
	return v, err
}

// DeleteProjectImage removes the image of a project.
func (s *Service) DeleteProjectImage(ctx context.Context, a Actor, ref string) error {
	p, err := s.project(ctx, a, ref, store.RoleOwner)
	if err != nil {
		return err
	}
	if err := s.DB.DeleteImage(ctx, store.ImageProject, p.ID); err != nil {
		return err
	}
	s.event(ctx, a, "project.image_delete", p.ID, nil)
	return nil
}

// UserImage returns a profile picture; every signed-in user of the instance may see it.
func (s *Service) UserImage(ctx context.Context, a Actor, userID string) (store.Image, error) {
	if a.User.ID == "" {
		return store.Image{}, ErrForbidden
	}
	return s.DB.GetImage(ctx, store.ImageUser, userID)
}

// ProjectImage returns a project image to anyone who can see the project.
func (s *Service) ProjectImage(ctx context.Context, a Actor, ref string) (store.Image, error) {
	p, err := s.project(ctx, a, ref, store.RoleDoc)
	if err != nil {
		return store.Image{}, err
	}
	return s.DB.GetImage(ctx, store.ImageProject, p.ID)
}
