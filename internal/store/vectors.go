package store

import (
	"context"
	"encoding/binary"
	"math"
	"sort"
	"strconv"
)

// Semantic search: embeddings of the search index entries.

// VectorJob is an index entry that has no embedding for a model yet.
type VectorJob struct {
	RefID     string
	TenantID  string
	ProjectID string
	Title     string
	Body      string
}

// VectorsMissing lists entries without an embedding of the model (all
// tenants in a system context).
func (d *DB) VectorsMissing(ctx context.Context, model string, limit int) ([]VectorJob, error) {
	rows, err := d.Query(ctx, `SELECT s.ref_id, s.tenant_id, s.project_id, s.title, substr(s.body, 1, 6000) FROM search_index s
		WHERE NOT EXISTS (SELECT 1 FROM search_vectors v WHERE v.ref_id = s.ref_id AND v.model = ?)
		ORDER BY s.ref_id LIMIT `+strconv.Itoa(limit), model)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []VectorJob
	for rows.Next() {
		var j VectorJob
		if err := rows.Scan(&j.RefID, &j.TenantID, &j.ProjectID, &j.Title, &j.Body); err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// VectorCounts tells how many entries have an embedding of the model.
func (d *DB) VectorCounts(ctx context.Context, model string) (have, total int, err error) {
	if err = d.QueryRow(ctx, `SELECT count(*) FROM search_index`).Scan(&total); err != nil {
		return
	}
	err = d.QueryRow(ctx, `SELECT count(*) FROM search_vectors WHERE model = ?`, model).Scan(&have)
	return
}

// PutVector stores the embedding of an entry (normalised to unit length).
func (d *DB) PutVector(ctx context.Context, j VectorJob, model string, vec []float32) error {
	_, err := d.Exec(ctx, `INSERT INTO search_vectors (ref_id, model, tenant_id, project_id, vec) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (ref_id, model) DO UPDATE SET vec = excluded.vec`, j.RefID, model, j.TenantID, j.ProjectID, encodeVec(Normalize(vec)))
	return err
}

// DropVectors removes the embeddings of other models (after a change of model).
func (d *DB) DropVectors(ctx context.Context, keepModel string) error {
	_, err := d.Exec(ctx, `DELETE FROM search_vectors WHERE model <> ?`, keepModel)
	return err
}

// Normalize scales a vector to unit length.
func Normalize(v []float32) []float32 {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	if sum == 0 {
		return v
	}
	n := float32(1 / math.Sqrt(sum))
	out := make([]float32, len(v))
	for i, x := range v {
		out[i] = x * n
	}
	return out
}

func encodeVec(v []float32) []byte {
	b := make([]byte, 4*len(v))
	for i, x := range v {
		binary.LittleEndian.PutUint32(b[4*i:], math.Float32bits(x))
	}
	return b
}

// VectorMatch is an entry close to a query.
type VectorMatch struct {
	RefID string
	Score float64 // cosine similarity
}

// NearestVectors compares a (normalised) query vector with the embeddings
// of the given projects and returns the closest ones above min.
func (d *DB) NearestVectors(ctx context.Context, projectIDs []string, model string, q []float32, min float64, limit int) ([]VectorMatch, error) {
	if len(projectIDs) == 0 {
		return nil, nil
	}
	args := []any{TenantOf(ctx), model}
	for _, p := range projectIDs {
		args = append(args, p)
	}
	rows, err := d.Query(ctx, `SELECT ref_id, vec FROM search_vectors WHERE tenant_id = ? AND model = ? AND project_id IN (`+placeholders(len(projectIDs))+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []VectorMatch
	for rows.Next() {
		var id string
		var b []byte
		if err := rows.Scan(&id, &b); err != nil {
			return nil, err
		}
		if len(b) != 4*len(q) {
			continue
		}
		var dot float64
		for i := range q {
			dot += float64(q[i]) * float64(math.Float32frombits(binary.LittleEndian.Uint32(b[4*i:])))
		}
		if dot >= min {
			out = append(out, VectorMatch{RefID: id, Score: dot})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, rows.Err()
}

// IndexHits returns the index entries with these ids as hits (no snippet).
func (d *DB) IndexHits(ctx context.Context, refIDs []string, words []string) ([]Hit, error) {
	if len(refIDs) == 0 {
		return nil, nil
	}
	args := []any{TenantOf(ctx)}
	for _, r := range refIDs {
		args = append(args, r)
	}
	rows, err := d.Query(ctx, `SELECT s.kind, s.project_id, p.slug, p.name, s.ref_id, s.slug, s.title, s.subtype, substr(s.body, 1, `+strconv.Itoa(bodyPrefix)+`), 0
		FROM search_index s JOIN projects p ON p.id = s.project_id WHERE s.tenant_id = ? AND s.ref_id IN (`+placeholders(len(refIDs))+`)`, args...)
	if err != nil {
		return nil, err
	}
	return scanHits(rows, words)
}
