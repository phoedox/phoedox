package core

import (
	"context"
	"errors"
	"testing"
)

func TestOrgDomains(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, _ := setup(t)
	s.PublicURL = "https://app.example.org"
	records := map[string][]string{}
	s.LookupTXT = func(_ context.Context, name string) ([]string, error) {
		if r, ok := records[name]; ok {
			return r, nil
		}
		return nil, errors.New("no such host")
	}
	owner.User.IsAdmin = true // creating organizations is for system admins
	o, err := s.CreateOrg(ctx, owner, OrgInput{Name: "Acme"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetOrgDomain(ctx, reader, o.Slug, "docs.acme.test"); err == nil {
		t.Fatal("non-member set a domain")
	}
	for _, bad := range []string{"acme", "http://docs.acme.test", "app.example.org"} {
		if _, err := s.SetOrgDomain(ctx, owner, o.Slug, bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
	v, err := s.SetOrgDomain(ctx, owner, o.Slug, "Docs.Acme.test.")
	if err != nil || v.Domain != "docs.acme.test" || v.Verified || v.RecordName != "_phoedox.docs.acme.test" || v.Target != "app.example.org" {
		t.Fatalf("set: %v %+v", err, v)
	}
	if c := s.AppContextFor(ctx, "docs.acme.test"); c.Org != nil {
		t.Fatal("unverified domain is active")
	}
	if _, err := s.VerifyOrgDomain(ctx, owner, o.Slug); invalidKey(err) != "validation.domain_unverified" {
		t.Fatalf("verify without record: %v", err)
	}
	records["_phoedox.docs.acme.test"] = []string{"other", v.RecordText}
	if v, err = s.VerifyOrgDomain(ctx, owner, o.Slug); err != nil || !v.Verified {
		t.Fatalf("verify: %v %+v", err, v)
	}
	c := s.AppContextFor(ctx, "docs.acme.test:443")
	if c.Org == nil || c.Org.Slug != o.Slug || c.Org.Name != "Acme" {
		t.Fatalf("context: %+v", c)
	}
	if tenant, err := s.DB.TenantByDomain(ctx, "docs.acme.test"); err != nil || tenant == "" {
		t.Fatalf("tenant by org domain: %v %q", err, tenant)
	}
	// another organization cannot take it
	o2, _ := s.CreateOrg(ctx, owner, OrgInput{Name: "Other"})
	if _, err := s.SetOrgDomain(ctx, owner, o2.Slug, "docs.acme.test"); invalidKey(err) != "validation.domain_taken" {
		t.Fatalf("taken: %v", err)
	}
	// removing it
	if _, err := s.SetOrgDomain(ctx, owner, o.Slug, ""); err != nil {
		t.Fatal(err)
	}
	if c := s.AppContextFor(ctx, "docs.acme.test"); c.Org != nil {
		t.Fatal("removed domain still active")
	}
}
