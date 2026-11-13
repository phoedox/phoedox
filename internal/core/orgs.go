package core

import (
	"context"
	"errors"
	"strings"

	"github.com/phoedox/phoedox/internal/graph"
	"github.com/phoedox/phoedox/internal/store"
)

// OrgInput creates or changes an organization.
type OrgInput struct {
	Name       string `json:"name,omitempty"`
	Slug       string `json:"slug,omitempty"`
	UsageType  string `json:"usageType,omitempty"`  // system admins only
	AIEnabled  *bool  `json:"aiEnabled,omitempty"`  // org admins
	QuotaMB    *int   `json:"quotaMb,omitempty"`    // org admins, below the instance limit
	AdminEmail string `json:"adminEmail,omitempty"` // first admin when creating (default: creator)
}

// OrgDetail is an organization with members and groups (for its members).
type OrgDetail struct {
	store.Org
	Members []store.OrgMember `json:"members"`
	Groups  []store.Group     `json:"groups"`
}

func (s *Service) fullUser(a Actor) error {
	if a.Via == "mcp" || a.Restricted() {
		return ErrForbidden
	}
	return nil
}

// org loads an organization; need is "" (any member), member or admin.
// System admins may do everything.
func (s *Service) org(ctx context.Context, a Actor, ref, need string) (store.Org, error) {
	o, err := s.DB.OrgFor(ctx, ref, a.User.ID)
	if err != nil {
		return o, err
	}
	if a.User.IsAdmin {
		if o.Role == "" {
			o.Role = store.OrgAdmin
		}
		return o, nil
	}
	if o.Role == "" {
		return o, ErrNotFound
	}
	if need == store.OrgAdmin && o.Role != store.OrgAdmin {
		return o, ErrForbidden
	}
	return o, nil
}

// Orgs lists the actor's organizations (all for system admins).
func (s *Service) Orgs(ctx context.Context, a Actor) ([]store.Org, error) {
	if a.Via == "mcp" {
		return nil, ErrForbidden
	}
	os, err := s.DB.ListOrgs(ctx, a.User.ID, a.User.IsAdmin)
	if err != nil {
		return nil, err
	}
	if a.User.IsAdmin {
		for i := range os {
			if os[i].Role == "" {
				os[i].Role = store.OrgAdmin
			}
		}
	}
	return os, nil
}

// CreateOrg creates an organization (system admins only).
func (s *Service) CreateOrg(ctx context.Context, a Actor, in OrgInput) (store.Org, error) {
	if !a.User.IsAdmin || s.fullUser(a) != nil {
		return store.Org{}, ErrForbidden
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len(in.Name) > 100 {
		return store.Org{}, invalid("validation.name_required", nil, "name is required")
	}
	if in.Slug == "" {
		in.Slug = graph.Slugify(in.Name)
	}
	if err := graph.ValidateSlug(in.Slug); err != nil {
		return store.Org{}, invalid("validation.slug", nil, "slug: %v", err)
	}
	if in.UsageType == "" {
		in.UsageType = "commercial"
	}
	if in.UsageType != "commercial" && in.UsageType != "noncommercial" {
		return store.Org{}, invalid("validation.usage_type", nil, "usage type must be commercial or noncommercial")
	}
	admin := a.User.ID
	if in.AdminEmail != "" {
		u, err := s.DB.UserByEmail(ctx, strings.TrimSpace(in.AdminEmail))
		if err != nil {
			return store.Org{}, invalid("validation.user_not_found", map[string]any{"email": in.AdminEmail}, "no user with email %s", in.AdminEmail)
		}
		admin = u.ID
	}
	o, err := s.DB.CreateOrg(ctx, store.Org{ID: newID(), Name: in.Name, Slug: in.Slug, UsageType: in.UsageType}, admin)
	if errors.Is(err, store.ErrConflict) {
		return o, invalid("validation.slug_taken", nil, "slug is taken")
	}
	if err != nil {
		return o, err
	}
	s.event(ctx, a, "org.create", o.ID, in)
	return o, nil
}

// Org returns an organization with members and groups (members only).
func (s *Service) Org(ctx context.Context, a Actor, ref string) (OrgDetail, error) {
	if a.Via == "mcp" {
		return OrgDetail{}, ErrForbidden
	}
	o, err := s.org(ctx, a, ref, "")
	if err != nil {
		return OrgDetail{}, err
	}
	ms, err := s.DB.OrgMembers(ctx, o.ID)
	if err != nil {
		return OrgDetail{}, err
	}
	gs, err := s.DB.Groups(ctx, o.ID)
	return OrgDetail{Org: o, Members: ms, Groups: gs}, err
}

// UpdateOrg changes name, AI switch and quota (org admins) and the usage
// type (system admins only).
func (s *Service) UpdateOrg(ctx context.Context, a Actor, ref string, in OrgInput) (store.Org, error) {
	if err := s.fullUser(a); err != nil {
		return store.Org{}, err
	}
	o, err := s.org(ctx, a, ref, store.OrgAdmin)
	if err != nil {
		return o, err
	}
	if n := strings.TrimSpace(in.Name); n != "" {
		o.Name = n
	}
	if in.UsageType != "" && in.UsageType != o.UsageType {
		if !a.User.IsAdmin {
			return o, ErrForbidden
		}
		if in.UsageType != "commercial" && in.UsageType != "noncommercial" {
			return o, invalid("validation.usage_type", nil, "usage type must be commercial or noncommercial")
		}
		o.UsageType = in.UsageType
	}
	if in.AIEnabled != nil {
		o.AIEnabled = *in.AIEnabled
	}
	if in.QuotaMB != nil {
		max := s.instanceQuotaMB(ctx)
		if *in.QuotaMB < 0 || (max > 0 && *in.QuotaMB > max) {
			return o, invalid("validation.quota_above_max", map[string]any{"max": max}, "the quota must be between 0 and %d MB", max)
		}
		o.QuotaMB = *in.QuotaMB
	}
	if err := s.DB.UpdateOrg(ctx, o); err != nil {
		return o, err
	}
	s.event(ctx, a, "org.update", o.ID, in)
	return o, nil
}

// SetOrgMember adds a user (by email) or changes their role (org admins).
func (s *Service) SetOrgMember(ctx context.Context, a Actor, ref, email, role string) error {
	if err := s.fullUser(a); err != nil {
		return err
	}
	o, err := s.org(ctx, a, ref, store.OrgAdmin)
	if err != nil {
		return err
	}
	if role != store.OrgAdmin && role != store.OrgMemberRole {
		return invalid("validation.role", nil, "role must be admin or member")
	}
	u, err := s.DB.UserByEmail(ctx, strings.TrimSpace(email))
	if err != nil {
		return invalid("validation.user_not_found", map[string]any{"email": email}, "no user with email %s", email)
	}
	if role != store.OrgAdmin {
		if err := s.keepOrgAdmin(ctx, o.ID, u.ID); err != nil {
			return err
		}
	}
	if err := s.DB.SetOrgMember(ctx, o.ID, u.ID, role); err != nil {
		return err
	}
	s.event(ctx, a, "org.member", o.ID, map[string]string{"user": u.ID, "role": role})
	return nil
}

// RemoveOrgMember removes a user from an organization and its groups.
func (s *Service) RemoveOrgMember(ctx context.Context, a Actor, ref, userID string) error {
	if err := s.fullUser(a); err != nil {
		return err
	}
	o, err := s.org(ctx, a, ref, store.OrgAdmin)
	if err != nil {
		return err
	}
	if err := s.keepOrgAdmin(ctx, o.ID, userID); err != nil {
		return err
	}
	if err := s.DB.RemoveOrgMember(ctx, o.ID, userID); err != nil {
		return err
	}
	s.event(ctx, a, "org.member_remove", o.ID, map[string]string{"user": userID})
	return nil
}

func (s *Service) keepOrgAdmin(ctx context.Context, orgID, userID string) error {
	ms, err := s.DB.OrgMembers(ctx, orgID)
	if err != nil {
		return err
	}
	for _, m := range ms {
		if m.UserID == userID && m.Role == store.OrgAdmin {
			if n, err := s.DB.CountOrgAdmins(ctx, orgID); err != nil || n <= 1 {
				return invalid("validation.last_org_admin", nil, "an organization needs at least one admin")
			}
		}
	}
	return nil
}

// GroupInput creates or changes a group.
type GroupInput struct {
	Name      string `json:"name"`
	OIDCGroup string `json:"oidcGroup"`
}

// SaveGroup creates (id "") or changes a group (org admins).
func (s *Service) SaveGroup(ctx context.Context, a Actor, ref, id string, in GroupInput) (store.Group, error) {
	if err := s.fullUser(a); err != nil {
		return store.Group{}, err
	}
	o, err := s.org(ctx, a, ref, store.OrgAdmin)
	if err != nil {
		return store.Group{}, err
	}
	in.Name, in.OIDCGroup = strings.TrimSpace(in.Name), strings.TrimSpace(in.OIDCGroup)
	if in.Name == "" || len(in.Name) > 100 || len(in.OIDCGroup) > 200 {
		return store.Group{}, invalid("validation.name_required", nil, "name is required")
	}
	g := store.Group{ID: id, OrgID: o.ID, Name: in.Name, OIDCGroup: in.OIDCGroup}
	if id == "" {
		g.ID = newID()
	} else if cur, err := s.DB.GetGroup(ctx, id); err != nil || cur.OrgID != o.ID {
		return g, ErrNotFound
	}
	if err := s.DB.SaveGroup(ctx, g); errors.Is(err, store.ErrConflict) {
		return g, invalid("validation.group_taken", nil, "a group with this name exists")
	} else if err != nil {
		return g, err
	}
	s.event(ctx, a, "group.save", g.ID, in)
	return g, nil
}

// DeleteGroup deletes a group with its grants (org admins).
func (s *Service) DeleteGroup(ctx context.Context, a Actor, ref, id string) error {
	g, err := s.groupIn(ctx, a, ref, id)
	if err != nil {
		return err
	}
	if err := s.DB.DeleteGroup(ctx, g.ID); err != nil {
		return err
	}
	s.event(ctx, a, "group.delete", g.ID, nil)
	return nil
}

// SetGroupMember adds or removes an organization member (org admins).
func (s *Service) SetGroupMember(ctx context.Context, a Actor, ref, id, email string, on bool) error {
	g, err := s.groupIn(ctx, a, ref, id)
	if err != nil {
		return err
	}
	u, err := s.DB.UserByEmail(ctx, strings.TrimSpace(email))
	if err != nil {
		return invalid("validation.user_not_found", map[string]any{"email": email}, "no user with email %s", email)
	}
	if on {
		ms, err := s.DB.OrgMembers(ctx, g.OrgID)
		if err != nil {
			return err
		}
		member := false
		for _, m := range ms {
			member = member || m.UserID == u.ID
		}
		if !member {
			return invalid("validation.not_org_member", map[string]any{"email": email}, "%s is not a member of the organization", email)
		}
	}
	if err := s.DB.SetGroupMember(ctx, g.ID, u.ID, "manual", on); err != nil {
		return err
	}
	s.event(ctx, a, "group.member", g.ID, map[string]any{"user": u.ID, "on": on})
	return nil
}

func (s *Service) groupIn(ctx context.Context, a Actor, ref, id string) (store.Group, error) {
	if err := s.fullUser(a); err != nil {
		return store.Group{}, err
	}
	o, err := s.org(ctx, a, ref, store.OrgAdmin)
	if err != nil {
		return store.Group{}, err
	}
	g, err := s.DB.GetGroup(ctx, id)
	if err != nil || g.OrgID != o.ID {
		return g, ErrNotFound
	}
	return g, nil
}

// ProjectGroups lists the groups with access to a project (readers).
func (s *Service) ProjectGroups(ctx context.Context, a Actor, ref string) ([]store.ProjectGroup, error) {
	p, err := s.project(ctx, a, ref, store.RoleReader)
	if err != nil {
		return nil, err
	}
	return s.DB.ProjectGroups(ctx, p.ID)
}

// SetProjectGroup gives a group of the project's organization a role ("" removes it; owners).
func (s *Service) SetProjectGroup(ctx context.Context, a Actor, ref, groupID, role string) error {
	if err := s.fullUser(a); err != nil {
		return err
	}
	p, err := s.project(ctx, a, ref, store.RoleOwner)
	if err != nil {
		return err
	}
	g, err := s.DB.GetGroup(ctx, groupID)
	if err != nil || g.OrgID == "" || g.OrgID != p.OrgID {
		return invalid("validation.group_org", nil, "only groups of the project's organization")
	}
	if role != "" && role != store.RoleReader && role != store.RoleEditor && role != store.RoleOwner {
		return invalid("validation.role", nil, "role must be reader, editor or owner")
	}
	if err := s.DB.SetProjectGroup(ctx, p.ID, g.ID, role); err != nil {
		return err
	}
	s.event(ctx, a, "project.group", p.ID, map[string]string{"group": g.ID, "role": role})
	return nil
}

// SyncOIDCGroups applies the groups an identity provider reports for a user.
func (s *Service) SyncOIDCGroups(ctx context.Context, userID string, groups []string) error {
	return s.DB.SyncOIDCGroups(ctx, userID, groups)
}
