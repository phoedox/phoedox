// Package botcheck is Phoedox's own bot protection (usage-prices-concept,
// "Bot protection"): invisible checks (proof of work, behaviour score,
// honeypot and timing, browser checks), visible challenges (slider and
// rotate puzzles), an e-mail code as the accessible fallback, and third-party
// providers (Cloudflare Turnstile, hCaptcha, reCAPTCHA) with the operator's
// own keys. Named policies combine the checks; each place that needs
// protection (sign-up, sign-in after failures, password reset, public forms,
// later feedback portals and public apps) picks one.
//
// The package has its own small HTTP API and talks to Phoedox only through
// the Store, Sealer and Mailer interfaces, so it can later run as its own
// service with the same API.
package botcheck

import (
	"fmt"
	"slices"
	"strings"
)

// Places that can be protected.
const (
	PlaceSignup      = "signup"
	PlaceSignin      = "signin"
	PlaceReset       = "reset"
	PlacePublicForms = "public_forms"
	PlaceFeedback    = "feedback"
	PlaceApps        = "apps"
)

// Places lists every place in display order.
var Places = []string{PlaceSignup, PlaceSignin, PlaceReset, PlacePublicForms, PlaceFeedback, PlaceApps}

// Providers.
const (
	ProviderOwn       = "own"
	ProviderTurnstile = "turnstile"
	ProviderHCaptcha  = "hcaptcha"
	ProviderReCaptcha = "recaptcha"
)

// ProviderKeys are the operator's keys for a third-party provider. The
// secret is stored sealed; DPAConfirmed records that the operator has a data
// processing agreement with the provider.
type ProviderKeys struct {
	Enabled      bool   `json:"enabled"`
	SiteKey      string `json:"siteKey"`
	Secret       string `json:"secret,omitempty"` // sealed at rest; never sent to clients
	HasSecret    bool   `json:"hasSecret"`
	DPAConfirmed bool   `json:"dpaConfirmed"`
}

// Policy combines checks. With the own provider, the invisible checks give
// a score between 0 and 1: at or above PassAbove the visitor passes, below
// BlockBelow the request is blocked, in between a visible challenge follows
// (Visible: slider, rotate or any). The e-mail code is offered as the last
// step when EmailFallback is set.
type Policy struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	Preset        string  `json:"preset,omitempty"` // gentle | normal | strict | puzzle; "" = advanced
	Provider      string  `json:"provider"`
	PassAbove     float64 `json:"passAbove"`
	BlockBelow    float64 `json:"blockBelow"`
	Visible       string  `json:"visible"`
	EmailFallback bool    `json:"emailFallback"`
	// Difficulty of the proof of work in leading zero bits (0 = default).
	Difficulty int `json:"difficulty,omitempty"`
}

// Config is the instance's bot protection: providers, policies and which
// policy each place uses ("" = off).
type Config struct {
	Providers map[string]*ProviderKeys `json:"providers"`
	Policies  []Policy                 `json:"policies"`
	Places    map[string]string        `json:"places"`
	// SigninFailures is how many failed sign-ins (per account or address
	// within 15 minutes) come before the check.
	SigninFailures int `json:"signinFailures"`
}

// Presets are the built-in policies; they cannot be removed.
var Presets = []Policy{
	{ID: "gentle", Name: "Gentle", Preset: "gentle", Provider: ProviderOwn, PassAbove: 0.3, BlockBelow: 0, Visible: "any", EmailFallback: true, Difficulty: 14},
	{ID: "normal", Name: "Normal", Preset: "normal", Provider: ProviderOwn, PassAbove: 0.5, BlockBelow: 0.1, Visible: "any", EmailFallback: true, Difficulty: 16},
	{ID: "strict", Name: "Strict", Preset: "strict", Provider: ProviderOwn, PassAbove: 0.75, BlockBelow: 0.25, Visible: "any", EmailFallback: true, Difficulty: 18},
	{ID: "puzzle", Name: "Always puzzle", Preset: "puzzle", Provider: ProviderOwn, PassAbove: 2, BlockBelow: 0, Visible: "any", EmailFallback: true, Difficulty: 14},
}

// DefaultConfig protects sign-up, sign-in after failures, password reset
// and public forms with the normal preset.
func DefaultConfig() Config {
	c := Config{Providers: map[string]*ProviderKeys{}, Places: map[string]string{}, SigninFailures: 3}
	c.Policies = slices.Clone(Presets)
	for _, p := range []string{PlaceSignup, PlaceSignin, PlaceReset, PlacePublicForms, PlaceFeedback, PlaceApps} {
		c.Places[p] = "normal"
	}
	return c
}

// normalize fills missing parts (presets, provider map) after loading.
func (c *Config) normalize() {
	if c.Providers == nil {
		c.Providers = map[string]*ProviderKeys{}
	}
	if c.Places == nil {
		c.Places = map[string]string{}
	}
	for _, p := range Presets {
		if !slices.ContainsFunc(c.Policies, func(x Policy) bool { return x.ID == p.ID }) {
			c.Policies = append(c.Policies, p)
		}
	}
	if c.SigninFailures <= 0 {
		c.SigninFailures = 3
	}
}

// Policy returns a policy by id.
func (c Config) Policy(id string) (Policy, bool) {
	for _, p := range c.Policies {
		if p.ID == id {
			return p, true
		}
	}
	return Policy{}, false
}

// ForPlace is the policy of a place; false when the place is not protected.
func (c Config) ForPlace(place string) (Policy, bool) {
	id := c.Places[place]
	if id == "" {
		return Policy{}, false
	}
	return c.Policy(id)
}

// ValidationError names a problem with a configuration (an i18n key).
type ValidationError struct {
	Key    string
	Params map[string]any
}

func (e *ValidationError) Error() string { return fmt.Sprintf("%s %v", e.Key, e.Params) }

func verr(key string, params map[string]any) error { return &ValidationError{Key: key, Params: params} }

// Validate checks a configuration before it is stored.
func (c Config) Validate() error {
	ids := map[string]bool{}
	for _, p := range c.Policies {
		if p.ID == "" || strings.TrimSpace(p.Name) == "" || ids[p.ID] {
			return verr("validation.bot_policy", map[string]any{"id": p.ID})
		}
		ids[p.ID] = true
		switch p.Provider {
		case ProviderOwn:
		case ProviderTurnstile, ProviderHCaptcha, ProviderReCaptcha:
			k := c.Providers[p.Provider]
			if k == nil || !k.Enabled || k.SiteKey == "" || !k.HasSecret {
				return verr("validation.bot_provider_missing", map[string]any{"provider": p.Provider})
			}
		default:
			return verr("validation.bot_provider", map[string]any{"provider": p.Provider})
		}
		if p.PassAbove < 0 || p.BlockBelow < 0 || p.BlockBelow > 1 || (p.PassAbove <= 1 && p.BlockBelow > p.PassAbove) {
			return verr("validation.bot_thresholds", map[string]any{"policy": p.Name})
		}
		if !slices.Contains([]string{"slider", "rotate", "any", "none"}, p.Visible) {
			return verr("validation.bot_visible", map[string]any{"policy": p.Name})
		}
		if p.Difficulty != 0 && (p.Difficulty < 8 || p.Difficulty > 24) {
			return verr("validation.bot_difficulty", map[string]any{"policy": p.Name})
		}
	}
	for place, id := range c.Places {
		if !slices.Contains(Places, place) {
			return verr("validation.bot_place", map[string]any{"place": place})
		}
		if id != "" && !ids[id] {
			return verr("validation.bot_policy_unknown", map[string]any{"place": place})
		}
	}
	for name, k := range c.Providers {
		if !slices.Contains([]string{ProviderTurnstile, ProviderHCaptcha, ProviderReCaptcha}, name) {
			return verr("validation.bot_provider", map[string]any{"provider": name})
		}
		if k != nil && k.Enabled && !k.DPAConfirmed {
			return verr("validation.bot_dpa", map[string]any{"provider": name})
		}
	}
	if c.SigninFailures < 1 || c.SigninFailures > 20 {
		return verr("validation.bot_signin_failures", nil)
	}
	return nil
}
