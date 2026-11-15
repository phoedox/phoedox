package core

import (
	"bytes"
	"context"
	"encoding/csv"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/phoedox/phoedox/internal/store"
)

// Time tracking on cards (service providers, "jobs with time tracking"):
// editors log time on a card; readers see the totals; owners export a
// report per month as CSV. Entries carry who logged them (actor).

// TimeInput logs time on a card. Duration takes "90", "1:30", "1h 30m" or "1.5h".
type TimeInput struct {
	Duration string `json:"duration"`
	Minutes  int    `json:"minutes,omitempty"`
	Day      string `json:"day,omitempty"` // YYYY-MM-DD, default today
	Note     string `json:"note,omitempty"`
	Billable *bool  `json:"billable,omitempty"`
}

var (
	durHM    = regexp.MustCompile(`^(\d{1,3}):([0-5]\d)$`)
	durParts = regexp.MustCompile(`^(?:(\d+(?:[.,]\d+)?)\s*h)?\s*(?:(\d+)\s*m(?:in)?)?$`)
)

// ParseDuration reads a duration in minutes (0 when it cannot be read).
func ParseDuration(s string) int {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return 0
	}
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	if m := durHM.FindStringSubmatch(s); m != nil {
		h, _ := strconv.Atoi(m[1])
		mm, _ := strconv.Atoi(m[2])
		return h*60 + mm
	}
	if m := durParts.FindStringSubmatch(s); m != nil && (m[1] != "" || m[2] != "") {
		total := 0.0
		if m[1] != "" {
			h, _ := strconv.ParseFloat(strings.ReplaceAll(m[1], ",", "."), 64)
			total += h * 60
		}
		if m[2] != "" {
			mm, _ := strconv.Atoi(m[2])
			total += float64(mm)
		}
		return int(total + 0.5)
	}
	return 0
}

func (s *Service) attachMinutes(ctx context.Context, projectID string, tasks []store.Task) {
	if len(tasks) == 0 {
		return
	}
	m, err := s.DB.TaskMinutes(ctx, projectID)
	if err != nil {
		return
	}
	for i := range tasks {
		tasks[i].Minutes = m[tasks[i].ID]
	}
}

// LogTime records time on a card (editors of its board).
func (s *Service) LogTime(ctx context.Context, a Actor, ref, taskID string, in TimeInput) (store.TimeEntry, error) {
	p, err := s.project(ctx, a, ref, store.RoleDoc)
	if err != nil {
		return store.TimeEntry{}, err
	}
	t, err := s.DB.GetTask(ctx, p.ID, taskID)
	if err != nil {
		return store.TimeEntry{}, err
	}
	if _, _, err := s.boardAccess(ctx, a, ref, t.BoardID, store.RoleEditor); err != nil {
		return store.TimeEntry{}, err
	}
	min := in.Minutes
	if min == 0 {
		min = ParseDuration(in.Duration)
	}
	if min <= 0 || min > 24*60 {
		return store.TimeEntry{}, invalid("validation.time_duration", nil, "give a duration between 1 minute and 24 hours, e.g. 1:30 or 45m")
	}
	day := strings.TrimSpace(in.Day)
	if day == "" {
		day = time.Now().Format(time.DateOnly)
	}
	if _, err := time.Parse(time.DateOnly, day); err != nil {
		return store.TimeEntry{}, invalid("validation.time_day", nil, "day must be YYYY-MM-DD")
	}
	billable := in.Billable == nil || *in.Billable
	e, err := s.DB.AddTime(ctx, p.ID, a.String(), store.TimeEntry{ID: newID(), TaskID: t.ID, UserID: a.User.ID, Day: day, Minutes: min, Note: clipRunes(in.Note, 500), Billable: billable})
	if err == nil {
		s.event(ctx, a, "task.time", t.ID, map[string]any{"minutes": min, "day": day})
		e.UserName, e.TaskTitle = a.User.Name, t.Title
	}
	return e, err
}

// TimeEntries lists time of a card or, with taskID "", of the project
// (filters from, to, user); readers of the boards see it.
func (s *Service) TimeEntries(ctx context.Context, a Actor, ref, taskID string, f store.TimeFilter) ([]store.TimeEntry, error) {
	p, err := s.project(ctx, a, ref, store.RoleDoc)
	if err != nil {
		return nil, err
	}
	if taskID != "" {
		t, err := s.DB.GetTask(ctx, p.ID, taskID)
		if err != nil {
			return nil, err
		}
		if _, _, err := s.boardAccess(ctx, a, ref, t.BoardID, store.RoleReader); err != nil {
			return nil, err
		}
		f.TaskID = t.ID
	} else if _, err := s.project(ctx, a, ref, store.RoleReader); err != nil {
		return nil, err
	}
	return s.DB.ListTime(ctx, p.ID, f)
}

// DeleteTime removes an entry (its author or project owners).
func (s *Service) DeleteTime(ctx context.Context, a Actor, ref, id string) error {
	p, err := s.project(ctx, a, ref, store.RoleEditor)
	if err != nil {
		return err
	}
	e, err := s.DB.GetTime(ctx, p.ID, id)
	if err != nil {
		return err
	}
	if e.UserID != a.User.ID && p.Role != store.RoleOwner {
		return ErrForbidden
	}
	if err := s.DB.DeleteTime(ctx, p.ID, id); err != nil {
		return err
	}
	s.event(ctx, a, "task.time_delete", e.TaskID, map[string]any{"minutes": e.Minutes, "day": e.Day})
	return nil
}

// TimeReportCSV is the time of a project in a period as CSV (owners).
func (s *Service) TimeReportCSV(ctx context.Context, a Actor, ref string, f store.TimeFilter) ([]byte, error) {
	p, err := s.project(ctx, a, ref, store.RoleOwner)
	if err != nil {
		return nil, err
	}
	list, err := s.DB.ListTime(ctx, p.ID, f)
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	w := csv.NewWriter(&b)
	_ = w.Write([]string{"day", "person", "board", "card", "minutes", "hours", "billable", "note"})
	for _, e := range list {
		_ = w.Write([]string{e.Day, csvSafe(e.UserName), csvSafe(e.BoardTitle), csvSafe(e.TaskTitle), strconv.Itoa(e.Minutes), strconv.FormatFloat(float64(e.Minutes)/60, 'f', 2, 64), strconv.FormatBool(e.Billable), csvSafe(e.Note)})
	}
	w.Flush()
	return b.Bytes(), w.Error()
}

// csvSafe keeps spreadsheet programs from running a cell as a formula.
func csvSafe(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}
