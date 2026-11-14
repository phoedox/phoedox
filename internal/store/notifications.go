package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// Notification is a message for one person.
type Notification struct {
	ID        string `json:"id"`
	TenantID  string `json:"-"`
	UserID    string `json:"-"`
	ProjectID string `json:"projectId,omitempty"`
	Kind      string `json:"kind"`
	SubjectID string `json:"subjectId,omitempty"`
	Title     string `json:"title"`
	Body      string `json:"body,omitempty"`
	Link      string `json:"link,omitempty"` // path in the app, e.g. /p/haus/d/netz#comment-…
	ActorName string `json:"actorName,omitempty"`
	ReadAt    Time   `json:"readAt"`
	CreatedAt Time   `json:"createdAt"`
}

// Mail modes of notification settings.
const (
	MailInstant = "instant"
	MailDaily   = "daily"
	MailOff     = "off"
)

// NotificationSettings are a person's channels.
type NotificationSettings struct {
	UserID     string   `json:"-"`
	TenantID   string   `json:"-"`
	Email      string   `json:"email"`
	NtfyURL    string   `json:"ntfyUrl"`
	Muted      []string `json:"muted"`
	LastDigest Time     `json:"-"`
}

// PushSubscription is a browser that receives web push messages.
type PushSubscription struct {
	ID       string `json:"id"`
	UserID   string `json:"-"`
	Endpoint string `json:"endpoint"`
	P256dh   string `json:"-"`
	Auth     string `json:"-"`
}

const notifCols = `id, tenant_id, user_id, project_id, kind, subject_id, title, body, link, actor_name, read_at, created_at`

func scanNotification(r interface{ Scan(...any) error }) (Notification, error) {
	var n Notification
	var project sql.NullString
	err := r.Scan(&n.ID, &n.TenantID, &n.UserID, &project, &n.Kind, &n.SubjectID, &n.Title, &n.Body, &n.Link, &n.ActorName, &n.ReadAt, &n.CreatedAt)
	n.ProjectID = project.String
	return n, err
}

func (d *DB) queryNotifications(ctx context.Context, where string, args ...any) ([]Notification, error) {
	rows, err := d.Query(ctx, `SELECT `+notifCols+` FROM notifications `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Notification{}
	for rows.Next() {
		n, err := scanNotification(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// AddNotification stores a notification; mailed marks it as already sent by mail.
func (d *DB) AddNotification(ctx context.Context, n Notification, mailed bool) (Notification, error) {
	n.CreatedAt = Now()
	var mailedAt any
	if mailed {
		mailedAt = n.CreatedAt
	}
	_, err := d.Exec(ctx, `INSERT INTO notifications (id, tenant_id, user_id, project_id, kind, subject_id, title, body, link, actor_name, mailed_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		n.ID, TenantOf(ctx), n.UserID, nullable(n.ProjectID), n.Kind, n.SubjectID, n.Title, n.Body, n.Link, n.ActorName, mailedAt, n.CreatedAt)
	return n, err
}

// UserNotifications lists the newest notifications of a person.
func (d *DB) UserNotifications(ctx context.Context, userID string, unread bool, limit int) ([]Notification, error) {
	where := `WHERE user_id = ?`
	if unread {
		where += ` AND read_at IS NULL`
	}
	return d.queryNotifications(ctx, where+` ORDER BY created_at DESC, id DESC LIMIT ?`, userID, limit)
}

// UnreadNotifications counts the unread notifications of a person.
func (d *DB) UnreadNotifications(ctx context.Context, userID string) (int, error) {
	var n int
	err := d.QueryRow(ctx, `SELECT COUNT(*) FROM notifications WHERE user_id = ? AND read_at IS NULL`, userID).Scan(&n)
	return n, err
}

// MarkNotificationsRead marks the given notifications (none = all) as read.
func (d *DB) MarkNotificationsRead(ctx context.Context, userID string, ids []string) error {
	if len(ids) == 0 {
		_, err := d.Exec(ctx, `UPDATE notifications SET read_at = ? WHERE user_id = ? AND read_at IS NULL`, Now(), userID)
		return err
	}
	return d.InTx(ctx, func(tx *Tx) error {
		for _, id := range ids {
			if _, err := tx.Exec(ctx, `UPDATE notifications SET read_at = ? WHERE user_id = ? AND id = ? AND read_at IS NULL`, Now(), userID, id); err != nil {
				return err
			}
		}
		return nil
	})
}

// DigestPending lists unread notifications of a person not sent by mail yet.
func (d *DB) DigestPending(ctx context.Context, userID string) ([]Notification, error) {
	return d.queryNotifications(ctx, `WHERE user_id = ? AND mailed_at IS NULL AND read_at IS NULL ORDER BY created_at, id`, userID)
}

// MarkDigestSent marks notifications as mailed and remembers the digest time.
func (d *DB) MarkDigestSent(ctx context.Context, userID string, ids []string, at Time) error {
	return d.InTx(ctx, func(tx *Tx) error {
		for _, id := range ids {
			if _, err := tx.Exec(ctx, `UPDATE notifications SET mailed_at = ? WHERE user_id = ? AND id = ?`, at, userID, id); err != nil {
				return err
			}
		}
		_, err := tx.Exec(ctx, `UPDATE notification_settings SET last_digest = ? WHERE user_id = ?`, at, userID)
		return err
	})
}

// PruneNotifications removes read notifications older than before.
func (d *DB) PruneNotifications(ctx context.Context, before Time) (int64, error) {
	res, err := d.Exec(ctx, `DELETE FROM notifications WHERE read_at IS NOT NULL AND created_at < ?`, before)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// GetNotificationSettings returns a person's settings (defaults if none are stored).
func (d *DB) GetNotificationSettings(ctx context.Context, userID string) (NotificationSettings, error) {
	s := NotificationSettings{UserID: userID, Email: MailInstant, Muted: []string{}}
	var muted string
	err := d.QueryRow(ctx, `SELECT tenant_id, email, ntfy_url, muted, last_digest FROM notification_settings WHERE user_id = ?`, userID).
		Scan(&s.TenantID, &s.Email, &s.NtfyURL, &muted, &s.LastDigest)
	if errors.Is(err, sql.ErrNoRows) {
		return s, nil
	}
	if muted != "" {
		s.Muted = strings.Split(muted, ",")
	}
	return s, err
}

// SaveNotificationSettings stores a person's settings.
func (d *DB) SaveNotificationSettings(ctx context.Context, s NotificationSettings) error {
	_, err := d.Exec(ctx, `INSERT INTO notification_settings (user_id, tenant_id, email, ntfy_url, muted, updated_at) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (user_id) DO UPDATE SET email = excluded.email, ntfy_url = excluded.ntfy_url, muted = excluded.muted, updated_at = excluded.updated_at`,
		s.UserID, TenantOf(ctx), s.Email, s.NtfyURL, strings.Join(s.Muted, ","), Now())
	return err
}

// DigestUsers lists the people of all tenants who get a daily digest (system context).
func (d *DB) DigestUsers(ctx context.Context) ([]NotificationSettings, error) {
	rows, err := d.Query(ctx, `SELECT user_id, tenant_id, last_digest FROM notification_settings WHERE email = ?`, MailDaily)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []NotificationSettings
	for rows.Next() {
		var s NotificationSettings
		if err := rows.Scan(&s.UserID, &s.TenantID, &s.LastDigest); err != nil {
			return nil, err
		}
		s.Email = MailDaily
		out = append(out, s)
	}
	return out, rows.Err()
}

// AddPushSubscription stores (or moves to this person) a browser subscription.
func (d *DB) AddPushSubscription(ctx context.Context, p PushSubscription) error {
	_, err := d.Exec(ctx, `INSERT INTO push_subscriptions (id, tenant_id, user_id, endpoint, p256dh, auth, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (endpoint) DO UPDATE SET user_id = excluded.user_id, p256dh = excluded.p256dh, auth = excluded.auth`,
		p.ID, TenantOf(ctx), p.UserID, p.Endpoint, p.P256dh, p.Auth, Now())
	return err
}

// PushSubscriptions lists the browsers of a person.
func (d *DB) PushSubscriptions(ctx context.Context, userID string) ([]PushSubscription, error) {
	rows, err := d.Query(ctx, `SELECT id, user_id, endpoint, p256dh, auth FROM push_subscriptions WHERE user_id = ? ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PushSubscription{}
	for rows.Next() {
		var p PushSubscription
		if err := rows.Scan(&p.ID, &p.UserID, &p.Endpoint, &p.P256dh, &p.Auth); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// DeletePushSubscription removes a browser subscription (userID "" = any person).
func (d *DB) DeletePushSubscription(ctx context.Context, userID, endpoint string) error {
	if userID == "" {
		_, err := d.Exec(ctx, `DELETE FROM push_subscriptions WHERE endpoint = ?`, endpoint)
		return err
	}
	_, err := d.Exec(ctx, `DELETE FROM push_subscriptions WHERE user_id = ? AND endpoint = ?`, userID, endpoint)
	return err
}
