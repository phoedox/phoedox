package core

import (
	"context"
	"testing"
	"time"

	"github.com/phoedox/phoedox/internal/store"
)

func TestEntitlements(t *testing.T) {
	ctx := context.Background()
	s, owner, _, _ := setup(t)
	s.PublicURL = "https://app.example.org"
	owner.User.IsAdmin = true
	early, err := s.CreateOrg(ctx, owner, OrgInput{Name: "Early"})
	if err != nil {
		t.Fatal(err)
	}
	late, _ := s.CreateOrg(ctx, owner, OrgInput{Name: "Late"})

	// Without pricing everything is free.
	if fa, _ := s.Entitled(ctx, FeatureCustomDomains, OrgSubject(late)); !fa.Allowed || fa.Source != EntFree {
		t.Fatalf("free: %+v", fa)
	}
	if a, err := s.OrgDomainAccess(ctx, owner, late.Slug); err != nil || a != nil {
		t.Fatalf("access without pricing: %v %+v", err, a)
	}

	// Custom domains become paid between the two organizations.
	cutoff := time.Now().UTC()
	_, err = s.DB.Exec(store.System(ctx), `UPDATE organizations SET created_at = ? WHERE id = ?`, store.Time{Time: cutoff.Add(-48 * time.Hour)}, early.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = s.DB.Exec(store.System(ctx), `UPDATE organizations SET created_at = ? WHERE id = ?`, store.Time{Time: cutoff.Add(time.Hour)}, late.ID)
	early, _ = s.DB.OrgFor(ctx, early.ID, "")
	late, _ = s.DB.OrgFor(ctx, late.ID, "")
	s.PaidFeatures = map[string]time.Time{FeatureCustomDomains: cutoff}

	// Before housekeeping the date decides.
	if fa, _ := s.Entitled(ctx, FeatureCustomDomains, OrgSubject(early)); !fa.Allowed || fa.Source != EntGrandfathered {
		t.Fatalf("early by date: %+v", fa)
	}
	if fa, _ := s.Entitled(ctx, FeatureCustomDomains, OrgSubject(late)); fa.Allowed || fa.Source != EntNone || fa.PaidFrom == nil {
		t.Fatalf("late: %+v", fa)
	}
	if _, err := s.SetOrgDomain(ctx, owner, late.Slug, "docs.late.test"); invalidKey(err) != "validation.feature_paid" {
		t.Fatalf("late org set a domain: %v", err)
	}
	if _, err := s.SetOrgDomain(ctx, owner, early.Slug, "docs.early.test"); err != nil {
		t.Fatalf("early org: %v", err)
	}

	// Housekeeping stores the right; a later date change does not take it away.
	s.GrandfatherFeatures(ctx)
	s.GrandfatherFeatures(ctx) // idempotent
	_, _ = s.DB.Exec(store.System(ctx), `UPDATE organizations SET created_at = ? WHERE id = ?`, store.Time{Time: cutoff.Add(24 * time.Hour)}, early.ID)
	early, _ = s.DB.OrgFor(ctx, early.ID, "")
	if fa, _ := s.Entitled(ctx, FeatureCustomDomains, OrgSubject(early)); !fa.Allowed || fa.Source != EntGrandfathered {
		t.Fatalf("stored grandfathering: %+v", fa)
	}
	a, err := s.OrgDomainAccess(ctx, owner, early.Slug)
	if err != nil || a == nil || a.Source != EntGrandfathered {
		t.Fatalf("access: %v %+v", err, a)
	}
	// Owners of personal projects count as users.
	if f, err := s.MyFeatures(ctx, owner); err != nil || len(f) != 1 || !f[0].Allowed {
		t.Fatalf("my features: %v %+v", err, f)
	}

	// A grant makes the late org entitled; revoking removes it again, but
	// never a grandfathered right.
	cli := Actor{User: store.User{ID: "cli", Name: "Admin", IsAdmin: true}, Via: "cli"}
	if err := s.GrantFeature(ctx, cli, OrgSubject(late), "unknown_feature", "", ""); invalidKey(err) != "validation.feature_unknown" {
		t.Fatalf("unknown feature: %v", err)
	}
	if err := s.GrantFeature(ctx, cli, OrgSubject(late), FeatureCustomDomains, "", "pilot"); err != nil {
		t.Fatal(err)
	}
	if fa, _ := s.Entitled(ctx, FeatureCustomDomains, OrgSubject(late)); !fa.Allowed || fa.Source != EntGrant {
		t.Fatalf("grant: %+v", fa)
	}
	if ok, err := s.RevokeFeature(ctx, cli, OrgSubject(late), FeatureCustomDomains); !ok || err != nil {
		t.Fatalf("revoke: %v %v", ok, err)
	}
	if ok, _ := s.RevokeFeature(ctx, cli, OrgSubject(early), FeatureCustomDomains); ok {
		t.Fatal("revoked a grandfathered right")
	}
	if err := s.GrantFeature(ctx, cli, OrgSubject(early), FeatureCustomDomains, EntPlan, ""); err != nil {
		t.Fatal(err)
	}
	if fa, _ := s.Entitled(ctx, FeatureCustomDomains, OrgSubject(early)); fa.Source != EntGrandfathered {
		t.Fatalf("a plan replaced grandfathering: %+v", fa)
	}
}
