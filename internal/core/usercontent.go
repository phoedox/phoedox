package core

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/phoedox/phoedox/internal/store"
	"github.com/phoedox/phoedox/internal/usercontent"
)

// The user content domain (apps-definition, "Why a separate domain"): apps
// run on <app>.<domain> when the system admin set one up; otherwise in a
// sandboxed frame without an origin on the instance's own address.

// UserContent is the user content server (set by the app; nil in tests).
type UserContent interface {
	Domain() string
	Status() usercontent.Status
	Apply(usercontent.Config) error
}

const settingUserContent = "usercontent"

// UserContentView is the setting without its secrets.
type UserContentView struct {
	Domain         string             `json:"domain"`
	Provider       string             `json:"provider"`
	Email          string             `json:"email"`
	Staging        bool               `json:"staging"`
	CustomerNumber string             `json:"customerNumber,omitempty"`
	HasSecret      bool               `json:"hasSecret"`
	Status         usercontent.Status `json:"status"`
	// Serving: the listener for the domain runs on this instance.
	Serving bool `json:"serving"`
}

func (s *Service) userContentConfig(ctx context.Context) usercontent.Config {
	var c usercontent.Config
	raw, _ := s.DB.Setting(store.System(ctx), settingUserContent, "")
	if raw == "" || s.Box == nil {
		return c
	}
	plain, err := s.Box.Open(raw, "usercontent")
	if err == nil {
		_ = json.Unmarshal(plain, &c)
	}
	return c
}

func (s *Service) userContentView(ctx context.Context) UserContentView {
	c := s.userContentConfig(ctx)
	v := UserContentView{Domain: c.Domain, Provider: c.Provider, Email: c.Email, Staging: c.Staging, CustomerNumber: c.CustomerNumber,
		HasSecret: c.Token != "" || c.APIKey != "", Status: usercontent.Status{State: "off"}}
	if s.UserContent != nil {
		v.Status, v.Serving = s.UserContent.Status(), true
	}
	return v
}

// UserContentSettings reads the user content domain (system admins).
func (s *Service) UserContentSettings(ctx context.Context, a Actor) (UserContentView, error) {
	if !a.User.IsAdmin || a.Via == "mcp" {
		return UserContentView{}, ErrForbidden
	}
	return s.userContentView(ctx), nil
}

// SaveUserContent sets the user content domain; empty secrets keep the
// stored ones, an empty domain switches it off.
func (s *Service) SaveUserContent(ctx context.Context, a Actor, in usercontent.Config) (UserContentView, error) {
	if !a.User.IsAdmin || a.Via == "mcp" {
		return UserContentView{}, ErrForbidden
	}
	if s.Box == nil {
		return UserContentView{}, invalid("validation.usercontent_box", nil, "the master key is missing")
	}
	old := s.userContentConfig(ctx)
	in.Domain = strings.ToLower(strings.TrimSpace(in.Domain))
	if in.Domain != "" {
		if in.Provider == old.Provider {
			if in.Token == "" {
				in.Token = old.Token
			}
			if in.APIKey == "" {
				in.APIKey = old.APIKey
			}
			if in.APIPassword == "" {
				in.APIPassword = old.APIPassword
			}
		}
		if err := in.Validate(); err != nil {
			return UserContentView{}, invalid("validation.usercontent_"+err.Error(), nil, "user content domain: %v", err)
		}
	}
	raw, _ := json.Marshal(in)
	sealed, err := s.Box.Seal(raw, "usercontent")
	if err != nil {
		return UserContentView{}, err
	}
	if in.Domain == "" {
		sealed = ""
	}
	if err := s.DB.SetSetting(store.System(ctx), settingUserContent, sealed, a.String()); err != nil {
		return UserContentView{}, err
	}
	s.event(ctx, a, "admin.usercontent", in.Domain, map[string]string{"provider": in.Provider})
	if s.UserContent != nil {
		if err := s.UserContent.Apply(in); err != nil {
			return UserContentView{}, invalid("validation.usercontent_apply", map[string]any{"error": err.Error()}, "could not start: %v", err)
		}
	}
	return s.userContentView(ctx), nil
}

// StartUserContent applies the stored setting at startup.
func (s *Service) StartUserContent(ctx context.Context) error {
	if s.UserContent == nil {
		return nil
	}
	return s.UserContent.Apply(s.userContentConfig(ctx))
}

// appDomain is the user content domain apps run on ("" = sandboxed frame
// on the instance's address).
func (s *Service) appDomain() string {
	if s.UserContent == nil {
		return ""
	}
	if s.UserContent.Status().State != "ready" {
		return ""
	}
	return s.UserContent.Domain()
}
