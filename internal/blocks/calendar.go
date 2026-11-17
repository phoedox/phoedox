package blocks

import (
	"strconv"
	"strings"
)

// The ```calendar block (ADR-0089): the project's dates in a page, as a
// month or a list, optionally with the filters of a calendar document.
//
//	view: list
//	sources: tasks, maintenance
//	kinds: car, contract
//	calendar: household-dates
//	days: 30

// CalendarBlock is a calendar block's source.
type CalendarBlock struct {
	View     string
	Sources  []string
	Kinds    []string
	Calendar string // a calendar document's slug
	Days     int
}

var calSourceNames = map[string]string{"maintenance": "maintenance", "wartung": "maintenance", "tasks": "tasks", "cards": "tasks", "karten": "tasks",
	"entities": "entities", "entries": "entities", "einträge": "entities", "external": "external", "extern": "external"}

func list(v string) []string {
	var out []string
	for _, x := range strings.Split(v, ",") {
		if x = strings.TrimSpace(x); x != "" {
			out = append(out, x)
		}
	}
	return out
}

// ParseCalendar reads a calendar block.
func ParseCalendar(src []byte) (CalendarBlock, []Problem) {
	c := CalendarBlock{View: "month"}
	var ps []Problem
	for _, l := range lines(src) {
		k, v, ok := key(l, "view", "ansicht", "sources", "quellen", "kinds", "arten", "calendar", "kalender", "days", "tage")
		if !ok {
			ps = append(ps, problem("unknown line %q (view, sources, kinds, calendar, days)", l))
			continue
		}
		switch k {
		case "view", "ansicht":
			switch strings.ToLower(v) {
			case "month", "monat":
				c.View = "month"
			case "list", "liste":
				c.View = "list"
			default:
				ps = append(ps, problem("%q is not a view (month or list)", v))
			}
		case "sources", "quellen":
			for _, s := range list(strings.ToLower(v)) {
				if n, ok := calSourceNames[s]; ok {
					c.Sources = append(c.Sources, n)
				} else {
					ps = append(ps, problem("%q is not a source (maintenance, tasks, entities, external)", s))
				}
			}
		case "kinds", "arten":
			c.Kinds = list(v)
		case "calendar", "kalender":
			if c.Calendar = strings.TrimSpace(v); !docSlug.MatchString(c.Calendar) {
				ps = append(ps, problem("%q is not a page name (the calendar document's slug)", v))
			}
		case "days", "tage":
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 || n > 366 {
				ps = append(ps, problem("days must be 1–366"))
			}
			c.Days = n
		}
	}
	return c, ps
}

func init() {
	Register(Def{Kind: "calendar", Data: true,
		Check: func(src []byte) []Problem { _, ps := ParseCalendar(src); return ps },
		Text:  func(src []byte) string { c, _ := ParseCalendar(src); return c.Calendar }})
}
