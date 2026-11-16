package core

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/phoedox/phoedox/internal/store"
)

func TestUsageReports(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, stranger := setup(t)
	admin := Actor{User: store.User{ID: owner.User.ID, Name: owner.User.Name, IsAdmin: true}, Via: "web"}
	o, err := s.CreateOrg(ctx, admin, OrgInput{Name: "Firma"})
	if err != nil {
		t.Fatal(err)
	}
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Shop", Org: &o.Slug})
	q, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Home"})
	_ = s.SetMember(ctx, owner, p.Slug, "reader@x.de", store.RoleEditor)
	use := func(org, user, project, model string, charge int64, own bool) {
		_ = s.DB.AddAIUsage(ctx, store.AIUsage{ID: newID(), OrgID: org, UserID: user, ProjectID: project, Model: model, InputTokens: 100, OutputTokens: 50,
			CostMicros: charge / 2, ChargeMicros: charge, OwnKey: own, CreatedAt: store.Now()})
	}
	use(o.ID, owner.User.ID, p.ID, "m1", 1_000_000, false) // $1.00
	use(o.ID, reader.User.ID, p.ID, "m2", 500_000, false)  // $0.50
	use("", owner.User.ID, q.ID, "m1", 250_000, false)     // $0.25, personal project
	_ = s.DB.AddAIUsage(ctx, store.AIUsage{ID: newID(), OrgID: o.ID, UserID: owner.User.ID, ProjectID: p.ID, Model: "m1", ChargeMicros: 9_000_000,
		CreatedAt: store.Time{Time: time.Now().AddDate(0, -2, 0)}}) // last months do not count

	pr, err := s.ProjectUsage(ctx, owner, p.Slug)
	if err != nil || pr.ChargedCents != 150 || len(pr.ByUser) != 2 || pr.ByUser[0].ChargedCents != 100 || len(pr.ByModel) != 2 || len(pr.Storage) != 1 {
		t.Fatalf("project usage: %v %+v", err, pr)
	}
	// Owner of the project and admin of the organization: a link up.
	if len(pr.Up) != 1 || pr.Up[0].Level != "org" || pr.Up[0].Href != "/orgs/"+o.Slug+"/usage" {
		t.Fatalf("up links: %+v", pr.Up)
	}
	if _, err := s.ProjectUsage(ctx, reader, p.Slug); !errors.Is(err, ErrForbidden) && !errors.Is(err, ErrNotFound) {
		t.Fatalf("editors must not see project usage: %v", err)
	}
	or, err := s.OrgUsage(ctx, owner, o.Slug)
	if err != nil || or.ChargedCents != 150 || len(or.ByProject) != 1 || or.ByProject[0].Name != "Shop" || len(or.Budgets) == 0 {
		t.Fatalf("org usage: %v %+v", err, or)
	}
	if _, err := s.OrgUsage(ctx, stranger, o.Slug); err == nil {
		t.Fatal("strangers must not see org usage")
	}
	if _, err := s.InstanceUsage(ctx, owner); !errors.Is(err, ErrForbidden) {
		t.Fatalf("instance usage for non-admins: %v", err)
	}
	ir, err := s.InstanceUsage(ctx, admin)
	if err != nil || ir.ChargedCents != 175 || ir.CostCents != 88 || len(ir.ByOrg) != 2 {
		t.Fatalf("instance usage: %v %+v", err, ir)
	}
	me, err := s.MyUsage(ctx, owner)
	if err != nil || me.ChargedCents != 125 || len(me.ByProject) != 2 || len(me.Budgets) != 2 || me.Budgets[0].UsedCents != 25 || me.Budgets[1].UsedCents != 100 {
		t.Fatalf("my usage: %v %+v", err, me)
	}
}
