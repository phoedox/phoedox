package core

import (
	"context"
	"errors"
	"strings"

	"github.com/phoedox/phoedox/internal/graph"
	"github.com/phoedox/phoedox/internal/store"
)

// Single-organization mode (ADR-0092, PHOEDOX_SINGLE_ORG): the instance runs
// for one organization. It is created when the first person signs in (with
// them as admin); everyone who signs in later joins it, system admins as
// admins. New projects belong to it, and no further organizations can be
// created. Projects that existed before stay where they are.

// TheOrg returns the instance's organization in single-organization mode
// (ok is false otherwise or before it exists).
func (s *Service) TheOrg(ctx context.Context) (store.Org, bool) {
	if !s.SingleOrg {
		return store.Org{}, false
	}
	os, err := s.DB.ListOrgs(ctx, "", true)
	if err != nil || len(os) == 0 {
		return store.Org{}, false
	}
	first := os[0]
	for _, o := range os[1:] {
		if o.CreatedAt.Before(first.CreatedAt.Time) {
			first = o
		}
	}
	return first, true
}

// JoinTheOrg makes the user a member of the instance's organization (and
// creates it for the first person). It does nothing outside
// single-organization mode or when the user is a member already.
func (s *Service) JoinTheOrg(ctx context.Context, u store.User) error {
	if !s.SingleOrg || u.ID == "" {
		return nil
	}
	role := store.OrgMemberRole
	if u.IsAdmin {
		role = store.OrgAdmin
	}
	o, ok := s.TheOrg(ctx)
	if !ok {
		name := strings.TrimSpace(s.SingleOrgName)
		if name == "" {
			name = "Phoedox"
		}
		slug := graph.Slugify(name)
		if graph.ValidateSlug(slug) != nil {
			slug = "org"
		}
		o, err := s.DB.CreateOrg(ctx, store.Org{ID: newID(), Name: name, Slug: slug, UsageType: "commercial"}, u.ID)
		if errors.Is(err, store.ErrConflict) {
			// Created at the same moment by another sign-in.
			return s.JoinTheOrg(ctx, u)
		}
		if err == nil {
			s.event(ctx, Actor{User: u, Via: "system"}, "org.create", o.ID, map[string]any{"singleOrg": true})
		}
		return err
	}
	cur, err := s.DB.OrgFor(ctx, o.ID, u.ID)
	if err != nil {
		return err
	}
	if cur.Role == store.OrgAdmin || (cur.Role != "" && role == store.OrgMemberRole) {
		return nil
	}
	if err := s.DB.SetOrgMember(ctx, o.ID, u.ID, role); err != nil {
		return err
	}
	s.event(ctx, Actor{User: u, Via: "system"}, "org.join", o.ID, map[string]any{"role": role, "singleOrg": true})
	return nil
}

// SingleOrgInfo is what the app shows in single-organization mode.
type SingleOrgInfo struct {
	ID   string `json:"id"`
	Slug string `json:"slug"`
	Name string `json:"name"`
}

// SingleOrgInfo returns the instance's organization, nil outside the mode.
func (s *Service) SingleOrgInfo(ctx context.Context) *SingleOrgInfo {
	o, ok := s.TheOrg(ctx)
	if !ok {
		return nil
	}
	return &SingleOrgInfo{ID: o.ID, Slug: o.Slug, Name: o.Name}
}
