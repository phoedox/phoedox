// Package ical reads and writes iCalendar data (RFC 5545) for the
// calendar (ADR-0089): Parse turns VCALENDAR objects from CalDAV servers
// and iCal/webcal feeds into event occurrences within a time window,
// recurring events expanded; Encode writes Phoedox's own dates as a
// VCALENDAR for CalDAV.
//
// Supported: VEVENT with DTSTART, DTEND or DURATION (DATE, UTC, TZID incl.
// common Windows zone names, floating = UTC), RRULE with FREQ
// DAILY/WEEKLY/MONTHLY/YEARLY and INTERVAL, COUNT, UNTIL, BYDAY (with
// ordinals in MONTHLY and YEARLY), BYMONTHDAY, BYMONTH, BYYEARDAY,
// BYSETPOS and WKST, EXDATE, RDATE, RECURRENCE-ID overrides and
// STATUS:CANCELLED. Not supported: VTIMEZONE definitions (zones are looked
// up by TZID name), FREQ=HOURLY/MINUTELY/SECONDLY (only DTSTART and RDATEs
// are used), BYWEEKNO, BYHOUR, BYMINUTE, BYSECOND (ignored),
// RECURRENCE-ID;RANGE=THISANDFUTURE (applied to the one occurrence only),
// EXRULE and RDATE periods (only their start counts).
package ical

import (
	"errors"
	"maps"
	"slices"
	"strings"
	"time"
)

// Event is one occurrence of a calendar event.
type Event struct {
	UID, Summary, Description, Location, URL string
	// Start and End are in UTC. End is exclusive; for all-day events both
	// are dates at 00:00 UTC and End is the day after the last day.
	Start, End time.Time
	AllDay     bool
	Updated    time.Time // LAST-MODIFIED or DTSTAMP
	// Recurrence is the RECURRENCE-ID of an overridden occurrence or the
	// occurrence start of an expanded one, in RFC 5545 form (YYYYMMDD for
	// all-day events, YYYYMMDDTHHMMSSZ otherwise); "" for single events.
	Recurrence string
}

const (
	// MaxInputSize is the largest input Parse accepts.
	MaxInputSize = 10 << 20
	// MaxEvents is the most occurrences Parse returns.
	MaxEvents = 5000
	// maxYears limits how far a recurrence rule is expanded.
	maxYears = 100
	// maxPeriods limits rule iterations per Parse call, so that many rules
	// with a COUNT far beyond the window cannot keep Parse busy.
	maxPeriods = 2_000_000
)

var (
	// ErrNotICalendar is returned for input without a VCALENDAR object.
	ErrNotICalendar = errors.New("ical: not iCalendar data")
	// ErrTooLarge is returned for input larger than MaxInputSize.
	ErrTooLarge = errors.New("ical: input larger than 10 MB")
)

// Parse reads one or more VCALENDAR objects and returns the VEVENT
// occurrences that overlap [from, to), recurring events expanded, sorted
// by Start. A zero from or to leaves that side of the window open (rules
// are then still limited to 100 years and MaxEvents occurrences). Broken
// lines and events are skipped; an error is returned only for input that
// is not iCalendar at all or larger than MaxInputSize.
func Parse(data []byte, from, to time.Time) ([]Event, error) {
	if len(data) > MaxInputSize {
		return nil, ErrTooLarge
	}
	vevents, ok := scan(data)
	if !ok {
		return nil, ErrNotICalendar
	}
	if !from.IsZero() && !to.IsZero() && !to.After(from) {
		return []Event{}, nil
	}

	// Group by UID: masters carry the rule, overrides a RECURRENCE-ID.
	var masters []*item
	overrides := map[string][]*item{}
	for _, v := range vevents {
		it, ok := buildItem(v)
		if !ok {
			continue
		}
		if it.hasRecurID && it.ev.UID != "" {
			overrides[it.ev.UID] = append(overrides[it.ev.UID], it)
			continue
		}
		masters = append(masters, it)
	}
	byUID := map[string][]*item{}
	for _, m := range masters {
		if m.ev.UID != "" {
			byUID[m.ev.UID] = append(byUID[m.ev.UID], m)
		}
	}

	p := &expander{from: from, to: to, budget: budget{periods: maxPeriods}}
	for _, m := range masters {
		if m.cancelled {
			continue
		}
		var ovs []*item
		if m.ev.UID != "" {
			ovs = overrides[m.ev.UID]
		}
		p.add(p.expand(m, ovs))
	}
	// Overrides of live masters, and overrides whose master is missing
	// (servers that expand return only those).
	uids := slices.Sorted(maps.Keys(overrides)) // deterministic order
	for _, uid := range uids {
		ovs, ms := overrides[uid], byUID[uid]
		var master *item
		live := len(ms) == 0
		for _, m := range ms {
			if !m.cancelled {
				live, master = true, m
				break
			}
		}
		if !live {
			continue // the whole series is cancelled
		}
		var evs []Event
		for _, ov := range ovs {
			if ev, ok := p.override(ov, master); ok {
				evs = append(evs, ev)
			}
		}
		p.add(evs)
	}
	p.finish()
	return p.out, nil
}

// vevent holds the properties of one VEVENT (nested components skipped).
type vevent map[string][]property

func (v vevent) first(name string) (property, bool) {
	if ps := v[name]; len(ps) > 0 {
		return ps[0], true
	}
	return property{}, false
}

// scan splits data into VEVENTs. ok is false when there is no VCALENDAR.
func scan(data []byte) (events []vevent, ok bool) {
	var stack []string
	var cur vevent
	for _, line := range unfold(data) {
		if line == "" {
			continue
		}
		p, good := parseLine(line)
		if !good {
			continue
		}
		switch p.name {
		case "BEGIN":
			name := strings.ToUpper(strings.TrimSpace(p.value))
			if name == "VCALENDAR" {
				ok = true
			}
			if name == "VEVENT" && (len(stack) == 0 || stack[len(stack)-1] == "VCALENDAR") {
				cur = vevent{}
			}
			stack = append(stack, name)
			continue
		case "END":
			name := strings.ToUpper(strings.TrimSpace(p.value))
			i := len(stack) - 1
			for i >= 0 && stack[i] != name {
				i--
			}
			if i < 0 {
				continue // END without BEGIN
			}
			for j := len(stack) - 1; j >= i; j-- {
				if stack[j] == "VEVENT" && cur != nil && (j == 0 || stack[j-1] == "VCALENDAR") {
					events = append(events, cur)
					cur = nil
				}
			}
			stack = stack[:i]
			continue
		}
		if cur != nil && len(stack) > 0 && stack[len(stack)-1] == "VEVENT" {
			cur[p.name] = append(cur[p.name], p)
		}
	}
	return events, ok
}

// span turns an occurrence start into its end.
type span struct {
	exact   time.Duration // from DTEND (RFC 5545: exact duration for all instances)
	nominal *duration     // from DURATION or the all-day default
}

func (s span) end(start time.Time) time.Time {
	if s.nominal != nil {
		return s.nominal.addTo(start)
	}
	return start.Add(s.exact)
}

// item is a VEVENT ready for expansion.
type item struct {
	ev         Event // text fields and Updated
	start      time.Time
	allDay     bool
	span       span
	hasEnd     bool // DTEND or DURATION given
	rule       *rrule
	rdates     []dateTime
	exInst     map[int64]bool  // excluded instants
	exDate     map[string]bool // excluded dates (DATE vs DATE-TIME mismatch)
	recurID    dateTime
	hasRecurID bool
	cancelled  bool
}

func buildItem(v vevent) (*item, bool) {
	it := &item{}
	text := func(name string) string {
		if p, ok := v.first(name); ok {
			return unescapeText(p.value)
		}
		return ""
	}
	it.ev.UID = strings.TrimSpace(text("UID"))
	it.ev.Summary = text("SUMMARY")
	it.ev.Description = text("DESCRIPTION")
	it.ev.Location = text("LOCATION")
	if p, ok := v.first("URL"); ok {
		it.ev.URL = strings.TrimSpace(p.value)
	}
	it.cancelled = strings.EqualFold(strings.TrimSpace(text("STATUS")), "CANCELLED")
	for _, name := range []string{"LAST-MODIFIED", "DTSTAMP"} {
		if p, ok := v.first(name); ok {
			if dt, err := parseDateProp(p); err == nil {
				it.ev.Updated = dt.t.UTC()
				break
			}
		}
	}
	if p, ok := v.first("RECURRENCE-ID"); ok {
		if dt, err := parseDateProp(p); err == nil {
			it.recurID, it.hasRecurID = dt, true
		}
	}
	if p, ok := v.first("DTSTART"); ok {
		dt, err := parseDateProp(p)
		if err != nil {
			return nil, false
		}
		it.start, it.allDay = dt.t, dt.allDay
	} else if it.hasRecurID {
		it.start, it.allDay = it.recurID.t, it.recurID.allDay
	} else {
		return nil, false
	}

	if p, ok := v.first("DTEND"); ok {
		if dt, err := parseDateProp(p); err == nil {
			it.span = span{exact: max(dt.t.Sub(it.start), 0)}
			it.hasEnd = true
		}
	}
	if !it.hasEnd {
		if p, ok := v.first("DURATION"); ok {
			if d, ok := parseDuration(p.value); ok {
				if d.days < 0 || d.exact < 0 {
					d = duration{}
				}
				it.span = span{nominal: &d}
				it.hasEnd = true
			}
		}
	}
	if !it.hasEnd && it.allDay {
		it.span = span{nominal: &duration{days: 1}}
	}

	if p, ok := v.first("RRULE"); ok && !it.hasRecurID {
		it.rule = parseRRule(p.value, it.start.Location())
	}
	dates := func(p property) []dateTime {
		isDate := strings.EqualFold(p.param("VALUE"), "DATE")
		tzid := p.param("TZID")
		var out []dateTime
		for _, s := range splitList(p.value) {
			if i := strings.IndexByte(s, '/'); i >= 0 { // PERIOD: use the start
				s = s[:i]
			}
			if dt, err := parseDateValue(s, isDate, tzid); err == nil {
				out = append(out, dt)
			}
		}
		return out
	}
	if !it.hasRecurID {
		for _, p := range v["RDATE"] {
			it.rdates = append(it.rdates, dates(p)...)
		}
		for _, p := range v["EXDATE"] {
			for _, dt := range dates(p) {
				if dt.allDay != it.allDay {
					if it.exDate == nil {
						it.exDate = map[string]bool{}
					}
					it.exDate[dt.t.Format(dateLayout)] = true
				} else {
					if it.exInst == nil {
						it.exInst = map[int64]bool{}
					}
					it.exInst[dt.t.Unix()] = true
				}
			}
		}
	}
	return it, true
}

// at converts a DATE or DATE-TIME to an occurrence start of it: DATEs of
// a timed event get DTSTART's time of day, DATE-TIMEs of an all-day event
// their date.
func (it *item) at(dt dateTime) time.Time {
	switch {
	case dt.allDay && !it.allDay:
		hh, mm, ss := it.start.Clock()
		return time.Date(dt.t.Year(), dt.t.Month(), dt.t.Day(), hh, mm, ss, 0, it.start.Location())
	case !dt.allDay && it.allDay:
		return civil(dt.t.Year(), dt.t.Month(), dt.t.Day())
	}
	return dt.t
}

// matcher finds occurrences by instant or, for DATE vs DATE-TIME
// mismatches, by date in the master's zone.
type matcher struct {
	inst map[int64]bool
	date map[string]bool
}

func (m matcher) has(t time.Time) bool {
	return m.inst[t.Unix()] || m.date[t.Format(dateLayout)]
}

type expander struct {
	from, to time.Time
	budget   budget
	out      []Event
}

func overlaps(s, e, from, to time.Time) bool {
	if !to.IsZero() && !s.Before(to) {
		return false
	}
	if from.IsZero() {
		return true
	}
	if e.After(s) {
		return e.After(from)
	}
	return !s.Before(from)
}

// expand returns the occurrences of a master in the window, except those
// replaced by overrides.
func (p *expander) expand(it *item, ovs []*item) []Event {
	recurring := it.rule != nil || len(it.rdates) > 0
	replaced := matcher{inst: map[int64]bool{}, date: map[string]bool{}}
	for _, ov := range ovs {
		if ov.recurID.allDay != it.allDay {
			replaced.date[ov.recurID.t.Format(dateLayout)] = true
		} else {
			replaced.inst[ov.recurID.t.Unix()] = true
		}
	}
	excluded := matcher{inst: it.exInst, date: it.exDate}
	var occ []Event
	consider := func(s time.Time) {
		if excluded.has(s) || replaced.has(s) {
			return
		}
		e := it.span.end(s)
		if !overlaps(s, e, p.from, p.to) {
			return
		}
		ev := it.ev
		ev.Start, ev.End, ev.AllDay = s.UTC(), e.UTC(), it.allDay
		switch {
		case it.hasRecurID: // an override without UID: keep its RECURRENCE-ID
			ev.Recurrence = formatRecurrence(it.recurID.t, it.recurID.allDay)
		case recurring:
			ev.Recurrence = formatRecurrence(s, it.allDay)
		}
		occ = append(occ, ev)
	}
	consider(it.start)
	if it.rule != nil {
		maxDur := it.span.end(it.start).Sub(it.start)
		it.rule.each(it.start, p.from, p.to, maxDur, &p.budget, func(s time.Time) bool {
			consider(s)
			return len(occ) < MaxEvents
		})
	}
	for _, rd := range it.rdates {
		consider(it.at(rd))
	}
	if len(it.rdates) > 0 {
		sortEvents(occ)
		occ = slices.CompactFunc(occ, func(a, b Event) bool { return a.Start.Equal(b.Start) })
	}
	return occ
}

// override turns an overriding VEVENT into its occurrence; master (may be
// nil) supplies the duration and empty texts.
func (p *expander) override(ov, master *item) (Event, bool) {
	if ov.cancelled {
		return Event{}, false
	}
	sp := ov.span
	if !ov.hasEnd && master != nil && (ov.allDay == master.allDay) {
		sp = master.span
	}
	e := sp.end(ov.start)
	if !overlaps(ov.start, e, p.from, p.to) {
		return Event{}, false
	}
	ev := ov.ev
	if master != nil {
		if ev.Summary == "" {
			ev.Summary = master.ev.Summary
		}
		if ev.Description == "" {
			ev.Description = master.ev.Description
		}
		if ev.Location == "" {
			ev.Location = master.ev.Location
		}
		if ev.URL == "" {
			ev.URL = master.ev.URL
		}
	}
	ev.Start, ev.End, ev.AllDay = ov.start.UTC(), e.UTC(), ov.allDay
	ev.Recurrence = formatRecurrence(ov.recurID.t, ov.recurID.allDay)
	return ev, true
}

// add collects occurrences. Whenever it holds well over MaxEvents it keeps
// the earliest and narrows the window, so memory stays bounded and later
// rules stop early.
func (p *expander) add(evs []Event) {
	p.out = append(p.out, evs...)
	if len(p.out) > 2*MaxEvents {
		p.trim()
	}
}

func (p *expander) trim() {
	sortEvents(p.out)
	if len(p.out) > MaxEvents {
		p.out = p.out[:MaxEvents]
		last := p.out[MaxEvents-1].Start.Add(time.Nanosecond)
		if p.to.IsZero() || last.Before(p.to) {
			p.to = last
		}
	}
}

func (p *expander) finish() {
	p.trim()
	if p.out == nil {
		p.out = []Event{}
	}
}

func sortEvents(evs []Event) {
	slices.SortStableFunc(evs, func(a, b Event) int {
		if c := a.Start.Compare(b.Start); c != 0 {
			return c
		}
		if c := a.End.Compare(b.End); c != 0 {
			return c
		}
		if c := strings.Compare(a.UID, b.UID); c != 0 {
			return c
		}
		return strings.Compare(a.Recurrence, b.Recurrence)
	})
}
