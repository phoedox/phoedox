package core

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/phoedox/phoedox/internal/requests"
	"github.com/phoedox/phoedox/internal/secret"
	"github.com/phoedox/phoedox/internal/store"
)

func TestCalendar(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, _ := setup(t)
	s.Box, _ = secret.New(make([]byte, 32))
	s.RequestPolicy = requests.Policy{AllowPrivate: true}
	s.PublicURL = "https://phx.test"
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Home"})
	_ = s.SetMember(ctx, owner, p.Slug, "reader@x.de", store.RoleReader)

	now := time.Now().UTC()
	in := func(days int) string { return now.AddDate(0, 0, days).Format("2006-01-02") }
	if _, err := s.UpsertEntity(ctx, owner, p.ID, EntityInput{Kind: "contract", Name: "Internet", Attrs: map[string]any{"cancel_by": in(10)}}); err != nil {
		t.Fatal(err)
	}
	_, _ = s.UpsertEntity(ctx, owner, p.ID, EntityInput{Kind: "device", Name: "Heat pump"})
	board, _ := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "board", Title: "Chores", Board: &Board{Columns: []BoardColumn{{ID: "todo", Name: "To do"}, {ID: "done", Name: "Done", Done: true}}}})
	if _, err := s.CreateTask(ctx, owner, p.Slug, board.Document.Slug, TaskInput{Title: "Buy salt", Due: in(3)}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveMaintenancePlan(ctx, owner, p.Slug, "", MaintenanceInput{Entity: "heat-pump", Board: board.Document.Slug, Title: "Change filter", Every: 1, Unit: "week"}); err != nil {
		t.Fatal(err)
	}

	from, to, err := CalendarRange(in(-1), in(30), now)
	if err != nil {
		t.Fatal(err)
	}
	v, err := s.Calendar(ctx, reader, p.Slug, from, to, nil)
	if err != nil {
		t.Fatal(err)
	}
	by := map[string][]CalEvent{}
	for _, e := range v.Events {
		by[e.Source] = append(by[e.Source], e)
	}
	if c := by[CalEntities]; len(c) != 1 || c[0].Attr != "cancel_by" || c[0].Start != in(10) || c[0].End != in(11) || !c[0].AllDay || c[0].Link != "e/internet" {
		t.Fatalf("date field: %+v", c)
	}
	if c := by[CalTasks]; len(c) != 2 || c[0].Link == "" || !strings.HasPrefix(c[0].Link, "d/"+board.Document.Slug+"?task=") {
		// the task and the maintenance card the plan made at once
		t.Fatalf("tasks: %+v", c)
	}
	if c := by[CalMaintenance]; len(c) < 4 || c[0].Link != "e/heat-pump" {
		t.Fatalf("maintenance: the plan's next dates every week: %+v", c)
	}
	if v.Feed != nil {
		t.Fatal("readers do not see the feed")
	}
	// Filters.
	v, _ = s.Calendar(ctx, reader, p.Slug, from, to, &CalendarSpec{Sources: []string{CalEntities}, Kinds: []string{"car"}})
	if len(v.Events) != 0 {
		t.Fatalf("kinds filter: %+v", v.Events)
	}
	if _, err := s.Calendar(ctx, reader, p.Slug, from, to, &CalendarSpec{Sources: []string{"birthdays"}}); invalidKey(err) != "validation.calendar_source" {
		t.Fatalf("unknown source: %v", err)
	}
	if _, _, err := CalendarRange(in(0), in(500), now); invalidKey(err) != "validation.calendar_range" {
		t.Fatalf("range: %v", err)
	}

	// Calendar documents keep their filters.
	doc, err := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: store.DocCalendar, Title: "Contracts", Calendar: &CalendarSpec{View: "list", Sources: []string{CalEntities}}})
	if err != nil {
		t.Fatal(err)
	}
	sp, err := s.CalendarDocSpec(ctx, reader, p.Slug, doc.Document.Slug)
	if err != nil || sp.View != "list" || len(sp.Sources) != 1 {
		t.Fatalf("doc spec: %+v %v", sp, err)
	}
	if _, err := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: store.DocCalendar, Title: "Bad", Calendar: &CalendarSpec{View: "year"}}); invalidKey(err) != "validation.calendar_view" {
		t.Fatalf("bad view: %v", err)
	}
	if _, err := s.CalendarDocSpec(ctx, reader, p.Slug, board.Document.Slug); invalidKey(err) != "validation.calendar_doc" {
		t.Fatalf("not a calendar: %v", err)
	}

	// An iCal calendar is read at once; its own events (written by Phoedox) are skipped.
	ics := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\nUID:holiday-1\r\nDTSTART;VALUE=DATE:" + strings.ReplaceAll(in(5), "-", "") +
		"\r\nSUMMARY:Holiday\r\nEND:VEVENT\r\nBEGIN:VEVENT\r\nUID:" + ownUID(p) + "t-x\r\nDTSTART;VALUE=DATE:" + strings.ReplaceAll(in(5), "-", "") +
		"\r\nSUMMARY:Mine\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/calendar")
		_, _ = w.Write([]byte(ics))
	}))
	defer srv.Close()
	if _, err := s.SaveExternalCalendar(ctx, reader, p.Slug, "", CalendarInput{Name: "x", Kind: "ical", URL: srv.URL}); err == nil {
		t.Fatal("readers cannot add calendars")
	}
	mcp := owner
	mcp.Via = "mcp"
	if _, err := s.SaveExternalCalendar(ctx, mcp, p.Slug, "", CalendarInput{Name: "x", Kind: "ical", URL: srv.URL}); err == nil {
		t.Fatal("AI clients cannot add calendars (passwords)")
	}
	if _, err := s.SaveExternalCalendar(ctx, owner, p.Slug, "", CalendarInput{Name: "x", Kind: "ical", URL: srv.URL, WriteBack: true}); invalidKey(err) != "validation.calendar_write_back" {
		t.Fatalf("write back needs CalDAV: %v", err)
	}
	if _, err := s.SaveExternalCalendar(ctx, owner, p.Slug, "", CalendarInput{Name: "x", Kind: "ical", URL: "ftp://example.org/x.ics"}); invalidKey(err) != "validation.calendar_url" {
		t.Fatalf("bad url: %v", err)
	}
	pw := "secret"
	cal, err := s.SaveExternalCalendar(ctx, owner, p.Slug, "", CalendarInput{Name: "Holidays", Kind: "ical", URL: strings.Replace(srv.URL, "http://", "http://", 1), Password: &pw, Color: "green"})
	if err != nil || cal.LastError != "" || cal.SyncedAt.IsZero() || !cal.HasPassword || cal.Secret == "" || strings.Contains(cal.Secret, "secret") {
		t.Fatalf("calendar: %+v %v", cal, err)
	}
	v, _ = s.Calendar(ctx, reader, p.Slug, from, to, &CalendarSpec{Sources: []string{CalExternal}})
	if len(v.Events) != 1 || v.Events[0].Title != "Holiday" || v.Events[0].Color != "green" || v.Events[0].Calendar != cal.ID || len(v.Calendars) != 1 {
		t.Fatalf("external: %+v", v)
	}
	// A broken address shows as the last error.
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "gone", http.StatusGone) })
	synced, err := s.SyncExternalCalendar(ctx, owner, p.Slug, cal.ID)
	if err != nil || synced.LastError == "" {
		t.Fatalf("sync error: %+v %v", synced, err)
	}
	if err := s.DeleteExternalCalendar(ctx, owner, p.Slug, cal.ID); err != nil {
		t.Fatal(err)
	}
	if v, _ = s.Calendar(ctx, reader, p.Slug, from, to, &CalendarSpec{Sources: []string{CalExternal}}); len(v.Events) != 0 {
		t.Fatalf("events of a removed calendar: %+v", v.Events)
	}

	// The feed: owners switch it on, the address is shown once.
	if _, err := s.SetCalendarFeed(ctx, reader, p.Slug, true); err == nil {
		t.Fatal("readers cannot make a feed")
	}
	st, err := s.SetCalendarFeed(ctx, owner, p.Slug, true)
	if err != nil || !st.On || !strings.HasPrefix(st.Token, "phxcal_") {
		t.Fatalf("feed: %+v %v", st, err)
	}
	name, data, err := s.CalendarFeed(ctx, st.Token)
	if err != nil || name != "Home" || !strings.Contains(string(data), "BEGIN:VCALENDAR") || !strings.Contains(string(data), "Buy salt") ||
		!strings.Contains(string(data), "Internet: cancel by") || !strings.Contains(string(data), "https://phx.test/p/"+p.Slug+"/e/internet") {
		t.Fatalf("feed: %v %s", err, data)
	}
	if v, _ := s.Calendar(ctx, owner, p.Slug, from, to, nil); v.Feed == nil || !v.Feed.On || v.Feed.Token != "" {
		t.Fatalf("owners see that the feed is on, not the token: %+v", v.Feed)
	}
	again, _ := s.SetCalendarFeed(ctx, owner, p.Slug, true)
	if _, _, err := s.CalendarFeed(ctx, st.Token); err == nil {
		t.Fatal("a new address replaces the old one")
	}
	if _, _, err := s.SetCalendarFeedOffAndCheck(ctx, owner, p.Slug, again.Token); err != nil {
		t.Fatal(err)
	}
}

// SetCalendarFeedOffAndCheck switches the feed off and checks the address is gone.
func (s *Service) SetCalendarFeedOffAndCheck(ctx context.Context, a Actor, ref, token string) (CalendarFeedStatus, []byte, error) {
	st, err := s.SetCalendarFeed(ctx, a, ref, false)
	if err != nil {
		return st, nil, err
	}
	if _, _, err := s.CalendarFeed(ctx, token); err == nil {
		return st, nil, ErrForbidden
	}
	return st, nil, nil
}

// TestCalendarWriteBack checks that a CalDAV calendar with write-back gets
// the project's dates, changed ones again and removed ones deleted, while
// its own events show in Phoedox.
func TestCalendarWriteBack(t *testing.T) {
	ctx := context.Background()
	s, owner, _, _ := setup(t)
	s.Box, _ = secret.New(make([]byte, 32))
	s.RequestPolicy = requests.Policy{AllowPrivate: true}
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Home"})
	now := time.Now().UTC()
	in := func(days int) string { return now.AddDate(0, 0, days).Format("2006-01-02") }

	var mu sync.Mutex
	res := map[string]string{
		"/dav/cal/dentist.ics": "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\nUID:dentist\r\nDTSTART;VALUE=DATE:" + strings.ReplaceAll(in(4), "-", "") +
			"\r\nSUMMARY:Dentist\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n",
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if u, pw, ok := r.BasicAuth(); !ok || u != "me" || pw != "pw" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.Method {
		case "PROPFIND":
			w.WriteHeader(http.StatusMultiStatus)
			fmt.Fprint(w, `<D:multistatus xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav"><D:response><D:href>/dav/cal/</D:href><D:propstat><D:prop>`+
				`<D:resourcetype><D:collection/><C:calendar/></D:resourcetype><D:displayname>Family</D:displayname>`+
				`<D:current-user-privilege-set><D:privilege><D:write-content/></D:privilege></D:current-user-privilege-set>`+
				`</D:prop><D:status>HTTP/1.1 200 OK</D:status></D:propstat></D:response></D:multistatus>`)
		case "REPORT":
			w.WriteHeader(http.StatusMultiStatus)
			fmt.Fprint(w, `<D:multistatus xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav">`)
			for path, data := range res {
				var b strings.Builder
				_ = xml.EscapeText(&b, []byte(data))
				fmt.Fprintf(w, `<D:response><D:href>%s</D:href><D:propstat><D:prop><C:calendar-data>%s</C:calendar-data></D:prop><D:status>HTTP/1.1 200 OK</D:status></D:propstat></D:response>`, path, b.String())
			}
			fmt.Fprint(w, `</D:multistatus>`)
		case "PUT":
			b, _ := io.ReadAll(r.Body)
			res[r.URL.Path] = string(b)
			w.Header().Set("ETag", `"e1"`)
			w.WriteHeader(http.StatusCreated)
		case "DELETE":
			delete(res, r.URL.Path)
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer srv.Close()
	pushedWith := func(text string) (n int) {
		mu.Lock()
		defer mu.Unlock()
		for path, data := range res {
			if strings.Contains(path, "phoedox-") && strings.Contains(data, text) {
				n++
			}
		}
		return n
	}

	board, _ := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: "board", Title: "Chores", Board: &Board{Columns: []BoardColumn{{ID: "todo", Name: "To do"}}}})
	task, err := s.CreateTask(ctx, owner, p.Slug, board.Document.Slug, TaskInput{Title: "Buy salt", Due: in(3)})
	if err != nil {
		t.Fatal(err)
	}
	user, pw := "me", "pw"
	cal, err := s.SaveExternalCalendar(ctx, owner, p.Slug, "", CalendarInput{Name: "Family", Kind: "caldav", URL: srv.URL + "/dav/cal/", Username: user, Password: &pw, WriteBack: true})
	if err != nil || cal.LastError != "" {
		t.Fatalf("calendar: %+v %v", cal, err)
	}
	if pushedWith("SUMMARY:Buy salt") != 1 {
		t.Fatalf("the task's date was not written: %v", res)
	}
	from, to, _ := CalendarRange(in(-1), in(30), now)
	v, _ := s.Calendar(ctx, owner, p.Slug, from, to, &CalendarSpec{Sources: []string{CalExternal}})
	if len(v.Events) != 1 || v.Events[0].Title != "Dentist" {
		t.Fatalf("only the calendar's own events show, not the written ones: %+v", v.Events)
	}

	// A changed date is written again, a removed one deleted.
	title := "Buy more salt"
	if _, err := s.UpdateTask(ctx, owner, p.Slug, task.ID, TaskPatch{Title: &title}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SyncExternalCalendar(ctx, owner, p.Slug, cal.ID); err != nil {
		t.Fatal(err)
	}
	if pushedWith("SUMMARY:Buy more salt") != 1 || pushedWith("SUMMARY:Buy salt") != 0 {
		t.Fatalf("changed date: %v", res)
	}
	if err := s.DeleteTask(ctx, owner, p.Slug, task.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SyncExternalCalendar(ctx, owner, p.Slug, cal.ID); err != nil {
		t.Fatal(err)
	}
	if pushedWith("SUMMARY:") != 0 {
		t.Fatalf("removed date still in the calendar: %v", res)
	}
	// A wrong password is a validation error when saving.
	bad := "nope"
	if _, err := s.SaveExternalCalendar(ctx, owner, p.Slug, cal.ID, CalendarInput{Name: "Family", Kind: "caldav", URL: srv.URL + "/dav/cal/", Username: user, Password: &bad}); invalidKey(err) == "" {
		t.Fatalf("wrong password: %v", err)
	}
}
