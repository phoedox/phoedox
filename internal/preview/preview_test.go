package preview

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// SamplePDF builds a one-page PDF with the given text (Helvetica, large).
func SamplePDF(text string) []byte {
	stream := fmt.Sprintf("BT /F1 28 Tf 40 700 Td (%s) Tj ET", text)
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objs))
	for i, o := range objs {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return b.Bytes()
}

func write(t *testing.T, name string, b []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestImages(t *testing.T) {
	ctx := context.Background()
	tools := Detect(false, "")
	img := image.NewRGBA(image.Rect(0, 0, 1200, 600))
	for x := range 1200 {
		for y := range 600 {
			img.Set(x, y, color.RGBA{uint8(x / 5), uint8(y / 3), 80, 255})
		}
	}
	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, img, nil)
	thumb, mime, err := tools.Thumbnail(ctx, write(t, "a.jpg", buf.Bytes()), "image/jpeg")
	if err != nil || mime != "image/jpeg" {
		t.Fatalf("jpeg: %v %s", err, mime)
	}
	cfg, _, _ := image.DecodeConfig(bytes.NewReader(thumb))
	if cfg.Width != 480 || cfg.Height != 240 {
		t.Fatalf("size %dx%d", cfg.Width, cfg.Height)
	}
	img.Set(0, 0, color.RGBA{0, 0, 0, 0}) // transparency keeps PNG
	buf.Reset()
	_ = png.Encode(&buf, img)
	if _, mime, _ := tools.Thumbnail(ctx, write(t, "a.png", buf.Bytes()), "image/png"); mime != "image/png" {
		t.Fatalf("png with alpha: %s", mime)
	}
	if _, _, err := tools.Thumbnail(ctx, write(t, "x.bin", []byte("x")), "application/zip"); err != ErrUnsupported {
		t.Fatalf("zip: %v", err)
	}
	tools.MaxPixels = 100
	if _, _, err := tools.Thumbnail(ctx, write(t, "a.png", buf.Bytes()), "image/png"); err != ErrUnsupported {
		t.Fatalf("pixel limit: %v", err)
	}
	txt, err := tools.Text(ctx, write(t, "n.md", []byte("  # Notizen\nRouter im Flur ")), "text/markdown; charset=utf-8")
	if err != nil || txt != "# Notizen\nRouter im Flur" {
		t.Fatalf("text: %v %q", err, txt)
	}
}

func TestPDFAndOCR(t *testing.T) {
	ctx := context.Background()
	tools := Detect(true, "eng")
	if !tools.Available()["pdf"] {
		t.Skip("poppler not installed")
	}
	pdf := write(t, "a.pdf", SamplePDF("Phoedox Handbuch Router"))
	text, err := tools.Text(ctx, pdf, "application/pdf")
	if err != nil || !strings.Contains(text, "Phoedox Handbuch Router") {
		t.Fatalf("pdf text: %v %q", err, text)
	}
	thumb, mime, err := tools.Thumbnail(ctx, pdf, "application/pdf")
	if err != nil || mime != "image/jpeg" || len(thumb) < 500 {
		t.Fatalf("pdf preview: %v %s %d", err, mime, len(thumb))
	}
	if !tools.Available()["ocr"] {
		t.Skip("tesseract not installed")
	}
	// OCR: a rendered page as image
	out := filepath.Join(t.TempDir(), "page")
	if _, err := tools.run(ctx, tools.PDFToPPM, "-r", "150", "-png", "-singlefile", pdf, out); err != nil {
		t.Fatal(err)
	}
	ocr, err := tools.Text(ctx, out+".png", "image/png")
	if err != nil || !strings.Contains(strings.ToLower(ocr), "handbuch") {
		t.Fatalf("ocr: %v %q", err, ocr)
	}
}
