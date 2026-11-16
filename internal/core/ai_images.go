package core

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif" // decoders for uploaded pictures
	"image/jpeg"
	"image/png"
	"os"
	"path"
	"strings"

	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"

	"github.com/phoedox/phoedox/internal/store"
)

// Pictures sent as base64 text, for clients that cannot send files, above
// all AI clients over MCP: screenshots or photos on task cards, pictures of
// devices, project logos. Raster images, and SVG after cleaning (scripts,
// event handlers and outside links removed, see svgclean.go).

// MaxInlineImage is the largest decoded picture accepted as base64.
const MaxInlineImage = 8 << 20

var inlineImageTypes = map[string]string{"image/png": ".png", "image/jpeg": ".jpg", "image/gif": ".gif", "image/webp": ".webp", "image/svg+xml": ".svg"}

// ImageUpload is a picture sent as base64 (optionally as a data: URL) and
// where it belongs: a page, an entity, or a task card (the picture is stored
// with the card's board and added to the card's description).
type ImageUpload struct {
	Name     string `json:"name"`
	Data     string `json:"data"`
	Alt      string `json:"alt,omitempty"`
	Document string `json:"document,omitempty"`
	Entity   string `json:"entity,omitempty"`
	Task     string `json:"task,omitempty"`
}

// ImageResult is the stored picture with the address and Markdown to use it.
type ImageResult struct {
	File     store.File  `json:"file"`
	URL      string      `json:"url"`
	Markdown string      `json:"markdown"`
	Task     *store.Task `json:"task,omitempty"`
}

// decodeBase64 accepts plain or URL-safe base64, with or without padding,
// line breaks or a data: prefix.
func decodeBase64(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "data:") {
		if i := strings.Index(s, ","); i >= 0 {
			s = s[i+1:]
		}
	}
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == ' ' || r == '\t' {
			return -1
		}
		return r
	}, s)
	if len(s) > MaxInlineImage/3*4+8 {
		return nil, invalid("validation.image_too_large", map[string]any{"max": MaxInlineImage >> 20}, "the picture is larger than %d MB", MaxInlineImage>>20)
	}
	s = strings.TrimRight(s, "=")
	for _, enc := range []*base64.Encoding{base64.RawStdEncoding, base64.RawURLEncoding} {
		if b, err := enc.DecodeString(s); err == nil {
			return b, nil
		}
	}
	return nil, invalid("validation.base64", nil, "data is not valid base64")
}

// inlineImage decodes and checks a base64 picture and returns bytes and type.
func inlineImage(data string) ([]byte, string, error) {
	b, err := decodeBase64(data)
	if err != nil {
		return nil, "", err
	}
	if len(b) == 0 {
		return nil, "", invalid("validation.file_empty", nil, "the file is empty")
	}
	if len(b) > MaxInlineImage {
		return nil, "", invalid("validation.image_too_large", map[string]any{"max": MaxInlineImage >> 20}, "the picture is larger than %d MB", MaxInlineImage>>20)
	}
	if looksSVG(b) {
		clean, err := cleanSVG(b)
		if err != nil {
			return nil, "", invalid("validation.image_type", nil, "the SVG picture cannot be read")
		}
		return clean, "image/svg+xml", nil
	}
	mime := sniffMime("", b)
	if _, ok := inlineImageTypes[mime]; !ok {
		return nil, "", invalid("validation.image_type", nil, "only PNG, JPEG, GIF, WebP or SVG pictures can be uploaded this way")
	}
	if _, _, err := image.DecodeConfig(bytes.NewReader(b)); err != nil {
		return nil, "", invalid("validation.image_type", nil, "the picture cannot be read")
	}
	return b, mime, nil
}

// aiMayUpload: AI clients store files only where the project lets AI write directly.
func aiMayUpload(a Actor, p store.Project) error {
	if a.Via == "mcp" && p.AIWriteMode != AIDirect {
		return invalid("validation.ai_upload_direct", nil, "uploading pictures needs the project's AI write mode \"direct\" (an owner sets it)")
	}
	return nil
}

// UploadImage stores a base64 picture in a project (editors; AI clients only
// with direct AI writes) and attaches it to a page, an entity or a task card.
func (s *Service) UploadImage(ctx context.Context, a Actor, ref string, in ImageUpload) (ImageResult, error) {
	p, err := s.project(ctx, a, ref, store.RoleEditor)
	if err != nil {
		return ImageResult{}, err
	}
	if err := aiMayUpload(a, p); err != nil {
		return ImageResult{}, err
	}
	if s.Blobs == nil {
		return ImageResult{}, errNoBlobs
	}
	data, mime, err := inlineImage(in.Data)
	if err != nil {
		return ImageResult{}, err
	}
	var task *store.Task
	doc := in.Document
	if in.Task != "" {
		t, err := s.DB.GetTask(ctx, p.ID, in.Task)
		if err != nil {
			return ImageResult{}, err
		}
		task, doc = &t, t.BoardID
	}
	docID, entID, err := s.attachTargets(ctx, p, doc, in.Entity)
	if err != nil {
		return ImageResult{}, err
	}
	name := cleanName(in.Name)
	if in.Name == "" {
		name = "image"
	}
	if ext := inlineImageTypes[mime]; !strings.EqualFold(path.Ext(name), ext) && !(ext == ".jpg" && strings.EqualFold(path.Ext(name), ".jpeg")) {
		name = strings.TrimSuffix(name, path.Ext(name)) + ext
	}
	sp, err := s.spool(bytes.NewReader(data))
	if err != nil {
		return ImageResult{}, err
	}
	defer os.Remove(sp.path)
	f, err := s.ingest(ctx, a, p, sp, name, docID, entID, nil)
	if err != nil {
		return ImageResult{}, err
	}
	alt := strings.NewReplacer("[", "", "]", "").Replace(strings.TrimSpace(in.Alt))
	if alt == "" {
		alt = strings.TrimSuffix(f.Name, path.Ext(f.Name))
	}
	res := ImageResult{File: f, URL: "/api/v1/files/" + f.ID, Markdown: "![" + alt + "](/api/v1/files/" + f.ID + ")"}
	if task != nil {
		desc := strings.TrimRight(task.Description, "\n")
		if desc != "" {
			desc += "\n\n"
		}
		desc += res.Markdown
		t, err := s.UpdateTask(ctx, a, ref, task.ID, TaskPatch{Description: &desc})
		if err != nil {
			return res, err
		}
		res.Task = &t
	}
	return res, nil
}

// SetProjectImageBase64 sets a project's picture (logo) from base64; larger
// pictures are scaled down to 512 px, as the browser does.
func (s *Service) SetProjectImageBase64(ctx context.Context, a Actor, ref, data string) (string, error) {
	p, err := s.project(ctx, a, ref, store.RoleOwner)
	if err != nil {
		return "", err
	}
	if err := aiMayUpload(a, p); err != nil {
		return "", err
	}
	b, _, err := inlineImage(data)
	if err != nil {
		return "", err
	}
	if b, err = fitPicture(b, 512); err != nil {
		return "", err
	}
	return s.SetProjectImage(ctx, a, ref, b)
}

// fitPicture scales a picture to at most px on its longer side and encodes it
// as PNG (with transparency) or JPEG, within the size limit for pictures.
func fitPicture(data []byte, px int) ([]byte, error) {
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, invalid("validation.image_type", nil, "the picture cannot be read")
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w > px || h > px {
		if w >= h {
			h, w = max(1, h*px/w), px
		} else {
			w, h = max(1, w*px/h), px
		}
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Src, nil)
	var buf bytes.Buffer
	if !isOpaque(dst) {
		if err := png.Encode(&buf, dst); err == nil && buf.Len() <= maxImageBytes {
			return buf.Bytes(), nil
		}
		// Too large as PNG: flatten onto white.
		flat := image.NewRGBA(dst.Bounds())
		draw.Draw(flat, flat.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
		draw.Draw(flat, flat.Bounds(), dst, image.Point{}, draw.Over)
		dst = flat
		buf.Reset()
	}
	err = jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 85})
	return buf.Bytes(), err
}

func isOpaque(img *image.RGBA) bool {
	for i := 3; i < len(img.Pix); i += 4 {
		if img.Pix[i] != 0xff {
			return false
		}
	}
	return true
}
