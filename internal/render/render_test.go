package render

import (
	"bytes"
	"context"
	"image/png"
	"os"
	"testing"
)

// PHOEDOX_TEST_RENDERER points at a running headless Chrome (DevTools), e.g.
// http://127.0.0.1:9222; without it the tests are skipped.
func testRenderer(t *testing.T) *Renderer {
	u := os.Getenv("PHOEDOX_TEST_RENDERER")
	if u == "" {
		t.Skip("PHOEDOX_TEST_RENDERER not set")
	}
	return New(u, 2)
}

func TestOff(t *testing.T) {
	r := New("", 1)
	if r.Configured() || r.Status(context.Background()).Configured {
		t.Fatal("configured without a URL")
	}
	if _, err := r.HTML(context.Background(), "<p>x</p>", 100, 100, 1); err != ErrOff {
		t.Fatalf("err %v", err)
	}
	if New("http://127.0.0.1:1", 1).Status(context.Background()).Reachable {
		t.Fatal("reachable at a closed port")
	}
}

func TestHTMLWithoutNetwork(t *testing.T) {
	r := testRenderer(t)
	defer r.Close()
	if st := r.Status(context.Background()); !st.Reachable || st.Browser == "" {
		t.Fatalf("status %+v", st)
	}
	doc := `<!doctype html><html><body style="margin:0;background:rgb(200,40,20)">
<img src="https://example.com/x.png" onerror="document.body.style.background='rgb(20,160,40)'"></body></html>`
	pic, err := r.HTML(context.Background(), doc, 300, 200, 1)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(pic))
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 300 || b.Dy() != 200 {
		t.Fatalf("size %v", b)
	}
	// The blocked image failed, so its error handler painted the body green.
	r8, g8, _, _ := img.At(150, 150).RGBA()
	if g8>>8 < 120 || r8>>8 > 80 {
		t.Fatalf("colour %d,%d: the request was not blocked", r8>>8, g8>>8)
	}
	// A second job reuses the connection.
	if _, err := r.HTML(context.Background(), "<p>again</p>", 100, 100, 2); err != nil {
		t.Fatal(err)
	}
}

func TestEndpointResolvesNames(t *testing.T) {
	for in, want := range map[string]string{
		"http://127.0.0.1:9222":   "http://127.0.0.1:9222",
		"ws://localhost:9222/":    "http://localhost:9222",
		"http://localhost:9222/x": "http://localhost:9222",
	} {
		if got, err := endpoint(context.Background(), in); err != nil || got != want {
			t.Errorf("%s: %s %v", in, got, err)
		}
	}
	// A name becomes its address (Chrome refuses other Host headers).
	got, err := endpoint(context.Background(), "http://ip6-localhost:9222")
	if err == nil && (got == "http://ip6-localhost:9222" || got == "") {
		t.Errorf("name kept: %s", got)
	}
}
