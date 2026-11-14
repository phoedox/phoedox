package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestTerminalRelay(t *testing.T) {
	a := &api{}
	got := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.runTerminal(w, r, "sess1", r.URL.Query().Get("side") == "agent", func() func(*recorder) {
			return func(rec *recorder) { got <- rec.buf.String() }
		})
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	url := "ws" + strings.TrimPrefix(srv.URL, "http")
	agent, _, err := websocket.Dial(ctx, url+"?side=agent", nil)
	if err != nil {
		t.Fatal(err)
	}
	browser, _, err := websocket.Dial(ctx, url+"?side=browser", nil)
	if err != nil {
		t.Fatal(err)
	}
	// A second browser for the same session is refused.
	if extra, _, err := websocket.Dial(ctx, url+"?side=browser", nil); err == nil {
		if _, _, err := extra.Read(ctx); err == nil {
			t.Fatal("second browser joined")
		}
	}
	_ = browser.Write(ctx, websocket.MessageText, []byte(`{"type":"resize","cols":100,"rows":40}`))
	_ = browser.Write(ctx, websocket.MessageBinary, []byte("ls\r"))
	for _, want := range []string{`{"type":"resize","cols":100,"rows":40}`, "ls\r"} {
		_, data, err := agent.Read(ctx)
		if err != nil || string(data) != want {
			t.Fatalf("agent got %q %v", data, err)
		}
	}
	_ = agent.Write(ctx, websocket.MessageBinary, []byte("file.txt\r\n"))
	if _, data, err := browser.Read(ctx); err != nil || string(data) != "file.txt\r\n" {
		t.Fatalf("browser got %q %v", data, err)
	}
	agent.Close(websocket.StatusNormalClosure, "exited")
	select {
	case rec := <-got:
		lines := strings.Split(strings.TrimSpace(rec), "\n")
		if len(lines) != 3 || !strings.Contains(lines[0], `"version":2`) || !strings.Contains(lines[1], `"i","ls\r"`) || !strings.Contains(lines[2], `"o","file.txt\r\n"`) {
			t.Fatalf("recording: %s", rec)
		}
	case <-ctx.Done():
		t.Fatal("relay did not end")
	}
}
