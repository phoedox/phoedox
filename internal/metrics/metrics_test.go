package metrics

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestMetrics(t *testing.T) {
	m := New(Sources{Version: "1.2.3", LiveEditors: func() int { return 3 },
		JobCounts: func(context.Context) (map[string]int, error) { return map[string]int{"queued": 2}, nil }})
	r := chi.NewRouter()
	r.Use(m.Middleware)
	r.Get("/api/v1/projects/{p}", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(404) })
	r.Handle("/metrics", m.Handler("geheim"))
	for range 2 {
		r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/v1/projects/secret-name", nil))
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	if rec.Code != 401 {
		t.Fatalf("without token: %d", rec.Code)
	}
	req := httptest.NewRequest("GET", "/metrics", nil)
	req.Header.Set("Authorization", "Bearer geheim")
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	body := rec.Body.String()
	for _, want := range []string{
		`phoedox_http_requests_total{code="404",method="GET",route="/api/v1/projects/{p}"} 2`,
		`phoedox_info{version="1.2.3"} 1`, `phoedox_live_editors 3`, `phoedox_jobs{status="queued"} 2`, "go_goroutines",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(body, "secret-name") {
		t.Error("URLs must not end up in labels")
	}
}
