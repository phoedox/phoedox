package blocks

import (
	"strings"
	"testing"
)

func TestInteractiveBlocks(t *testing.T) {
	ok := map[string]string{
		"poll":   "multiple\nWohin?\n- Zoo\n- Museum\nclosed",
		"matrix": "title: NAS\ncriteria: Preis*3, Lautstärke*2, Speicher\n- A: 3, 4, 3\n- B: 5; 2; 5",
		"calc":   "title: Strom\nLeistung (W) = 25\nPreis (€/kWh) = 0,32\nKosten pro Jahr (€) = ROUND(Leistung * 24 * 365 / 1000 * Preis; 2)\nNAS (W) = @nas.power_w\nSumme = Kosten_pro_Jahr + NAS",
		"quiz":   "title: Netz\n? Router?\n- 1.1\n+ 10.1\n> Seit März.\n\n? WLAN?\n= Gartenhaus",
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
		"matrix count":   {"matrix", "criteria: a, b\n- X: 1\n- Y: 1, 2"},
		"matrix number":  {"matrix", "criteria: a\n- X: gut\n- Y: 1"},
		"calc unknown":   {"calc", "A = C + 1"},
		"calc cycle":     {"calc", "A = B + 1\nB = A * 2"},
		"calc brackets":  {"calc", "A = (1 + 2"},
		"calc line":      {"calc", "nur Text"},
		"quiz no right":  {"quiz", "? A\n- x\n- y"},
		"quiz no answer": {"quiz", "? A"},
	}
	for name, c := range bad {
		if ps := Check(c[0], []byte(c[1])); len(ps) == 0 {
			t.Errorf("%s: no problem found", name)
		}
	}
	if CalcName("Kosten pro Jahr (€)") != "Kosten_pro_Jahr" || CalcName("Größe") != "Größe" {
		t.Error("calc names")
	}
	if d, _ := Get("quiz"); d.Board {
		t.Error("quiz is for pages only")
	}
	if ps := Check("calc", []byte("A = 1\nB = SUM(A, 2)")); len(ps) > 0 || !strings.Contains(strings.Join(lines([]byte("A = 1")), ""), "A") {
		t.Errorf("functions are not names: %v", ps)
	}
}
