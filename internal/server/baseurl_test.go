package server

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/phoedox/phoedox/internal/config"
)

// The public address follows the request only on a verified app domain of
// an organization; any other Host header keeps the configured address.
func TestBaseURLPerHost(t *testing.T) {
	known := func(_ context.Context, host string) bool { return host == "docs.acme.test" }
	for _, c := range []struct {
		cfg        config.Config
		host, want string
	}{
		{config.Config{Domain: "app.example.org"}, "app.example.org", "https://app.example.org"},
		{config.Config{Domain: "app.example.org"}, "docs.acme.test", "https://docs.acme.test"},
		{config.Config{Domain: "app.example.org"}, "Docs.Acme.test:443", "https://docs.acme.test"},
		{config.Config{Domain: "app.example.org"}, "evil.example", "https://app.example.org"},
		{config.Config{PublicURL: "https://docs.example.org"}, "docs.acme.test", "https://docs.acme.test"},
		{config.Config{PublicURL: "https://docs.example.org"}, "evil.example", "https://docs.example.org"},
		// Without a configured address the request's own origin is used (as before).
		{config.Config{}, "localhost:8080", "http://localhost:8080"},
	} {
		r := httptest.NewRequest("GET", "/", nil)
		r.Host = c.host
		if got := BaseURL(c.cfg, known)(r); got != c.want {
			t.Errorf("%+v %s: %q, want %q", c.cfg, c.host, got, c.want)
		}
		if got := BaseURL(c.cfg, nil)(r); c.cfg.Domain != "" && got != "https://app.example.org" {
			t.Errorf("without lookup %s: %q", c.host, got)
		}
	}
}
