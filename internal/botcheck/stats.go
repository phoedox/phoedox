package botcheck

import (
	"context"
	"sort"
	"time"
)

// Stats are the outcomes per policy and place for the last 7 days.
type Stats struct {
	Rows    []StatRow `json:"rows"`
	Blocked []Event   `json:"blocked"`
	// Hourly blocked counts for the last 24 hours (oldest first).
	Hourly []int `json:"hourly"`
}

// StatRow counts the outcomes of one policy at one place.
type StatRow struct {
	Place      string `json:"place"`
	Policy     string `json:"policy"`
	Passed     int    `json:"passed"`
	Challenged int    `json:"challenged"`
	Solved     int    `json:"solved"`
	Email      int    `json:"email"`
	Blocked    int    `json:"blocked"`
	AvgSolveMS int    `json:"avgSolveMs"`
}

// Stats sums the event log of the last 7 days and lists recent blocks.
func (s *Service) Stats(ctx context.Context) (Stats, error) {
	now := s.Now()
	_ = s.Store.PurgeBot(ctx, now.Add(-7*24*time.Hour))
	evs, err := s.Store.BotEvents(ctx, now.Add(-7*24*time.Hour), "", 100_000)
	if err != nil {
		return Stats{}, err
	}
	out := Stats{Rows: []StatRow{}, Blocked: []Event{}, Hourly: make([]int, 24)}
	rows := map[string]*StatRow{}
	solve := map[string][2]int{}
	for _, e := range evs {
		k := e.Place + "\x00" + e.Policy
		r := rows[k]
		if r == nil {
			r = &StatRow{Place: e.Place, Policy: e.Policy}
			rows[k] = r
		}
		switch e.Outcome {
		case "passed":
			r.Passed++
		case "challenged":
			r.Challenged++
		case "solved":
			r.Solved++
		case "email":
			r.Email++
		case "blocked", "failed":
			r.Blocked++
			if len(out.Blocked) < 200 {
				out.Blocked = append(out.Blocked, e)
			}
			if h := int(now.Sub(e.At).Hours()); h < 24 {
				out.Hourly[23-h]++
			}
		}
		if e.SolveMS > 0 && (e.Outcome == "passed" || e.Outcome == "solved" || e.Outcome == "email") {
			v := solve[k]
			solve[k] = [2]int{v[0] + e.SolveMS, v[1] + 1}
		}
	}
	for k, r := range rows {
		if v := solve[k]; v[1] > 0 {
			r.AvgSolveMS = v[0] / v[1]
		}
		out.Rows = append(out.Rows, *r)
	}
	sort.Slice(out.Rows, func(i, j int) bool {
		if out.Rows[i].Place != out.Rows[j].Place {
			return out.Rows[i].Place < out.Rows[j].Place
		}
		return out.Rows[i].Policy < out.Rows[j].Policy
	})
	sort.Slice(out.Blocked, func(i, j int) bool { return out.Blocked[i].At.After(out.Blocked[j].At) })
	return out, nil
}

// checkSpike alerts system admins when the last hour had far more blocked
// requests than usual (at most once per hour).
func (s *Service) checkSpike(ctx context.Context, place string) {
	if s.Alert == nil {
		return
	}
	now := s.Now()
	s.mu.Lock()
	recent := now.Sub(s.lastAlert) < time.Hour
	s.mu.Unlock()
	if recent {
		return
	}
	evs, err := s.Store.BotEvents(ctx, now.Add(-25*time.Hour), "blocked", 100_000)
	if err != nil {
		return
	}
	var lastHour, before int
	for _, e := range evs {
		if now.Sub(e.At) < time.Hour {
			lastHour++
		} else {
			before++
		}
	}
	usual := float64(before) / 24
	if lastHour >= 50 && float64(lastHour) > 5*usual {
		s.mu.Lock()
		s.lastAlert = now
		s.mu.Unlock()
		s.Alert(ctx, lastHour, place)
	}
}
