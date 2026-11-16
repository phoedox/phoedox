package server

import (
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/phoedox/phoedox/internal/botcheck"
)

// Bot protection (internal/botcheck): its public API under /bot, and the
// check of pass tokens on protected requests.

var botPaths = []string{"/challenge", "/verify", "/solve", "/email", "/email/verify", "/provider"}

func (a *api) botRoutes(r interface {
	Post(string, http.HandlerFunc)
}) {
	var h http.Handler
	if a.core.Bot != nil {
		h = http.StripPrefix("/api/v1/bot", a.core.Bot.Handler(clientIP))
	}
	for _, p := range botPaths {
		r.Post("/bot"+p, func(w http.ResponseWriter, req *http.Request) {
			if h == nil {
				if p == "/challenge" {
					writeJSON(w, http.StatusOK, map[string]bool{"off": true})
					return
				}
				writeJSON(w, http.StatusNotFound, errBody("not_found", "Bot protection is off."))
				return
			}
			h.ServeHTTP(w, req)
		})
	}
}

// botCheck consumes a pass token for a place; false means the answer was
// written (the check is missing or failed).
func (a *api) botCheck(w http.ResponseWriter, r *http.Request, place, token string) bool {
	if a.core.Bot == nil {
		return true
	}
	err := a.core.Bot.Check(r.Context(), place, token)
	switch {
	case err == nil:
		return true
	case errors.Is(err, botcheck.ErrRequired):
		writeJSON(w, http.StatusBadRequest, errBody("bot_required", "Please complete the security check."))
	default:
		writeJSON(w, http.StatusBadRequest, errBody("bot_invalid", "The security check failed or expired. Please try again."))
	}
	return false
}

// failures counts failed sign-ins per address and account (15 minutes).
type failures struct {
	mu   sync.Mutex
	hits map[string][]time.Time
}

func (f *failures) count(key string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, t := range f.hits[key] {
		if time.Since(t) < 15*time.Minute {
			n++
		}
	}
	return n
}

func (f *failures) add(keys ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.hits == nil || len(f.hits) > 100_000 {
		f.hits = map[string][]time.Time{}
	}
	for _, k := range keys {
		var recent []time.Time
		for _, t := range f.hits[k] {
			if time.Since(t) < 15*time.Minute {
				recent = append(recent, t)
			}
		}
		f.hits[k] = append(recent, time.Now())
	}
}

func (f *failures) clear(keys ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, k := range keys {
		delete(f.hits, k)
	}
}

func loginKeys(r *http.Request, email string) []string {
	return []string{"ip:" + clientIP(r), "mail:" + strings.ToLower(strings.TrimSpace(email))}
}

// signinNeedsBot tells whether a sign-in needs the bot check: after the
// configured number of failures for the address or the account.
func (a *api) signinNeedsBot(r *http.Request, email string) bool {
	if a.core.Bot == nil || !a.core.Bot.Enabled(r.Context(), botcheck.PlaceSignin) {
		return false
	}
	n := a.core.Bot.Config(r.Context()).SigninFailures
	for _, k := range loginKeys(r, email) {
		if a.loginFails.count(k) >= n {
			return true
		}
	}
	return false
}

func (a *api) botSettings(w http.ResponseWriter, r *http.Request) {
	v, err := a.core.BotSettingsOf(r.Context(), actorOf(r))
	respond(w, v, err)
}

func (a *api) saveBotSettings(w http.ResponseWriter, r *http.Request) {
	var in botcheck.Config
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	v, err := a.core.SaveBotSettings(r.Context(), actorOf(r), in)
	respond(w, v, err)
}
