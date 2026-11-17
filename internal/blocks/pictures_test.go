package blocks

import (
	"strings"
	"testing"
)

func TestPictureBlocks(t *testing.T) {
	ok := map[string]string{
		"mindmap":     "Umzug\n  Wohnung\n    Kündigen\n  - Internet",
		"gantt":       "title: Bad\ntoday\n## Planung\nAngebote [a]: 2026-01-05, 2w, done\nWahl: after a, 3d, 50%\nsection Umbau\nAbriss: 2026-02-02 – 2026-02-06, red\nFertig: 2026-03-20",
		"beforeafter": "title: Rack\nbefore: /api/v1/files/01M7ND6Y5KRMY1R09EK7Q7NMXS Vorher\nafter: 01M7ND6Y5KRMY1R09EK7Q7NMXT\n![b](https://example.org/b.jpg)",
		"annotate":    "title: Kasten\nimage: /api/v1/files/AB12 Flur\n- 22, 30: F1 Küche und @herd\n- 74,5% ; 64 blue: FI",
	}
	for k, src := range ok {
		if ps := Check(k, []byte(src)); len(ps) > 0 {
			t.Errorf("%s: %v", k, ps)
		}
	}
	if s := mustText(t, "gantt", ok["gantt"]); !strings.Contains(s, "Fertig") || !strings.Contains(s, "Planung") || !strings.Contains(s, "Bad") {
		t.Errorf("gantt text: %q", s)
	}
	if s := mustText(t, "annotate", ok["annotate"]); !strings.Contains(s, "@herd") {
		t.Errorf("annotate text keeps mentions: %q", s)
	}
	// Templates start without a photo.
	for k, src := range map[string]string{"beforeafter": "title: x\nbefore:\nafter:", "annotate": "title: x\nimage:\n- 1, 2: a"} {
		if ps := Check(k, []byte(src)); len(ps) > 0 {
			t.Errorf("%s template: %v", k, ps)
		}
	}
	bad := map[string][2]string{
		"mindmap empty": {"mindmap", "  \n"},
		"gantt after":   {"gantt", "X: after y, 2d"},
		"gantt date":    {"gantt", "A: morgen"},
		"gantt part":    {"gantt", "A: 2026-01-01, bald"},
		"gantt order":   {"gantt", "A: 2026-01-10, 2026-01-05"},
		"ba script":     {"beforeafter", "before: javascript:alert(1)"},
		"ba line":       {"beforeafter", "irgendwas"},
		"an pos":        {"annotate", "image: /api/v1/files/AB12\n- 120, 3: x"},
		"an colour":     {"annotate", "image: /api/v1/files/AB12\n- 1, 3 teal: x"},
		"an empty":      {"annotate", "title: nur Titel"},
	}
	for name, c := range bad {
		if len(Check(c[0], []byte(c[1]))) == 0 {
			t.Errorf("%s: no problem found", name)
		}
	}
}

func mustText(t *testing.T, kind, src string) string {
	d, ok := Get(kind)
	if !ok || d.Text == nil {
		t.Fatalf("%s: not registered", kind)
	}
	return d.Text([]byte(src))
}
