package core

import (
	"context"
	"slices"

	"github.com/phoedox/phoedox/internal/store"
)

// Own domains by CNAME (publications, feedback portals): personal projects
// and projects of non-commercial organizations get one per project; for
// commercial organizations they are the paid custom-domains feature. The
// app on an organization's own domain is separate (orgdomains.go): only for
// organizations a system admin switched it on for.

// cnameDomainAllowed checks whether projects may use one more own domain;
// the publication and portal named are the ones being changed.
func (s *Service) cnameDomainAllowed(ctx context.Context, projectIDs []string, exceptPub, exceptPortal string) error {
	pubs, err := s.DB.ListPublications(ctx)
	if err != nil {
		return err
	}
	for _, id := range projectIDs {
		p, err := s.DB.ProjectFor(ctx, id, "")
		if err != nil {
			return err
		}
		commercial := false
		if p.OrgID != "" {
			o, err := s.DB.OrgFor(ctx, p.OrgID, "")
			if err != nil {
				return err
			}
			commercial = o.UsageType != "noncommercial"
		}
		if commercial {
			sub, err := s.projectSubject(ctx, p)
			if err != nil {
				return err
			}
			if err := s.requireFeature(ctx, FeatureCustomDomains, sub); err != nil {
				return err
			}
			continue
		}
		used := 0
		for _, pub := range pubs {
			if pub.ID != exceptPub && pub.Domain != "" && slices.Contains(pub.Projects, id) {
				used++
			}
		}
		portals, err := s.DB.FeedbackPortals(ctx, id)
		if err != nil {
			return err
		}
		for _, pt := range portals {
			if pt.ID != exceptPortal && pt.Domain != "" {
				used++
			}
		}
		if used >= 1 {
			return invalid("validation.domain_limit", map[string]any{"project": p.Name}, "%s already has its own domain (one per project)", p.Name)
		}
	}
	return nil
}

// orgAppDomainAllowed: the app on an own domain needs a system admin's switch.
func orgAppDomainAllowed(o store.Org) error {
	if !o.AppDomainAllowed {
		return invalid("validation.org_domain_admin", nil, "a system admin has to switch own app domains on for this organization")
	}
	return nil
}
