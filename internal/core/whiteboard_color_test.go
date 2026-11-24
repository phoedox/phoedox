package core

import "testing"

func TestWBColorOK(t *testing.T) {
	for c, want := range map[string]bool{
		"gray": true, "pink": true, "#1a2b3c": true, "#ABCDEF": true,
		"#abc": false, "1a2b3c": false, "#1a2b3g": false, "teal": false,
		"#1a2b3c;x": false, "var(--x)": false,
	} {
		if got := WBColorOK(c); got != want {
			t.Errorf("WBColorOK(%q) = %v, want %v", c, got, want)
		}
	}
}
