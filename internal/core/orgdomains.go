package core

import (
	"context"
	"errors"
	"net"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/phoedox/phoedox/internal/store"
)

// Own app domains of organizations: an org admin chooses a domain, proves
// control with a DNS TXT record (_phoedox.<domain> = phoedox-verify=<token>)
// and the app on that domain carries the organization's name and lists its
// projects first. The instance admin adds the domain to the ingress.

var domainRe = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)

// OrgDomainView is an organization's domain with the DNS record to create.
type OrgDomainView struct {
	store.OrgDomain
	Verified   bool   `json:"verified"`
	RecordName string `json:"recordName"`
	RecordText string `json:"recordText"`
	Target     string `json:"target,omitempty"` // CNAME target (the instance)
}

func (s *Service) domainView(d store.OrgDomain) OrgDomainView {
	v := OrgDomainView{OrgDomain: d, Verified: !d.VerifiedAt.IsZero(), RecordName: "_phoedox." + d.Domain, RecordText: "phoedox-verify=" + d.Token}
	if u, err := url.Parse(s.PublicURL); err == nil {
		v.Target = u.Hostname()
	}
	return v
}

// OrgDomain returns the organization's app domain (org admins).
func (s *Service) OrgDomain(ctx context.Context, a Actor, ref string) (*OrgDomainView, error) {
	if err := s.fullUser(a); err != nil {
		return nil, err
	}
	o, err := s.org(ctx, a, ref, store.OrgAdmin)
	if err != nil {
		return nil, err
	}
	d, err := s.DB.GetOrgDomain(ctx, o.ID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	v := s.domainView(d)
	return &v, nil
}

// SetOrgDomain chooses a new domain; it must be verified before use.
func (s *Service) SetOrgDomain(ctx context.Context, a Actor, ref, domain string) (*OrgDomainView, error) {
	if err := s.fullUser(a); err != nil {
		return nil, err
	}
	o, err := s.org(ctx, a, ref, store.OrgAdmin)
	if err != nil {
		return nil, err
	}
	domain = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(domain)), ".")
	if domain == "" {
		if err := s.DB.DeleteOrgDomain(ctx, o.ID); err != nil {
			return nil, err
		}
		s.event(ctx, a, "org.domain", o.ID, map[string]any{"domain": ""})
		return nil, nil
	}
	if !domainRe.MatchString(domain) || len(domain) > 253 {
		return nil, invalid("validation.domain", nil, "invalid domain")
	}
	if u, err := url.Parse(s.PublicURL); err == nil && strings.EqualFold(u.Hostname(), domain) {
		return nil, invalid("validation.domain_taken", nil, "this domain is already in use")
	}
	if taken, err := s.DB.DomainTaken(ctx, domain, o.ID); err != nil {
		return nil, err
	} else if taken {
		return nil, invalid("validation.domain_taken", nil, "this domain is already in use")
	}
	if cur, err := s.DB.GetOrgDomain(ctx, o.ID); err == nil && cur.Domain == domain {
		v := s.domainView(cur)
		return &v, nil
	}
	if err := s.DB.SetOrgDomain(ctx, o.ID, domain, strings.TrimPrefix(randomToken("v"), "v"), a.User.ID); err != nil {
		return nil, err
	}
	s.event(ctx, a, "org.domain", o.ID, map[string]any{"domain": domain})
	return s.OrgDomain(ctx, a, ref)
}

// VerifyOrgDomain looks up the TXT record and activates the domain.
func (s *Service) VerifyOrgDomain(ctx context.Context, a Actor, ref string) (*OrgDomainView, error) {
	if err := s.fullUser(a); err != nil {
		return nil, err
	}
	o, err := s.org(ctx, a, ref, store.OrgAdmin)
	if err != nil {
		return nil, err
	}
	d, err := s.DB.GetOrgDomain(ctx, o.ID)
	if err != nil {
		return nil, invalid("validation.domain_missing", nil, "no domain set")
	}
	lookup := s.LookupTXT
	if lookup == nil {
		lookup = net.DefaultResolver.LookupTXT
	}
	lctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	records, _ := lookup(lctx, "_phoedox."+d.Domain)
	if !slices.Contains(records, "phoedox-verify="+d.Token) {
		return nil, invalid("validation.domain_unverified", map[string]any{"name": "_phoedox." + d.Domain}, "the TXT record was not found (DNS changes can take a while)")
	}
	if err := s.DB.VerifyOrgDomain(ctx, o.ID); err != nil {
		return nil, err
	}
	s.event(ctx, a, "org.domain_verified", o.ID, map[string]any{"domain": d.Domain})
	return s.OrgDomain(ctx, a, ref)
}

// AppContext is what the app shows on a host: an organization's branding
// on its own domain, nothing on the instance's address.
type AppContext struct {
	Org *AppOrg `json:"org,omitempty"`
}

// AppOrg is the organization behind an app domain.
type AppOrg struct {
	ID   string `json:"id"`
	Slug string `json:"slug"`
	Name string `json:"name"`
}

// AppContextFor returns the context for a request host.
func (s *Service) AppContextFor(ctx context.Context, host string) AppContext {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	d, err := s.DB.OrgByDomain(ctx, host)
	if err != nil {
		return AppContext{}
	}
	o, err := s.DB.OrgFor(store.WithTenant(ctx, d.TenantID), d.OrgID, "")
	if err != nil {
		return AppContext{}
	}
	return AppContext{Org: &AppOrg{ID: o.ID, Slug: o.Slug, Name: o.Name}}
}
