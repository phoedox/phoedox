package core

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/phoedox/phoedox/internal/store"
)

// Instance licenses (ADR-0092): a file signed by the store
// (phoedox-store: internal/license/instance.go – change both together)
// unlocks enterprise features of this instance. It is checked offline
// against the trusted store keys (LicenseKeys) and the instance's domain.
// After the end date it keeps working for its grace days, with a notice to
// the system admins; afterwards the free features apply. Data is never
// locked. The file comes from PHOEDOX_LICENSE_FILE (LicenseFile, read-only)
// or is uploaded under Administration → License.

// Enterprise features a license can unlock.
const (
	FeatureSingleOrg         = "single_org"
	FeatureCustomRoles       = "custom_roles"
	FeatureFolderPermissions = "folder_permissions"
	FeatureSSOGroups         = "sso_groups"
	FeatureFourEyes          = "four_eyes"
	FeatureAuditExport       = "audit_export"
	licenseAllFeatures       = "*"
	settingLicense           = "instance.license"
)

// InstanceLicense is the content of an instance license file.
type InstanceLicense struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	Licensee  string    `json:"licensee"`
	Domains   []string  `json:"domains"`
	Features  []string  `json:"features"`
	Seats     int       `json:"seats,omitempty"`
	IssuedAt  time.Time `json:"issuedAt"`
	Expires   time.Time `json:"expires"`
	GraceDays int       `json:"graceDays"`
}

// InstanceLicenseFile is the signed file.
type InstanceLicenseFile struct {
	License   InstanceLicense `json:"license"`
	KeyID     string          `json:"keyId"`
	Signature string          `json:"signature"`
}

// LicenseInfo is the license state shown to system admins.
type LicenseInfo struct {
	Present   bool       `json:"present"`
	Active    bool       `json:"active"`  // features unlocked
	InGrace   bool       `json:"inGrace"` // ended, within the grace days
	Expiring  bool       `json:"expiring"`
	GraceEnd  *time.Time `json:"graceEnd,omitempty"`
	Problem   string     `json:"problem,omitempty"` // signature, domain, expired, invalid
	FromFile  bool       `json:"fromFile"`          // PHOEDOX_LICENSE_FILE (not changeable in the app)
	Domain    string     `json:"domain,omitempty"`  // this instance's domain
	SeatsUsed int        `json:"seatsUsed"`
	*InstanceLicense
}

func licenseMessage(l InstanceLicense) ([]byte, error) {
	b, err := json.Marshal(l)
	if err != nil {
		return nil, err
	}
	return append([]byte("phoedox-instance-license-v1\n"), b...), nil
}

// checkLicense verifies a file for domain at now.
func (s *Service) checkLicense(f InstanceLicenseFile, domain string, now time.Time) LicenseInfo {
	info := LicenseInfo{Present: true, Domain: domain, InstanceLicense: &f.License}
	pub, ok := s.LicenseKeys[f.KeyID]
	sig, err := base64.StdEncoding.DecodeString(f.Signature)
	msg, merr := licenseMessage(f.License)
	if !ok || err != nil || merr != nil || !ed25519.Verify(pub, msg, sig) || f.License.Kind != "instance" {
		info.Problem = "signature"
		return info
	}
	d := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(domain)), ".")
	if d == "" || !slices.Contains(f.License.Domains, d) {
		info.Problem = "domain"
		return info
	}
	if f.License.Expires.IsZero() || now.Before(f.License.Expires) {
		info.Active = true
		info.Expiring = !f.License.Expires.IsZero() && now.After(f.License.Expires.AddDate(0, 0, -30))
		return info
	}
	end := f.License.Expires.Add(time.Duration(f.License.GraceDays) * 24 * time.Hour)
	info.GraceEnd = &end
	if now.Before(end) {
		info.Active, info.InGrace = true, true
		return info
	}
	info.Problem = "expired"
	return info
}

// licenseRaw is the license file in use: PHOEDOX_LICENSE_FILE or the uploaded one.
func (s *Service) licenseRaw(ctx context.Context) (string, bool) {
	if s.LicenseFile != "" {
		return s.LicenseFile, true
	}
	v, _ := s.DB.Setting(store.System(ctx), settingLicense, "")
	return v, false
}

// License returns the state of the instance license.
func (s *Service) License(ctx context.Context) LicenseInfo {
	raw, fromFile := s.licenseRaw(ctx)
	if strings.TrimSpace(raw) == "" {
		return LicenseInfo{FromFile: fromFile, Domain: s.LicenseDomain}
	}
	var f InstanceLicenseFile
	if err := json.Unmarshal([]byte(raw), &f); err != nil {
		return LicenseInfo{Present: true, Problem: "invalid", FromFile: fromFile, Domain: s.LicenseDomain}
	}
	info := s.checkLicense(f, s.LicenseDomain, time.Now())
	info.FromFile = fromFile
	if n, err := s.DB.CountUsers(store.System(ctx)); err == nil {
		info.SeatsUsed = n
	}
	return info
}

// Licensed tells whether the instance license unlocks a feature.
func (s *Service) Licensed(ctx context.Context, feature string) bool {
	info := s.License(ctx)
	return info.Active && info.InstanceLicense != nil &&
		(slices.Contains(info.Features, licenseAllFeatures) || slices.Contains(info.Features, feature))
}

// LicenseForAdmin returns the license state (system admins).
func (s *Service) LicenseForAdmin(ctx context.Context, a Actor) (LicenseInfo, error) {
	if !a.User.IsAdmin || s.fullUser(a) != nil {
		return LicenseInfo{}, ErrForbidden
	}
	return s.License(ctx), nil
}

// SetLicense stores an uploaded license file after checking it (system
// admins); an empty file removes it. Not possible while
// PHOEDOX_LICENSE_FILE is set.
func (s *Service) SetLicense(ctx context.Context, a Actor, raw json.RawMessage) (LicenseInfo, error) {
	if !a.User.IsAdmin || s.fullUser(a) != nil {
		return LicenseInfo{}, ErrForbidden
	}
	if s.LicenseFile != "" {
		return LicenseInfo{}, invalid("validation.license_from_file", nil, "the license comes from PHOEDOX_LICENSE_FILE")
	}
	text := strings.TrimSpace(string(raw))
	if text == "" || text == "null" || text == `""` {
		if err := s.DB.SetSetting(store.System(ctx), settingLicense, "", a.User.ID); err != nil {
			return LicenseInfo{}, err
		}
		s.event(ctx, a, "license.remove", "instance", nil)
		return s.License(ctx), nil
	}
	if len(text) > 64<<10 {
		return LicenseInfo{}, invalid("validation.license_invalid", nil, "not a license file")
	}
	var f InstanceLicenseFile
	if err := json.Unmarshal([]byte(text), &f); err != nil || f.Signature == "" {
		return LicenseInfo{}, invalid("validation.license_invalid", nil, "not a license file")
	}
	info := s.checkLicense(f, s.LicenseDomain, time.Now())
	switch info.Problem {
	case "signature":
		return info, invalid("validation.license_signature", nil, "the license is not signed by a trusted store key")
	case "domain":
		return info, invalid("validation.license_domain", map[string]any{"domain": s.LicenseDomain}, "the license is not issued for %s", s.LicenseDomain)
	case "expired":
		return info, invalid("validation.license_expired", nil, "the license has expired")
	}
	if err := s.DB.SetSetting(store.System(ctx), settingLicense, text, a.User.ID); err != nil {
		return LicenseInfo{}, err
	}
	s.event(ctx, a, "license.set", "instance", map[string]any{"id": f.License.ID, "licensee": f.License.Licensee, "features": f.License.Features})
	return s.License(ctx), nil
}
