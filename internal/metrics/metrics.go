// Package metrics exposes Prometheus metrics at /metrics: HTTP requests by
// route, background jobs, live editing, the database pool and Go runtime.
package metrics

import (
	"bufio"
	"context"
	"crypto/subtle"
	"database/sql"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics holds the registry and the HTTP instruments.
type Metrics struct {
	reg      *prometheus.Registry
	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
}

// Sources are read at scrape time.
type Sources struct {
	DB          *sql.DB
	JobCounts   func(ctx context.Context) (map[string]int, error)
	LiveEditors func() int
	Version     string
}

// New registers the collectors.
func New(src Sources) *Metrics {
	reg := prometheus.NewRegistry()
	m := &Metrics{
		reg: reg,
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "phoedox_http_requests_total", Help: "HTTP requests by route pattern, method and status."},
			[]string{"route", "method", "code"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "phoedox_http_request_duration_seconds", Help: "Time to answer HTTP requests.",
			Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10}}, []string{"route", "method"}),
	}
	reg.MustRegister(m.requests, m.duration, collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	reg.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "phoedox_info", Help: "Version of this server.", ConstLabels: prometheus.Labels{"version": src.Version}},
		func() float64 { return 1 }))
	if src.DB != nil {
		reg.MustRegister(collectors.NewDBStatsCollector(src.DB, "phoedox"))
	}
	if src.LiveEditors != nil {
		reg.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "phoedox_live_editors", Help: "People editing pages live right now."},
			func() float64 { return float64(src.LiveEditors()) }))
	}
	if src.JobCounts != nil {
		reg.MustRegister(&jobCollector{counts: src.JobCounts, desc: prometheus.NewDesc("phoedox_jobs", "Background jobs by status.", []string{"status"}, nil)})
	}
	return m
}

type jobCollector struct {
	counts func(ctx context.Context) (map[string]int, error)
	desc   *prometheus.Desc
}

func (c *jobCollector) Describe(ch chan<- *prometheus.Desc) { ch <- c.desc }
func (c *jobCollector) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	counts, err := c.counts(ctx)
	if err != nil {
		return
	}
	for status, n := range counts {
		ch <- prometheus.MustNewConstMetric(c.desc, prometheus.GaugeValue, float64(n), status)
	}
}

type recorder struct {
	http.ResponseWriter
	code int
}

func (r *recorder) WriteHeader(code int) {
	r.code = code
	r.ResponseWriter.WriteHeader(code)
}

// Unwrap lets http.ResponseController reach the original writer.
func (r *recorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// Hijack keeps websockets (live editing) working behind the middleware.
func (r *recorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	r.code = http.StatusSwitchingProtocols
	return http.NewResponseController(r.ResponseWriter).Hijack()
}

// Flush passes streaming responses (MCP) through.
func (r *recorder) Flush() { _ = http.NewResponseController(r.ResponseWriter).Flush() }

// Middleware counts requests by chi route pattern (not by URL, to keep
// the number of series small and to keep ids out of metrics).
func (m *Metrics) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &recorder{ResponseWriter: w, code: http.StatusOK}
		next.ServeHTTP(rec, r)
		route := "other"
		if rc := chi.RouteContext(r.Context()); rc != nil && rc.RoutePattern() != "" {
			route = rc.RoutePattern()
		}
		m.requests.WithLabelValues(route, r.Method, strconv.Itoa(rec.code)).Inc()
		m.duration.WithLabelValues(route, r.Method).Observe(time.Since(start).Seconds())
	})
}

// Handler serves the metrics to callers with the bearer token.
func (m *Metrics) Handler(token string) http.Handler {
	h := promhttp.HandlerFor(m.reg, promhttp.HandlerOpts{})
	want := []byte("Bearer " + token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), want) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="metrics"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		h.ServeHTTP(w, r)
	})
}
