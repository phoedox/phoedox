package server

import (
	"crypto/rand"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/phoedox/phoedox/internal/core"
)

// Speed test between the browser and this server (Wi-Fi measurements on
// floor plans, ADR-0047). Signed-in users only; nothing is stored.

const maxSpeedBytes = 100 << 20

var speedBlock = sync.OnceValue(func() []byte {
	b := make([]byte, 1<<20)
	_, _ = rand.Read(b) // random so no proxy can compress it
	return b
})

func (a *api) speedPing(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) speedDown(w http.ResponseWriter, r *http.Request) {
	n, _ := strconv.Atoi(r.URL.Query().Get("bytes"))
	if n <= 0 || n > maxSpeedBytes {
		n = 25 << 20
	}
	h := w.Header()
	h.Set("Content-Type", "application/octet-stream")
	h.Set("Content-Length", strconv.Itoa(n))
	h.Set("Cache-Control", "no-store")
	h.Set("Content-Encoding", "identity")
	block := speedBlock()
	for n > 0 {
		k := min(n, len(block))
		if _, err := w.Write(block[:k]); err != nil {
			return
		}
		n -= k
	}
}

type speedUpOut struct {
	Bytes   int64   `json:"bytes"`
	Seconds float64 `json:"seconds"`
}

func (a *api) speedUp(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	n, err := io.Copy(io.Discard, http.MaxBytesReader(w, r.Body, maxSpeedBytes))
	if err != nil {
		writeErr(w, core.Invalid("validation.speedtest_size", map[string]any{"max": maxSpeedBytes >> 20}, "at most %d MB", maxSpeedBytes>>20))
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	respond(w, speedUpOut{Bytes: n, Seconds: time.Since(start).Seconds()}, nil)
}

func (a *api) ipam(w http.ResponseWriter, r *http.Request) {
	res, err := a.core.IPAM(r.Context(), actorOf(r), chi.URLParam(r, "p"))
	respond(w, res, err)
}
