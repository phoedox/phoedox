package core

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/phoedox/phoedox/internal/store"
)

// Maintenance plans: recurring tasks per entry (filter changes, battery
// swaps, certificate renewals). Phoedox puts a card on the plan's board
// LeadDays before it is due; when the card is done, the next date follows
// from the day it was done.

// MaintenanceInput creates or changes a plan.
type MaintenanceInput struct {
	Entity      string `json:"entity"` // slug or id
	Board       string `json:"board"`  // slug or id of a board
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Every       int    `json:"every"`
	Unit        string `json:"unit"` // day | week | month | year
	LeadDays    *int   `json:"leadDays,omitempty"`
	Assignee    string `json:"assignee,omitempty"`
	NextDue     string `json:"nextDue,omitempty"` // YYYY-MM-DD, default today
}

// advance moves a date by a plan's interval.
func advance(t time.Time, every int, unit string) time.Time {
	switch unit {
	case "week":
		return t.AddDate(0, 0, 7*every)
	case "month":
		return t.AddDate(0, every, 0)
	case "year":
		return t.AddDate(every, 0, 0)
	}
	return t.AddDate(0, 0, every)
}

// MaintenancePlans lists the plans of a project or one entry (readers).
func (s *Service) MaintenancePlans(ctx context.Context, a Actor, ref, entRef string) ([]store.MaintenancePlan, error) {
	p, err := s.project(ctx, a, ref, store.RoleReader)
	if err != nil {
		return nil, err
	}
	entityID := ""
	if entRef != "" {
		e, err := s.DB.GetEntity(ctx, p.ID, entRef)
		if err != nil {
			return nil, err
		}
		entityID = e.ID
	}
	return s.DB.MaintenancePlans(ctx, p.ID, entityID)
}

// SaveMaintenancePlan creates (id "") or changes a plan (editors).
func (s *Service) SaveMaintenancePlan(ctx context.Context, a Actor, ref, id string, in MaintenanceInput) (store.MaintenancePlan, error) {
	p, err := s.project(ctx, a, ref, store.RoleEditor)
	if err != nil {
		return store.MaintenancePlan{}, err
	}
	if a.Via == "mcp" && p.AIWriteMode != AIDirect {
		return store.MaintenancePlan{}, invalid("validation.ai_tasks_direct", nil, "AI clients change tasks only in projects where AI writes directly")
	}
	var plan store.MaintenancePlan
	if id != "" {
		if plan, err = s.DB.GetMaintenancePlan(ctx, p.ID, id); err != nil {
			return plan, err
		}
	} else {
		plan = store.MaintenancePlan{ID: newID(), ProjectID: p.ID, LeadDays: 7, CreatedBy: a.User.ID, CreatedAt: store.Now()}
		e, err := s.DB.GetEntity(ctx, p.ID, in.Entity)
		if err != nil {
			return plan, invalid("validation.entity_not_found", map[string]any{"entity": in.Entity}, "unknown entry %q", in.Entity)
		}
		plan.EntityID = e.ID
	}
	plan.Title, plan.Description = strings.TrimSpace(in.Title), strings.TrimSpace(in.Description)
	if plan.Title == "" {
		return plan, invalid("validation.title_required", nil, "title is required")
	}
	if in.Every < 1 || in.Every > 1000 || !slices.Contains(store.MaintenanceUnits, in.Unit) {
		return plan, invalid("validation.maintenance_interval", nil, "interval must be 1 to 1000 days, weeks, months or years")
	}
	plan.Every, plan.Unit = in.Every, in.Unit
	if in.LeadDays != nil {
		plan.LeadDays = min(max(*in.LeadDays, 0), 365)
	}
	_, board, err := s.boardAccess(ctx, a, p.Slug, in.Board, store.RoleEditor)
	if err != nil {
		return plan, err
	}
	plan.BoardID = board.ID
	if _, _, err := s.assignee(ctx, p, in.Assignee); err != nil {
		return plan, err
	}
	plan.Assignee = strings.TrimSpace(in.Assignee)
	if in.NextDue != "" {
		if err := validDue(in.NextDue); err != nil {
			return plan, err
		}
		plan.NextDue = in.NextDue
	} else if plan.NextDue == "" {
		plan.NextDue = time.Now().UTC().Format(time.DateOnly)
	}
	if err := s.DB.SaveMaintenancePlan(ctx, plan); err != nil {
		return plan, err
	}
	s.event(ctx, a, "maintenance.save", plan.ID, map[string]any{"entity": plan.EntityID, "title": plan.Title, "every": plan.Every, "unit": plan.Unit})
	plan, _ = s.DB.GetMaintenancePlan(ctx, p.ID, plan.ID)
	s.runPlan(ctx, plan, time.Now())
	return s.DB.GetMaintenancePlan(ctx, p.ID, plan.ID)
}

// DeleteMaintenancePlan removes a plan; its cards stay on the board (editors).
func (s *Service) DeleteMaintenancePlan(ctx context.Context, a Actor, ref, id string) error {
	p, err := s.project(ctx, a, ref, store.RoleEditor)
	if err != nil {
		return err
	}
	if a.Via == "mcp" && p.AIWriteMode != AIDirect {
		return invalid("validation.ai_tasks_direct", nil, "AI clients change tasks only in projects where AI writes directly")
	}
	if _, err := s.DB.GetMaintenancePlan(ctx, p.ID, id); err != nil {
		return err
	}
	if err := s.DB.DeleteMaintenancePlan(ctx, p.ID, id); err != nil {
		return err
	}
	s.event(ctx, a, "maintenance.delete", id, nil)
	return nil
}

// RunMaintenance creates due cards and moves plans on (System context, all tenants).
func (s *Service) RunMaintenance(ctx context.Context, now time.Time) {
	plans, err := s.DB.MaintenancePlans(store.System(ctx), "", "")
	if err != nil {
		slog.Warn("maintenance plans", "err", err)
		return
	}
	for _, plan := range plans {
		s.runPlan(store.WithTenant(ctx, plan.TenantID), plan, now)
	}
}

// runPlan checks one plan: a finished card sets the next date, a due date
// without an open card creates one. Cards are created by the plan's author.
func (s *Service) runPlan(ctx context.Context, plan store.MaintenancePlan, now time.Time) {
	changed := false
	if plan.OpenTaskID != "" {
		t, err := s.DB.GetTask(ctx, plan.ProjectID, plan.OpenTaskID)
		switch {
		case errors.Is(err, store.ErrNotFound):
			// The card was deleted: this round is skipped.
			if due, err := time.Parse(time.DateOnly, plan.NextDue); err == nil {
				plan.NextDue = advance(due, plan.Every, plan.Unit).Format(time.DateOnly)
			}
			plan.OpenTaskID, changed = "", true
		case err != nil:
			return
		case !t.DoneAt.IsZero():
			plan.LastDone = t.DoneAt
			plan.NextDue = advance(t.DoneAt.UTC(), plan.Every, plan.Unit).Format(time.DateOnly)
			plan.OpenTaskID, changed = "", true
		}
	}
	due, err := time.Parse(time.DateOnly, plan.NextDue)
	if err == nil && plan.OpenTaskID == "" && !due.AddDate(0, 0, -plan.LeadDays).After(now) {
		if id := s.planTask(ctx, plan); id != "" {
			plan.OpenTaskID, changed = id, true
		}
	}
	if changed {
		if err := s.DB.SaveMaintenancePlan(ctx, plan); err != nil {
			slog.Warn("maintenance plan", "plan", plan.ID, "err", err)
		}
	}
}

func (s *Service) planTask(ctx context.Context, plan store.MaintenancePlan) string {
	u, err := s.DB.UserByID(ctx, plan.CreatedBy)
	if err != nil || u.Disabled {
		return ""
	}
	a := Actor{User: u, Via: "web"}
	p, err := s.DB.ProjectFor(ctx, plan.ProjectID, u.ID)
	if err != nil {
		return ""
	}
	desc := plan.Description
	if desc != "" {
		desc += "\n\n"
	}
	desc += "@" + plan.EntitySlug
	t, err := s.CreateTask(ctx, a, p.Slug, plan.BoardID, TaskInput{Title: plan.Title + ": " + plan.EntityName, Description: desc, Due: plan.NextDue,
		Assignee: plan.Assignee, Labels: []string{"maintenance"}})
	if err != nil {
		slog.Info("maintenance task", "plan", plan.ID, "err", err)
		return ""
	}
	s.event(ctx, a, "maintenance.task", plan.ID, map[string]string{"task": t.ID, "due": plan.NextDue})
	return t.ID
}
