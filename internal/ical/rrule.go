package ical

import (
	"slices"
	"strconv"
	"strings"
	"time"
)

// weekdayNum is one BYDAY entry: an optional ordinal (2MO, -1FR) and a weekday.
type weekdayNum struct {
	n  int
	wd time.Weekday
}

// rrule is the supported subset of an RFC 5545 recurrence rule.
type rrule struct {
	freq       string // DAILY, WEEKLY, MONTHLY or YEARLY
	interval   int
	count      int
	until      time.Time
	hasUntil   bool
	untilDate  bool // UNTIL was a DATE: compare calendar dates
	byDay      []weekdayNum
	byMonthDay []int
	byMonth    []int
	byYearDay  []int
	bySetPos   []int
	wkst       time.Weekday
}

var weekdays = map[string]time.Weekday{
	"SU": time.Sunday, "MO": time.Monday, "TU": time.Tuesday, "WE": time.Wednesday,
	"TH": time.Thursday, "FR": time.Friday, "SA": time.Saturday,
}

// parseRRule parses an RRULE value. It returns nil for rules it cannot
// expand at all (missing or unsupported FREQ); unknown parts are ignored.
// loc is the zone of DTSTART, used for a floating UNTIL.
func parseRRule(v string, loc *time.Location) *rrule {
	r := &rrule{interval: 1, wkst: time.Monday}
	ints := func(s string, min, max int) []int {
		var out []int
		for _, p := range splitList(s) {
			n, err := strconv.Atoi(strings.TrimPrefix(p, "+"))
			if err == nil && n != 0 && n >= min && n <= max {
				out = append(out, n)
			}
		}
		return out
	}
	for _, part := range strings.Split(v, ";") {
		key, val, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		key, val = strings.ToUpper(strings.TrimSpace(key)), strings.ToUpper(strings.TrimSpace(val))
		switch key {
		case "FREQ":
			r.freq = val
		case "INTERVAL":
			if n, err := strconv.Atoi(val); err == nil && n > 0 && n <= 10000 {
				r.interval = n
			}
		case "COUNT":
			if n, err := strconv.Atoi(val); err == nil && n > 0 {
				r.count = n
			}
		case "UNTIL":
			dt, err := parseDateValue(val, false, "")
			if err != nil {
				continue
			}
			r.hasUntil, r.untilDate = true, dt.allDay
			r.until = dt.t
			if !dt.allDay && !strings.HasSuffix(val, "Z") && loc != nil {
				// Floating UNTIL: same wall clock as DTSTART's zone.
				r.until = time.Date(dt.t.Year(), dt.t.Month(), dt.t.Day(), dt.t.Hour(), dt.t.Minute(), dt.t.Second(), 0, loc)
			}
		case "BYDAY":
			for _, p := range splitList(val) {
				if len(p) < 2 {
					continue
				}
				wd, ok := weekdays[p[len(p)-2:]]
				if !ok {
					continue
				}
				n := 0
				if num := strings.TrimPrefix(p[:len(p)-2], "+"); num != "" {
					var err error
					if n, err = strconv.Atoi(num); err != nil || n < -53 || n > 53 {
						continue
					}
				}
				r.byDay = append(r.byDay, weekdayNum{n: n, wd: wd})
			}
		case "BYMONTHDAY":
			r.byMonthDay = ints(val, -31, 31)
		case "BYMONTH":
			r.byMonth = ints(val, 1, 12)
		case "BYYEARDAY":
			r.byYearDay = ints(val, -366, 366)
		case "BYSETPOS":
			r.bySetPos = ints(val, -366, 366)
		case "WKST":
			if wd, ok := weekdays[val]; ok {
				r.wkst = wd
			}
		}
	}
	switch r.freq {
	case "DAILY", "WEEKLY", "MONTHLY", "YEARLY":
		return r
	}
	return nil
}

// civil dates are represented as time.Time at 00:00 UTC.
func civil(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

func daysIn(y int, m time.Month) int { return civil(y, m+1, 0).Day() }

func (r *rrule) monthAllowed(m time.Month) bool {
	return len(r.byMonth) == 0 || slices.Contains(r.byMonth, int(m))
}

func (r *rrule) weekdayAllowed(wd time.Weekday) bool {
	if len(r.byDay) == 0 {
		return true
	}
	for _, d := range r.byDay {
		if d.wd == wd {
			return true
		}
	}
	return false
}

func (r *rrule) monthDayAllowed(c time.Time) bool {
	if len(r.byMonthDay) == 0 {
		return true
	}
	n := daysIn(c.Year(), c.Month())
	for _, md := range r.byMonthDay {
		if md == c.Day() || (md < 0 && n+md+1 == c.Day()) {
			return true
		}
	}
	return false
}

// nthWeekdays returns the days among [first, first+length) with weekday
// wd: all of them for n == 0, otherwise the n-th (negative from the end).
func nthWeekdays(first time.Time, length int, w weekdayNum) []time.Time {
	var all []time.Time
	off := (int(w.wd) - int(first.Weekday()) + 7) % 7
	for d := off; d < length; d += 7 {
		all = append(all, first.AddDate(0, 0, d))
	}
	switch {
	case w.n == 0:
		return all
	case w.n > 0 && w.n <= len(all):
		return all[w.n-1 : w.n]
	case w.n < 0 && -w.n <= len(all):
		return all[len(all)+w.n : len(all)+w.n+1]
	}
	return nil
}

// monthDays returns the candidate days of one month for MONTHLY rules
// and month-scoped YEARLY rules. dtDay is DTSTART's day of month.
func (r *rrule) monthDays(y int, m time.Month, dtDay int) []time.Time {
	n := daysIn(y, m)
	var out []time.Time
	switch {
	case len(r.byMonthDay) > 0:
		for _, md := range r.byMonthDay {
			d := md
			if md < 0 {
				d = n + md + 1
			}
			if d >= 1 && d <= n {
				c := civil(y, m, d)
				if r.weekdayAllowed(c.Weekday()) {
					out = append(out, c)
				}
			}
		}
	case len(r.byDay) > 0:
		for _, w := range r.byDay {
			out = append(out, nthWeekdays(civil(y, m, 1), n, w)...)
		}
	default:
		if dtDay <= n {
			out = append(out, civil(y, m, dtDay))
		}
	}
	return out
}

// period returns the candidate days (civil, unsorted) of period number k.
func (r *rrule) period(dt time.Time, k int) []time.Time {
	y, m, d := dt.Date()
	switch r.freq {
	case "DAILY":
		c := civil(y, m, d+k)
		if r.monthAllowed(c.Month()) && r.weekdayAllowed(c.Weekday()) && r.monthDayAllowed(c) {
			return []time.Time{c}
		}
		return nil
	case "WEEKLY":
		start := civil(y, m, d-(int(dt.Weekday())-int(r.wkst)+7)%7+7*k)
		var out []time.Time
		if len(r.byDay) == 0 {
			out = append(out, start.AddDate(0, 0, (int(dt.Weekday())-int(r.wkst)+7)%7))
		}
		for _, w := range r.byDay {
			out = append(out, start.AddDate(0, 0, (int(w.wd)-int(r.wkst)+7)%7))
		}
		return slices.DeleteFunc(out, func(c time.Time) bool { return !r.monthAllowed(c.Month()) })
	case "MONTHLY":
		first := civil(y, m+time.Month(k), 1)
		if !r.monthAllowed(first.Month()) {
			return nil
		}
		return r.monthDays(first.Year(), first.Month(), d)
	case "YEARLY":
		yy := y + k
		ylen := civil(yy+1, 1, 1).Sub(civil(yy, 1, 1)).Hours() / 24
		var out []time.Time
		switch {
		case len(r.byYearDay) > 0:
			for _, yd := range r.byYearDay {
				if yd < 0 {
					yd = int(ylen) + yd + 1
				}
				if yd < 1 || yd > int(ylen) {
					continue
				}
				c := civil(yy, 1, yd)
				if r.monthAllowed(c.Month()) && r.weekdayAllowed(c.Weekday()) && r.monthDayAllowed(c) {
					out = append(out, c)
				}
			}
		case len(r.byDay) > 0 && len(r.byMonth) == 0 && len(r.byMonthDay) == 0:
			// Weekdays within the whole year (e.g. 20MO = 20th Monday).
			for _, w := range r.byDay {
				out = append(out, nthWeekdays(civil(yy, 1, 1), int(ylen), w)...)
			}
		default:
			months := r.byMonth
			if len(months) == 0 {
				if len(r.byMonthDay) > 0 || len(r.byDay) > 0 {
					months = []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
				} else {
					months = []int{int(m)}
				}
			}
			for _, mm := range months {
				out = append(out, r.monthDays(yy, time.Month(mm), d)...)
			}
		}
		return out
	}
	return nil
}

// limits shared by one Parse call.
type budget struct {
	periods int // period iterations left for all rules together
}

// skipPeriods returns how many periods can be skipped without missing an
// occurrence that ends after target. Only valid without COUNT.
func (r *rrule) skipPeriods(dt, target time.Time) int {
	if !target.After(dt) {
		return 0
	}
	ty, tm, td := target.Date()
	y, m, d := dt.Date()
	var units int
	switch r.freq {
	case "DAILY":
		units = int(civil(ty, tm, td).Sub(civil(y, m, d)).Hours() / 24)
	case "WEEKLY":
		units = int(civil(ty, tm, td).Sub(civil(y, m, d)).Hours()/24) / 7
	case "MONTHLY":
		units = (ty-y)*12 + int(tm) - int(m)
	case "YEARLY":
		units = ty - y
	}
	k := units/r.interval - 1
	if k < 0 {
		return 0
	}
	return k * r.interval
}

// each calls yield with the occurrence starts the rule generates after
// DTSTART (DTSTART itself is not passed but counts toward COUNT), in
// ascending order, until the rule ends, a start reaches stop (zero: no
// stop), the 100-year horizon is passed, the budget is used up or yield
// returns false. maxDur is the longest occurrence length, used to skip
// periods that end before from.
func (r *rrule) each(dt, from, stop time.Time, maxDur time.Duration, b *budget, yield func(time.Time) bool) {
	loc := dt.Location()
	hh, mi, ss := dt.Clock()
	skip := 0
	horizon := dt.AddDate(maxYears, 0, 0)
	if r.count == 0 && !from.IsZero() {
		target := from.Add(-maxDur).AddDate(0, 0, -2).In(loc)
		skip = r.skipPeriods(dt, target)
		if target.After(dt) {
			horizon = target.AddDate(maxYears, 0, 0)
		}
	}
	count := 1 // DTSTART is the first occurrence
	for k := skip; ; k += r.interval {
		if b.periods <= 0 {
			return
		}
		b.periods--
		days := r.period(dt, k)
		slices.SortFunc(days, func(a, b time.Time) int { return a.Compare(b) })
		days = slices.Compact(days)
		if len(r.bySetPos) > 0 && len(days) > 0 {
			var sel []time.Time
			for _, p := range r.bySetPos {
				switch {
				case p > 0 && p <= len(days):
					sel = append(sel, days[p-1])
				case p < 0 && -p <= len(days):
					sel = append(sel, days[len(days)+p])
				}
			}
			slices.SortFunc(sel, func(a, b time.Time) int { return a.Compare(b) })
			days = slices.Compact(sel)
		}
		for _, c := range days {
			t := time.Date(c.Year(), c.Month(), c.Day(), hh, mi, ss, 0, loc)
			if !t.After(dt) {
				continue
			}
			if r.hasUntil {
				if r.untilDate {
					if c.After(r.until) {
						return
					}
				} else if t.After(r.until) {
					return
				}
			}
			if t.After(horizon) || (!stop.IsZero() && !t.Before(stop)) {
				return
			}
			count++
			if r.count > 0 && count > r.count {
				return
			}
			if !yield(t) {
				return
			}
		}
		// A period that starts beyond the horizon cannot yield anything.
		// (Two days of margin cover zone offsets between civil dates and instants.)
		if periodStart := r.periodStart(dt, k).AddDate(0, 0, -2); periodStart.After(horizon) || (!stop.IsZero() && periodStart.After(stop)) {
			return
		}
	}
}

// periodStart is a lower bound for the days of period k.
func (r *rrule) periodStart(dt time.Time, k int) time.Time {
	y, m, d := dt.Date()
	switch r.freq {
	case "DAILY":
		return civil(y, m, d+k)
	case "WEEKLY":
		return civil(y, m, d-6+7*k)
	case "MONTHLY":
		return civil(y, m+time.Month(k), 1)
	default:
		return civil(y+k, 1, 1)
	}
}
