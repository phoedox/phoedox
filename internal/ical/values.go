package ical

import (
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"
	_ "time/tzdata" // TZID lookup must not depend on the host's zoneinfo
)

const (
	dateLayout  = "20060102"
	localLayout = "20060102T150405"
	utcLayout   = "20060102T150405Z"
)

var errBadDate = errors.New("ical: bad date")

// dateTime is a parsed DATE or DATE-TIME value.
type dateTime struct {
	t      time.Time // DATE: 00:00 UTC; DATE-TIME: in its location (UTC for Z and floating)
	allDay bool
}

// parseDateProp parses the value of a DTSTART-like property.
func parseDateProp(p property) (dateTime, error) {
	return parseDateValue(p.value, strings.EqualFold(p.param("VALUE"), "DATE"), p.param("TZID"))
}

// parseDateValue parses a DATE (YYYYMMDD) or DATE-TIME value. DATE-TIMEs
// ending in Z are UTC, others are in the TZID zone; unknown zones and
// floating times count as UTC.
func parseDateValue(v string, isDate bool, tzid string) (dateTime, error) {
	v = strings.TrimSpace(v)
	if isDate || len(v) == 8 {
		if len(v) < 8 {
			return dateTime{}, errBadDate
		}
		t, err := time.Parse(dateLayout, v[:8])
		if err != nil {
			return dateTime{}, errBadDate
		}
		return dateTime{t: t, allDay: true}, nil
	}
	loc := time.UTC
	if strings.HasSuffix(v, "Z") || strings.HasSuffix(v, "z") {
		v = v[:len(v)-1]
	} else if tzid != "" {
		loc = lookupTZ(tzid)
	}
	layout := localLayout
	if len(v) == len("20060102T1504") { // tolerate missing seconds
		layout = "20060102T1504"
	}
	t, err := time.ParseInLocation(layout, v, loc)
	if err != nil {
		return dateTime{}, errBadDate
	}
	return dateTime{t: t}, nil
}

// formatRecurrence renders an occurrence start the way RECURRENCE-ID
// values look: YYYYMMDD for all-day, YYYYMMDDTHHMMSSZ (UTC) otherwise.
func formatRecurrence(t time.Time, allDay bool) string {
	if allDay {
		return t.Format(dateLayout)
	}
	return t.UTC().Format(utcLayout)
}

// windowsZones maps Windows/Outlook zone names that time.LoadLocation does
// not know to IANA names.
var windowsZones = map[string]string{
	"W. Europe Standard Time":         "Europe/Berlin",
	"Central Europe Standard Time":    "Europe/Budapest",
	"Central European Standard Time":  "Europe/Warsaw",
	"Romance Standard Time":           "Europe/Paris",
	"GMT Standard Time":               "Europe/London",
	"Greenwich Standard Time":         "Atlantic/Reykjavik",
	"E. Europe Standard Time":         "Europe/Chisinau",
	"FLE Standard Time":               "Europe/Kiev",
	"GTB Standard Time":               "Europe/Bucharest",
	"Russian Standard Time":           "Europe/Moscow",
	"Turkey Standard Time":            "Europe/Istanbul",
	"Israel Standard Time":            "Asia/Jerusalem",
	"South Africa Standard Time":      "Africa/Johannesburg",
	"Arabian Standard Time":           "Asia/Dubai",
	"India Standard Time":             "Asia/Kolkata",
	"China Standard Time":             "Asia/Shanghai",
	"Singapore Standard Time":         "Asia/Singapore",
	"Tokyo Standard Time":             "Asia/Tokyo",
	"Korea Standard Time":             "Asia/Seoul",
	"W. Australia Standard Time":      "Australia/Perth",
	"Cen. Australia Standard Time":    "Australia/Adelaide",
	"E. Australia Standard Time":      "Australia/Brisbane",
	"AUS Eastern Standard Time":       "Australia/Sydney",
	"New Zealand Standard Time":       "Pacific/Auckland",
	"Eastern Standard Time":           "America/New_York",
	"Central Standard Time":           "America/Chicago",
	"Mountain Standard Time":          "America/Denver",
	"US Mountain Standard Time":       "America/Phoenix",
	"Pacific Standard Time":           "America/Los_Angeles",
	"Alaskan Standard Time":           "America/Anchorage",
	"Hawaiian Standard Time":          "Pacific/Honolulu",
	"Atlantic Standard Time":          "America/Halifax",
	"Canada Central Standard Time":    "America/Regina",
	"Central Standard Time (Mexico)":  "America/Mexico_City",
	"SA Pacific Standard Time":        "America/Bogota",
	"E. South America Standard Time":  "America/Sao_Paulo",
	"Argentina Standard Time":         "America/Argentina/Buenos_Aires",
	"UTC":                             "UTC",
	"Coordinated Universal Time":      "UTC",
	"Greenwich Mean Time":             "Europe/London",
	"Central European Time":           "Europe/Berlin",
	"Mitteleuropäische Zeit":          "Europe/Berlin",
	"W. Central Africa Standard Time": "Africa/Lagos",
	// Outlook display names.
	"(UTC+01:00) Amsterdam, Berlin, Bern, Rome, Stockholm, Vienna":  "Europe/Berlin",
	"(UTC+01:00) Belgrade, Bratislava, Budapest, Ljubljana, Prague": "Europe/Budapest",
	"(UTC+01:00) Brussels, Copenhagen, Madrid, Paris":               "Europe/Paris",
	"(UTC+01:00) Sarajevo, Skopje, Warsaw, Zagreb":                  "Europe/Warsaw",
	"(UTC+00:00) Dublin, Edinburgh, Lisbon, London":                 "Europe/London",
	"(UTC-05:00) Eastern Time (US & Canada)":                        "America/New_York",
	"(UTC-06:00) Central Time (US & Canada)":                        "America/Chicago",
	"(UTC-08:00) Pacific Time (US & Canada)":                        "America/Los_Angeles",
}

var tzCache sync.Map // TZID → *time.Location

// lookupTZ resolves a TZID parameter. Unknown zones fall back to UTC.
func lookupTZ(tzid string) *time.Location {
	tzid = strings.Trim(strings.TrimSpace(tzid), `"`)
	if v, ok := tzCache.Load(tzid); ok {
		return v.(*time.Location)
	}
	loc := resolveTZ(tzid)
	tzCache.Store(tzid, loc)
	return loc
}

func resolveTZ(tzid string) *time.Location {
	load := func(name string) *time.Location {
		if name == "" || name == "Local" {
			return nil
		}
		if loc, err := time.LoadLocation(name); err == nil {
			return loc
		}
		return nil
	}
	if loc := load(tzid); loc != nil {
		return loc
	}
	if name, ok := windowsZones[tzid]; ok {
		if loc := load(name); loc != nil {
			return loc
		}
	}
	// Prefixed names such as "/mozilla.org/20050126_1/Europe/Berlin" or
	// "/citadel.org/20190914_1/Europe/Berlin": try the trailing segments.
	if strings.Contains(tzid, "/") {
		segs := strings.Split(strings.Trim(tzid, "/"), "/")
		for n := 3; n >= 1; n-- {
			if len(segs) >= n {
				if loc := load(strings.Join(segs[len(segs)-n:], "/")); loc != nil {
					return loc
				}
			}
		}
	}
	if strings.Contains(tzid, " ") {
		if loc := load(strings.ReplaceAll(tzid, " ", "_")); loc != nil {
			return loc
		}
	}
	return time.UTC
}

// duration is an RFC 5545 DURATION: weeks and days are nominal (calendar
// days in the event's zone), hours, minutes and seconds exact.
type duration struct {
	days  int
	exact time.Duration
}

func (d duration) addTo(t time.Time) time.Time {
	return t.AddDate(0, 0, d.days).Add(d.exact)
}

// parseDuration parses values like P1W, P2D, PT1H30M, -PT15M, P1DT12H.
func parseDuration(s string) (duration, bool) {
	s = strings.TrimSpace(strings.ToUpper(s))
	neg := false
	switch {
	case strings.HasPrefix(s, "-"):
		neg, s = true, s[1:]
	case strings.HasPrefix(s, "+"):
		s = s[1:]
	}
	if !strings.HasPrefix(s, "P") || len(s) < 3 {
		return duration{}, false
	}
	s = s[1:]
	var d duration
	inTime := false
	for len(s) > 0 {
		if s[0] == 'T' {
			inTime, s = true, s[1:]
			continue
		}
		i := 0
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		if i == 0 || i == len(s) || i > 9 {
			return duration{}, false
		}
		n, _ := strconv.Atoi(s[:i])
		switch unit := s[i]; {
		case unit == 'W' && !inTime:
			d.days += 7 * n
		case unit == 'D' && !inTime:
			d.days += n
		case unit == 'H' && inTime:
			d.exact += time.Duration(n) * time.Hour
		case unit == 'M' && inTime:
			d.exact += time.Duration(n) * time.Minute
		case unit == 'S' && inTime:
			d.exact += time.Duration(n) * time.Second
		default:
			return duration{}, false
		}
		s = s[i+1:]
	}
	if neg {
		d.days, d.exact = -d.days, -d.exact
	}
	return d, true
}
