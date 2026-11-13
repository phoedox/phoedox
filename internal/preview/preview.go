// Package preview makes preview images and extracts text from uploaded
// files. Images are handled in Go; PDFs and OCR use poppler (pdftoppm,
// pdftotext) and tesseract when they are installed. Every external tool
// runs without a shell, with a timeout and on a copy in a temporary folder.
package preview

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/draw"
	_ "image/gif" // register decoders
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

// ErrUnsupported means no preview or text can be made for this type.
var ErrUnsupported = errors.New("preview: unsupported type")

// Tools holds the found external programs and limits.
type Tools struct {
	PDFToPPM  string // path or "" if missing
	PDFToText string
	Tesseract string
	OCRLangs  string        // e.g. "deu+eng"
	OCR       bool          // run OCR at all
	MaxPages  int           // OCR at most this many PDF pages
	MaxPixels int           // images larger than this are not decoded
	MaxText   int           // bytes of text kept for search
	Size      int           // longest side of preview images
	Timeout   time.Duration // per tool call
}

// Detect looks for the tools on PATH.
func Detect(ocr bool, langs string) Tools {
	look := func(name string) string {
		p, err := exec.LookPath(name)
		if err != nil {
			return ""
		}
		return p
	}
	if langs == "" {
		langs = "deu+eng"
	}
	return Tools{PDFToPPM: look("pdftoppm"), PDFToText: look("pdftotext"), Tesseract: look("tesseract"),
		OCRLangs: langs, OCR: ocr, MaxPages: 20, MaxPixels: 60_000_000, MaxText: 2 << 20, Size: 480, Timeout: 90 * time.Second}
}

// Available lists the found tools (for the admin page and logs).
func (t Tools) Available() map[string]bool {
	return map[string]bool{"pdf": t.PDFToPPM != "" && t.PDFToText != "", "ocr": t.OCR && t.Tesseract != ""}
}

func (t Tools) run(ctx context.Context, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, t.Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = []string{"PATH=/usr/bin:/bin", "OMP_THREAD_LIMIT=1", "LANG=C.UTF-8"}
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%s: %w: %s", filepath.Base(name), err, strings.TrimSpace(stderr.String()))
	}
	return out.Bytes(), nil
}

func isImage(mime string) bool {
	switch mime {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return true
	}
	return false
}

// Thumbnail returns a preview image (JPEG, or PNG with transparency) of the
// file at path, at most Size pixels on the longest side.
func (t Tools) Thumbnail(ctx context.Context, path, mime string) ([]byte, string, error) {
	switch {
	case isImage(mime):
		f, err := os.Open(path)
		if err != nil {
			return nil, "", err
		}
		defer f.Close()
		cfg, _, err := image.DecodeConfig(f)
		if err != nil {
			return nil, "", err
		}
		if cfg.Width*cfg.Height > t.MaxPixels {
			return nil, "", ErrUnsupported
		}
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return nil, "", err
		}
		img, _, err := image.Decode(f)
		if err != nil {
			return nil, "", err
		}
		return t.encode(img)
	case mime == "application/pdf" && t.PDFToPPM != "":
		dir, err := os.MkdirTemp("", "phx-preview-*")
		if err != nil {
			return nil, "", err
		}
		defer os.RemoveAll(dir)
		out := filepath.Join(dir, "page")
		if _, err := t.run(ctx, t.PDFToPPM, "-f", "1", "-l", "1", "-scale-to", fmt.Sprint(t.Size*2), "-png", "-singlefile", path, out); err != nil {
			return nil, "", err
		}
		f, err := os.Open(out + ".png")
		if err != nil {
			return nil, "", err
		}
		defer f.Close()
		img, err := png.Decode(f)
		if err != nil {
			return nil, "", err
		}
		return t.encode(img)
	}
	return nil, "", ErrUnsupported
}

func (t Tools) encode(src image.Image) ([]byte, string, error) {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w > t.Size || h > t.Size {
		if w >= h {
			h, w = max(1, h*t.Size/w), t.Size
		} else {
			w, h = max(1, w*t.Size/h), t.Size
		}
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Src, nil)
	var buf bytes.Buffer
	if !opaque(dst) {
		err := png.Encode(&buf, dst)
		return buf.Bytes(), "image/png", err
	}
	err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 82})
	return buf.Bytes(), "image/jpeg", err
}

func opaque(img *image.RGBA) bool {
	for i := 3; i < len(img.Pix); i += 4 {
		if img.Pix[i] != 0xff {
			return false
		}
	}
	return true
}

func isText(mime string) bool {
	base, _, _ := strings.Cut(mime, ";")
	switch strings.TrimSpace(base) {
	case "text/plain", "text/markdown", "text/csv", "application/json", "text/x-yaml", "application/yaml", "text/xml", "application/xml":
		return true
	}
	return false
}

// Text extracts searchable text. Scanned PDFs and images are read with OCR
// if enabled. The result is cut to MaxText bytes.
func (t Tools) Text(ctx context.Context, path, mime string) (string, error) {
	var text string
	switch {
	case isText(mime):
		f, err := os.Open(path)
		if err != nil {
			return "", err
		}
		defer f.Close()
		b, err := io.ReadAll(io.LimitReader(f, int64(t.MaxText)))
		if err != nil {
			return "", err
		}
		text = string(b)
	case mime == "application/pdf" && t.PDFToText != "":
		out, err := t.run(ctx, t.PDFToText, "-enc", "UTF-8", "-nopgbrk", path, "-")
		if err != nil {
			return "", err
		}
		text = string(out)
		if len(strings.Fields(text)) < 5 && t.ocrReady() && t.PDFToPPM != "" {
			ocr, err := t.ocrPDF(ctx, path)
			if err != nil {
				return "", err
			}
			text = ocr
		}
	case isImage(mime) && t.ocrReady():
		out, err := t.run(ctx, t.Tesseract, path, "-", "-l", t.OCRLangs)
		if err != nil {
			return "", err
		}
		text = string(out)
	default:
		return "", ErrUnsupported
	}
	return clip(strings.TrimSpace(strings.ToValidUTF8(text, "")), t.MaxText), nil
}

func (t Tools) ocrReady() bool { return t.OCR && t.Tesseract != "" }

func (t Tools) ocrPDF(ctx context.Context, path string) (string, error) {
	dir, err := os.MkdirTemp("", "phx-ocr-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	if _, err := t.run(ctx, t.PDFToPPM, "-l", fmt.Sprint(t.MaxPages), "-r", "200", "-gray", "-png", path, filepath.Join(dir, "p")); err != nil {
		return "", err
	}
	pages, _ := filepath.Glob(filepath.Join(dir, "p*.png"))
	sort.Strings(pages)
	var b strings.Builder
	for _, pg := range pages {
		out, err := t.run(ctx, t.Tesseract, pg, "-", "-l", t.OCRLangs)
		if err != nil {
			return "", err
		}
		b.Write(out)
		b.WriteString("\n")
	}
	return b.String(), nil
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}
