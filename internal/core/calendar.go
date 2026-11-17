package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/phoedox/phoedox/internal/caldav"
	"github.com/phoedox/phoedox/internal/ical"
	"github.com/phoedox/phoedox/internal/store"
)

// Calendar (ADR-0089): the project's dates – maintenance plans, cards with
// a due date and date fields of entries – together with external calendars
// (CalDAV or an iCal URL). The same events feed the document type
// "calendar", the ```calendar block, the optional project tab and a secret
// iCal feed; external CalDAV calendars may get the project's dates written
// into them (write_back).

// Calendar sources.
const (
	CalMaintenance = "maintenance"
	CalTasks       = "tasks"
	CalEntities    = "entities"
	CalExternal    = "external"
)

var calSources = []string{CalMaintenance, CalTasks, CalEntities, CalExternal}

// CalendarSpec chooses what a calendar shows (content of a calendar
// document, options of the block).
type CalendarSpec struct {
	View      string   `json:"view,omitempty"`      // month | list
	Sources   []string `json:"sources,omitempty"`   // empty: all
	Calendars []string `json:"calendars,omitempty"` // external calendar ids; empty: all
	Kinds     []string `json:"kinds,omitempty"`     // entry kinds for date fields; empty: all
	Days      int      `json:"days,omitempty"`      // list view: how many days ahead (default 60)
}

// CalEvent is one date in a calendar.
type CalEvent struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Start    string `json:"start"` // YYYY-MM-DD (all day) or RFC 3339
	End      string `json:"end"`   // exclusive
	AllDay   bool   `json:"allDay"`
	Source   string `json:"source"`
	Calendar string `json:"calendar,omitempty"` // external calendar id
	Color    string `json:"color,omitempty"`
	// Link is the place in the project: "e/<entry>" or "d/<board>?task=<id>".
	Link     string `json:"link,omitempty"`
	Entity   string `json:"entity,omitempty"`
	Kind     string `json:"kind,omitempty"`
	Attr     string `json:"attr,omitempty"` // date field of an entry (labels: attr.<name>)
	Done     bool   `json:"done,omitempty"`
	Location string `json:"location,omitempty"`
	Note     string `json:"note,omitempty"`
	URL      string `json:"url,omitempty"`
}

// CalendarView is what a calendar shows for a time range.
type CalendarView struct {
	From      string              `json:"from"`
	To        string              `json:"to"`
	Events    []CalEvent          `json:"events"`
	Calendars []ExternalCalendar  `json:"calendars"`
	Feed      *CalendarFeedStatus `json:"feed,omitempty"`
}

// ExternalCalendar is an external calendar as clients see it (no secret).
type ExternalCalendar struct {
	store.Calendar
	HasPassword bool `json:"hasPassword"`
}

const (
	calMaxRange  = 400 * 24 * time.Hour
	calSyncEvery = 30 * time.Minute
	calProdID    = "-//Phoedox//Calendar//EN"
)

var calColor = regexp.MustCompile(`^(gray|red|orange|yellow|green|blue|purple|pink)?$`)

func normalizeCalSpec(in *CalendarSpec) (CalendarSpec, error) {
	var sp CalendarSpec
	if in != nil {
		sp = *in
	}
	switch sp.View {
	case "":
		sp.View = "month"
	case "month", "list":
	default:
		return sp, invalid("validation.calendar_view", nil, "view must be month or list")
	}
	for _, s := range sp.Sources {
		if !slices.Contains(calSources, s) {
			return sp, invalid("validation.calendar_source", map[string]any{"source": s}, "unknown source %q (maintenance, tasks, entities, external)", s)
		}
	}
	if len(sp.Calendars) > 50 || len(sp.Kinds) > 50 || len(sp.Sources) > 4 {
		return sp, invalid("validation.calendar_spec", nil, "too many filters")
	}
	if sp.Days < 0 || sp.Days > 366 {
		return sp, invalid("validation.calendar_spec", nil, "days must be 1–366")
	}
	return sp, nil
}

func calendarContent(sp *CalendarSpec) (store.JSON, string, error) {
	n, err := normalizeCalSpec(sp)
	if err != nil {
		return nil, "", err
	}
	b, _ := json.Marshal(n)
	return b, "", nil
}

func (sp CalendarSpec) wants(src string) bool {
	return len(sp.Sources) == 0 || slices.Contains(sp.Sources, src)
}

func day(t time.Time) string { return t.UTC().Format("2006-01-02") }

func parseDay(s string) (time.Time, bool) {
	t, err := time.Parse("2006-01-02", strings.TrimSpace(s))
	return t, err == nil
}

// CalendarRange parses from/to of an API call (dates or RFC 3339; default
// the current month ±1 week).
func CalendarRange(from, to string, now time.Time) (time.Time, time.Time, error) {
	parse := func(v string) (time.Time, error) {
		if t, ok := parseDay(v); ok {
			return t, nil
		}
		return time.Parse(time.RFC3339, v)
	}
	var f, t time.Time
	var err error
	if from == "" {
		y, m, _ := now.UTC().Date()
		f = time.Date(y, m, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, -7)
	} else if f, err = parse(from); err != nil {
		return f, t, invalid("validation.calendar_range", nil, "from must be a date")
	}
	if to == "" {
		t = f.AddDate(0, 1, 14)
	} else if t, err = parse(to); err != nil {
		return f, t, invalid("validation.calendar_range", nil, "to must be a date")
	}
	if !t.After(f) || t.Sub(f) > calMaxRange {
		return f, t, invalid("validation.calendar_range", nil, "the range must be positive and at most 400 days")
	}
	return f.UTC(), t.UTC(), nil
}

// Calendar returns the events of a project in [from, to) (readers).
func (s *Service) Calendar(ctx context.Context, a Actor, ref string, from, to time.Time, spec *CalendarSpec) (CalendarView, error) {
	p, err := s.project(ctx, a, ref, store.RoleReader)
	if err != nil {
		return CalendarView{}, err
	}
	sp, err := normalizeCalSpec(spec)
	if err != nil {
		return CalendarView{}, err
	}
	evs, err := s.calendarEvents(ctx, p, from, to, sp)
	if err != nil {
		return CalendarView{}, err
	}
	cals, err := s.externalCalendars(ctx, p)
	if err != nil {
		return CalendarView{}, err
	}
	v := CalendarView{From: day(from), To: day(to), Events: evs, Calendars: cals}
	if p.Role == store.RoleOwner {
		st, _ := s.calendarFeedStatus(ctx, p)
		v.Feed = &st
	}
	return v, nil
}

// calendarEvents collects the events without rights checks (callers check).
func (s *Service) calendarEvents(ctx context.Context, p store.Project, from, to time.Time, sp CalendarSpec) ([]CalEvent, error) {
	out := []CalEvent{}
	inRange := func(d time.Time) bool { return !d.Before(from.Truncate(24*time.Hour)) && d.Before(to) }
	allDay := func(e CalEvent, d time.Time) CalEvent {
		e.AllDay, e.Start, e.End = true, day(d), day(d.AddDate(0, 0, 1))
		return e
	}
	slugs := map[string]store.Entity{}
	if sp.wants(CalMaintenance) || sp.wants(CalEntities) {
		ents, err := s.DB.ListEntities(ctx, p.ID, "")
		if err != nil {
			return nil, err
		}
		for _, e := range ents {
			slugs[e.ID] = e
		}
		if sp.wants(CalEntities) {
			m, err := s.model(ctx, p.ID)
			if err != nil {
				return nil, err
			}
			dates := map[string][]string{} // kind → date fields
			for _, k := range m.Kinds {
				for _, at := range k.Attrs {
					if at.Type == "date" {
						dates[k.ID] = append(dates[k.ID], at.Name)
					}
				}
			}
			for _, e := range ents {
				if len(sp.Kinds) > 0 && !slices.Contains(sp.Kinds, e.Kind) {
					continue
				}
				var attrs map[string]any
				_ = json.Unmarshal(e.Attrs, &attrs)
				for _, name := range dates[e.Kind] {
					v, _ := attrs[name].(string)
					d, ok := parseDay(v)
					if !ok || !inRange(d) {
						continue
					}
					out = append(out, allDay(CalEvent{ID: "e:" + e.ID + ":" + name, Title: e.Name, Source: CalEntities,
						Link: "e/" + e.Slug, Entity: e.Slug, Kind: e.Kind, Attr: name}, d))
				}
			}
		}
	}
	if sp.wants(CalMaintenance) {
		plans, err := s.DB.MaintenancePlans(ctx, p.ID, "")
		if err != nil {
			return nil, err
		}
		for _, pl := range plans {
			d, ok := parseDay(pl.NextDue)
			if !ok {
				continue
			}
			ent := slugs[pl.EntityID]
			if len(sp.Kinds) > 0 && !slices.Contains(sp.Kinds, ent.Kind) {
				continue
			}
			// The next date and, in a longer view, the ones after it.
			for i := 0; i < 400 && d.Before(to); i++ {
				if inRange(d) {
					e := CalEvent{ID: fmt.Sprintf("m:%s:%s", pl.ID, day(d)), Title: pl.Title, Source: CalMaintenance, Entity: ent.Slug, Kind: ent.Kind}
					if ent.Slug != "" {
						e.Link = "e/" + ent.Slug
					}
					out = append(out, allDay(e, d))
				}
				if pl.Every <= 0 {
					break
				}
				d = advance(d, pl.Every, pl.Unit)
			}
		}
	}
	if sp.wants(CalTasks) {
		tasks, err := s.DB.ListTasks(ctx, p.ID, store.TaskFilter{})
		if err != nil {
			return nil, err
		}
		for _, t := range tasks {
			d, ok := parseDay(t.Due)
			if !ok || !inRange(d) {
				continue
			}
			out = append(out, allDay(CalEvent{ID: "t:" + t.ID, Title: t.Title, Source: CalTasks, Done: !t.DoneAt.IsZero(),
				Link: "d/" + t.BoardSlug + "?task=" + t.ID}, d))
		}
	}
	if sp.wants(CalExternal) {
		cals, err := s.DB.Calendars(ctx, p.ID)
		if err != nil {
			return nil, err
		}
		color := map[string]string{}
		for _, c := range cals {
			color[c.ID] = c.Color
		}
		evs, err := s.DB.CalendarEvents(ctx, p.ID, store.Time{Time: from}, store.Time{Time: to})
		if err != nil {
			return nil, err
		}
		for _, e := range evs {
			if len(sp.Calendars) > 0 && !slices.Contains(sp.Calendars, e.CalendarID) {
				continue
			}
			c := CalEvent{ID: "x:" + e.CalendarID + ":" + e.UID + ":" + e.Recurrence, Title: e.Summary, Source: CalExternal, Calendar: e.CalendarID,
				Color: color[e.CalendarID], Location: e.Location, Note: e.Description, URL: e.URL, AllDay: e.AllDay}
			if e.AllDay {
				c.Start, c.End = day(e.Start.Time), day(e.End.Time)
			} else {
				c.Start, c.End = e.Start.UTC().Format(time.RFC3339), e.End.UTC().Format(time.RFC3339)
			}
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Start != out[j].Start {
			return out[i].Start < out[j].Start
		}
		return out[i].Title < out[j].Title
	})
	if len(out) > 5000 {
		out = out[:5000]
	}
	return out, nil
}

// --- external calendars ---

// CalendarInput adds or changes an external calendar.
type CalendarInput struct {
	Name     string `json:"name"`
	Kind     string `json:"kind"` // caldav | ical
	URL      string `json:"url"`
	Username string `json:"username,omitempty"`
	// Password: nil keeps the stored one, "" removes it.
	Password  *string `json:"password,omitempty"`
	Color     string  `json:"color,omitempty"`
	WriteBack bool    `json:"writeBack"`
}

func (s *Service) externalCalendars(ctx context.Context, p store.Project) ([]ExternalCalendar, error) {
	list, err := s.DB.Calendars(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	out := make([]ExternalCalendar, 0, len(list))
	for _, c := range list {
		out = append(out, ExternalCalendar{Calendar: c, HasPassword: c.Secret != ""})
	}
	return out, nil
}

// ExternalCalendars lists the external calendars of a project (readers).
func (s *Service) ExternalCalendars(ctx context.Context, a Actor, ref string) ([]ExternalCalendar, error) {
	p, err := s.project(ctx, a, ref, store.RoleReader)
	if err != nil {
		return nil, err
	}
	return s.externalCalendars(ctx, p)
}

func calURL(kind, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if kind == "ical" && strings.HasPrefix(strings.ToLower(raw), "webcal://") {
		raw = "https://" + raw[len("webcal://"):]
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || len(raw) > 1000 {
		return "", invalid("validation.calendar_url", nil, "the calendar address must be an http(s) URL without credentials")
	}
	return u.String(), nil
}

// SaveExternalCalendar adds (id "") or changes an external calendar
// (owners; not over MCP, because it holds a password). It reads the
// calendar once, so a wrong address or password shows at once.
func (s *Service) SaveExternalCalendar(ctx context.Context, a Actor, ref, id string, in CalendarInput) (ExternalCalendar, error) {
	p, err := s.project(ctx, a, ref, store.RoleOwner)
	if err != nil {
		return ExternalCalendar{}, err
	}
	if a.Via == "mcp" || a.Plugin != "" {
		return ExternalCalendar{}, ErrForbidden
	}
	c := store.Calendar{ID: newID(), ProjectID: p.ID, CreatedBy: a.User.ID, CreatedAt: store.Now()}
	if id != "" {
		if c, err = s.DB.GetCalendar(ctx, p.ID, id); err != nil {
			return ExternalCalendar{}, err
		}
	}
	in.Name = strings.TrimSpace(in.Name)
	switch {
	case in.Name == "" || len(in.Name) > 100:
		return ExternalCalendar{}, invalid("validation.calendar_name", nil, "a calendar needs a name of at most 100 characters")
	case in.Kind != "caldav" && in.Kind != "ical":
		return ExternalCalendar{}, invalid("validation.calendar_kind", nil, "kind must be caldav or ical")
	case in.WriteBack && in.Kind != "caldav":
		return ExternalCalendar{}, invalid("validation.calendar_write_back", nil, "only CalDAV calendars can get Phoedox's dates written into them")
	case !calColor.MatchString(in.Color):
		return ExternalCalendar{}, invalid("validation.calendar_color", nil, "unknown colour")
	case len(in.Username) > 200:
		return ExternalCalendar{}, invalid("validation.calendar_url", nil, "the user name is too long")
	}
	u, err := calURL(in.Kind, in.URL)
	if err != nil {
		return ExternalCalendar{}, err
	}
	c.Name, c.Kind, c.URL, c.Username, c.Color, c.WriteBack = in.Name, in.Kind, u, strings.TrimSpace(in.Username), in.Color, in.WriteBack
	if in.Password != nil {
		c.Secret = ""
		if *in.Password != "" {
			if s.Box == nil {
				return ExternalCalendar{}, invalid("validation.calendar_secret", nil, "the server has no key to store passwords")
			}
			if c.Secret, err = s.Box.Seal([]byte(*in.Password), "calendar:"+c.ID); err != nil {
				return ExternalCalendar{}, err
			}
		}
	}
	if err := s.checkCalendar(ctx, c); err != nil {
		return ExternalCalendar{}, err
	}
	if id == "" {
		err = s.DB.InsertCalendar(ctx, c)
	} else {
		err = s.DB.UpdateCalendar(ctx, c)
	}
	if err != nil {
		return ExternalCalendar{}, err
	}
	s.event(ctx, a, "calendar.save", c.ID, map[string]any{"project": p.Slug, "name": c.Name, "kind": c.Kind, "writeBack": c.WriteBack})
	_ = s.syncCalendar(ctx, p, c)
	c, _ = s.DB.GetCalendar(ctx, p.ID, c.ID)
	return ExternalCalendar{Calendar: c, HasPassword: c.Secret != ""}, nil
}

// DeleteExternalCalendar removes an external calendar (owners). Dates
// Phoedox wrote into it stay there.
func (s *Service) DeleteExternalCalendar(ctx context.Context, a Actor, ref, id string) error {
	p, err := s.project(ctx, a, ref, store.RoleOwner)
	if err != nil {
		return err
	}
	if err := s.DB.DeleteCalendar(ctx, p.ID, id); err != nil {
		return err
	}
	s.event(ctx, a, "calendar.delete", id, map[string]any{"project": p.Slug})
	return nil
}

// SyncExternalCalendar reads an external calendar now (editors).
func (s *Service) SyncExternalCalendar(ctx context.Context, a Actor, ref, id string) (ExternalCalendar, error) {
	p, err := s.project(ctx, a, ref, store.RoleEditor)
	if err != nil {
		return ExternalCalendar{}, err
	}
	c, err := s.DB.GetCalendar(ctx, p.ID, id)
	if err != nil {
		return ExternalCalendar{}, err
	}
	_ = s.syncCalendar(ctx, p, c)
	c, err = s.DB.GetCalendar(ctx, p.ID, id)
	return ExternalCalendar{Calendar: c, HasPassword: c.Secret != ""}, err
}

func (s *Service) calClient(c store.Calendar) (*caldav.Client, error) {
	cl := &caldav.Client{HTTP: s.RequestPolicy.Client(), User: c.Username}
	if c.Secret != "" {
		if s.Box == nil {
			return nil, errors.New("no key to open the stored password")
		}
		pw, err := s.Box.Open(c.Secret, "calendar:"+c.ID)
		if err != nil {
			return nil, errors.New("the stored password cannot be opened")
		}
		cl.Password = string(pw)
	}
	return cl, nil
}

// checkCalendar reads the calendar once and turns failures into messages
// for people.
func (s *Service) checkCalendar(ctx context.Context, c store.Calendar) error {
	cl, err := s.calClient(c)
	if err != nil {
		return invalid("validation.calendar_connect", map[string]any{"reason": err.Error()}, "%v", err)
	}
	if c.Kind == "ical" {
		now := time.Now()
		_, err = caldav.FetchICS(ctx, cl.HTTP, c.URL, now, now.AddDate(0, 0, 1))
	} else {
		var writable bool
		_, writable, err = cl.Check(ctx, c.URL)
		if err == nil && c.WriteBack && !writable {
			return invalid("validation.calendar_readonly", nil, "this calendar cannot be written to with these credentials")
		}
	}
	switch {
	case err == nil:
		return nil
	case errors.Is(err, caldav.ErrAuth):
		return invalid("validation.calendar_auth", nil, "the calendar refused the user name or password")
	case errors.Is(err, caldav.ErrNotCalendar):
		return invalid("validation.calendar_not_calendar", nil, "this address is not a calendar (use the calendar's own CalDAV address)")
	default:
		return invalid("validation.calendar_connect", map[string]any{"reason": err.Error()}, "the calendar cannot be read: %v", err)
	}
}

// ownUID is the prefix of the events Phoedox writes for a project; reading
// skips them so they do not show twice.
func ownUID(p store.Project) string { return "phoedox-" + strings.ToLower(p.ID) + "-" }

// syncCalendar reads the calendar's events for [-31 days, +1 year) and, with
// write_back, writes the project's own dates into it.
func (s *Service) syncCalendar(ctx context.Context, p store.Project, c store.Calendar) error {
	now := time.Now().UTC()
	from, to := now.AddDate(0, 0, -31).Truncate(24*time.Hour), now.AddDate(1, 0, 0)
	fail := func(err error) error {
		msg := err.Error()
		if errors.Is(err, caldav.ErrAuth) {
			msg = "the calendar refused the user name or password"
		}
		if len(msg) > 300 {
			msg = msg[:300]
		}
		_ = s.DB.MarkCalendarSynced(ctx, c.ID, msg)
		return err
	}
	cl, err := s.calClient(c)
	if err != nil {
		return fail(err)
	}
	var evs []ical.Event
	if c.Kind == "ical" {
		evs, err = caldav.FetchICS(ctx, cl.HTTP, c.URL, from, to)
	} else {
		evs, err = cl.Events(ctx, c.URL, from, to)
	}
	if err != nil {
		return fail(err)
	}
	own := ownUID(p)
	rows := make([]store.CalendarEvent, 0, len(evs))
	for _, e := range evs {
		if strings.HasPrefix(e.UID, own) {
			continue
		}
		rows = append(rows, store.CalendarEvent{CalendarID: c.ID, UID: cutText(e.UID, 500), Recurrence: e.Recurrence, ProjectID: p.ID,
			Summary: cutText(e.Summary, 500), Description: cutText(e.Description, 2000), Location: cutText(e.Location, 300), URL: safeURL(e.URL),
			Start: store.Time{Time: e.Start}, End: store.Time{Time: e.End}, AllDay: e.AllDay})
	}
	if err := s.DB.ReplaceCalendarEvents(ctx, c, store.Time{Time: from}, store.Time{Time: to}, rows); err != nil {
		return fail(err)
	}
	if c.WriteBack && c.Kind == "caldav" {
		if err := s.pushCalendar(ctx, p, c, cl, now); err != nil {
			return fail(fmt.Errorf("writing Phoedox's dates: %w", err))
		}
	}
	return s.DB.MarkCalendarSynced(ctx, c.ID, "")
}

func cutText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8Start(s[n]) {
		n--
	}
	return s[:n]
}

func utf8Start(b byte) bool { return b&0xC0 != 0x80 }

func safeURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || len(raw) > 1000 {
		return ""
	}
	return u.String()
}

// ownEvents are the project's dates as iCalendar events (feed, write back).
func (s *Service) ownEvents(ctx context.Context, p store.Project, from, to time.Time) ([]ical.Event, error) {
	evs, err := s.calendarEvents(ctx, p, from, to, CalendarSpec{Sources: []string{CalMaintenance, CalTasks, CalEntities}})
	if err != nil {
		return nil, err
	}
	base := strings.TrimRight(s.PublicURL, "/")
	out := make([]ical.Event, 0, len(evs))
	for _, e := range evs {
		start, _ := parseDay(e.Start)
		end, _ := parseDay(e.End)
		title := e.Title
		if e.Attr != "" {
			title += ": " + strings.ReplaceAll(e.Attr, "_", " ")
		}
		if e.Done {
			title = "✓ " + title
		}
		ev := ical.Event{UID: ownUID(p) + strings.NewReplacer(":", "-").Replace(e.ID), Summary: title, Start: start, End: end, AllDay: true}
		if base != "" && e.Link != "" {
			ev.URL = base + "/p/" + p.Slug + "/" + e.Link
			ev.Description = ev.URL
		}
		out = append(out, ev)
	}
	return out, nil
}

func eventHash(e ical.Event) string {
	h := sha256.Sum256([]byte(strings.Join([]string{e.Summary, e.Start.Format(time.RFC3339), e.End.Format(time.RFC3339), e.URL}, "\x00")))
	return hex.EncodeToString(h[:12])
}

// pushCalendar writes changed dates, removes those that are gone.
func (s *Service) pushCalendar(ctx context.Context, p store.Project, c store.Calendar, cl *caldav.Client, now time.Time) error {
	evs, err := s.ownEvents(ctx, p, now.AddDate(0, 0, -31), now.AddDate(1, 0, 0))
	if err != nil {
		return err
	}
	pushed, err := s.DB.CalendarPushed(ctx, c.ID)
	if err != nil {
		return err
	}
	keep := map[string]bool{}
	for i, e := range evs {
		if i >= 2000 {
			break
		}
		keep[e.UID] = true
		h := eventHash(e)
		if pushed[e.UID].Hash == h {
			continue
		}
		etag, err := cl.Put(ctx, c.URL, e, calProdID)
		if err != nil {
			return err
		}
		if err := s.DB.SetCalendarPushed(ctx, c.ID, store.Pushed{UID: e.UID, Hash: h, ETag: etag}); err != nil {
			return err
		}
	}
	for uid := range pushed {
		if keep[uid] {
			continue
		}
		if err := cl.Delete(ctx, c.URL, uid); err != nil {
			return err
		}
		if err := s.DB.SetCalendarPushed(ctx, c.ID, store.Pushed{UID: uid}); err != nil {
			return err
		}
	}
	return nil
}

// SyncCalendars reads the external calendars that are due, of all tenants.
func (s *Service) SyncCalendars(ctx context.Context) {
	all, err := s.DB.AllCalendars(ctx)
	if err != nil {
		slog.Warn("calendars", "err", err)
		return
	}
	for tenant, cals := range all {
		tctx := store.WithTenant(ctx, tenant)
		for _, c := range cals {
			if !c.SyncedAt.IsZero() && time.Since(c.SyncedAt.Time) < calSyncEvery {
				continue
			}
			p, err := s.DB.ProjectByID(tctx, c.ProjectID)
			if err != nil {
				continue
			}
			if err := s.syncCalendar(tctx, p, c); err != nil {
				slog.Info("calendar sync", "calendar", c.ID, "err", err)
			}
		}
	}
}

// --- iCal feed ---

// CalendarFeedStatus tells owners whether the project has a feed.
type CalendarFeedStatus struct {
	On    bool       `json:"on"`
	Since store.Time `json:"since"`
	// Token is only set right after it was created.
	Token string `json:"token,omitempty"`
}

func (s *Service) calendarFeedStatus(ctx context.Context, p store.Project) (CalendarFeedStatus, error) {
	at, ok, err := s.DB.CalendarFeed(ctx, p.ID)
	return CalendarFeedStatus{On: ok, Since: at}, err
}

// SetCalendarFeed creates a new secret feed address (on) or removes it
// (owners). A new address replaces the old one.
func (s *Service) SetCalendarFeed(ctx context.Context, a Actor, ref string, on bool) (CalendarFeedStatus, error) {
	p, err := s.project(ctx, a, ref, store.RoleOwner)
	if err != nil {
		return CalendarFeedStatus{}, err
	}
	if a.Via == "mcp" || a.Plugin != "" {
		return CalendarFeedStatus{}, ErrForbidden
	}
	token := ""
	if on {
		token = randomToken("phxcal_")
	}
	hash := ""
	if token != "" {
		hash = hashToken(token)
	}
	if err := s.DB.SetCalendarFeed(ctx, p.ID, hash, a.User.ID); err != nil {
		return CalendarFeedStatus{}, err
	}
	s.event(ctx, a, "calendar.feed", p.ID, map[string]any{"project": p.Slug, "on": on})
	st, err := s.calendarFeedStatus(ctx, p)
	st.Token = token
	return st, err
}

// CalendarFeed returns the iCalendar file of a feed token: the project's
// own dates from 60 days ago to a year ahead.
func (s *Service) CalendarFeed(ctx context.Context, token string) (string, []byte, error) {
	if !strings.HasPrefix(token, "phxcal_") || len(token) > 100 {
		return "", nil, ErrNotFound
	}
	tenant, projectID, _, err := s.DB.CalendarFeedByToken(ctx, hashToken(token))
	if err != nil {
		return "", nil, ErrNotFound
	}
	ctx = store.WithTenant(ctx, tenant)
	p, err := s.DB.ProjectByID(ctx, projectID)
	if err != nil {
		return "", nil, ErrNotFound
	}
	now := time.Now().UTC()
	evs, err := s.ownEvents(ctx, p, now.AddDate(0, 0, -60), now.AddDate(1, 0, 0))
	if err != nil {
		return "", nil, err
	}
	return p.Name, ical.Encode(calProdID, evs), nil
}

// CalendarDocSpec returns the filters of a calendar document (readers of it).
func (s *Service) CalendarDocSpec(ctx context.Context, a Actor, ref, docRef string) (*CalendarSpec, error) {
	_, doc, err := s.docAccess(ctx, a, ref, docRef, store.RoleDoc)
	if err != nil {
		return nil, err
	}
	if doc.Type != store.DocCalendar {
		return nil, invalid("validation.calendar_doc", nil, "%s is not a calendar", docRef)
	}
	sp := &CalendarSpec{}
	_ = json.Unmarshal(doc.Content, sp)
	return sp, nil
}
