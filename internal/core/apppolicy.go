package core

import (
	"context"
	"encoding/json"
	"regexp"
	"slices"
	"strings"

	"github.com/phoedox/phoedox/internal/store"
)

// App policy (apps-definition "Admin switches"): the instance and each
// organization can switch apps off, cap the bridge level, forbid AI calls
// and allow network hosts apps may connect to. The stricter setting wins;
// hosts of an organization count only where the instance lets
// organizations add hosts.

// AppPolicy is one level's switches.
type AppPolicy struct {
	Off bool `json:"off,omitempty"`
	// MaxLevel caps the bridge level (0–3); nil = no cap.
	MaxLevel *int `json:"maxLevel,omitempty"`
	NoAI     bool `json:"noAi,omitempty"`
	// Hosts apps may connect to (https only, exact host names).
	Hosts []string `json:"hosts,omitempty"`
	// OrgHosts (instance only): organizations may allow their own hosts.
	OrgHosts bool `json:"orgHosts,omitempty"`
}

const settingAppPolicy = "apps.policy"

func appOrgKey(orgID string) string { return "apps.org:" + orgID }

var appHostRe = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)

func (p *AppPolicy) normalize() error {
	if p.MaxLevel != nil && (*p.MaxLevel < 0 || *p.MaxLevel > 3) {
		return invalid("validation.app_level", nil, "the level is 0 to 3")
	}
	hosts := []string{}
	for _, h := range p.Hosts {
		h = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(h, "https://")))
		if h == "" {
			continue
		}
		if !appHostRe.MatchString(h) {
			return invalid("validation.app_host", map[string]any{"host": h}, "%q is not a host name", h)
		}
		if !slices.Contains(hosts, h) {
			hosts = append(hosts, h)
		}
	}
	if len(hosts) > 50 {
		return invalid("validation.app_host", map[string]any{"host": "…"}, "at most 50 hosts")
	}
	p.Hosts = hosts
	return nil
}

func (s *Service) readAppPolicy(ctx context.Context, key string) AppPolicy {
	var p AppPolicy
	if raw, _ := s.DB.Setting(ctx, key, ""); raw != "" {
		_ = json.Unmarshal([]byte(raw), &p)
	}
	return p
}

// effectiveAppPolicy combines the instance's and the project's
// organization's switches.
func (s *Service) effectiveAppPolicy(ctx context.Context, p store.Project) AppPolicy {
	inst := s.readAppPolicy(ctx, settingAppPolicy)
	out := AppPolicy{Off: inst.Off, MaxLevel: inst.MaxLevel, NoAI: inst.NoAI, Hosts: slices.Clone(inst.Hosts)}
	if p.OrgID != "" {
		org := s.readAppPolicy(ctx, appOrgKey(p.OrgID))
		out.Off = out.Off || org.Off
		out.NoAI = out.NoAI || org.NoAI
		if org.MaxLevel != nil && (out.MaxLevel == nil || *org.MaxLevel < *out.MaxLevel) {
			out.MaxLevel = org.MaxLevel
		}
		if inst.OrgHosts {
			for _, h := range org.Hosts {
				if !slices.Contains(out.Hosts, h) {
					out.Hosts = append(out.Hosts, h)
				}
			}
		}
	}
	return out
}

// capLevel applies the policy's level cap.
func (p AppPolicy) capLevel(level int) int {
	if p.MaxLevel != nil {
		return min(level, *p.MaxLevel)
	}
	return level
}

func errAppsOff() error { return invalid("validation.apps_off", nil, "apps are switched off here") }

// InstanceAppPolicy reads the instance's app switches (system admins).
func (s *Service) InstanceAppPolicy(ctx context.Context, a Actor) (AppPolicy, error) {
	if err := s.adminOnly(a); err != nil {
		return AppPolicy{}, err
	}
	return s.readAppPolicy(ctx, settingAppPolicy), nil
}

// SetInstanceAppPolicy changes the instance's app switches.
func (s *Service) SetInstanceAppPolicy(ctx context.Context, a Actor, in AppPolicy) (AppPolicy, error) {
	if err := s.adminOnly(a); err != nil {
		return AppPolicy{}, err
	}
	if err := in.normalize(); err != nil {
		return AppPolicy{}, err
	}
	raw, _ := json.Marshal(in)
	if err := s.DB.SetSetting(ctx, settingAppPolicy, string(raw), a.User.ID); err != nil {
		return AppPolicy{}, err
	}
	s.event(ctx, a, "apps.policy", "instance", in)
	return in, nil
}

// OrgAppPolicy reads an organization's app switches (its admins); Hosts
// counts only if the instance allows organization hosts (InstanceOrgHosts).
type OrgAppPolicy struct {
	AppPolicy
	InstanceOrgHosts bool `json:"instanceOrgHosts"`
}

// GetOrgAppPolicy reads an organization's app switches.
func (s *Service) GetOrgAppPolicy(ctx context.Context, a Actor, ref string) (OrgAppPolicy, error) {
	o, err := s.org(ctx, a, ref, store.OrgAdmin)
	if err != nil {
		return OrgAppPolicy{}, err
	}
	return OrgAppPolicy{AppPolicy: s.readAppPolicy(ctx, appOrgKey(o.ID)), InstanceOrgHosts: s.readAppPolicy(ctx, settingAppPolicy).OrgHosts}, nil
}

// SetOrgAppPolicy changes an organization's app switches (its admins).
func (s *Service) SetOrgAppPolicy(ctx context.Context, a Actor, ref string, in AppPolicy) (OrgAppPolicy, error) {
	o, err := s.org(ctx, a, ref, store.OrgAdmin)
	if err != nil {
		return OrgAppPolicy{}, err
	}
	in.OrgHosts = false
	if err := in.normalize(); err != nil {
		return OrgAppPolicy{}, err
	}
	raw, _ := json.Marshal(in)
	if err := s.DB.SetSetting(ctx, appOrgKey(o.ID), string(raw), a.User.ID); err != nil {
		return OrgAppPolicy{}, err
	}
	s.event(ctx, a, "apps.policy", o.ID, in)
	return OrgAppPolicy{AppPolicy: in, InstanceOrgHosts: s.readAppPolicy(ctx, settingAppPolicy).OrgHosts}, nil
}
