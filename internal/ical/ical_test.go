package ical

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// cal wraps VEVENT lines into a VCALENDAR with CRLF line ends.
func cal(lines ...string) []byte {
	all := append([]string{"BEGIN:VCALENDAR", "VERSION:2.0", "PRODID:-//test//EN"}, lines...)
	all = append(all, "END:VCALENDAR", "")
	return []byte(strings.Join(all, "\r\n"))
}

// vevent wraps property lines into a VEVENT.
func vev(lines ...string) []string {
	return append(append([]string{"BEGIN:VEVENT"}, lines...), "END:VEVENT")
}

func d(s string) time.Time {
	for _, layout := range []string{"2006-01-02T15:04:05Z07:00", "2006-01-02T15:04Z07:00", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	panic("bad test time " + s)
}

func mustParse(t *testing.T, data []byte, from, to time.Time) []Event {
	t.Helper()
	evs, err := Parse(data, from, to)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return evs
}

func startsOf(evs []Event) []string {
	out := []string{}
	for _, e := range evs {
		if e.AllDay {
			out = append(out, e.Start.Format("2006-01-02"))
		} else {
			out = append(out, e.Start.Format("2006-01-02T15:04Z"))
		}
	}
	return out
}

var (
	wide0 = d("1900-01-01")
	wide1 = d("2200-01-01")
)

func TestParseLine(t *testing.T) {
	tests := []struct {
		line   string
		name   string
		params map[string][]string
		value  string
		ok     bool
	}{
		{"SUMMARY:Hello", "SUMMARY", nil, "Hello", true},
		{"summary:lower", "SUMMARY", nil, "lower", true},
		{"DTSTART;TZID=Europe/Berlin:20260101T090000", "DTSTART", map[string][]string{"TZID": {"Europe/Berlin"}}, "20260101T090000", true},
		{`LOCATION;ALTREP="http://x.test/a;b:c":Room 1`, "LOCATION", map[string][]string{"ALTREP": {"http://x.test/a;b:c"}}, "Room 1", true},
		{`ATTENDEE;MEMBER="mailto:a@x.test","mailto:b@x.test";CN=Jane:mailto:j@x.test`, "ATTENDEE",
			map[string][]string{"MEMBER": {"mailto:a@x.test", "mailto:b@x.test"}, "CN": {"Jane"}}, "mailto:j@x.test", true},
		{`X-A;P=a,b:v:w`, "X-A", map[string][]string{"P": {"a", "b"}}, "v:w", true},
		{"no colon", "", nil, "", false},
		{`X;P="unterminated:v`, "", nil, "", false},
		{":value", "", nil, "", false},
	}
	for _, tt := range tests {
		p, ok := parseLine(tt.line)
		if ok != tt.ok {
			t.Errorf("%q: ok=%v", tt.line, ok)
			continue
		}
		if !ok {
			continue
		}
		if p.name != tt.name || p.value != tt.value || fmt.Sprint(p.params) != fmt.Sprint(tt.params) {
			t.Errorf("%q: got %q %v %q", tt.line, p.name, p.params, p.value)
		}
	}
}

func TestUnfoldAndEscape(t *testing.T) {
	tests := []struct {
		name string
		data string
		sum  string
		desc string
		loc  string
	}{
		{
			name: "CRLF folding with space and tab",
			data: "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:a\r\nDTSTART:20260105T100000Z\r\nSUMMARY:Team\r\n  meet\r\n\ting\r\nDESCRIPTION:Line 1\\nLine 2\\, with comma\\; semi \\\\ back\\Nslash\r\nLOCATION;ALTREP=\"cid:x\":Room\\, 1\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n",
			sum:  "Team meeting",
			desc: "Line 1\nLine 2, with comma; semi \\ back\nslash",
			loc:  "Room, 1",
		},
		{
			name: "bare LF and BOM",
			data: "\xef\xbb\xbfBEGIN:VCALENDAR\nBEGIN:VEVENT\nUID:a\nDTSTART:20260105T100000Z\nSUMMARY:Ab\n c\nEND:VEVENT\nEND:VCALENDAR\n",
			sum:  "Abc",
		},
		{
			name: "folded inside a multi-byte character",
			data: "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:a\r\nDTSTART:20260105T100000Z\r\nSUMMARY:Gr\xc3\r\n \xbc\xc3\x9fe\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n",
			sum:  "Grüße",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evs := mustParse(t, []byte(tt.data), wide0, wide1)
			if len(evs) != 1 {
				t.Fatalf("got %d events", len(evs))
			}
			e := evs[0]
			if e.Summary != tt.sum || e.Description != tt.desc || e.Location != tt.loc {
				t.Errorf("got %q / %q / %q", e.Summary, e.Description, e.Location)
			}
		})
	}
}

func TestDatesAndDurations(t *testing.T) {
	tests := []struct {
		name       string
		lines      []string
		start, end string
		allDay     bool
	}{
		{"all-day without DTEND", []string{"DTSTART;VALUE=DATE:20260105"}, "2026-01-05", "2026-01-06", true},
		{"all-day 8-digit without VALUE", []string{"DTSTART:20260105"}, "2026-01-05", "2026-01-06", true},
		{"all-day three days", []string{"DTSTART;VALUE=DATE:20260105", "DTEND;VALUE=DATE:20260108"}, "2026-01-05", "2026-01-08", true},
		{"all-day DURATION P1W", []string{"DTSTART;VALUE=DATE:20260105", "DURATION:P1W"}, "2026-01-05", "2026-01-12", true},
		{"UTC", []string{"DTSTART:20260105T100000Z", "DTEND:20260105T113000Z"}, "2026-01-05T10:00:00Z", "2026-01-05T11:30:00Z", false},
		{"timed without DTEND", []string{"DTSTART:20260105T100000Z"}, "2026-01-05T10:00:00Z", "2026-01-05T10:00:00Z", false},
		{"floating is UTC", []string{"DTSTART:20260105T100000", "DTEND:20260105T110000"}, "2026-01-05T10:00:00Z", "2026-01-05T11:00:00Z", false},
		{"unknown TZID is UTC", []string{"DTSTART;TZID=Mars/Olympus:20260105T100000"}, "2026-01-05T10:00:00Z", "2026-01-05T10:00:00Z", false},
		{"Berlin winter", []string{"DTSTART;TZID=Europe/Berlin:20260105T100000", "DTEND;TZID=Europe/Berlin:20260105T110000"}, "2026-01-05T09:00:00Z", "2026-01-05T10:00:00Z", false},
		{"Berlin summer", []string{"DTSTART;TZID=Europe/Berlin:20260705T100000", "DTEND;TZID=Europe/Berlin:20260705T110000"}, "2026-07-05T08:00:00Z", "2026-07-05T09:00:00Z", false},
		{"quoted TZID", []string{`DTSTART;TZID="Europe/Berlin":20260105T100000`}, "2026-01-05T09:00:00Z", "2026-01-05T09:00:00Z", false},
		{"mozilla TZID prefix", []string{"DTSTART;TZID=/mozilla.org/20050126_1/Europe/Berlin:20260105T100000"}, "2026-01-05T09:00:00Z", "2026-01-05T09:00:00Z", false},
		{"Berlin across DST", []string{"DTSTART;TZID=Europe/Berlin:20260328T220000", "DTEND;TZID=Europe/Berlin:20260329T040000"}, "2026-03-28T21:00:00Z", "2026-03-29T02:00:00Z", false},
		{"DURATION PT1H30M", []string{"DTSTART:20260105T100000Z", "DURATION:PT1H30M"}, "2026-01-05T10:00:00Z", "2026-01-05T11:30:00Z", false},
		{"DURATION P1DT2H", []string{"DTSTART:20260105T100000Z", "DURATION:P1DT2H"}, "2026-01-05T10:00:00Z", "2026-01-06T12:00:00Z", false},
		{"DURATION P1D nominal over DST", []string{"DTSTART;TZID=Europe/Berlin:20260328T120000", "DURATION:P1D"}, "2026-03-28T11:00:00Z", "2026-03-29T10:00:00Z", false},
		{"negative DURATION ignored", []string{"DTSTART:20260105T100000Z", "DURATION:-PT1H"}, "2026-01-05T10:00:00Z", "2026-01-05T10:00:00Z", false},
		{"DTEND before DTSTART", []string{"DTSTART:20260105T100000Z", "DTEND:20260105T090000Z"}, "2026-01-05T10:00:00Z", "2026-01-05T10:00:00Z", false},
		{"DTEND wins over DURATION", []string{"DTSTART:20260105T100000Z", "DTEND:20260105T103000Z", "DURATION:PT2H"}, "2026-01-05T10:00:00Z", "2026-01-05T10:30:00Z", false},
		{"time without seconds", []string{"DTSTART:20260105T1000Z"}, "2026-01-05T10:00:00Z", "2026-01-05T10:00:00Z", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evs := mustParse(t, cal(vev(append([]string{"UID:x"}, tt.lines...)...)...), wide0, wide1)
			if len(evs) != 1 {
				t.Fatalf("got %d events", len(evs))
			}
			e := evs[0]
			if !e.Start.Equal(d(tt.start)) || !e.End.Equal(d(tt.end)) || e.AllDay != tt.allDay {
				t.Errorf("got %s – %s allDay=%v", e.Start, e.End, e.AllDay)
			}
			if e.Start.Location() != time.UTC || e.End.Location() != time.UTC {
				t.Errorf("times not in UTC: %v", e.Start.Location())
			}
			if e.Recurrence != "" {
				t.Errorf("single event has Recurrence %q", e.Recurrence)
			}
		})
	}
}

func TestWindowsTZID(t *testing.T) {
	tests := []struct {
		tzid string
		want string // UTC of 2026-01-15 10:00 local
	}{
		{"W. Europe Standard Time", "2026-01-15T09:00:00Z"},
		{"Central Europe Standard Time", "2026-01-15T09:00:00Z"},
		{"Romance Standard Time", "2026-01-15T09:00:00Z"},
		{"GMT Standard Time", "2026-01-15T10:00:00Z"},
		{"Eastern Standard Time", "2026-01-15T15:00:00Z"},
		{"Central Standard Time", "2026-01-15T16:00:00Z"},
		{"Pacific Standard Time", "2026-01-15T18:00:00Z"},
		{"FLE Standard Time", "2026-01-15T08:00:00Z"},
		{"Tokyo Standard Time", "2026-01-15T01:00:00Z"},
		{"(UTC+01:00) Amsterdam, Berlin, Bern, Rome, Stockholm, Vienna", "2026-01-15T09:00:00Z"},
	}
	for _, tt := range tests {
		evs := mustParse(t, cal(vev("UID:w", `DTSTART;TZID="`+tt.tzid+`":20260115T100000`)...), wide0, wide1)
		if len(evs) != 1 || !evs[0].Start.Equal(d(tt.want)) {
			t.Errorf("%s: got %v", tt.tzid, startsOf(evs))
		}
	}
	for tzid, name := range windowsZones {
		if _, err := time.LoadLocation(name); err != nil {
			t.Errorf("%s maps to unknown zone %s", tzid, name)
		}
	}
	// Summer time is applied for mapped zones too.
	evs := mustParse(t, cal(vev("UID:w", "DTSTART;TZID=W. Europe Standard Time:20260715T100000")...), wide0, wide1)
	if len(evs) != 1 || !evs[0].Start.Equal(d("2026-07-15T08:00:00Z")) {
		t.Errorf("summer: got %v", startsOf(evs))
	}
}

func TestRecurrence(t *testing.T) {
	tests := []struct {
		name     string
		lines    []string
		from, to string
		want     []string
	}{
		{"daily interval count", []string{"DTSTART:20260105T090000Z", "RRULE:FREQ=DAILY;INTERVAL=2;COUNT=4"}, "2026-01-01", "2026-12-31",
			[]string{"2026-01-05T09:00Z", "2026-01-07T09:00Z", "2026-01-09T09:00Z", "2026-01-11T09:00Z"}},
		{"daily until inclusive", []string{"DTSTART:20260105T090000Z", "RRULE:FREQ=DAILY;UNTIL=20260107T090000Z"}, "2026-01-01", "2026-12-31",
			[]string{"2026-01-05T09:00Z", "2026-01-06T09:00Z", "2026-01-07T09:00Z"}},
		{"daily until date for timed event", []string{"DTSTART;TZID=Europe/Berlin:20260105T230000", "RRULE:FREQ=DAILY;UNTIL=20260106"}, "2026-01-01", "2026-12-31",
			[]string{"2026-01-05T22:00Z", "2026-01-06T22:00Z"}},
		{"daily byday weekdays", []string{"DTSTART:20260102T090000Z", "RRULE:FREQ=DAILY;BYDAY=MO,TU,WE,TH,FR;COUNT=4"}, "2026-01-01", "2026-12-31",
			[]string{"2026-01-02T09:00Z", "2026-01-05T09:00Z", "2026-01-06T09:00Z", "2026-01-07T09:00Z"}},
		{"daily bymonth", []string{"DTSTART;VALUE=DATE:20260130", "RRULE:FREQ=DAILY;BYMONTH=1,3;COUNT=4"}, "2026-01-01", "2026-12-31",
			[]string{"2026-01-30", "2026-01-31", "2026-03-01", "2026-03-02"}},
		{"weekly plain", []string{"DTSTART:20260105T090000Z", "RRULE:FREQ=WEEKLY;COUNT=3"}, "2026-01-01", "2026-12-31",
			[]string{"2026-01-05T09:00Z", "2026-01-12T09:00Z", "2026-01-19T09:00Z"}},
		{"weekly byday", []string{"DTSTART:20260105T090000Z", "RRULE:FREQ=WEEKLY;BYDAY=MO,WE,FR;COUNT=5"}, "2026-01-01", "2026-12-31",
			[]string{"2026-01-05T09:00Z", "2026-01-07T09:00Z", "2026-01-09T09:00Z", "2026-01-12T09:00Z", "2026-01-14T09:00Z"}},
		{"weekly interval 2 until", []string{"DTSTART:20260106T090000Z", "RRULE:FREQ=WEEKLY;INTERVAL=2;BYDAY=TU,TH;UNTIL=20260130T000000Z"}, "2026-01-01", "2026-12-31",
			[]string{"2026-01-06T09:00Z", "2026-01-08T09:00Z", "2026-01-20T09:00Z", "2026-01-22T09:00Z"}},
		{"weekly wkst su", []string{"DTSTART:20260106T090000Z", "RRULE:FREQ=WEEKLY;INTERVAL=2;BYDAY=TU,SU;WKST=SU;COUNT=4"}, "2026-01-01", "2026-12-31",
			[]string{"2026-01-06T09:00Z", "2026-01-18T09:00Z", "2026-01-20T09:00Z", "2026-02-01T09:00Z"}},
		{"monthly by date", []string{"DTSTART:20260115T090000Z", "RRULE:FREQ=MONTHLY;COUNT=3"}, "2026-01-01", "2026-12-31",
			[]string{"2026-01-15T09:00Z", "2026-02-15T09:00Z", "2026-03-15T09:00Z"}},
		{"monthly 31st skips short months", []string{"DTSTART:20260131T090000Z", "RRULE:FREQ=MONTHLY;COUNT=4"}, "2026-01-01", "2026-12-31",
			[]string{"2026-01-31T09:00Z", "2026-03-31T09:00Z", "2026-05-31T09:00Z", "2026-07-31T09:00Z"}},
		{"monthly bymonthday incl. last", []string{"DTSTART:20260115T090000Z", "RRULE:FREQ=MONTHLY;BYMONTHDAY=15,-1;COUNT=5"}, "2026-01-01", "2026-12-31",
			[]string{"2026-01-15T09:00Z", "2026-01-31T09:00Z", "2026-02-15T09:00Z", "2026-02-28T09:00Z", "2026-03-15T09:00Z"}},
		{"monthly second monday", []string{"DTSTART:20260112T090000Z", "RRULE:FREQ=MONTHLY;BYDAY=2MO;COUNT=3"}, "2026-01-01", "2026-12-31",
			[]string{"2026-01-12T09:00Z", "2026-02-09T09:00Z", "2026-03-09T09:00Z"}},
		{"monthly last friday", []string{"DTSTART:20260130T090000Z", "RRULE:FREQ=MONTHLY;BYDAY=-1FR;COUNT=3"}, "2026-01-01", "2026-12-31",
			[]string{"2026-01-30T09:00Z", "2026-02-27T09:00Z", "2026-03-27T09:00Z"}},
		{"monthly every tuesday", []string{"DTSTART:20260106T090000Z", "RRULE:FREQ=MONTHLY;BYDAY=TU;UNTIL=20260131T000000Z"}, "2026-01-01", "2026-12-31",
			[]string{"2026-01-06T09:00Z", "2026-01-13T09:00Z", "2026-01-20T09:00Z", "2026-01-27T09:00Z"}},
		{"monthly friday the 13th", []string{"DTSTART:20260213T090000Z", "RRULE:FREQ=MONTHLY;BYDAY=FR;BYMONTHDAY=13;COUNT=3"}, "2026-01-01", "2028-12-31",
			[]string{"2026-02-13T09:00Z", "2026-03-13T09:00Z", "2026-11-13T09:00Z"}},
		{"monthly last workday (bysetpos)", []string{"DTSTART:20260130T090000Z", "RRULE:FREQ=MONTHLY;BYDAY=MO,TU,WE,TH,FR;BYSETPOS=-1;COUNT=3"}, "2026-01-01", "2026-12-31",
			[]string{"2026-01-30T09:00Z", "2026-02-27T09:00Z", "2026-03-31T09:00Z"}},
		{"monthly interval 3", []string{"DTSTART;VALUE=DATE:20260101", "RRULE:FREQ=MONTHLY;INTERVAL=3;COUNT=3"}, "2026-01-01", "2026-12-31",
			[]string{"2026-01-01", "2026-04-01", "2026-07-01"}},
		{"yearly plain", []string{"DTSTART;VALUE=DATE:20200310", "RRULE:FREQ=YEARLY"}, "2025-01-01", "2028-01-01",
			[]string{"2025-03-10", "2026-03-10", "2027-03-10"}},
		{"yearly bymonth", []string{"DTSTART:20260310T090000Z", "RRULE:FREQ=YEARLY;BYMONTH=3,6;COUNT=4"}, "2026-01-01", "2030-12-31",
			[]string{"2026-03-10T09:00Z", "2026-06-10T09:00Z", "2027-03-10T09:00Z", "2027-06-10T09:00Z"}},
		{"yearly feb 29", []string{"DTSTART;VALUE=DATE:20240229", "RRULE:FREQ=YEARLY;COUNT=3"}, "2024-01-01", "2040-01-01",
			[]string{"2024-02-29", "2028-02-29", "2032-02-29"}},
		{"yearly thanksgiving", []string{"DTSTART:20261126T170000Z", "RRULE:FREQ=YEARLY;BYMONTH=11;BYDAY=4TH;COUNT=3"}, "2026-01-01", "2030-12-31",
			[]string{"2026-11-26T17:00Z", "2027-11-25T17:00Z", "2028-11-23T17:00Z"}},
		{"yearly 20th monday", []string{"DTSTART:20260518T090000Z", "RRULE:FREQ=YEARLY;BYDAY=20MO;COUNT=2"}, "2026-01-01", "2030-12-31",
			[]string{"2026-05-18T09:00Z", "2027-05-17T09:00Z"}},
		{"yearly byyearday", []string{"DTSTART;VALUE=DATE:20260101", "RRULE:FREQ=YEARLY;BYYEARDAY=1,-1;COUNT=3"}, "2026-01-01", "2030-12-31",
			[]string{"2026-01-01", "2026-12-31", "2027-01-01"}},
		{"count with window in the middle", []string{"DTSTART:20260101T090000Z", "RRULE:FREQ=DAILY;COUNT=10"}, "2026-01-08", "2026-01-31",
			[]string{"2026-01-08T09:00Z", "2026-01-09T09:00Z", "2026-01-10T09:00Z"}},
		{"open rule clipped to window", []string{"DTSTART:20000103T090000Z", "RRULE:FREQ=WEEKLY"}, "2026-01-05", "2026-01-20",
			[]string{"2026-01-05T09:00Z", "2026-01-12T09:00Z", "2026-01-19T09:00Z"}},
		{"open daily rule from 1990", []string{"DTSTART:19900101T090000Z", "RRULE:FREQ=DAILY;INTERVAL=3"}, "2026-01-01", "2026-01-08",
			[]string{"2026-01-01T09:00Z", "2026-01-04T09:00Z", "2026-01-07T09:00Z"}},
		{"long occurrence overlapping window start", []string{"DTSTART;VALUE=DATE:20260101", "DTEND;VALUE=DATE:20260111", "RRULE:FREQ=MONTHLY"}, "2026-01-10", "2026-02-05",
			[]string{"2026-01-01", "2026-02-01"}},
		{"exdate list and tzid", []string{"DTSTART;TZID=Europe/Berlin:20260105T100000", "RRULE:FREQ=DAILY;COUNT=5",
			"EXDATE;TZID=Europe/Berlin:20260106T100000,20260108T100000"}, "2026-01-01", "2026-12-31",
			[]string{"2026-01-05T09:00Z", "2026-01-07T09:00Z", "2026-01-09T09:00Z"}},
		{"exdate utc and repeated property", []string{"DTSTART;TZID=Europe/Berlin:20260105T100000", "RRULE:FREQ=DAILY;COUNT=4",
			"EXDATE:20260106T090000Z", "EXDATE:20260107T090000Z"}, "2026-01-01", "2026-12-31",
			[]string{"2026-01-05T09:00Z", "2026-01-08T09:00Z"}},
		{"exdate date for timed event", []string{"DTSTART:20260105T090000Z", "RRULE:FREQ=DAILY;COUNT=3", "EXDATE;VALUE=DATE:20260106"}, "2026-01-01", "2026-12-31",
			[]string{"2026-01-05T09:00Z", "2026-01-07T09:00Z"}},
		{"all-day exdate", []string{"DTSTART;VALUE=DATE:20260105", "RRULE:FREQ=WEEKLY;COUNT=3", "EXDATE;VALUE=DATE:20260112"}, "2026-01-01", "2026-12-31",
			[]string{"2026-01-05", "2026-01-19"}},
		{"exdate of dtstart", []string{"DTSTART:20260105T090000Z", "RRULE:FREQ=DAILY;COUNT=2", "EXDATE:20260105T090000Z"}, "2026-01-01", "2026-12-31",
			[]string{"2026-01-06T09:00Z"}},
		{"rdate date-times and dates", []string{"DTSTART:20260105T090000Z", "RRULE:FREQ=WEEKLY;COUNT=2",
			"RDATE:20260107T150000Z,20260112T090000Z", "RDATE;VALUE=DATE:20260120"}, "2026-01-01", "2026-12-31",
			[]string{"2026-01-05T09:00Z", "2026-01-07T15:00Z", "2026-01-12T09:00Z", "2026-01-20T09:00Z"}},
		{"rdate without rrule", []string{"DTSTART:20260105T090000Z", "RDATE;VALUE=PERIOD:20260110T090000Z/PT1H"}, "2026-01-01", "2026-12-31",
			[]string{"2026-01-05T09:00Z", "2026-01-10T09:00Z"}},
		{"Berlin weekly across DST keeps wall clock", []string{"DTSTART;TZID=Europe/Berlin:20260316T090000", "RRULE:FREQ=WEEKLY;COUNT=4"}, "2026-01-01", "2026-12-31",
			[]string{"2026-03-16T08:00Z", "2026-03-23T08:00Z", "2026-03-30T07:00Z", "2026-04-06T07:00Z"}},
		{"unsupported freq keeps dtstart", []string{"DTSTART:20260105T090000Z", "RRULE:FREQ=HOURLY;COUNT=5"}, "2026-01-01", "2026-12-31",
			[]string{"2026-01-05T09:00Z"}},
		{"unknown parts ignored", []string{"DTSTART:20260105T090000Z", "RRULE:FREQ=WEEKLY;BYHOUR=9;X-FOO=1;COUNT=2"}, "2026-01-01", "2026-12-31",
			[]string{"2026-01-05T09:00Z", "2026-01-12T09:00Z"}},
		{"until before dtstart", []string{"DTSTART:20260105T090000Z", "RRULE:FREQ=DAILY;UNTIL=20250101T000000Z"}, "2026-01-01", "2026-12-31",
			[]string{"2026-01-05T09:00Z"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evs := mustParse(t, cal(vev(append([]string{"UID:r", "SUMMARY:R"}, tt.lines...)...)...), d(tt.from), d(tt.to))
			if got := startsOf(evs); !slices.Equal(got, tt.want) {
				t.Errorf("got  %v\nwant %v", got, tt.want)
			}
		})
	}
}

func TestRecurrenceFields(t *testing.T) {
	evs := mustParse(t, cal(vev("UID:r", "SUMMARY:R", "DTSTART;TZID=Europe/Berlin:20260105T100000",
		"DTEND;TZID=Europe/Berlin:20260105T113000", "RRULE:FREQ=DAILY;COUNT=2")...), wide0, wide1)
	if len(evs) != 2 {
		t.Fatalf("got %d", len(evs))
	}
	if evs[1].Recurrence != "20260106T090000Z" || !evs[1].End.Equal(d("2026-01-06T10:30:00Z")) || evs[1].Summary != "R" {
		t.Errorf("got %+v", evs[1])
	}
	evs = mustParse(t, cal(vev("UID:a", "DTSTART;VALUE=DATE:20260105", "RRULE:FREQ=DAILY;COUNT=2")...), wide0, wide1)
	if len(evs) != 2 || evs[0].Recurrence != "20260105" || !evs[0].AllDay || !evs[0].End.Equal(d("2026-01-06")) {
		t.Errorf("got %+v", evs)
	}
}

func TestOverrides(t *testing.T) {
	master := vev("UID:m", "SUMMARY:Standup", "LOCATION:Room 1", "DTSTART;TZID=Europe/Berlin:20260105T100000",
		"DTEND;TZID=Europe/Berlin:20260105T101500", "RRULE:FREQ=DAILY;COUNT=4")
	moved := vev("UID:m", "RECURRENCE-ID;TZID=Europe/Berlin:20260106T100000", "SUMMARY:Standup (moved)",
		"DTSTART;TZID=Europe/Berlin:20260106T140000", "DTEND;TZID=Europe/Berlin:20260106T143000")
	cancelled := vev("UID:m", "RECURRENCE-ID:20260107T090000Z", "STATUS:CANCELLED", "DTSTART:20260107T090000Z")
	noEnd := vev("UID:m", "RECURRENCE-ID;TZID=Europe/Berlin:20260108T100000", "DTSTART;TZID=Europe/Berlin:20260108T110000")

	// Overrides may come before the master and in another VCALENDAR.
	data := append(cal(append(moved, cancelled...)...), cal(append(master, noEnd...)...)...)
	evs := mustParse(t, data, wide0, wide1)
	want := []string{"2026-01-05T09:00Z", "2026-01-06T13:00Z", "2026-01-08T10:00Z"}
	if got := startsOf(evs); !slices.Equal(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	if evs[1].Summary != "Standup (moved)" || evs[1].Recurrence != "20260106T090000Z" || evs[1].Location != "Room 1" ||
		!evs[1].End.Equal(d("2026-01-06T13:30:00Z")) {
		t.Errorf("moved: %+v", evs[1])
	}
	if evs[2].Summary != "Standup" || !evs[2].End.Equal(d("2026-01-08T10:15:00Z")) || evs[2].Recurrence != "20260108T090000Z" {
		t.Errorf("no end: %+v", evs[2])
	}

	// An override moved out of the window removes the occurrence from it;
	// one moved into the window shows up.
	evs = mustParse(t, data, d("2026-01-06T08:00:00Z"), d("2026-01-06T12:00:00Z"))
	if len(evs) != 0 {
		t.Errorf("moved out: %v", startsOf(evs))
	}

	// Cancelled master drops the whole series including overrides.
	cm := vev("UID:c", "STATUS:CANCELLED", "DTSTART:20260105T090000Z", "RRULE:FREQ=DAILY;COUNT=3")
	co := vev("UID:c", "RECURRENCE-ID:20260106T090000Z", "DTSTART:20260106T100000Z")
	if evs := mustParse(t, cal(append(cm, co...)...), wide0, wide1); len(evs) != 0 {
		t.Errorf("cancelled series: %v", startsOf(evs))
	}

	// Server-side expanded data: instances only, no master.
	exp := cal(append(append(
		vev("UID:e", "SUMMARY:E", "RECURRENCE-ID:20260105T090000Z", "DTSTART:20260105T090000Z", "DURATION:PT1H"),
		vev("UID:e", "SUMMARY:E", "RECURRENCE-ID:20260112T090000Z", "DTSTART:20260112T090000Z", "DURATION:PT1H")...),
		vev("UID:e", "SUMMARY:E", "RECURRENCE-ID;VALUE=DATE:20260119", "DTSTART;VALUE=DATE:20260119")...)...)
	evs = mustParse(t, exp, wide0, wide1)
	if len(evs) != 3 || evs[0].Recurrence != "20260105T090000Z" || evs[2].Recurrence != "20260119" || !evs[2].AllDay {
		t.Errorf("expanded: %+v", evs)
	}

	// All-day override by DATE of an all-day series.
	ad := cal(append(vev("UID:a", "DTSTART;VALUE=DATE:20260105", "RRULE:FREQ=WEEKLY;COUNT=3"),
		vev("UID:a", "RECURRENCE-ID;VALUE=DATE:20260112", "DTSTART;VALUE=DATE:20260113", "SUMMARY:Later")...)...)
	if got := startsOf(mustParse(t, ad, wide0, wide1)); !slices.Equal(got, []string{"2026-01-05", "2026-01-13", "2026-01-19"}) {
		t.Errorf("all-day override: %v", got)
	}
}

func TestWindowClipping(t *testing.T) {
	data := cal(slices.Concat(
		vev("UID:before", "DTSTART:20260104T100000Z", "DTEND:20260105T000000Z"), // ends exactly at from
		vev("UID:span", "DTSTART:20260104T220000Z", "DTEND:20260105T010000Z"),   // spans from
		vev("UID:atfrom", "DTSTART:20260105T000000Z"),                           // zero length at from
		vev("UID:inside", "DTSTART:20260106T100000Z", "DTEND:20260106T110000Z"),
		vev("UID:atto", "DTSTART:20260108T000000Z", "DTEND:20260108T010000Z"), // starts at to
		vev("UID:allday", "DTSTART;VALUE=DATE:20260107"),                      // last day
		vev("UID:allday-before", "DTSTART;VALUE=DATE:20260104"),               // ends at from
	)...)
	evs := mustParse(t, data, d("2026-01-05"), d("2026-01-08"))
	var uids []string
	for _, e := range evs {
		uids = append(uids, e.UID)
	}
	if want := []string{"span", "atfrom", "inside", "allday"}; !slices.Equal(uids, want) {
		t.Errorf("got %v want %v", uids, want)
	}
	if evs := mustParse(t, data, d("2026-01-08"), d("2026-01-05")); len(evs) != 0 {
		t.Errorf("reversed window: %d events", len(evs))
	}
	if evs := mustParse(t, data, time.Time{}, time.Time{}); len(evs) != 7 {
		t.Errorf("open window: %d events", len(evs))
	}
}

func TestSkippedComponents(t *testing.T) {
	data := cal(
		"BEGIN:VTIMEZONE", "TZID:Europe/Berlin", "BEGIN:STANDARD", "DTSTART:19701025T030000", "END:STANDARD", "END:VTIMEZONE",
		"BEGIN:VTODO", "UID:todo", "DTSTART:20260105T090000Z", "SUMMARY:Todo", "END:VTODO",
		"BEGIN:VJOURNAL", "UID:journal", "DTSTART:20260105T090000Z", "END:VJOURNAL",
		"BEGIN:VEVENT", "UID:ev", "SUMMARY:Event", "DTSTART:20260105T090000Z",
		"BEGIN:VALARM", "ACTION:DISPLAY", "DESCRIPTION:Alarm text", "TRIGGER:-PT15M", "SUMMARY:Alarm", "END:VALARM",
		"DESCRIPTION:Real", "END:VEVENT",
		"BEGIN:VEVENT", "UID:cancelled", "STATUS:CANCELLED", "DTSTART:20260105T090000Z", "END:VEVENT",
		"BEGIN:VEVENT", "UID:nostart", "SUMMARY:No start", "END:VEVENT",
		"BEGIN:VEVENT", "UID:badstart", "DTSTART:2026-01-05", "END:VEVENT",
		"garbage line without colon",
		"END:VALARM",
	)
	evs := mustParse(t, data, wide0, wide1)
	if len(evs) != 1 || evs[0].UID != "ev" || evs[0].Summary != "Event" || evs[0].Description != "Real" {
		t.Errorf("got %+v", evs)
	}
}

func TestUpdatedAndURL(t *testing.T) {
	evs := mustParse(t, cal(slices.Concat(
		vev("UID:a", "DTSTART:20260105T090000Z", "DTSTAMP:20260101T000000Z", "LAST-MODIFIED:20251231T120000Z", "URL:https://x.test/a?b=1,2"),
		vev("UID:b", "DTSTART:20260105T100000Z", "DTSTAMP:20260102T000000Z"),
	)...), wide0, wide1)
	if !evs[0].Updated.Equal(d("2025-12-31T12:00:00Z")) || evs[0].URL != "https://x.test/a?b=1,2" {
		t.Errorf("a: %+v", evs[0])
	}
	if !evs[1].Updated.Equal(d("2026-01-02T00:00:00Z")) {
		t.Errorf("b: %+v", evs[1])
	}
}

func TestErrors(t *testing.T) {
	for _, in := range []string{"", "hello world", "<html><body>BEGIN:VEVENT</body></html>", "BEGIN:VEVENT\r\nEND:VEVENT\r\n"} {
		if _, err := Parse([]byte(in), wide0, wide1); !errors.Is(err, ErrNotICalendar) {
			t.Errorf("%q: err=%v", in, err)
		}
	}
	big := make([]byte, MaxInputSize+1)
	if _, err := Parse(big, wide0, wide1); !errors.Is(err, ErrTooLarge) {
		t.Errorf("too large: %v", err)
	}
	// An empty calendar is fine.
	evs, err := Parse(cal(), wide0, wide1)
	if err != nil || evs == nil || len(evs) != 0 {
		t.Errorf("empty: %v %v", evs, err)
	}
}

func TestCaps(t *testing.T) {
	// An open daily rule without window bounds stops at MaxEvents.
	evs := mustParse(t, cal(vev("UID:a", "DTSTART:20260101T090000Z", "RRULE:FREQ=DAILY")...), time.Time{}, time.Time{})
	if len(evs) != MaxEvents {
		t.Errorf("open rule: %d events", len(evs))
	}
	// The 100-year horizon: a yearly rule with a huge COUNT ends after 100 years.
	evs = mustParse(t, cal(vev("UID:y", "DTSTART:20000101T090000Z", "RRULE:FREQ=YEARLY;COUNT=100000")...), time.Time{}, time.Time{})
	if len(evs) != 101 || evs[100].Start.Year() != 2100 {
		t.Errorf("100 years: %d events, last %v", len(evs), evs[len(evs)-1].Start)
	}
	// Starting long ago, an open rule still reaches today (window-based horizon).
	evs = mustParse(t, cal(vev("UID:b", "DTSTART;VALUE=DATE:19000101", "RRULE:FREQ=YEARLY")...), d("2026-01-01"), d("2027-01-01"))
	if got := startsOf(evs); !slices.Equal(got, []string{"2026-01-01"}) {
		t.Errorf("old yearly: %v", got)
	}
	// A rule that never matches terminates.
	start := time.Now()
	evs = mustParse(t, cal(vev("UID:n", "DTSTART:20260101T090000Z", "RRULE:FREQ=DAILY;BYMONTH=2;BYMONTHDAY=30")...), time.Time{}, time.Time{})
	if len(evs) != 1 || time.Since(start) > 10*time.Second {
		t.Errorf("never matching rule: %d events after %v", len(evs), time.Since(start))
	}
	// Many single events: at most MaxEvents, the earliest ones, sorted.
	var lines []string
	base := d("2026-01-01")
	for i := 6000; i > 0; i-- {
		lines = append(lines, vev(fmt.Sprintf("UID:e%d", i), "DTSTART:"+base.Add(time.Duration(i)*time.Hour).Format(utcLayout))...)
	}
	evs = mustParse(t, cal(lines...), time.Time{}, time.Time{})
	if len(evs) != MaxEvents || evs[0].UID != "e1" || evs[MaxEvents-1].UID != fmt.Sprintf("e%d", MaxEvents) {
		t.Errorf("many: %d events, first %s, last %s", len(evs), evs[0].UID, evs[len(evs)-1].UID)
	}
	if !slices.IsSortedFunc(evs, func(a, b Event) int { return a.Start.Compare(b.Start) }) {
		t.Error("not sorted")
	}
	// Many recurring events together stay capped and keep the earliest.
	lines = nil
	for i := range 20 {
		lines = append(lines, vev(fmt.Sprintf("UID:r%d", i), fmt.Sprintf("DTSTART:202601%02dT090000Z", i+1), "RRULE:FREQ=DAILY")...)
	}
	evs = mustParse(t, cal(lines...), d("2026-01-01"), d("2036-01-01"))
	if len(evs) != MaxEvents || evs[0].UID != "r0" || !evs[0].Start.Equal(d("2026-01-01T09:00:00Z")) {
		t.Errorf("many rules: %d events, first %+v", len(evs), evs[0])
	}
}

func TestEncode(t *testing.T) {
	now = func() time.Time { return d("2026-11-17T08:30:00Z") }
	defer func() { now = time.Now }()
	got := string(Encode("-//Phoedox//Calendar//EN", []Event{
		{UID: "a@phoedox", Summary: "Service; boiler, yearly", Description: "Line 1\nLine 2 \\ end",
			Location: "Cellar", URL: "https://x.test/e/home/boiler",
			Start: d("2026-12-01T09:00:00+01:00"), End: d("2026-12-01T10:00:00+01:00"), Updated: d("2026-11-01T12:00:00Z")},
		{UID: "b@phoedox", Summary: "Holiday", AllDay: true, Start: d("2026-12-24"), End: d("2026-12-27")},
		{UID: "c@phoedox", Summary: "Day", AllDay: true, Start: d("2026-12-31")},
	}))
	want := strings.Join([]string{
		"BEGIN:VCALENDAR",
		"VERSION:2.0",
		"PRODID:-//Phoedox//Calendar//EN",
		"CALSCALE:GREGORIAN",
		"BEGIN:VEVENT",
		"UID:a@phoedox",
		"DTSTAMP:20261117T083000Z",
		"DTSTART:20261201T080000Z",
		"DTEND:20261201T090000Z",
		"LAST-MODIFIED:20261101T120000Z",
		`SUMMARY:Service\; boiler\, yearly`,
		`DESCRIPTION:Line 1\nLine 2 \\ end`,
		"LOCATION:Cellar",
		"URL:https://x.test/e/home/boiler",
		"END:VEVENT",
		"BEGIN:VEVENT",
		"UID:b@phoedox",
		"DTSTAMP:20261117T083000Z",
		"DTSTART;VALUE=DATE:20261224",
		"DTEND;VALUE=DATE:20261227",
		"SUMMARY:Holiday",
		"END:VEVENT",
		"BEGIN:VEVENT",
		"UID:c@phoedox",
		"DTSTAMP:20261117T083000Z",
		"DTSTART;VALUE=DATE:20261231",
		"DTEND;VALUE=DATE:20270101",
		"SUMMARY:Day",
		"END:VEVENT",
		"END:VCALENDAR",
		"",
	}, "\r\n")
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestEncodeFolding(t *testing.T) {
	summary := strings.Repeat("Grüße 😀 ", 30) + "end"
	desc := strings.Repeat("x", 300) + "\nzweite Zeile ä"
	out := Encode("p", []Event{{UID: "f", Summary: summary, Description: desc, Start: d("2026-01-05T09:00:00Z")}})
	if !bytes.HasSuffix(out, []byte("\r\n")) {
		t.Error("no CRLF at the end")
	}
	for i, line := range bytes.Split(bytes.TrimSuffix(out, []byte("\r\n")), []byte("\r\n")) {
		if len(line) > 75 {
			t.Errorf("line %d has %d octets", i, len(line))
		}
		if !utf8.Valid(line) {
			t.Errorf("line %d splits a UTF-8 sequence: %q", i, line)
		}
		if bytes.ContainsAny(line, "\r\n") {
			t.Errorf("line %d contains a bare line break", i)
		}
	}
	evs := mustParse(t, out, wide0, wide1)
	if len(evs) != 1 || evs[0].Summary != summary || evs[0].Description != desc {
		t.Errorf("round trip: %+v", evs)
	}
	// Exactly 75 octets stay on one line, 76 fold.
	var b bytes.Buffer
	writeFolded(&b, strings.Repeat("a", 75))
	writeFolded(&b, strings.Repeat("a", 76))
	if want := strings.Repeat("a", 75) + "\r\n" + strings.Repeat("a", 75) + "\r\n a\r\n"; b.String() != want {
		t.Errorf("fold: %q", b.String())
	}
	// A 2-byte character at the boundary moves to the next line.
	b.Reset()
	writeFolded(&b, strings.Repeat("a", 74)+"ü")
	if want := strings.Repeat("a", 74) + "\r\n ü\r\n"; b.String() != want {
		t.Errorf("fold multi-byte: %q", b.String())
	}
}

func TestEncodeRoundTrip(t *testing.T) {
	in := []Event{
		{UID: "t1", Summary: "A, B; C \\ D", Description: "multi\nline", Location: "Hall", URL: "https://x.test/1",
			Start: d("2026-03-29T00:30:00Z"), End: d("2026-03-29T02:00:00Z"), Updated: d("2026-03-01T00:00:00Z")},
		{UID: "t2", Summary: "All day", AllDay: true, Start: d("2026-04-01"), End: d("2026-04-03"), Updated: d("2026-03-02T00:00:00Z")},
		{UID: "t3", Summary: "Point", Start: d("2026-04-05T12:00:00Z"), End: d("2026-04-05T12:00:00Z"), Updated: d("2026-03-03T00:00:00Z")},
	}
	out, err := Parse(Encode("p", in), wide0, wide1)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != len(in) {
		t.Fatalf("got %d events", len(out))
	}
	for i := range in {
		if out[i] != in[i] {
			t.Errorf("event %d:\n got %+v\nwant %+v", i, out[i], in[i])
		}
	}
}

func FuzzParse(f *testing.F) {
	f.Add(cal(vev("UID:a", "DTSTART;TZID=Europe/Berlin:20260105T100000", "RRULE:FREQ=MONTHLY;BYDAY=-1FR;BYSETPOS=1", "EXDATE:20260105T090000Z")...))
	f.Add(cal(vev("UID:b", "DTSTART;VALUE=DATE:20240229", "DURATION:P1W", "RRULE:FREQ=YEARLY;BYYEARDAY=-1;BYMONTH=2")...))
	f.Add([]byte("BEGIN:VCALENDAR\nBEGIN:VEVENT\nDTSTART:2\n END:VEVENT"))
	f.Fuzz(func(t *testing.T, data []byte) {
		evs, err := Parse(data, d("2026-01-01"), d("2027-01-01"))
		if err == nil && len(evs) > MaxEvents {
			t.Fatalf("%d events", len(evs))
		}
		_ = Encode("p", evs)
	})
}
