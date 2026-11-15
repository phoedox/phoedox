package core

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"path/filepath"
	"strings"
	"testing"

	"github.com/phoedox/phoedox/internal/blob"
	"github.com/phoedox/phoedox/internal/store"
)

func pngBase64(t *testing.T, w, h int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		img.Set(x, x%h, color.RGBA{R: 200, A: 255})
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}

func TestUploadImageBase64(t *testing.T) {
	ctx := context.Background()
	s, owner, editor, reader := setup(t)
	dir := t.TempDir()
	s.Blobs, s.TempDir = blob.Local{Dir: filepath.Join(dir, "files")}, filepath.Join(dir, "tmp")
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Haus", AIWriteMode: AISuggest})
	_ = s.SetMember(ctx, owner, p.Slug, editor.User.Email, store.RoleEditor)
	_ = s.SetMember(ctx, owner, p.Slug, reader.User.Email, store.RoleReader)
	board, _ := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "board", Title: "Aufgaben"})
	task, err := s.CreateTask(ctx, owner, p.Slug, board.Document.Slug, TaskInput{Title: "Rack aufräumen", Description: "Vorher:"})
	if err != nil {
		t.Fatal(err)
	}
	ai := Actor{User: owner.User, Via: "mcp", TokenID: "t"}
	data := pngBase64(t, 40, 30)

	// AI clients upload only where the project lets AI write directly.
	if _, err := s.UploadImage(ctx, ai, p.Slug, ImageUpload{Data: data, Task: task.ID}); invalidKey(err) != "validation.ai_upload_direct" {
		t.Fatalf("suggest mode: %v", err)
	}
	if _, err := s.UploadImage(ctx, reader, p.Slug, ImageUpload{Data: data}); err == nil {
		t.Fatal("readers must not upload")
	}
	_, _ = s.UpdateProject(ctx, owner, p.Slug, ProjectInput{Name: "Haus", AIWriteMode: AIDirect})

	// To a task card: stored with the board, added to the description.
	r, err := s.UploadImage(ctx, ai, p.Slug, ImageUpload{Name: "rack", Data: "data:image/png;base64," + data, Task: task.ID, Alt: "Rack [vorne]"})
	if err != nil {
		t.Fatal(err)
	}
	if r.File.DocumentID != board.Document.ID || r.File.Mime != "image/png" || r.File.Name != "rack.png" {
		t.Fatalf("file: %+v", r.File)
	}
	if r.Task == nil || r.Task.Description != "Vorher:\n\n![Rack vorne](/api/v1/files/"+r.File.ID+")" {
		t.Fatalf("task: %+v", r.Task)
	}

	// To an entity, with line breaks and URL-safe base64 without padding.
	nas, _ := s.UpsertEntity(ctx, editor, p.Slug, EntityInput{Kind: "server", Name: "NAS"})
	raw, _ := base64.StdEncoding.DecodeString(data)
	urlSafe := base64.RawURLEncoding.EncodeToString(raw)
	if r, err := s.UploadImage(ctx, editor, p.Slug, ImageUpload{Name: "nas.jpg", Data: urlSafe[:20] + "\n" + urlSafe[20:], Entity: nas.Entity.Slug}); err != nil || r.File.EntityID != nas.Entity.ID || r.File.Name != "nas.png" {
		t.Fatalf("entity: %+v %v", r.File, err)
	}

	// No SVG, no other files, no broken base64.
	svg := base64.StdEncoding.EncodeToString([]byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`))
	if _, err := s.UploadImage(ctx, ai, p.Slug, ImageUpload{Data: svg}); invalidKey(err) != "validation.image_type" {
		t.Fatalf("svg: %v", err)
	}
	if _, err := s.UploadImage(ctx, ai, p.Slug, ImageUpload{Data: "%%%"}); invalidKey(err) != "validation.base64" {
		t.Fatalf("base64: %v", err)
	}

	// Project logo: owners only, scaled to 512 px.
	if _, err := s.SetProjectImageBase64(ctx, editor, p.Slug, data); err == nil {
		t.Fatal("editors must not set the project picture")
	}
	if _, err := s.SetProjectImageBase64(ctx, ai, p.Slug, pngBase64(t, 2000, 900)); err != nil {
		t.Fatal(err)
	}
	img, err := s.ProjectImage(ctx, owner, p.Slug)
	if err != nil {
		t.Fatal(err)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(img.Data))
	if err != nil || cfg.Width != 512 || cfg.Height != 230 {
		t.Fatalf("scaled logo: %+v %v", cfg, err)
	}
	if !strings.HasPrefix(img.Mime, "image/") {
		t.Fatalf("mime %s", img.Mime)
	}
}
