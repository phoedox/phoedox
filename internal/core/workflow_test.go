package core

import (
	"context"
	"testing"
	"time"

	"github.com/phoedox/phoedox/internal/store"
)

func TestApprovalWorkflow(t *testing.T) {
	ctx := context.Background()
	s, owner, editor, _ := setup(t)
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Ops"})
	_ = s.SetMember(ctx, owner, p.Slug, "reader@x.de", store.RoleEditor)
	page, _ := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "markdown", Title: "Runbook", Markdown: "Restart the service."})
	slug := page.Document.Slug

	if err := s.SetApproval(ctx, editor, p.Slug, true); err == nil {
		t.Fatal("editor switched approval on")
	}
	if err := s.SetApproval(ctx, owner, p.Slug, true); err != nil {
		t.Fatal(err)
	}
	// A direct change by an editor lands in a draft.
	res, err := s.UpdateDocument(ctx, editor, p.Slug, slug, DocumentPatch{Markdown: strp("Restart the service twice.")})
	if err != nil || res.Draft == nil {
		t.Fatalf("editor change: %+v %v", res, err)
	}
	if doc, _ := s.Document(ctx, owner, p.Slug, slug); markdownOf(doc) != "Restart the service.\n" && markdownOf(doc) != "Restart the service." {
		t.Fatalf("published changed: %q", markdownOf(doc))
	}
	d, _ := s.Draft(ctx, editor, p.Slug, slug)
	if !d.Approval || d.CanPublish || !d.CanEnd {
		t.Fatalf("editor draft info: %+v", d)
	}
	if _, err := s.PublishDraft(ctx, editor, p.Slug, slug); invalidKey(err) != "validation.approval_required" {
		t.Fatalf("editor published: %v", err)
	}
	if d, err = s.RequestReview(ctx, editor, p.Slug, slug, "please check"); err != nil || d.ReviewBy == nil {
		t.Fatalf("review: %+v %v", d, err)
	}
	if list, _ := s.Notifications(ctx, owner, false, 0); len(list.Items) == 0 || list.Items[0].Kind != "draft.review" {
		t.Fatalf("owner not notified: %+v", list.Items)
	}
	if _, err := s.RejectReview(ctx, editor, p.Slug, slug, "no"); err == nil {
		t.Fatal("editor rejected")
	}
	if d, err = s.RejectReview(ctx, owner, p.Slug, slug, "add the command"); err != nil || d.ReviewBy != nil || d.ReviewNote != "add the command" {
		t.Fatalf("reject: %+v %v", d, err)
	}
	_, _ = s.RequestReview(ctx, editor, p.Slug, slug, "")
	od, _ := s.Draft(ctx, owner, p.Slug, slug)
	if !od.CanPublish {
		t.Fatalf("owner cannot publish: %+v", od)
	}
	if _, err := s.PublishDraft(ctx, owner, p.Slug, slug); err != nil {
		t.Fatal(err)
	}
	if doc, _ := s.Document(ctx, owner, p.Slug, slug); markdownOf(doc) != "Restart the service twice.\n" {
		t.Fatalf("not published: %q", markdownOf(doc))
	}
	if list, _ := s.Notifications(ctx, editor, false, 0); len(list.Items) == 0 || list.Items[0].Kind != "draft.approved" {
		t.Fatalf("editor not told: %+v", list.Items)
	}
	// Owners change directly.
	if res, err := s.UpdateDocument(ctx, owner, p.Slug, slug, DocumentPatch{Markdown: strp("Owner text")}); err != nil || res.Draft != nil {
		t.Fatalf("owner change: %+v %v", res, err)
	}
}

func TestPageRules(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, _ := setup(t)
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Ops"})
	_ = s.SetMember(ctx, owner, p.Slug, "reader@x.de", store.RoleReader)
	page, _ := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "markdown", Title: "Safety", Markdown: "Wear gloves."})
	slug := page.Document.Slug

	days, yes := 30, true
	if _, err := s.SetPageRules(ctx, reader, p.Slug, slug, PageRulesInput{ReviewDays: &days}); err == nil {
		t.Fatal("reader set rules")
	}
	info, err := s.SetPageRules(ctx, owner, p.Slug, slug, PageRulesInput{ReviewDays: &days, RequiredReading: &yes})
	if err != nil || info.ReviewDue == "" || info.Overdue || !info.RequiredReading || len(info.Readers) == 0 {
		t.Fatalf("rules: %+v %v", info, err)
	}
	if list, _ := s.Notifications(ctx, reader, false, 0); len(list.Items) != 1 || list.Items[0].Kind != "page.required_reading" {
		t.Fatalf("reader not asked to read: %+v", list.Items)
	}
	r, _ := s.PageRules(ctx, reader, p.Slug, slug)
	if r.Confirmed || r.Readers != nil {
		t.Fatalf("reader view: %+v", r)
	}
	if r, err = s.ConfirmRead(ctx, reader, p.Slug, slug); err != nil || !r.Confirmed {
		t.Fatalf("confirm: %+v %v", r, err)
	}
	// A new version needs a new confirmation.
	_, _ = s.UpdateDocument(ctx, owner, p.Slug, slug, DocumentPatch{Markdown: strp("Wear gloves and goggles.")})
	if r, _ = s.PageRules(ctx, reader, p.Slug, slug); r.Confirmed {
		t.Fatal("still confirmed after a change")
	}

	// Review reminders: once, when due; a review resets the date.
	s.RemindReviews(ctx, time.Now())
	n := func() int {
		list, _ := s.Notifications(ctx, owner, false, 0)
		c := 0
		for _, it := range list.Items {
			if it.Kind == "page.review_due" {
				c++
			}
		}
		return c
	}
	if n() != 0 {
		t.Fatal("reminded early")
	}
	later := time.Now().AddDate(0, 0, 31)
	s.RemindReviews(ctx, later)
	s.RemindReviews(ctx, later.Add(time.Hour))
	if n() != 1 {
		t.Fatalf("reminders: %d", n())
	}
	if gaps, _ := s.DocumentationGaps(ctx, owner, p.Slug, 0); gaps.Counts["review_due"] != 0 {
		t.Fatalf("not yet due today: %v", gaps.Counts)
	}
	if r, err := s.MarkReviewed(ctx, owner, p.Slug, slug); err != nil || r.ReviewedBy == "" {
		t.Fatalf("reviewed: %+v %v", r, err)
	}
	bad := -1
	if _, err := s.SetPageRules(ctx, owner, p.Slug, slug, PageRulesInput{ReviewDays: &bad}); invalidKey(err) != "validation.review_days" {
		t.Fatalf("bad interval: %v", err)
	}
}

func TestMaintenancePlans(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, _ := setup(t)
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Home"})
	_ = s.SetMember(ctx, owner, p.Slug, "reader@x.de", store.RoleReader)
	_, _ = s.UpsertEntity(ctx, owner, p.ID, EntityInput{Kind: "device", Name: "Heat pump"})
	board, _ := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "board", Title: "Chores", Board: &Board{Columns: []BoardColumn{{ID: "todo", Name: "To do"}, {ID: "done", Name: "Done", Done: true}}}})

	in := MaintenanceInput{Entity: "heat-pump", Board: board.Document.Slug, Title: "Change filter", Every: 6, Unit: "month"}
	if _, err := s.SaveMaintenancePlan(ctx, reader, p.Slug, "", in); err == nil {
		t.Fatal("reader created a plan")
	}
	bad := in
	bad.Unit = "fortnight"
	if _, err := s.SaveMaintenancePlan(ctx, owner, p.Slug, "", bad); invalidKey(err) != "validation.maintenance_interval" {
		t.Fatalf("bad unit: %v", err)
	}
	// Due today: the card appears at once.
	plan, err := s.SaveMaintenancePlan(ctx, owner, p.Slug, "", in)
	if err != nil || plan.OpenTaskID == "" || plan.EntitySlug != "heat-pump" {
		t.Fatalf("plan: %+v %v", plan, err)
	}
	task, err := s.DB.GetTask(ctx, p.ID, plan.OpenTaskID)
	if err != nil || task.Title != "Change filter: Heat pump" || task.Due != plan.NextDue {
		t.Fatalf("task: %+v %v", task, err)
	}
	// Running again does not add a second card.
	s.RunMaintenance(ctx, time.Now())
	if open, _ := s.Tasks(ctx, owner, p.Slug, board.Document.Slug, "", "", true); len(open) != 1 {
		t.Fatalf("open cards: %d", len(open))
	}
	// Done: the next date is six months after the day it was done.
	if _, err := s.UpdateTask(ctx, owner, p.Slug, task.ID, TaskPatch{Column: strp("done")}); err != nil {
		t.Fatal(err)
	}
	s.RunMaintenance(ctx, time.Now())
	plans, _ := s.MaintenancePlans(ctx, reader, p.Slug, "heat-pump")
	want := time.Now().UTC().AddDate(0, 6, 0).Format(time.DateOnly)
	if len(plans) != 1 || plans[0].OpenTaskID != "" || plans[0].NextDue != want || plans[0].LastDone.IsZero() {
		t.Fatalf("after done: %+v (want %s)", plans, want)
	}
	// Five months later nothing is due yet, a week before it is.
	s.RunMaintenance(ctx, time.Now().AddDate(0, 5, 0))
	if plans, _ = s.MaintenancePlans(ctx, reader, p.Slug, ""); plans[0].OpenTaskID != "" {
		t.Fatal("card too early")
	}
	s.RunMaintenance(ctx, time.Now().AddDate(0, 6, -3))
	if plans, _ = s.MaintenancePlans(ctx, reader, p.Slug, ""); plans[0].OpenTaskID == "" {
		t.Fatal("no card within the lead time")
	}
	if err := s.DeleteMaintenancePlan(ctx, owner, p.Slug, plan.ID); err != nil {
		t.Fatal(err)
	}
	if plans, _ = s.MaintenancePlans(ctx, owner, p.Slug, ""); len(plans) != 0 {
		t.Fatal("plan not deleted")
	}
}
