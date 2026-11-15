package core

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/phoedox/phoedox/internal/store"
)

func TestParseDuration(t *testing.T) {
	for in, want := range map[string]int{"90": 90, "1:30": 90, "1h 30m": 90, "1.5h": 90, "1,5h": 90, "45m": 45, "45 min": 45, "2h": 120, "x": 0, "": 0} {
		if got := ParseDuration(in); got != want {
			t.Errorf("%q: %d, want %d", in, got, want)
		}
	}
}

func TestTimeTracking(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, _ := setup(t)
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Customer"})
	_ = s.SetMember(ctx, owner, p.Slug, "reader@x.de", store.RoleReader)
	board, _ := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "board", Title: "Jobs"})
	task, _ := s.CreateTask(ctx, owner, p.Slug, board.Document.Slug, TaskInput{Title: "=Replace UPS battery"})

	e, err := s.LogTime(ctx, owner, p.Slug, task.ID, TimeInput{Duration: "1:30", Day: "2026-11-14", Note: "on site"})
	if err != nil || e.Minutes != 90 {
		t.Fatalf("%+v %v", e, err)
	}
	if _, err := s.LogTime(ctx, owner, p.Slug, task.ID, TimeInput{Duration: "nope"}); err == nil {
		t.Fatal("bad duration accepted")
	}
	if _, err := s.LogTime(ctx, reader, p.Slug, task.ID, TimeInput{Duration: "10m"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("reader logged time: %v", err)
	}
	_, _ = s.LogTime(ctx, owner, p.Slug, task.ID, TimeInput{Minutes: 15})
	list, err := s.TimeEntries(ctx, reader, p.Slug, task.ID, store.TimeFilter{})
	if err != nil || len(list) != 2 {
		t.Fatalf("list: %+v %v", list, err)
	}
	tasks, _ := s.Tasks(ctx, owner, p.Slug, board.Document.Slug, "", "", false)
	if len(tasks) != 1 || tasks[0].Minutes != 105 {
		t.Fatalf("total: %+v", tasks)
	}
	csv, err := s.TimeReportCSV(ctx, owner, p.Slug, store.TimeFilter{From: "2026-11-01", To: "2026-11-30"})
	if err != nil || !strings.Contains(string(csv), "2026-11-14,owner@x.de,Jobs,'=Replace UPS battery,90,1.50,true,on site") {
		t.Fatalf("csv: %s %v", csv, err)
	}
	if _, err := s.TimeReportCSV(ctx, reader, p.Slug, store.TimeFilter{}); err == nil {
		t.Fatal("reader exported time")
	}
	if err := s.DeleteTime(ctx, owner, p.Slug, e.ID); err != nil {
		t.Fatal(err)
	}
}
