// Package pdftext writes simple text documents as PDF (A4, Helvetica, the
// Windows-1252 character set): headings, paragraphs and lists from a small
// Markdown subset. It serves documents such as an accepted data processing
// agreement, where a dependency for full PDF layout is not worth it.
package pdftext

import (
	"bytes"
	"fmt"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/encoding/charmap"
)

const (
	pageW, pageH = 595.28, 841.89
	margin       = 56.0
	bodySize     = 10.5
)

// widths of Helvetica and Helvetica-Bold for ASCII 32..126 (1/1000 em).
var (
	helv = []int{278, 278, 355, 556, 556, 889, 667, 191, 333, 333, 389, 584, 278, 333, 278, 278, 556, 556, 556, 556, 556, 556, 556, 556, 556, 556,
		278, 278, 584, 584, 584, 556, 1015, 667, 667, 722, 722, 667, 611, 778, 722, 278, 500, 667, 556, 833, 722, 778, 667, 778, 722, 667, 611,
		722, 667, 944, 667, 667, 611, 278, 278, 278, 469, 556, 333, 556, 556, 500, 556, 556, 278, 556, 556, 222, 222, 500, 222, 833, 556, 556,
		556, 556, 333, 500, 278, 556, 500, 722, 500, 500, 500, 334, 260, 334, 584}
	helvBold = []int{278, 333, 474, 556, 556, 889, 722, 238, 333, 333, 389, 584, 278, 333, 278, 278, 556, 556, 556, 556, 556, 556, 556, 556, 556, 556,
		333, 333, 584, 584, 584, 611, 975, 722, 722, 722, 722, 667, 611, 778, 722, 278, 556, 722, 611, 833, 722, 778, 667, 778, 722, 667, 611,
		722, 667, 944, 667, 667, 611, 333, 278, 333, 584, 556, 333, 556, 611, 556, 611, 556, 333, 611, 611, 278, 278, 556, 278, 889, 611, 611,
		611, 611, 389, 556, 333, 611, 556, 778, 556, 556, 500, 389, 280, 389, 584}
)

// base maps accented letters to the letter whose width they share.
var base = map[rune]rune{'ä': 'a', 'ö': 'o', 'ü': 'u', 'Ä': 'A', 'Ö': 'O', 'Ü': 'U', 'é': 'e', 'è': 'e', 'á': 'a', 'à': 'a', 'ç': 'c', 'ñ': 'n',
	'É': 'E', '–': '-', '—': 'M', '„': ',', '“': '"', '”': '"', '‘': '\'', '’': '\'', '…': 'm', '•': 'o', '€': '0', '§': 's', 'ß': 'b'}

func width(s string, size float64, bold bool) float64 {
	tbl := helv
	if bold {
		tbl = helvBold
	}
	w := 0
	for _, r := range s {
		if b, ok := base[r]; ok {
			r = b
		}
		if r >= 32 && r <= 126 {
			w += tbl[r-32]
		} else {
			w += 556
		}
	}
	return float64(w) * size / 1000
}

// encode turns text into a PDF string in Windows-1252 (unknown characters become "?").
func encode(s string) string {
	var b strings.Builder
	enc := charmap.Windows1252.NewEncoder()
	for _, r := range s {
		out, err := enc.String(string(r))
		if err != nil || out == "" {
			out = "?"
		}
		for _, c := range []byte(out) {
			switch c {
			case '(', ')', '\\':
				b.WriteByte('\\')
				b.WriteByte(c)
			default:
				if c < 32 {
					continue
				}
				b.WriteByte(c)
			}
		}
	}
	return b.String()
}

type line struct {
	text   string
	size   float64
	bold   bool
	indent float64
	gap    float64 // space before the line
}

// wrap breaks text into lines that fit the width.
func wrap(text string, size float64, bold bool, max float64) []string {
	var out []string
	cur := ""
	for _, w := range strings.Fields(text) {
		try := w
		if cur != "" {
			try = cur + " " + w
		}
		if width(try, size, bold) <= max || cur == "" {
			cur = try
			continue
		}
		out = append(out, cur)
		cur = w
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

func clean(s string) string {
	s = strings.ReplaceAll(s, "**", "")
	s = strings.ReplaceAll(s, "__", "")
	s = strings.ReplaceAll(s, "`", "")
	return strings.TrimRightFunc(s, unicode.IsSpace)
}

// layout turns the Markdown subset into lines.
func layout(md string) []line {
	var out []line
	textW := pageW - 2*margin
	var para []string
	gap := 0.0
	flush := func() {
		if len(para) == 0 {
			return
		}
		for i, l := range wrap(strings.Join(para, " "), bodySize, false, textW) {
			g := 0.0
			if i == 0 {
				g = gap
			}
			out = append(out, line{text: l, size: bodySize, gap: g})
		}
		para, gap = nil, 6
	}
	for _, raw := range strings.Split(strings.ReplaceAll(md, "\r\n", "\n"), "\n") {
		t := clean(strings.TrimSpace(raw))
		switch {
		case t == "":
			flush()
			gap = 6
		case strings.HasPrefix(t, "#"):
			flush()
			level := len(t) - len(strings.TrimLeft(t, "#"))
			size := map[int]float64{1: 16, 2: 13, 3: 11.5}[min(level, 3)]
			for i, l := range wrap(strings.TrimSpace(strings.TrimLeft(t, "#")), size, true, textW) {
				g := 0.0
				if i == 0 && len(out) > 0 {
					g = size * 0.9
				}
				out = append(out, line{text: l, size: size, bold: true, gap: g})
			}
			gap = 4
		case strings.HasPrefix(t, "- ") || strings.HasPrefix(t, "* ") || strings.HasPrefix(t, "• "):
			flush()
			item := strings.TrimSpace(t[strings.Index(t, " ")+1:])
			for i, l := range wrap(item, bodySize, false, textW-14) {
				if i == 0 {
					out = append(out, line{text: "•", size: bodySize, indent: 2, gap: gap})
					out[len(out)-1].text = "• " + l
				} else {
					out = append(out, line{text: l, size: bodySize, indent: 14})
				}
			}
			gap = 2
		default:
			para = append(para, t)
		}
	}
	flush()
	return out
}

// Render writes the Markdown subset as a PDF; footer is printed on every
// page next to the page number.
func Render(md, title, footer string) []byte {
	lines := layout(md)
	type page struct{ content bytes.Buffer }
	var pages []*page
	var cur *page
	y := 0.0
	newPage := func() {
		cur = &page{}
		pages = append(pages, cur)
		y = pageH - margin
	}
	newPage()
	for _, l := range lines {
		lh := l.size * 1.4
		if y-l.gap-lh < margin+20 {
			newPage()
		} else {
			y -= l.gap
		}
		y -= lh
		font := "F1"
		if l.bold {
			font = "F2"
		}
		x := margin + l.indent
		if strings.HasPrefix(l.text, "• ") {
			fmt.Fprintf(&cur.content, "BT /%s %.2f Tf %.2f %.2f Td (%s) Tj ET\n", font, l.size, x, y, encode("•"))
			fmt.Fprintf(&cur.content, "BT /%s %.2f Tf %.2f %.2f Td (%s) Tj ET\n", font, l.size, x+12, y, encode(strings.TrimPrefix(l.text, "• ")))
			continue
		}
		fmt.Fprintf(&cur.content, "BT /%s %.2f Tf %.2f %.2f Td (%s) Tj ET\n", font, l.size, x, y, encode(l.text))
	}
	var objs []string
	add := func(s string) int { objs = append(objs, s); return len(objs) }
	catalog := add("") // filled below
	pagesObj := add("")
	f1 := add("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>")
	f2 := add("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica-Bold /Encoding /WinAnsiEncoding >>")
	var kids []string
	for i, p := range pages {
		foot := fmt.Sprintf("%s  ·  %d / %d", footer, i+1, len(pages))
		fmt.Fprintf(&p.content, "0.4 g BT /F1 8 Tf %.2f %.2f Td (%s) Tj ET 0 g\n", margin, margin-18, encode(strings.TrimLeft(foot, " ·")))
		stream := p.content.String()
		c := add(fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(stream), stream))
		pg := add(fmt.Sprintf("<< /Type /Page /Parent %d 0 R /MediaBox [0 0 %.2f %.2f] /Resources << /Font << /F1 %d 0 R /F2 %d 0 R >> >> /Contents %d 0 R >>",
			pagesObj, pageW, pageH, f1, f2, c))
		kids = append(kids, fmt.Sprintf("%d 0 R", pg))
	}
	objs[catalog-1] = fmt.Sprintf("<< /Type /Catalog /Pages %d 0 R >>", pagesObj)
	objs[pagesObj-1] = fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), len(pages))
	info := add(fmt.Sprintf("<< /Title (%s) /Producer (Phoedox) /CreationDate (D:%s) >>", encode(title), time.Now().UTC().Format("20060102150405Z")))

	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n")
	offsets := make([]int, len(objs))
	for i, o := range objs {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, o := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root %d 0 R /Info %d 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, catalog, info, xref)
	return b.Bytes()
}
