package pdftext

import (
	"bytes"
	"strings"
	"testing"
)

func TestRender(t *testing.T) {
	md := "# Auftragsverarbeitungsvertrag\n\nZwischen **Kunde** und Anbieter (Größe: 5 €).\n\n## 1. Gegenstand\n\n" + strings.Repeat("Ein langer Satz über die Verarbeitung personenbezogener Daten. ", 120) +
		"\n\n- erster Punkt\n- zweiter Punkt mit (Klammern) und \\ Backslash\n"
	pdf := Render(md, "AVV", "Acme GmbH · v1")
	if !bytes.HasPrefix(pdf, []byte("%PDF-1.4")) || !bytes.HasSuffix(pdf, []byte("%%EOF\n")) {
		t.Fatal("frame")
	}
	s := string(pdf)
	for _, want := range []string{"/Helvetica-Bold", "/WinAnsiEncoding", "(Auftragsverarbeitungsvertrag)", "Gr\xf6\xdfe: 5 \x80", `\(Klammern\)`, `\\ Backslash`, "/Count 2", "1 / 2"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q", want)
		}
	}
	if strings.Contains(s, "**") {
		t.Fatal("markdown emphasis left")
	}
	// xref offsets point at objects.
	i := strings.LastIndex(s, "startxref\n")
	var off int
	_, _ = fmtSscan(s[i+10:], &off)
	if !strings.HasPrefix(s[off:], "xref") {
		t.Fatal("startxref")
	}
	if lines := wrap("aaaa bbbb cccc", 10, false, width("aaaa bbbb", 10, false)); len(lines) != 2 || lines[0] != "aaaa bbbb" {
		t.Fatalf("wrap: %q", lines)
	}
}

func fmtSscan(s string, n *int) (int, error) {
	v := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		v = v*10 + int(c-'0')
	}
	*n = v
	return 1, nil
}
