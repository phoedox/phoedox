package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// FeedbackTracker is an external tracker topics are forwarded to.
type FeedbackTracker struct {
	ID          string   `json:"id"`
	ProjectID   string   `json:"projectId"`
	Name        string   `json:"name"`
	Kind        string   `json:"kind"`
	URL         string   `json:"url"`
	JiraProject string   `json:"jiraProject,omitempty"`
	IssueType   string   `json:"issueType,omitempty"`
	Username    string   `json:"username,omitempty"`
	TokenVar    string   `json:"tokenVar,omitempty"`
	Labels      []string `json:"labels"`
	CreatedBy   string   `json:"createdBy"`
	CreatedAt   Time     `json:"createdAt"`
}

const trackerCols = `id, project_id, name, kind, url, jira_project, issue_type, username, token_var, labels, created_by, created_at`

func scanTracker(r interface{ Scan(...any) error }) (FeedbackTracker, error) {
	var t FeedbackTracker
	var labels string
	err := r.Scan(&t.ID, &t.ProjectID, &t.Name, &t.Kind, &t.URL, &t.JiraProject, &t.IssueType, &t.Username, &t.TokenVar, &labels, &t.CreatedBy, &t.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return t, ErrNotFound
	}
	_ = json.Unmarshal([]byte(labels), &t.Labels)
	if t.Labels == nil {
		t.Labels = []string{}
	}
	return t, err
}

// SaveFeedbackTracker creates or changes a tracker.
func (d *DB) SaveFeedbackTracker(ctx context.Context, t FeedbackTracker) error {
	labels, _ := json.Marshal(t.Labels)
	_, err := d.Exec(ctx, `INSERT INTO feedback_trackers (`+trackerCols+`, tenant_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET name = excluded.name, kind = excluded.kind, url = excluded.url, jira_project = excluded.jira_project,
		issue_type = excluded.issue_type, username = excluded.username, token_var = excluded.token_var, labels = excluded.labels`,
		t.ID, t.ProjectID, t.Name, t.Kind, t.URL, t.JiraProject, t.IssueType, t.Username, t.TokenVar, string(labels), t.CreatedBy, t.CreatedAt, TenantOf(ctx))
	return err
}

// FeedbackTrackers lists a project's trackers.
func (d *DB) FeedbackTrackers(ctx context.Context, projectID string) ([]FeedbackTracker, error) {
	rows, err := d.Query(ctx, `SELECT `+trackerCols+` FROM feedback_trackers WHERE project_id = ? ORDER BY name`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FeedbackTracker{}
	for rows.Next() {
		t, err := scanTracker(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// FeedbackTracker reads one tracker of a project ("" = any project).
func (d *DB) FeedbackTracker(ctx context.Context, projectID, id string) (FeedbackTracker, error) {
	return scanTracker(d.QueryRow(ctx, `SELECT `+trackerCols+` FROM feedback_trackers WHERE id = ? AND (project_id = ? OR ? = '')`, id, projectID, projectID))
}

// DeleteFeedbackTracker removes a tracker (and the links of its issues).
func (d *DB) DeleteFeedbackTracker(ctx context.Context, projectID, id string) error {
	res, err := d.Exec(ctx, `DELETE FROM feedback_trackers WHERE id = ? AND project_id = ?`, id, projectID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// FeedbackForward is an issue made from a topic in an external tracker.
type FeedbackForward struct {
	ID          string `json:"id"`
	TopicID     string `json:"topicId"`
	TrackerID   string `json:"trackerId"`
	Tracker     string `json:"tracker"` // the tracker's name
	Kind        string `json:"kind"`
	Ref         string `json:"ref"`
	URL         string `json:"url"`
	ReportsSent int    `json:"reportsSent"`
	Closed      bool   `json:"closed"`
	LastError   string `json:"lastError,omitempty"`
	CreatedBy   string `json:"createdBy"`
	CreatedAt   Time   `json:"createdAt"`
	CheckedAt   *Time  `json:"checkedAt,omitempty"`
}

const forwardSelect = `SELECT f.id, f.topic_id, f.tracker_id, k.name, k.kind, f.ref, f.url, f.reports_sent, f.closed, f.last_error, f.created_by, f.created_at, f.checked_at
	FROM feedback_forwards f JOIN feedback_trackers k ON k.id = f.tracker_id`

func (d *DB) forwards(ctx context.Context, where string, args ...any) ([]FeedbackForward, error) {
	rows, err := d.Query(ctx, forwardSelect+" WHERE "+where+" ORDER BY f.created_at", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FeedbackForward{}
	for rows.Next() {
		var f FeedbackForward
		var checked Time
		if err := rows.Scan(&f.ID, &f.TopicID, &f.TrackerID, &f.Tracker, &f.Kind, &f.Ref, &f.URL, &f.ReportsSent, &f.Closed, &f.LastError, &f.CreatedBy, &f.CreatedAt, &checked); err != nil {
			return nil, err
		}
		if !checked.IsZero() {
			f.CheckedAt = &checked
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// AddFeedbackForward records an issue made from a topic.
func (d *DB) AddFeedbackForward(ctx context.Context, f FeedbackForward) error {
	_, err := d.Exec(ctx, `INSERT INTO feedback_forwards (id, tenant_id, topic_id, tracker_id, ref, url, reports_sent, closed, last_error, created_by, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, '', ?, ?)`, f.ID, TenantOf(ctx), f.TopicID, f.TrackerID, f.Ref, f.URL, f.ReportsSent, f.Closed, f.CreatedBy, f.CreatedAt)
	return err
}

// TopicForwards lists the issues of a topic.
func (d *DB) TopicForwards(ctx context.Context, topicID string) ([]FeedbackForward, error) {
	return d.forwards(ctx, "f.topic_id = ?", topicID)
}

// OpenFeedbackForwards lists issues of the tenant that are still open.
func (d *DB) OpenFeedbackForwards(ctx context.Context) ([]FeedbackForward, error) {
	return d.forwards(ctx, "f.closed = ?", false)
}

// UpdateFeedbackForward stores what a sync found.
func (d *DB) UpdateFeedbackForward(ctx context.Context, id string, reportsSent int, closed bool, lastError string) error {
	_, err := d.Exec(ctx, `UPDATE feedback_forwards SET reports_sent = ?, closed = ?, last_error = ?, checked_at = ? WHERE id = ?`, reportsSent, closed, lastError, Now(), id)
	return err
}

// MoveTopicForwards moves the issues of a merged topic (one per tracker).
func (d *DB) MoveTopicForwards(ctx context.Context, from, to string) error {
	_, err := d.Exec(ctx, `UPDATE feedback_forwards SET topic_id = ? WHERE topic_id = ? AND tracker_id NOT IN (SELECT tracker_id FROM feedback_forwards WHERE topic_id = ?)`, to, from, to)
	return err
}
