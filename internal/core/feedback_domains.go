package core

import (
	"context"
	"net"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/phoedox/phoedox/internal/store"
)

// Own domains of feedback portals (e.g. feedback.example.com): the owner
// chooses a domain, proves control with a DNS TXT record
// (_phoedox.<domain> = phoedox-verify=<token>) and points it at the
// instance (CNAME); the instance admin adds it to the ingress like other
// own domains. Part of the custom-domains feature (paid where the instance
// says so, ADR-0061).

// PortalDomain is a portal's domain with the DNS records to create.
type PortalDomain struct {
	Domain     string `json:"domain"`
	Verified   bool   `json:"verified"`
	RecordName string `json:"recordName"`
	RecordText string `json:"recordText"`
	Target     string `json:"target,omitempty"`
}

func (s *Service) portalDomain(p store.FeedbackPortal) *PortalDomain {
	if p.Domain == "" {
		return nil
	}
	d := &PortalDomain{Domain: p.Domain, Verified: p.DomainVerifiedAt != nil, RecordName: "_phoedox." + p.Domain, RecordText: "phoedox-verify=" + p.DomainToken}
	if u, err := url.Parse(s.PublicURL); err == nil {
		d.Target = u.Hostname()
	}
	return d
}

// SetFeedbackPortalDomain chooses a portal's own domain ("" removes it).
func (s *Service) SetFeedbackPortalDomain(ctx context.Context, a Actor, ref, id, domain string) (*PortalDomain, error) {
	p, err := s.feedbackProject(ctx, a, ref, store.RoleOwner)
	if err != nil {
		return nil, err
	}
	portal, err := s.DB.FeedbackPortal(ctx, p.ID, id)
	if err != nil {
		return nil, err
	}
	domain = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(domain)), ".")
	if domain == portal.Domain {
		return s.portalDomain(portal), nil
	}
	if domain == "" {
		if err := s.DB.SetFeedbackPortalDomain(ctx, portal.ID, "", ""); err != nil {
			return nil, err
		}
		s.event(ctx, a, "feedback.domain", portal.ID, map[string]any{"domain": ""})
		return nil, nil
	}
	sub, err := s.projectSubject(ctx, p)
	if err != nil {
		return nil, err
	}
	if err := s.requireFeature(ctx, FeatureCustomDomains, sub); err != nil {
		return nil, err
	}
	if !domainRe.MatchString(domain) || len(domain) > 253 {
		return nil, invalid("validation.domain", nil, "invalid domain")
	}
	if u, err := url.Parse(s.PublicURL); err == nil && strings.EqualFold(u.Hostname(), domain) {
		return nil, invalid("validation.domain_taken", nil, "this domain is already in use")
	}
	if taken, err := s.DB.PortalDomainTaken(ctx, domain, portal.ID); err != nil {
		return nil, err
	} else if taken {
		return nil, invalid("validation.domain_taken", nil, "this domain is already in use")
	}
	token := strings.TrimPrefix(randomToken("v"), "v")
	if err := s.DB.SetFeedbackPortalDomain(ctx, portal.ID, domain, token); err != nil {
		if err == store.ErrConflict {
			return nil, invalid("validation.domain_taken", nil, "this domain is already in use")
		}
		return nil, err
	}
	s.event(ctx, a, "feedback.domain", portal.ID, map[string]any{"domain": domain})
	portal.Domain, portal.DomainToken, portal.DomainVerifiedAt = domain, token, nil
	return s.portalDomain(portal), nil
}

// VerifyFeedbackPortalDomain looks up the TXT record and activates the domain.
func (s *Service) VerifyFeedbackPortalDomain(ctx context.Context, a Actor, ref, id string) (*PortalDomain, error) {
	p, err := s.feedbackProject(ctx, a, ref, store.RoleOwner)
	if err != nil {
		return nil, err
	}
	portal, err := s.DB.FeedbackPortal(ctx, p.ID, id)
	if err != nil {
		return nil, err
	}
	if portal.Domain == "" {
		return nil, invalid("validation.domain_missing", nil, "no domain set")
	}
	lookup := s.LookupTXT
	if lookup == nil {
		lookup = net.DefaultResolver.LookupTXT
	}
	lctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	records, _ := lookup(lctx, "_phoedox."+portal.Domain)
	if !slices.Contains(records, "phoedox-verify="+portal.DomainToken) {
		return nil, invalid("validation.domain_unverified", map[string]any{"name": "_phoedox." + portal.Domain}, "the TXT record was not found (DNS changes can take a while)")
	}
	if err := s.DB.VerifyFeedbackPortalDomain(ctx, portal.ID); err != nil {
		return nil, err
	}
	s.event(ctx, a, "feedback.domain_verified", portal.ID, map[string]any{"domain": portal.Domain})
	now := store.Now()
	portal.DomainVerifiedAt = &now
	return s.portalDomain(portal), nil
}

// FeedbackPortalForHost returns the handle of the portal served on a host
// ("" when the host is no portal's own domain).
func (s *Service) FeedbackPortalForHost(ctx context.Context, host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	handle, _, err := s.DB.FeedbackPortalByDomain(ctx, host)
	if err != nil {
		return ""
	}
	return handle
}
