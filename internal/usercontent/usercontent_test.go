package usercontent

import (
	"net/http"

	"github.com/caddyserver/certmagic"
	"net/http/httptest"
	"testing"
)

func TestValidate(t *testing.T) {
	ok := Config{Domain: " PhoedoxUserContent.com ", Provider: "netcup", Email: "ops@example.org", CustomerNumber: "1", APIKey: "k", APIPassword: "p"}
	if err := ok.Validate(); err != nil || ok.Domain != "phoedoxusercontent.com" {
		t.Fatalf("valid: %v %q", err, ok.Domain)
	}
	for want, c := range map[string]Config{
		"domain":   {Domain: "not a domain", Provider: "hetzner", Token: "x", Email: "a@b"},
		"token":    {Domain: "x.com", Provider: "cloudflare", Email: "a@b"},
		"netcup":   {Domain: "x.com", Provider: "netcup", APIKey: "k", Email: "a@b"},
		"provider": {Domain: "x.com", Provider: "route53", Email: "a@b"},
		"email":    {Domain: "x.com", Provider: "hetzner", Token: "t"},
	} {
		if err := c.Validate(); err == nil || err.Error() != want {
			t.Errorf("%s: %v", want, err)
		}
	}
}

func TestHosts(t *testing.T) {
	var label string
	s := &Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { label = r.Header.Get("X-Phoedox-Host-Label") })}
	s.cfg.Domain = "uc.test"
	s.magic = nil
	// Without a running configuration everything is refused.
	rec := httptest.NewRecorder()
	s.hosts().ServeHTTP(rec, httptest.NewRequest("GET", "https://abc.uc.test/f/x/", nil))
	if rec.Code != 404 {
		t.Fatalf("off: %d", rec.Code)
	}
	// Running: only <label>.<domain> passes, with the label for the handler.
	s.magic = &certmagic.Config{}
	for host, code := range map[string]int{"abc.uc.test": 200, "uc.test": 404, "a.b.uc.test": 404, "abc.other.test": 404, "evil-uc.test": 404} {
		label = ""
		rec := httptest.NewRecorder()
		s.hosts().ServeHTTP(rec, httptest.NewRequest("GET", "https://"+host+"/f/x/", nil))
		if rec.Code != code || (code == 200 && label != "abc") {
			t.Errorf("%s: %d %q", host, rec.Code, label)
		}
	}
	if Label("01ABC") != "01abc" {
		t.Fatal("label")
	}
}
