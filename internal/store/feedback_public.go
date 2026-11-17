package store

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"
)

// Public view of feedback portals: released topics, votes, subscriptions
// and moderated comments.

// PublicTopics lists a portal's released topics ("votes" or "new" first;
// query filters title and summary).
func (d *DB) PublicTopics(ctx context.Context, portalID, sort, query, typ string, limit int) ([]FeedbackTopic, error) {
	q := `SELECT ` + topicCols + ` FROM feedback_topics t WHERE t.portal_id = ? AND t.public AND NOT t.spam`
	args := []any{portalID}
	if typ != "" {
		q += ` AND t.type = ?`
		args = append(args, typ)
	}
	if s := strings.TrimSpace(query); s != "" {
		pat := "%" + escapeLike(strings.ToLower(s)) + "%"
		q += ` AND (lower(t.title) LIKE ? ESCAPE '\' OR lower(t.summary) LIKE ? ESCAPE '\')`
		args = append(args, pat, pat)
	}
	if sort == "new" {
		q += ` ORDER BY t.created_at DESC`
	} else {
		q += ` ORDER BY t.votes DESC, t.report_count DESC, t.created_at DESC`
	}
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	q += ` LIMIT ` + strconv.Itoa(limit)
	rows, err := d.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FeedbackTopic{}
	for rows.Next() {
		t, err := scanTopic(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ToggleVote adds or removes a vote and returns whether it is set now and
// the new count.
func (d *DB) ToggleVote(ctx context.Context, topicID, voter string) (bool, int, error) {
	on := false
	err := d.InTx(ctx, func(tx *Tx) error {
		res, err := tx.Exec(ctx, `DELETE FROM feedback_votes WHERE topic_id = ? AND voter = ?`, topicID, voter)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			if _, err := tx.Exec(ctx, `INSERT INTO feedback_votes (topic_id, voter, tenant_id, created_at) VALUES (?, ?, ?, ?)`, topicID, voter, TenantOf(ctx), Now()); err != nil {
				return err
			}
			on = true
		}
		_, err = tx.Exec(ctx, `UPDATE feedback_topics SET votes = (SELECT COUNT(*) FROM feedback_votes v WHERE v.topic_id = ?) WHERE id = ?`, topicID, topicID)
		return err
	})
	var n int
	_ = d.QueryRow(ctx, `SELECT votes FROM feedback_topics WHERE id = ?`, topicID).Scan(&n)
	return on, n, err
}

// VotedTopics tells which of the topics a voter voted for.
func (d *DB) VotedTopics(ctx context.Context, voter string, topicIDs []string) (map[string]bool, error) {
	out := map[string]bool{}
	if voter == "" || len(topicIDs) == 0 {
		return out, nil
	}
	args := []any{voter}
	for _, id := range topicIDs {
		args = append(args, id)
	}
	rows, err := d.Query(ctx, `SELECT topic_id FROM feedback_votes WHERE voter = ? AND topic_id IN (`+placeholders(len(topicIDs))+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// FeedbackSubscription is a subscription to news of a topic.
type FeedbackSubscription struct {
	ID        string
	TopicID   string
	UserID    string
	Email     string
	Lang      string
	TokenHash string
	CreatedAt Time
}

// AddFeedbackSubscription stores a subscription (once per person or address).
func (d *DB) AddFeedbackSubscription(ctx context.Context, s FeedbackSubscription) (bool, error) {
	var n int
	_ = d.QueryRow(ctx, `SELECT COUNT(*) FROM feedback_subscriptions WHERE topic_id = ? AND ((user_id <> '' AND user_id = ?) OR (email <> '' AND lower(email) = lower(?)))`,
		s.TopicID, s.UserID, s.Email).Scan(&n)
	if n > 0 {
		return false, nil
	}
	_, err := d.Exec(ctx, `INSERT INTO feedback_subscriptions (id, tenant_id, topic_id, user_id, email, lang, token_hash, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		s.ID, TenantOf(ctx), s.TopicID, s.UserID, s.Email, s.Lang, s.TokenHash, s.CreatedAt)
	return err == nil, err
}

// FeedbackSubscriptions lists a topic's subscriptions.
func (d *DB) FeedbackSubscriptions(ctx context.Context, topicID string) ([]FeedbackSubscription, error) {
	rows, err := d.Query(ctx, `SELECT id, topic_id, user_id, email, lang, token_hash, created_at FROM feedback_subscriptions WHERE topic_id = ?`, topicID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FeedbackSubscription
	for rows.Next() {
		var s FeedbackSubscription
		if err := rows.Scan(&s.ID, &s.TopicID, &s.UserID, &s.Email, &s.Lang, &s.TokenHash, &s.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// IsSubscribed tells whether a person follows a topic.
func (d *DB) IsSubscribed(ctx context.Context, topicID, userID string) bool {
	var n int
	_ = d.QueryRow(ctx, `SELECT COUNT(*) FROM feedback_subscriptions WHERE topic_id = ? AND user_id = ? AND user_id <> ''`, topicID, userID).Scan(&n)
	return n > 0
}

// DeleteFeedbackSubscription ends a subscription by its link or by person.
func (d *DB) DeleteFeedbackSubscription(ctx context.Context, tokenHash, topicID, userID string) (bool, error) {
	var res sql.Result
	var err error
	if tokenHash != "" {
		res, err = d.Exec(ctx, `DELETE FROM feedback_subscriptions WHERE token_hash = ?`, tokenHash)
	} else {
		res, err = d.Exec(ctx, `DELETE FROM feedback_subscriptions WHERE topic_id = ? AND user_id = ? AND user_id <> ''`, topicID, userID)
	}
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// FeedbackComment is a comment on a public topic.
type FeedbackComment struct {
	ID         string `json:"id"`
	TopicID    string `json:"topicId"`
	UserID     string `json:"-"`
	AuthorName string `json:"authorName"`
	Body       string `json:"body"`
	Status     string `json:"status"` // pending | approved | rejected
	Team       bool   `json:"team"`
	CreatedAt  Time   `json:"createdAt"`
}

// AddFeedbackComment stores a comment.
func (d *DB) AddFeedbackComment(ctx context.Context, c FeedbackComment) error {
	_, err := d.Exec(ctx, `INSERT INTO feedback_comments (id, tenant_id, topic_id, user_id, author_name, body, status, team, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, TenantOf(ctx), c.TopicID, c.UserID, c.AuthorName, c.Body, c.Status, c.Team, c.CreatedAt)
	return err
}

// FeedbackComments lists a topic's comments (status "" = all), oldest first.
func (d *DB) FeedbackComments(ctx context.Context, topicID, status string) ([]FeedbackComment, error) {
	rows, err := d.Query(ctx, `SELECT id, topic_id, user_id, author_name, body, status, team, created_at FROM feedback_comments
		WHERE topic_id = ? AND (? = '' OR status = ?) ORDER BY created_at`, topicID, status, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FeedbackComment{}
	for rows.Next() {
		var c FeedbackComment
		if err := rows.Scan(&c.ID, &c.TopicID, &c.UserID, &c.AuthorName, &c.Body, &c.Status, &c.Team, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// FeedbackComment reads one comment.
func (d *DB) FeedbackComment(ctx context.Context, id string) (FeedbackComment, error) {
	var c FeedbackComment
	err := d.QueryRow(ctx, `SELECT id, topic_id, user_id, author_name, body, status, team, created_at FROM feedback_comments WHERE id = ?`, id).
		Scan(&c.ID, &c.TopicID, &c.UserID, &c.AuthorName, &c.Body, &c.Status, &c.Team, &c.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return c, err
}

// DecideFeedbackComment approves or rejects a comment.
func (d *DB) DecideFeedbackComment(ctx context.Context, id, status, by string) error {
	_, err := d.Exec(ctx, `UPDATE feedback_comments SET status = ?, decided_by = ? WHERE id = ?`, status, by, id)
	return err
}

// PendingCommentCounts counts comments waiting for moderation per topic.
func (d *DB) PendingCommentCounts(ctx context.Context, projectID string) (map[string]int, error) {
	rows, err := d.Query(ctx, `SELECT c.topic_id, COUNT(*) FROM feedback_comments c JOIN feedback_topics t ON t.id = c.topic_id
		WHERE t.project_id = ? AND c.status = 'pending' GROUP BY c.topic_id`, projectID)
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
