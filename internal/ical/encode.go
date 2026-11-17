package ical

import (
	"bytes"
	"strings"
	"time"
	"unicode/utf8"
)

// now is the clock for DTSTAMP; tests replace it.
var now = time.Now

// Encode writes a VCALENDAR with the events (PRODID prodID): RFC 5545
// escaping, 75-octet line folding, CRLF line ends, DTSTAMP now, all-day
// events as VALUE=DATE (dates taken from Start and End in their own
// location), others in UTC. Each event is written as a single VEVENT;
// Recurrence is not written. Events need a UID to be valid iCalendar.
func Encode(prodID string, events []Event) []byte {
	var b bytes.Buffer
	w := func(line string) { writeFolded(&b, line) }
	w("BEGIN:VCALENDAR")
	w("VERSION:2.0")
	w("PRODID:" + escapeText(prodID))
	w("CALSCALE:GREGORIAN")
	stamp := now().UTC().Format(utcLayout)
	for _, ev := range events {
		w("BEGIN:VEVENT")
		if ev.UID != "" {
			w("UID:" + escapeText(ev.UID))
		}
		w("DTSTAMP:" + stamp)
		if ev.AllDay {
			start := civil(ev.Start.Date())
			end := civil(ev.End.Date())
			if !end.After(start) {
				end = start.AddDate(0, 0, 1)
			}
			w("DTSTART;VALUE=DATE:" + start.Format(dateLayout))
			w("DTEND;VALUE=DATE:" + end.Format(dateLayout))
		} else {
			w("DTSTART:" + ev.Start.UTC().Format(utcLayout))
			if !ev.End.IsZero() && !ev.End.Before(ev.Start) {
				w("DTEND:" + ev.End.UTC().Format(utcLayout))
			}
		}
		if !ev.Updated.IsZero() {
			w("LAST-MODIFIED:" + ev.Updated.UTC().Format(utcLayout))
		}
		for _, p := range []struct{ name, value string }{
			{"SUMMARY", ev.Summary},
			{"DESCRIPTION", ev.Description},
			{"LOCATION", ev.Location},
		} {
			if p.value != "" {
				w(p.name + ":" + escapeText(p.value))
			}
		}
		if u := strings.Map(dropControl, ev.URL); u != "" {
			w("URL:" + u)
		}
		w("END:VEVENT")
	}
	w("END:VCALENDAR")
	return b.Bytes()
}

func dropControl(r rune) rune {
	if r < 0x20 || r == 0x7f {
		return -1
	}
	return r
}

// writeFolded writes one content line, folded so that no physical line is
// longer than 75 octets (without CRLF) and no UTF-8 sequence is split.
func writeFolded(b *bytes.Buffer, line string) {
	const limit = 75
	n := 0
	for i := 0; i < len(line); {
		_, size := utf8.DecodeRuneInString(line[i:])
		if n+size > limit {
			b.WriteString("\r\n ")
			n = 1
		}
		b.WriteString(line[i : i+size])
		n += size
		i += size
	}
	b.WriteString("\r\n")
}
