package core

import (
	"context"
	"log/slog"
	"strings"

	"github.com/phoedox/phoedox/internal/mail"
	"github.com/phoedox/phoedox/internal/store"
	"github.com/phoedox/phoedox/resources"
)

// Account deletion (ADR-0055): the account row stays, anonymised, so that
// content in shared projects keeps an author shown as "deleted person".
// Deletion is blocked while the person is the only owner of an
// organization or project that has other members; own projects without
// other members are deleted with the account.

// deletedName is the name shown for deleted accounts, in the instance's
// default language (it is stored, not translated per viewer).
var deletedName = map[string]string{"de": "Gelöschte Person", "en": "Deleted person"}

// DeletionRef names a project or organization in the deletion plan.
type DeletionRef struct {
	Kind string `json:"kind"` // project | org | org_projects | last_admin
	Slug string `json:"slug,omitempty"`
	Name string `json:"name,omitempty"`
}

// DeletionPlan is what deleting an account would do.
type DeletionPlan struct {
	// Blockers must be solved first (transfer ownership, add an admin).
	Blockers []DeletionRef `json:"blockers"`
	// Deletes are own projects and organizations without other members.
	Deletes []DeletionRef `json:"deletes"`
	// BackupDays: how long the data can still be in backups.
	BackupDays int `json:"backupDays"`

	transfers map[string]string
	projects  []store.OwnedProject
	orgs      []store.AdminOrg
}

func (s *Service) planDeletion(ctx context.Context, u store.User) (DeletionPlan, error) {
	p := DeletionPlan{Blockers: []DeletionRef{}, Deletes: []DeletionRef{}, BackupDays: s.BackupDays, transfers: map[string]string{}}
	if u.IsAdmin {
		n, err := s.DB.CountActiveAdmins(ctx)
		if err != nil {
			return p, err
		}
		if n <= 1 {
			p.Blockers = append(p.Blockers, DeletionRef{Kind: "last_admin"})
		}
	}
	owned, err := s.DB.OwnedProjects(ctx, u.ID, store.Now())
	if err != nil {
		return p, err
	}
	gone := map[string]bool{}
	perOrg := map[string]int{}
	for _, op := range owned {
		switch {
		case op.OtherOwner != "":
			if op.OwnerID == u.ID {
				p.transfers[op.ID] = op.OtherOwner
			}
		case op.OtherMembers > 0:
			p.Blockers = append(p.Blockers, DeletionRef{Kind: "project", Slug: op.Slug, Name: op.Name})
		default:
			p.projects = append(p.projects, op)
			p.Deletes = append(p.Deletes, DeletionRef{Kind: "project", Slug: op.Slug, Name: op.Name})
			gone[op.ID] = true
			if op.OrgID != "" {
				perOrg[op.OrgID]++
			}
		}
	}
	orgs, err := s.DB.AdminOrgs(ctx, u.ID)
	if err != nil {
		return p, err
	}
	for _, o := range orgs {
		switch {
		case o.OtherMembers > 0 && o.OtherAdmins == 0:
			p.Blockers = append(p.Blockers, DeletionRef{Kind: "org", Slug: o.Slug, Name: o.Name})
		case o.OtherMembers == 0:
			n, err := s.DB.OrgProjectCount(ctx, o.ID)
			if err != nil {
				return p, err
			}
			if n > perOrg[o.ID] {
				p.Blockers = append(p.Blockers, DeletionRef{Kind: "org_projects", Slug: o.Slug, Name: o.Name})
				continue
			}
			p.orgs = append(p.orgs, o)
			p.Deletes = append(p.Deletes, DeletionRef{Kind: "org", Slug: o.Slug, Name: o.Name})
		}
	}
	return p, nil
}

// AccountDeletion returns what deleting the signed-in account would do.
func (s *Service) AccountDeletion(ctx context.Context, a Actor) (DeletionPlan, error) {
	if err := ownAccount(a); err != nil {
		return DeletionPlan{}, err
	}
	return s.planDeletion(ctx, a.User)
}

// DeleteAccount deletes and anonymises an account. The caller checked who
// may do this (the person after confirming, or the CLI).
func (s *Service) DeleteAccount(ctx context.Context, a Actor, userID string) (DeletionPlan, error) {
	u, err := s.DB.UserByID(ctx, userID)
	if err != nil || u.Deleted {
		return DeletionPlan{}, ErrNotFound
	}
	plan, err := s.planDeletion(ctx, u)
	if err != nil {
		return plan, err
	}
	if len(plan.Blockers) > 0 {
		names := make([]string, 0, len(plan.Blockers))
		for _, b := range plan.Blockers {
			if b.Name != "" {
				names = append(names, b.Name)
			} else {
				names = append(names, b.Kind)
			}
		}
		return plan, invalid("validation.account_delete_blocked", map[string]any{"names": strings.Join(names, ", ")}, "transfer ownership first: %s", strings.Join(names, ", "))
	}
	// The event carries no personal data; the actor is the pseudonymous id.
	s.event(ctx, a, "user.delete", u.ID, map[string]int{"projects": len(plan.projects), "orgs": len(plan.orgs)})
	for _, p := range plan.projects {
		hashes, err := s.DB.PurgeProject(ctx, p.ID)
		if err != nil {
			return plan, err
		}
		s.freeBlobs(ctx, hashes)
	}
	for _, o := range plan.orgs {
		if err := s.DB.DeleteOrg(ctx, o.ID); err != nil {
			return plan, err
		}
	}
	exports, err := s.DB.UserExports(ctx, u.ID)
	if err != nil {
		return plan, err
	}
	name := deletedName[s.DefaultLocale]
	if name == "" {
		name = deletedName["en"]
	}
	if err := s.DB.AnonymizeUser(ctx, store.Anonymize{
		UserID: u.ID, Email: "deleted-" + strings.ToLower(u.ID) + "@deleted.invalid", Name: name, NewOwners: plan.transfers, At: store.Now(),
	}); err != nil {
		return plan, err
	}
	var hashes []string
	for _, e := range exports {
		if e.SHA256 != "" {
			hashes = append(hashes, e.SHA256)
		}
	}
	s.freeBlobs(ctx, hashes)
	s.mailDeleted(ctx, u, plan.BackupDays)
	return plan, nil
}

func (s *Service) mailDeleted(ctx context.Context, u store.User, backupDays int) {
	if s.Mail == nil || !s.Mail.Enabled() || mail.Undeliverable(u.Email) {
		return
	}
	subject, body, err := resources.Mail("account_deleted", s.locale(u), map[string]any{"Name": u.Name, "BackupDays": backupDays})
	if err == nil {
		err = s.Mail.Send(ctx, mail.Message{To: u.Email, Subject: subject, Body: body})
	}
	if err != nil {
		slog.Warn("account deletion mail failed", "user", u.ID, "err", err)
	}
}
