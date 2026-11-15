package core

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"time"

	"github.com/phoedox/phoedox/internal/store"
)

// Paid features and grandfathering (ADR-0061). Which features are paid, and
// from when, is configuration (PaidFeatures); self-hosted instances leave
// it empty, so everything stays free. Organizations and users created
// before a feature's cutoff keep it for free, recorded as a grandfathered
// entitlement that is never removed.

// Feature names used by gates.
const FeatureCustomDomains = "custom_domains"

// Entitlement sources.
const (
	EntFree          = "free" // not a paid feature (or no pricing configured)
	EntGrandfathered = "grandfathered"
	EntPlan          = "plan"
	EntGrant         = "grant"
	EntNone          = "none" // paid and not entitled
)

// Subject is who an entitlement belongs to: an organization or a user.
type Subject struct {
	Kind    string // org | user
	ID      string
	Created time.Time
}

func (s Subject) key() string { return s.Kind + ":" + s.ID }

// FeatureAccess tells whether a subject may use a feature and why.
type FeatureAccess struct {
	Feature  string     `json:"feature"`
	Allowed  bool       `json:"allowed"`
	Source   string     `json:"source"`
	PaidFrom *time.Time `json:"paidFrom,omitempty"`
}

// Entitled decides whether the subject may use the feature.
func (s *Service) Entitled(ctx context.Context, feature string, sub Subject) (FeatureAccess, error) {
	cutoff, paid := s.PaidFeatures[feature]
	if !paid {
		return FeatureAccess{Feature: feature, Allowed: true, Source: EntFree}, nil
	}
	fa := FeatureAccess{Feature: feature, PaidFrom: &cutoff, Source: EntNone}
	e, err := s.DB.GetEntitlement(ctx, sub.key(), feature)
	switch {
	case err == nil:
		fa.Allowed, fa.Source = true, e.Source
	case !errors.Is(err, store.ErrNotFound):
		return fa, err
	case !sub.Created.IsZero() && sub.Created.Before(cutoff):
		// Not written yet (the housekeeping run comes later): the date decides.
		fa.Allowed, fa.Source = true, EntGrandfathered
	}
	return fa, nil
}

// requireFeature returns validation.feature_paid when the subject lacks it.
func (s *Service) requireFeature(ctx context.Context, feature string, sub Subject) error {
	fa, err := s.Entitled(ctx, feature, sub)
	if err != nil {
		return err
	}
	if !fa.Allowed {
		return invalid("validation.feature_paid", map[string]any{"feature": feature}, "%s is part of a paid plan", feature)
	}
	return nil
}

// OrgSubject and UserSubject name who an entitlement belongs to.
func OrgSubject(o store.Org) Subject { return orgSubject(o) }

func UserSubject(u store.User) Subject {
	return Subject{Kind: "user", ID: u.ID, Created: u.CreatedAt.Time}
}

func orgSubject(o store.Org) Subject {
	return Subject{Kind: "org", ID: o.ID, Created: o.CreatedAt.Time}
}

// projectSubject is the project's organization, else its owner.
func (s *Service) projectSubject(ctx context.Context, p store.Project) (Subject, error) {
	if p.OrgID != "" {
		o, err := s.DB.OrgFor(ctx, p.OrgID, "")
		if err != nil {
			return Subject{}, err
		}
		return orgSubject(o), nil
	}
	u, err := s.DB.UserByID(ctx, p.OwnerID)
	if err != nil {
		return Subject{}, err
	}
	return Subject{Kind: "user", ID: u.ID, Created: u.CreatedAt.Time}, nil
}

// OrgFeatures lists the paid features and the organization's access (org admins).
func (s *Service) OrgFeatures(ctx context.Context, a Actor, ref string) ([]FeatureAccess, error) {
	o, err := s.org(ctx, a, ref, store.OrgAdmin)
	if err != nil {
		return nil, err
	}
	return s.features(ctx, orgSubject(o))
}

// MyFeatures lists the paid features and the user's access (personal projects).
func (s *Service) MyFeatures(ctx context.Context, a Actor) ([]FeatureAccess, error) {
	if a.User.ID == "" {
		return nil, ErrForbidden
	}
	return s.features(ctx, Subject{Kind: "user", ID: a.User.ID, Created: a.User.CreatedAt.Time})
}

func (s *Service) features(ctx context.Context, sub Subject) ([]FeatureAccess, error) {
	names := make([]string, 0, len(s.PaidFeatures))
	for f := range s.PaidFeatures {
		names = append(names, f)
	}
	sort.Strings(names)
	out := []FeatureAccess{}
	for _, f := range names {
		fa, err := s.Entitled(ctx, f, sub)
		if err != nil {
			return nil, err
		}
		out = append(out, fa)
	}
	return out, nil
}

// GrandfatherFeatures records grandfathered entitlements for everyone
// created before each paid feature's cutoff (start and housekeeping).
func (s *Service) GrandfatherFeatures(ctx context.Context) {
	for f, cutoff := range s.PaidFeatures {
		n, err := s.DB.Grandfather(ctx, f, store.Time{Time: cutoff})
		if err != nil {
			slog.Warn("grandfather features", "feature", f, "err", err)
			continue
		}
		if n > 0 {
			slog.Info("grandfathered", "feature", f, "subjects", n)
		}
	}
}

// GrantFeature gives an organization or user a feature (system admin, CLI).
// An empty source means a manual grant.
func (s *Service) GrantFeature(ctx context.Context, a Actor, sub Subject, feature, source, note string) error {
	if source == "" {
		source = EntGrant
	}
	if source != EntGrant && source != EntPlan {
		return invalid("validation.entitlement_source", nil, "source must be grant or plan")
	}
	if _, ok := s.PaidFeatures[feature]; !ok {
		return invalid("validation.feature_unknown", map[string]any{"feature": feature}, "%s is not a paid feature (PHOEDOX_PAID_FEATURES)", feature)
	}
	if err := s.DB.SetEntitlement(ctx, store.Entitlement{Subject: sub.key(), Feature: feature, Source: source, GrantedBy: a.String(), Note: note}); err != nil {
		return err
	}
	s.event(ctx, a, "entitlement.grant", sub.key(), map[string]any{"feature": feature, "source": source})
	return nil
}

// RevokeFeature removes a grant or plan; grandfathered rights stay.
func (s *Service) RevokeFeature(ctx context.Context, a Actor, sub Subject, feature string) (bool, error) {
	ok, err := s.DB.RevokeEntitlement(ctx, sub.key(), feature)
	if err == nil && ok {
		s.event(ctx, a, "entitlement.revoke", sub.key(), map[string]any{"feature": feature})
	}
	return ok, err
}
