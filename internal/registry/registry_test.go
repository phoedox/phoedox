package registry

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	cases := map[string]Ref{
		"nginx":                        {"registry-1.docker.io", "library/nginx", "latest"},
		"nginx:1.27":                   {"registry-1.docker.io", "library/nginx", "1.27"},
		"grafana/grafana:11.3.0":       {"registry-1.docker.io", "grafana/grafana", "11.3.0"},
		"docker.io/library/redis:7":    {"registry-1.docker.io", "library/redis", "7"},
		"ghcr.io/phoedox/phoedox:v1.2": {"ghcr.io", "phoedox/phoedox", "v1.2"},
		"localhost:5000/app":           {"localhost:5000", "app", "latest"},
	}
	for in, want := range cases {
		if got, err := Parse(in); err != nil || got != want {
			t.Errorf("%s: %+v %v", in, got, err)
		}
	}
	if _, err := Parse("nginx@sha256:abc"); !errors.Is(err, ErrPinned) {
		t.Fatalf("pinned: %v", err)
	}
}

func TestDigestWithToken(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/token":
			if r.URL.Query().Get("scope") != "repository:org/app:pull" {
				http.Error(w, "scope", 400)
				return
			}
			_, _ = w.Write([]byte(`{"token":"t0k"}`))
		case strings.HasPrefix(r.URL.Path, "/v2/org/app/manifests/1.0"):
			if r.Header.Get("Authorization") != "Bearer t0k" {
				w.Header().Set("WWW-Authenticate", `Bearer realm="`+srv.URL+`/token",service="reg",scope="repository:org/app:pull"`)
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			if !strings.Contains(r.Header.Get("Accept"), "image.index") {
				http.Error(w, "accept", 400)
				return
			}
			w.Header().Set("Docker-Content-Digest", "sha256:new")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "https://")
	d, err := Digest(context.Background(), srv.Client(), Ref{Registry: host, Repo: "org/app", Tag: "1.0"})
	if err != nil || d != "sha256:new" {
		t.Fatalf("digest: %q %v", d, err)
	}
	if _, err := Digest(context.Background(), srv.Client(), Ref{Registry: host, Repo: "org/none", Tag: "1"}); err == nil {
		t.Fatal("missing image gave no error")
	}
}
