package store

import (
	"context"
	"database/sql"
	"errors"
)

// Snapshot is a named point in a project's history.
type Snapshot struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`
	Name      string `json:"name"`
	Note      string `json:"note"`
	At        Time   `json:"at"`
	Source    string `json:"source"`              // manual | tag
	SourceRef string `json:"sourceRef,omitempty"` // repository and tag
	CreatedBy string `json:"createdBy"`
	CreatedAt Time   `json:"createdAt"`
}

const snapshotCols = `id, project_id, name, note, at, source, source_ref, created_by, created_at`

func scanSnapshot(r interface{ Scan(...any) error }) (Snapshot, error) {
	var s Snapshot
	err := r.Scan(&s.ID, &s.ProjectID, &s.Name, &s.Note, &s.At, &s.Source, &s.SourceRef, &s.CreatedBy, &s.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return s, ErrNotFound
	}
	return s, err
}

// AddSnapshot stores a snapshot; ErrConflict when the name is taken.
func (d *DB) AddSnapshot(ctx context.Context, s Snapshot) error {
	_, err := d.Exec(ctx, `INSERT INTO project_snapshots (id, tenant_id, project_id, name, note, at, source, source_ref, created_by, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		s.ID, TenantOf(ctx), s.ProjectID, s.Name, s.Note, s.At, s.Source, s.SourceRef, s.CreatedBy, s.CreatedAt)
	if err != nil && isUniqueViolation(err) {
		return ErrConflict
	}
	return err
}

// Snapshots lists a project's snapshots, newest first.
func (d *DB) Snapshots(ctx context.Context, projectID string) ([]Snapshot, error) {
	rows, err := d.Query(ctx, `SELECT `+snapshotCols+` FROM project_snapshots WHERE project_id = ? ORDER BY at DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Snapshot{}
	for rows.Next() {
		s, err := scanSnapshot(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// SnapshotByName reads one snapshot.
func (d *DB) SnapshotByName(ctx context.Context, projectID, name string) (Snapshot, error) {
	return scanSnapshot(d.QueryRow(ctx, `SELECT `+snapshotCols+` FROM project_snapshots WHERE project_id = ? AND name = ?`, projectID, name))
}

// DeleteSnapshot removes a snapshot (the history itself stays).
func (d *DB) DeleteSnapshot(ctx context.Context, projectID, name string) error {
	_, err := d.Exec(ctx, `DELETE FROM project_snapshots WHERE project_id = ? AND name = ?`, projectID, name)
	return err
}
