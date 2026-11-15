package store

import (
	"context"
	"database/sql"
	"errors"
)

// TimeEntry is time someone spent on a card.
type TimeEntry struct {
	ID        string `json:"id"`
	TaskID    string `json:"taskId"`
	UserID    string `json:"userId"`
	UserName  string `json:"userName,omitempty"`
	Day       string `json:"day"` // YYYY-MM-DD
	Minutes   int    `json:"minutes"`
	Note      string `json:"note,omitempty"`
	Billable  bool   `json:"billable"`
	CreatedAt Time   `json:"createdAt"`
	// Filled for reports: the card and its board.
	TaskTitle  string `json:"taskTitle,omitempty"`
	BoardTitle string `json:"boardTitle,omitempty"`
}

const timeCols = `x.id, x.task_id, x.user_id, COALESCE(u.name, ''), x.day, x.minutes, x.note, x.billable, x.created_at, COALESCE(t.title, ''), COALESCE(d.title, '')`
const timeFrom = ` FROM task_time x LEFT JOIN users u ON u.id = x.user_id LEFT JOIN tasks t ON t.id = x.task_id LEFT JOIN documents d ON d.id = t.board_id`

// AddTime records time on a card.
func (d *DB) AddTime(ctx context.Context, projectID, actor string, e TimeEntry) (TimeEntry, error) {
	e.CreatedAt = Now()
	_, err := d.Exec(ctx, `INSERT INTO task_time (id, tenant_id, project_id, task_id, user_id, day, minutes, note, billable, actor, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID, TenantOf(ctx), projectID, e.TaskID, e.UserID, e.Day, e.Minutes, e.Note, e.Billable, actor, e.CreatedAt)
	return e, err
}

// TimeFilter selects time entries of a project.
type TimeFilter struct {
	TaskID  string
	BoardID string
	UserID  string
	From    string // day, inclusive
	To      string // day, inclusive
}

// ListTime lists time entries, newest day first.
func (d *DB) ListTime(ctx context.Context, projectID string, f TimeFilter) ([]TimeEntry, error) {
	q := `SELECT ` + timeCols + timeFrom + ` WHERE x.project_id = ?`
	args := []any{projectID}
	for _, c := range []struct{ v, sql string }{{f.TaskID, ` AND x.task_id = ?`}, {f.BoardID, ` AND t.board_id = ?`}, {f.UserID, ` AND x.user_id = ?`}, {f.From, ` AND x.day >= ?`}, {f.To, ` AND x.day <= ?`}} {
		if c.v != "" {
			q += c.sql
			args = append(args, c.v)
		}
	}
	rows, err := d.Query(ctx, q+` ORDER BY x.day DESC, x.created_at DESC LIMIT 5000`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TimeEntry{}
	for rows.Next() {
		var e TimeEntry
		if err := rows.Scan(&e.ID, &e.TaskID, &e.UserID, &e.UserName, &e.Day, &e.Minutes, &e.Note, &e.Billable, &e.CreatedAt, &e.TaskTitle, &e.BoardTitle); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// GetTime returns one entry.
func (d *DB) GetTime(ctx context.Context, projectID, id string) (TimeEntry, error) {
	var e TimeEntry
	err := d.QueryRow(ctx, `SELECT `+timeCols+timeFrom+` WHERE x.project_id = ? AND x.id = ?`, projectID, id).
		Scan(&e.ID, &e.TaskID, &e.UserID, &e.UserName, &e.Day, &e.Minutes, &e.Note, &e.Billable, &e.CreatedAt, &e.TaskTitle, &e.BoardTitle)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return e, err
}

// DeleteTime removes an entry.
func (d *DB) DeleteTime(ctx context.Context, projectID, id string) error {
	res, err := d.Exec(ctx, `DELETE FROM task_time WHERE project_id = ? AND id = ?`, projectID, id)
	return affected(res, err)
}

// TaskMinutes sums the time of the cards of a project (task id → minutes).
func (d *DB) TaskMinutes(ctx context.Context, projectID string) (map[string]int, error) {
	rows, err := d.Query(ctx, `SELECT task_id, SUM(minutes) FROM task_time WHERE project_id = ? GROUP BY task_id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}
