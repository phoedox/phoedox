package store

import (
	"context"
	"strings"
)

// Reading is one measured value of an entry.
type Reading struct {
	EntityID string  `json:"entityId"`
	Metric   string  `json:"metric"`
	Value    float64 `json:"value"`
	Unit     string  `json:"unit,omitempty"`
	At       Time    `json:"at"`
}

// AddReadings stores readings of one project.
func (d *DB) AddReadings(ctx context.Context, projectID string, rs []Reading) error {
	for len(rs) > 0 {
		n := min(len(rs), 100)
		var b strings.Builder
		args := make([]any, 0, n*7)
		b.WriteString(`INSERT INTO entity_readings (tenant_id, project_id, entity_id, metric, value, unit, at) VALUES `)
		for i, r := range rs[:n] {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString("(?, ?, ?, ?, ?, ?, ?)")
			args = append(args, TenantOf(ctx), projectID, r.EntityID, r.Metric, r.Value, r.Unit, r.At)
		}
		if _, err := d.Exec(ctx, b.String(), args...); err != nil {
			return err
		}
		rs = rs[n:]
	}
	return nil
}

// MetricInfo describes one series of an entry.
type MetricInfo struct {
	Metric string  `json:"metric"`
	Unit   string  `json:"unit"`
	Last   float64 `json:"last"`
	LastAt Time    `json:"lastAt"`
	Count  int     `json:"count"`
}

// Metrics lists the series of an entry with their latest value.
func (d *DB) Metrics(ctx context.Context, entityID string) ([]MetricInfo, error) {
	rows, err := d.Query(ctx, `SELECT r.metric, r.unit, r.value, r.at, c.n FROM entity_readings r
		JOIN (SELECT metric, MAX(at) AS last, COUNT(*) AS n FROM entity_readings WHERE entity_id = ? GROUP BY metric) c ON c.metric = r.metric AND c.last = r.at
		WHERE r.entity_id = ? ORDER BY r.metric`, entityID, entityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MetricInfo{}
	for rows.Next() {
		var m MetricInfo
		if err := rows.Scan(&m.Metric, &m.Unit, &m.Last, &m.LastAt, &m.Count); err != nil {
			return nil, err
		}
		if len(out) > 0 && out[len(out)-1].Metric == m.Metric {
			continue // two readings at the same instant
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// Readings returns the values of one series since a time, oldest first
// (at most limit).
func (d *DB) Readings(ctx context.Context, entityID, metric string, since Time, limit int) ([]Reading, error) {
	rows, err := d.Query(ctx, `SELECT entity_id, metric, value, unit, at FROM entity_readings WHERE entity_id = ? AND metric = ? AND at >= ? ORDER BY at LIMIT ?`,
		entityID, metric, since, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Reading{}
	for rows.Next() {
		var r Reading
		if err := rows.Scan(&r.EntityID, &r.Metric, &r.Value, &r.Unit, &r.At); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// PruneReadings deletes readings older than before (all tenants with a System context).
func (d *DB) PruneReadings(ctx context.Context, before Time) (int64, error) {
	res, err := d.Exec(ctx, `DELETE FROM entity_readings WHERE at < ?`, before)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
