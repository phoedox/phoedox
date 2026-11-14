package core

import (
	"context"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/phoedox/phoedox/internal/store"
)

// Readings (ADR-0051): measured values of entries over time, such as a
// temperature or whether a switch is on. Plugins (graph.record), the agent
// and the API record them; they are kept for ReadingDays.

// ReadingDays is how long readings are kept.
const ReadingDays = 90

// readingGap is the shortest time between two stored readings of one series.
const readingGap = 30 * time.Second

var metricRe = regexp.MustCompile(`^[A-Za-z0-9_.:/-]{1,100}$`)

// ReadingInput is one value to record.
type ReadingInput struct {
	Entity string     `json:"entity"` // slug or id
	Metric string     `json:"metric"`
	Value  float64    `json:"value"`
	Unit   string     `json:"unit,omitempty"`
	At     *time.Time `json:"at,omitempty"`
}

// RecordResult tells how many readings were stored.
type RecordResult struct {
	Recorded int `json:"recorded"`
	Skipped  int `json:"skipped"` // too frequent
}

// recordReadings stores readings of a project; allow tells whether the
// writer may record on entries of a kind.
func (s *Service) recordReadings(ctx context.Context, p store.Project, in []ReadingInput, allow func(kind string) bool) (RecordResult, error) {
	var res RecordResult
	if len(in) > 500 {
		return res, invalid("validation.readings_count", map[string]any{"max": 500}, "at most 500 readings per call")
	}
	now := time.Now()
	ents := map[string]store.Entity{}
	out := make([]store.Reading, 0, len(in))
	for i, r := range in {
		e, ok := ents[r.Entity]
		if !ok {
			var err error
			if e, err = s.DB.GetEntity(ctx, p.ID, r.Entity); err != nil {
				return res, invalid("validation.reading", map[string]any{"row": i}, "reading %d: unknown entry %q", i, r.Entity)
			}
			ents[r.Entity] = e
		}
		if !allow(e.Kind) {
			return res, ErrForbidden
		}
		at := now
		if r.At != nil {
			at = *r.At
		}
		if !metricRe.MatchString(r.Metric) || math.IsNaN(r.Value) || math.IsInf(r.Value, 0) || len(r.Unit) > 20 ||
			at.After(now.Add(5*time.Minute)) || at.Before(now.AddDate(0, 0, -ReadingDays)) {
			return res, invalid("validation.reading", map[string]any{"row": i}, "reading %d: metric (letters, digits, _.:/-), a finite value, unit up to 20 characters and a time within %d days", i, ReadingDays)
		}
		key := store.TenantOf(ctx) + "|" + e.ID + "|" + r.Metric
		if last, ok := s.readingSeen.Load(key); ok && at.Sub(last.(time.Time)).Abs() < readingGap {
			res.Skipped++
			continue
		}
		s.readingSeen.Store(key, at)
		out = append(out, store.Reading{EntityID: e.ID, Metric: r.Metric, Value: r.Value, Unit: strings.TrimSpace(r.Unit), At: store.Time{Time: at.UTC().Truncate(time.Millisecond)}})
	}
	if err := s.DB.AddReadings(ctx, p.ID, out); err != nil {
		return res, err
	}
	res.Recorded = len(out)
	return res, nil
}

// RecordReadings stores readings sent over the API (editors; not AI clients,
// which must not invent measurements).
func (s *Service) RecordReadings(ctx context.Context, a Actor, ref string, in []ReadingInput) (RecordResult, error) {
	if a.Via == "mcp" {
		return RecordResult{}, ErrForbidden
	}
	p, err := s.project(ctx, a, ref, store.RoleEditor)
	if err != nil {
		return RecordResult{}, err
	}
	return s.recordReadings(ctx, p, in, func(string) bool { return true })
}

// Series is one metric of an entry, condensed to at most maxPoints buckets.
type Series struct {
	Metric string  `json:"metric"`
	Unit   string  `json:"unit"`
	Last   float64 `json:"last"`
	LastAt string  `json:"lastAt"`
	// Points are [unix ms, average, min, max] per bucket, oldest first.
	Points [][4]float64 `json:"points"`
}

const maxPoints = 240

// EntityReadings returns the series of an entry over the last hours
// (readers). metric "" returns all series (at most 20).
func (s *Service) EntityReadings(ctx context.Context, a Actor, ref, slug, metric string, hours int) ([]Series, error) {
	p, err := s.project(ctx, a, ref, store.RoleReader)
	if err != nil {
		return nil, err
	}
	e, err := s.DB.GetEntity(ctx, p.ID, slug)
	if err != nil {
		return nil, err
	}
	if hours <= 0 || hours > ReadingDays*24 {
		hours = 24
	}
	metrics, err := s.DB.Metrics(ctx, e.ID)
	if err != nil {
		return nil, err
	}
	since := time.Now().Add(-time.Duration(hours) * time.Hour)
	out := []Series{}
	for _, m := range metrics {
		if (metric != "" && m.Metric != metric) || len(out) >= 20 {
			continue
		}
		rs, err := s.DB.Readings(ctx, e.ID, m.Metric, store.Time{Time: since}, 50000)
		if err != nil {
			return nil, err
		}
		out = append(out, Series{Metric: m.Metric, Unit: m.Unit, Last: m.Last, LastAt: m.LastAt.UTC().Format(time.RFC3339), Points: condense(rs, since, time.Now())})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Metric < out[j].Metric })
	return out, nil
}

// condense averages readings into at most maxPoints buckets.
func condense(rs []store.Reading, from, to time.Time) [][4]float64 {
	out := [][4]float64{}
	if len(rs) == 0 {
		return out
	}
	if len(rs) <= maxPoints {
		for _, r := range rs {
			out = append(out, [4]float64{float64(r.At.UnixMilli()), r.Value, r.Value, r.Value})
		}
		return out
	}
	width := to.Sub(from) / maxPoints
	if width <= 0 {
		width = time.Second
	}
	var sum float64
	var n int
	var cur [4]float64
	bucket := int64(-1)
	flush := func() {
		if n > 0 {
			cur[1] = sum / float64(n)
			out = append(out, cur)
		}
	}
	for _, r := range rs {
		b := int64(r.At.Sub(from) / width)
		if b != bucket {
			flush()
			bucket, sum, n = b, 0, 0
			cur = [4]float64{float64(from.Add(time.Duration(b)*width + width/2).UnixMilli()), 0, r.Value, r.Value}
		}
		sum += r.Value
		n++
		cur[2] = math.Min(cur[2], r.Value)
		cur[3] = math.Max(cur[3], r.Value)
	}
	flush()
	return out
}

// PruneReadings drops readings older than ReadingDays (housekeeping, all tenants).
func (s *Service) PruneReadings(ctx context.Context, now time.Time) {
	_, _ = s.DB.PruneReadings(ctx, store.Time{Time: now.AddDate(0, 0, -ReadingDays)})
}
