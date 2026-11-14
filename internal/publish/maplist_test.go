package publish

import (
	"strings"
	"testing"
)

func TestMapList(t *testing.T) {
	out := mapList("marker 52.52, 13.405 Berlin <b>\nmarker @router\ntrack Hike: 52.5,13.4 52.6,13.5\ngpx 01ABC Morning run\ngpx ../x bad\nmarker 999,1 nope")
	for _, want := range []string{"mlat=52.520000", "Berlin &lt;b&gt;", "<li>Hike</li>", `/api/v1/files/01ABC">Morning run`} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in %s", want, out)
		}
	}
	if strings.Contains(out, "router") || strings.Contains(out, "nope") || strings.Contains(out, "../x") {
		t.Fatalf("unexpected item: %s", out)
	}
	if mapList("# nothing") != "" {
		t.Fatal("empty block rendered")
	}
}
