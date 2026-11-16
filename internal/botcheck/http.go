package botcheck

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
)

// Handler is the small public API of the bot check (under /api/v1/bot):
//
//	POST /challenge     {place}                  → Challenge
//	POST /verify        {challenge, nonce, signals} → Result
//	POST /solve         {challenge, value}       → Result
//	POST /email         {challenge, email}       → Result (visible.kind = email)
//	POST /email/verify  {challenge, code}        → Result
//	POST /provider      {challenge, response}    → Result
//
// A Result carries a pass token, a visible challenge or blocked. Pages send
// the token with the protected request; the server checks it with Check.
func (s *Service) Handler(ipOf func(*http.Request) string) http.Handler {
	mux := http.NewServeMux()
	route := func(path string, h func(w http.ResponseWriter, r *http.Request, ip, ipHash string)) {
		mux.HandleFunc("POST "+path, func(w http.ResponseWriter, r *http.Request) {
			ip := ipOf(r)
			h(w, r, ip, s.IPHash(ip))
		})
	}
	decode := func(w http.ResponseWriter, r *http.Request, v any) bool {
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
		if err := dec.Decode(v); err != nil {
			writeErr(w, ErrInvalid)
			return false
		}
		return true
	}
	route("/challenge", func(w http.ResponseWriter, r *http.Request, _, ipHash string) {
		var in struct{ Place string }
		if !decode(w, r, &in) {
			return
		}
		out, err := s.Issue(r.Context(), in.Place, ipHash)
		reply(w, out, err)
	})
	route("/verify", func(w http.ResponseWriter, r *http.Request, _, ipHash string) {
		var in VerifyInput
		if decode(w, r, &in) {
			out, err := s.Verify(r.Context(), in, ipHash)
			reply(w, out, err)
		}
	})
	route("/solve", func(w http.ResponseWriter, r *http.Request, _, ipHash string) {
		var in SolveInput
		if decode(w, r, &in) {
			out, err := s.Solve(r.Context(), in, ipHash)
			reply(w, out, err)
		}
	})
	route("/email", func(w http.ResponseWriter, r *http.Request, _, ipHash string) {
		var in EmailInput
		if decode(w, r, &in) {
			out, err := s.EmailCode(r.Context(), in, ipHash)
			reply(w, out, err)
		}
	})
	route("/email/verify", func(w http.ResponseWriter, r *http.Request, _, ipHash string) {
		var in EmailInput
		if decode(w, r, &in) {
			out, err := s.EmailVerify(r.Context(), in, ipHash)
			reply(w, out, err)
		}
	})
	route("/provider", func(w http.ResponseWriter, r *http.Request, ip, ipHash string) {
		var in ProviderInput
		if decode(w, r, &in) {
			out, err := s.VerifyProvider(r.Context(), in, ip, ipHash)
			reply(w, out, err)
		}
	})
	return mux
}

func reply(w http.ResponseWriter, v any, err error) {
	if err != nil {
		writeErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json")
	if errors.Is(err, ErrInvalid) || errors.Is(err, ErrRequired) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "The check failed or expired. Please try again.", "code": "bot_invalid", "key": "bot.invalid"})
		return
	}
	slog.Warn("bot check", "err", err)
	w.WriteHeader(http.StatusInternalServerError)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": "Something went wrong.", "code": "internal", "key": "error.internal"})
}
