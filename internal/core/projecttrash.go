package core

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/phoedox/phoedox/internal/store"
)

// Deleting a project (ADR-0071): the project goes to the trash for the
// trash period (PHOEDOX_TRASH_DAYS, default 30). Meanwhile it is invisible,
// its background work (checks, rules, Git sync, plugins, agents) stops and
// share links no longer open; owners can restore it. After the period a
// housekeeping run deletes it with everything in it. AI clients cannot
// delete projects.

// TrashedProjectInfo is a deleted project in the restore list.
type TrashedProjectInfo struct {
	store.TrashedProject
	// PurgeAt is when it is deleted for good (zero = kept until restored).
	PurgeAt *time.Time `json:"purgeAt,omitempty"`
}

func (s *Service) purgeAt(t store.TrashedProject) *time.Time {
	if s.TrashDays <= 0 {
		return nil
	}
	at := t.DeletedAt.Time.Add(time.Duration(s.TrashDays) * 24 * time.Hour)
	return &at
}

// DeleteProject moves a project to the trash. Only owners may do it, never
// over MCP; confirm must be the project's address (slug).
func (s *Service) DeleteProject(ctx context.Context, a Actor, ref, confirm string) error {
	if a.Via == "mcp" {
		return ErrForbidden
	}
	p, err := s.project(ctx, a, ref, store.RoleOwner)
	if err != nil {
		return err
	}
	if strings.TrimSpace(confirm) != p.Slug {
		return invalid("validation.project_delete_confirm", map[string]any{"slug": p.Slug}, "type %q to confirm", p.Slug)
	}
	if err := s.DB.TrashProject(ctx, p.ID, a.User.ID); err != nil {
		return err
	}
	s.event(ctx, a, "project.delete", p.ID, map[string]any{"project": p.Slug, "days": s.TrashDays})
	return nil
}

// TrashedProjects lists the deleted projects the actor may restore (system
// admins on the web see all of the instance).
func (s *Service) TrashedProjects(ctx context.Context, a Actor) ([]TrashedProjectInfo, error) {
	if a.Via == "mcp" {
		return nil, ErrForbidden
	}
	list, err := s.DB.TrashedProjects(ctx, a.User.ID, a.User.IsAdmin && !a.Restricted())
	if err != nil {
		return nil, err
	}
	out := make([]TrashedProjectInfo, len(list))
	for i, t := range list {
		out[i] = TrashedProjectInfo{TrashedProject: t, PurgeAt: s.purgeAt(t)}
	}
	return out, nil
}

// RestoreProject takes a project out of the trash (owners and system admins).
func (s *Service) RestoreProject(ctx context.Context, a Actor, ref string) (store.Project, error) {
	if a.Via == "mcp" {
		return store.Project{}, ErrForbidden
	}
	t, err := s.DB.TrashedProjectFor(ctx, ref, a.User.ID)
	if err != nil {
		return t.Project, err
	}
	if !(a.User.IsAdmin && !a.Restricted()) && t.Role != store.RoleOwner {
		return t.Project, ErrNotFound
	}
	// The address stays reserved while the project is in the trash, so
	// restoring cannot collide with a newer project.
	if err := s.DB.RestoreProject(ctx, t.ID); err != nil {
		return t.Project, err
	}
	s.event(ctx, a, "project.restore", t.ID, map[string]any{"project": t.Slug})
	return s.DB.ProjectFor(ctx, t.ID, a.User.ID)
}

// PurgeDeletedProjects deletes projects whose trash period is over
// (housekeeping, all tenants).
func (s *Service) PurgeDeletedProjects(ctx context.Context, now time.Time) {
	if s.TrashDays <= 0 {
		return
	}
	list, err := s.DB.ExpiredTrashedProjects(ctx, store.Time{Time: now.Add(-time.Duration(s.TrashDays) * 24 * time.Hour)})
	if err != nil {
		slog.Warn("listing deleted projects failed", "err", err)
		return
	}
	for _, e := range list {
		tctx := store.WithTenant(ctx, e.TenantID)
		hashes, err := s.DB.PurgeProject(tctx, e.ID)
		if err != nil {
			slog.Warn("purging a deleted project failed", "project", e.Slug, "err", err)
			continue
		}
		s.freeBlobs(tctx, hashes)
		_ = s.DB.AddEvent(tctx, "system", "project.purge", e.ID, map[string]any{"project": e.Slug, "days": s.TrashDays})
		slog.Info("deleted project purged", "project", e.Slug)
	}
}
