package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// Task is a card on a board (ADR-0030).
type Task struct {
	ID          string   `json:"id"`
	ProjectID   string   `json:"projectId"`
	BoardID     string   `json:"boardId"`
	Column      string   `json:"column"`
	Position    float64  `json:"position"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	AssigneeID  string   `json:"assigneeId,omitempty"`
	Due         string   `json:"due,omitempty"`
	Priority    int      `json:"priority"`
	Labels      []string `json:"labels"`
	CreatedBy   string   `json:"createdBy"`
	CreatedAt   Time     `json:"createdAt"`
	UpdatedAt   Time     `json:"updatedAt"`
	DoneAt      Time     `json:"doneAt"`
	// Filled by queries that join: the assignee's name and the board.
	AssigneeName   string `json:"assigneeName,omitempty"`
	AssigneeAvatar string `json:"assigneeAvatar,omitempty"`
	BoardSlug      string `json:"boardSlug,omitempty"`
	BoardTitle     string `json:"boardTitle,omitempty"`
}

const taskCols = `t.id, t.project_id, t.board_id, t.column_id, t.position, t.title, t.description, COALESCE(t.assignee_id, ''), t.due, t.priority, t.labels,
	t.created_by, t.created_at, t.updated_at, t.done_at, COALESCE(u.name, ''), COALESCE(u.avatar, ''), d.slug, d.title`

const taskFrom = ` FROM tasks t JOIN documents d ON d.id = t.board_id LEFT JOIN users u ON u.id = t.assignee_id`

func scanTask(r interface{ Scan(...any) error }) (Task, error) {
	var t Task
	var labels string
	err := r.Scan(&t.ID, &t.ProjectID, &t.BoardID, &t.Column, &t.Position, &t.Title, &t.Description, &t.AssigneeID, &t.Due, &t.Priority, &labels,
		&t.CreatedBy, &t.CreatedAt, &t.UpdatedAt, &t.DoneAt, &t.AssigneeName, &t.AssigneeAvatar, &t.BoardSlug, &t.BoardTitle)
	if errors.Is(err, sql.ErrNoRows) {
		return t, ErrNotFound
	}
	t.Labels = []string{}
	if labels != "" {
		t.Labels = strings.Split(labels, ",")
	}
	return t, err
}

// TaskFilter selects tasks of a project.
type TaskFilter struct {
	BoardID    string
	Column     string
	AssigneeID string
	Open       bool // only tasks not done
}

// ListTasks returns tasks ordered by board, column and position.
func (d *DB) ListTasks(ctx context.Context, projectID string, f TaskFilter) ([]Task, error) {
	q := `SELECT ` + taskCols + taskFrom + ` WHERE t.project_id = ? AND d.deleted_at IS NULL`
	args := []any{projectID}
	if f.BoardID != "" {
		q += ` AND t.board_id = ?`
		args = append(args, f.BoardID)
	}
	if f.Column != "" {
		q += ` AND t.column_id = ?`
		args = append(args, f.Column)
	}
	if f.AssigneeID != "" {
		q += ` AND t.assignee_id = ?`
		args = append(args, f.AssigneeID)
	}
	if f.Open {
		q += ` AND t.done_at IS NULL`
	}
	rows, err := d.Query(ctx, q+` ORDER BY d.title, t.column_id, t.position, t.created_at`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Task{}
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// GetTask returns a task of a project.
func (d *DB) GetTask(ctx context.Context, projectID, id string) (Task, error) {
	return scanTask(d.QueryRow(ctx, `SELECT `+taskCols+taskFrom+` WHERE t.project_id = ? AND t.id = ? AND d.deleted_at IS NULL`, projectID, id))
}

// MaxTaskPosition returns the highest position in a column (0 if empty).
func (d *DB) MaxTaskPosition(ctx context.Context, boardID, column string) (float64, error) {
	var p sql.NullFloat64
	err := d.QueryRow(ctx, `SELECT MAX(position) FROM tasks WHERE board_id = ? AND column_id = ?`, boardID, column).Scan(&p)
	return p.Float64, err
}

// SaveTask inserts or updates a task.
func (d *DB) SaveTask(ctx context.Context, t Task) error {
	var done any
	if !t.DoneAt.IsZero() {
		done = t.DoneAt
	}
	_, err := d.Exec(ctx, `INSERT INTO tasks (id, tenant_id, project_id, board_id, column_id, position, title, description, assignee_id, due, priority, labels, created_by, created_at, updated_at, done_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET board_id = excluded.board_id, column_id = excluded.column_id, position = excluded.position, title = excluded.title,
		  description = excluded.description, assignee_id = excluded.assignee_id, due = excluded.due, priority = excluded.priority, labels = excluded.labels,
		  updated_at = excluded.updated_at, done_at = excluded.done_at`,
		t.ID, TenantOf(ctx), t.ProjectID, t.BoardID, t.Column, t.Position, t.Title, t.Description, nullable(t.AssigneeID), t.Due, t.Priority,
		strings.Join(t.Labels, ","), t.CreatedBy, t.CreatedAt, t.UpdatedAt, done)
	return err
}

// DeleteTask removes a task.
func (d *DB) DeleteTask(ctx context.Context, projectID, id string) error {
	res, err := d.Exec(ctx, `DELETE FROM tasks WHERE project_id = ? AND id = ?`, projectID, id)
	return affected(res, err)
}

// MoveOrphanTasks moves tasks of columns that no longer exist to a column.
func (d *DB) MoveOrphanTasks(ctx context.Context, boardID string, columns []string, to string, done bool) error {
	if len(columns) == 0 {
		return nil
	}
	ph := strings.TrimSuffix(strings.Repeat("?, ", len(columns)), ", ")
	args := []any{to}
	var doneAt any
	if done {
		doneAt = Now()
	}
	args = append(args, doneAt, boardID)
	for _, c := range columns {
		args = append(args, c)
	}
	_, err := d.Exec(ctx, `UPDATE tasks SET column_id = ?, done_at = ? WHERE board_id = ? AND column_id NOT IN (`+ph+`)`, args...)
	return err
}
