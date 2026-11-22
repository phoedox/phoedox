package core

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// licenseVector was signed by the store's code (phoedox-store:
// internal/license.SignInstance) with a fixed test key; it pins the shared
// format and signed message.
const licenseVector = `{"license":{"id":"lic_vector","kind":"instance","licensee":"Acme GmbH","domains":["docs.acme.example"],"features":["single_org","custom_roles"],"seats":50,"issuedAt":"2026-11-22T10:00:00Z","expires":"2027-11-30T23:59:59Z","graceDays":14},"keyId":"test-1","signature":"Ex8h4GaS3im3BgA9CaRfuEtEIDvX690SQIi7v6PBEjrKluzHm34Pk7Q+Mv/67R9zLbDCmEWhCIjpu30UFK3tDQ=="}`

func TestInstanceLicense(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, _ := setup(t)
	pub, _ := base64.StdEncoding.DecodeString("A6EHv/POEL4dcN0Y50vAmWfk1jCbpQ1fHdyGZBJVMbg=")
	s.LicenseKeys = map[string]ed25519.PublicKey{"test-1": pub}
	s.LicenseDomain = "docs.acme.example"
	admin := owner
	admin.User.IsAdmin = true

	if info := s.License(ctx); info.Present || s.Licensed(ctx, FeatureSingleOrg) {
		t.Fatalf("no license yet: %+v", info)
	}
	if _, err := s.SetLicense(ctx, reader, json.RawMessage(licenseVector)); err != ErrForbidden {
		t.Fatalf("only system admins: %v", err)
	}
	info, err := s.SetLicense(ctx, admin, json.RawMessage(licenseVector))
	if err != nil || !info.Active || info.Licensee != "Acme GmbH" || info.Seats != 50 {
		t.Fatalf("set: %v %+v", err, info)
	}
	if !s.Licensed(ctx, FeatureSingleOrg) || !s.Licensed(ctx, FeatureCustomRoles) || s.Licensed(ctx, FeatureFourEyes) {
		t.Fatal("features")
	}
	// Another domain, an unknown key, tampering.
	s.LicenseDomain = "other.example"
	if _, err := s.SetLicense(ctx, admin, json.RawMessage(licenseVector)); invalidKey(err) != "validation.license_domain" {
		t.Fatalf("domain: %v", err)
	}
	if s.Licensed(ctx, FeatureSingleOrg) || s.License(ctx).Problem != "domain" {
		t.Fatal("stored license on another domain must not count")
	}
	s.LicenseDomain = "docs.acme.example"
	tampered := strings.Replace(licenseVector, `"seats":50`, `"seats":5000`, 1)
	if _, err := s.SetLicense(ctx, admin, json.RawMessage(tampered)); invalidKey(err) != "validation.license_signature" {
		t.Fatalf("tampered: %v", err)
	}
	if _, err := s.SetLicense(ctx, admin, json.RawMessage(`{"hello":1}`)); invalidKey(err) != "validation.license_invalid" {
		t.Fatalf("not a license: %v", err)
	}

	// Grace and expiry, checked on the file directly.
	var f InstanceLicenseFile
	_ = json.Unmarshal([]byte(licenseVector), &f)
	end := f.License.Expires
	if i := s.checkLicense(f, "docs.acme.example", end.AddDate(0, 0, -10)); !i.Active || !i.Expiring {
		t.Fatalf("expiring: %+v", i)
	}
	if i := s.checkLicense(f, "docs.acme.example", end.AddDate(0, 0, 5)); !i.Active || !i.InGrace || i.GraceEnd == nil || !i.GraceEnd.Equal(end.AddDate(0, 0, 14)) {
		t.Fatalf("grace: %+v", i)
	}
	if i := s.checkLicense(f, "docs.acme.example", end.AddDate(0, 0, 15)); i.Active || i.Problem != "expired" {
		t.Fatalf("expired: %+v", i)
	}
	if i := s.checkLicense(f, "docs.acme.example", time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)); !i.Active || i.Expiring {
		t.Fatalf("valid: %+v", i)
	}

	// Removing it; a file from PHOEDOX_LICENSE_FILE wins and cannot be changed.
	if info, err := s.SetLicense(ctx, admin, nil); err != nil || info.Present {
		t.Fatalf("remove: %v %+v", err, info)
	}
	s.LicenseFile = licenseVector
	if info := s.License(ctx); !info.Active || !info.FromFile {
		t.Fatalf("from file: %+v", info)
	}
	if _, err := s.SetLicense(ctx, admin, nil); invalidKey(err) != "validation.license_from_file" {
		t.Fatalf("change a file license: %v", err)
	}
}
