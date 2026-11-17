package blocks

import "testing"

func TestInteractiveBlocks(t *testing.T) {
	ok := map[string]string{
		"poll": "multiple\nWohin?\n- Zoo\n- Museum\nclosed",
		"quiz": "title: Netz\n? Router?\n- 1.1\n+ 10.1\n> Seit März.\n\n? WLAN?\n= Gartenhaus",
	}
	for k, src := range ok {
		if ps := Check(k, []byte(src)); len(ps) > 0 {
			t.Errorf("%s: %v", k, ps)
		}
		if d, _ := Get(k); d.Text == nil || d.Text([]byte(src)) == "" {
			t.Errorf("%s: no search text", k)
		}
	}
	bad := map[string][2]string{
		"poll dup":       {"poll", "Q\n- a\n- a"},
		"poll one":       {"poll", "Q\n- a"},
		"quiz no right":  {"quiz", "? A\n- x\n- y"},
		"quiz no answer": {"quiz", "? A"},
	}
	for name, c := range bad {
		if ps := Check(c[0], []byte(c[1])); len(ps) == 0 {
			t.Errorf("%s: no problem found", name)
		}
	}
	if d, _ := Get("quiz"); d.Board {
		t.Error("quiz is for pages only")
	}
}
