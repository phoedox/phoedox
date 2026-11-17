package store

import (
	"context"
	"database/sql"
	"errors"
)

// Calendar is an external calendar of a project (ADR-0089).
type Calendar struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`
	Name      string `json:"name"`
	Kind      string `json:"kind"` // caldav | ical
	URL       string `json:"url"`
	Username  string `json:"username,omitempty"`
	Secret    string `json:"-"` // sealed password
	Color     string `json:"color,omitempty"`
	WriteBack bool   `json:"writeBack"`
	SyncedAt  Time   `json:"syncedAt"`
	LastError string `json:"lastError,omitempty"`
	CreatedBy string `json:"createdBy"`
	CreatedAt Time   `json:"createdAt"`
}

// CalendarEvent is an event of an external calendar as last read.
type CalendarEvent struct {
	CalendarID  string `json:"calendarId"`
	UID         string `json:"uid"`
	Recurrence  string `json:"recurrence,omitempty"`
	ProjectID   string `json:"-"`
	Summary     string `json:"summary"`
	Description string `json:"description,omitempty"`
	Location    string `json:"location,omitempty"`
	URL         string `json:"url,omitempty"`
	Start       Time   `json:"start"`
	End         Time   `json:"end"`
	AllDay      bool   `json:"allDay"`
}

const calendarCols = `id, project_id, name, kind, url, username, secret, color, write_back, synced_at, last_error, created_by, created_at`

func scanCalendar(r interface{ Scan(...any) error }) (Calendar, error) {
	var c Calendar
	err := r.Scan(&c.ID, &c.ProjectID, &c.Name, &c.Kind, &c.URL, &c.Username, &c.Secret, &c.Color, &c.WriteBack, &c.SyncedAt, &c.LastError, &c.CreatedBy, &c.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	return c, err
}

// Calendars lists the external calendars of a project ("" = all of the tenant).
func (d *DB) Calendars(ctx context.Context, projectID string) ([]Calendar, error) {
	q, args := `SELECT `+calendarCols+` FROM calendars WHERE tenant_id = ?`, []any{TenantOf(ctx)}
	if projectID != "" {
		q, args = q+` AND project_id = ?`, append(args, projectID)
	}
	rows, err := d.Query(ctx, q+` ORDER BY name`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Calendar{}
	for rows.Next() {
		c, err := scanCalendar(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetCalendar returns one external calendar of a project.
func (d *DB) GetCalendar(ctx context.Context, projectID, id string) (Calendar, error) {
	return scanCalendar(d.QueryRow(ctx, `SELECT `+calendarCols+` FROM calendars WHERE tenant_id = ? AND project_id = ? AND id = ?`, TenantOf(ctx), projectID, id))
}

// InsertCalendar adds an external calendar.
func (d *DB) InsertCalendar(ctx context.Context, c Calendar) error {
	_, err := d.Exec(ctx, `INSERT INTO calendars (`+calendarCols+`, tenant_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.ProjectID, c.Name, c.Kind, c.URL, c.Username, c.Secret, c.Color, c.WriteBack, c.SyncedAt, c.LastError, c.CreatedBy, c.CreatedAt, TenantOf(ctx))
	return err
}

// UpdateCalendar changes an external calendar's settings.
func (d *DB) UpdateCalendar(ctx context.Context, c Calendar) error {
	_, err := d.Exec(ctx, `UPDATE calendars SET name = ?, kind = ?, url = ?, username = ?, secret = ?, color = ?, write_back = ? WHERE tenant_id = ? AND project_id = ? AND id = ?`,
		c.Name, c.Kind, c.URL, c.Username, c.Secret, c.Color, c.WriteBack, TenantOf(ctx), c.ProjectID, c.ID)
	return err
}

// MarkCalendarSynced records the outcome of a sync.
func (d *DB) MarkCalendarSynced(ctx context.Context, id, lastError string) error {
	_, err := d.Exec(ctx, `UPDATE calendars SET synced_at = ?, last_error = ? WHERE tenant_id = ? AND id = ?`, Now(), lastError, TenantOf(ctx), id)
	return err
}

// DeleteCalendar removes an external calendar with its cached events.
func (d *DB) DeleteCalendar(ctx context.Context, projectID, id string) error {
	return d.InTx(ctx, func(tx *Tx) error {
		for _, q := range []string{
			`DELETE FROM calendar_events WHERE tenant_id = ? AND calendar_id = ?`,
			`DELETE FROM calendar_pushed WHERE tenant_id = ? AND calendar_id = ?`,
		} {
			if _, err := tx.Exec(ctx, q, TenantOf(ctx), id); err != nil {
				return err
			}
		}
		res, err := tx.Exec(ctx, `DELETE FROM calendars WHERE tenant_id = ? AND project_id = ? AND id = ?`, TenantOf(ctx), projectID, id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// ReplaceCalendarEvents stores the events read from a calendar for a window,
// replacing what was stored for that window before.
func (d *DB) ReplaceCalendarEvents(ctx context.Context, cal Calendar, from, to Time, events []CalendarEvent) error {
	return d.InTx(ctx, func(tx *Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM calendar_events WHERE tenant_id = ? AND calendar_id = ? AND ends_at > ? AND starts_at < ?`, TenantOf(ctx), cal.ID, from, to); err != nil {
			return err
		}
		for _, e := range events {
			if _, err := tx.Exec(ctx, `DELETE FROM calendar_events WHERE tenant_id = ? AND calendar_id = ? AND uid = ? AND recurrence = ?`, TenantOf(ctx), cal.ID, e.UID, e.Recurrence); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO calendar_events (calendar_id, uid, recurrence, tenant_id, project_id, summary, description, location, url, starts_at, ends_at, all_day)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, cal.ID, e.UID, e.Recurrence, TenantOf(ctx), cal.ProjectID, e.Summary, e.Description, e.Location, e.URL, e.Start, e.End, e.AllDay); err != nil {
				return err
			}
		}
		return nil
	})
}

// CalendarEvents returns the stored external events of a project that
// overlap [from, to).
func (d *DB) CalendarEvents(ctx context.Context, projectID string, from, to Time) ([]CalendarEvent, error) {
	rows, err := d.Query(ctx, `SELECT calendar_id, uid, recurrence, project_id, summary, description, location, url, starts_at, ends_at, all_day
		FROM calendar_events WHERE tenant_id = ? AND project_id = ? AND ends_at > ? AND starts_at < ? ORDER BY starts_at LIMIT 5000`, TenantOf(ctx), projectID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CalendarEvent
	for rows.Next() {
		var e CalendarEvent
		if err := rows.Scan(&e.CalendarID, &e.UID, &e.Recurrence, &e.ProjectID, &e.Summary, &e.Description, &e.Location, &e.URL, &e.Start, &e.End, &e.AllDay); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Pushed is a Phoedox date written into an external calendar.
type Pushed struct {
	UID  string
	Hash string
	ETag string
}

// CalendarPushed lists what was written into a calendar (uid → entry).
func (d *DB) CalendarPushed(ctx context.Context, calendarID string) (map[string]Pushed, error) {
	rows, err := d.Query(ctx, `SELECT uid, hash, etag FROM calendar_pushed WHERE tenant_id = ? AND calendar_id = ?`, TenantOf(ctx), calendarID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]Pushed{}
	for rows.Next() {
		var p Pushed
		if err := rows.Scan(&p.UID, &p.Hash, &p.ETag); err != nil {
			return nil, err
		}
		out[p.UID] = p
	}
	return out, rows.Err()
}

// SetCalendarPushed records (or with hash "" forgets) a written date.
func (d *DB) SetCalendarPushed(ctx context.Context, calendarID string, p Pushed) error {
	return d.InTx(ctx, func(tx *Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM calendar_pushed WHERE tenant_id = ? AND calendar_id = ? AND uid = ?`, TenantOf(ctx), calendarID, p.UID); err != nil {
			return err
		}
		if p.Hash == "" {
			return nil
		}
		_, err := tx.Exec(ctx, `INSERT INTO calendar_pushed (calendar_id, uid, tenant_id, hash, etag) VALUES (?, ?, ?, ?, ?)`, calendarID, p.UID, TenantOf(ctx), p.Hash, p.ETag)
		return err
	})
}

// SetCalendarFeed stores the (hashed) feed token of a project, replacing
// an older one; tokenHash "" removes the feed.
func (d *DB) SetCalendarFeed(ctx context.Context, projectID, tokenHash, by string) error {
	return d.InTx(ctx, func(tx *Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM calendar_feeds WHERE tenant_id = ? AND project_id = ?`, TenantOf(ctx), projectID); err != nil {
			return err
		}
		if tokenHash == "" {
			return nil
		}
		_, err := tx.Exec(ctx, `INSERT INTO calendar_feeds (token_hash, tenant_id, project_id, created_by, created_at) VALUES (?, ?, ?, ?, ?)`, tokenHash, TenantOf(ctx), projectID, by, Now())
		return err
	})
}

// CalendarFeed tells whether a project has a feed and since when.
func (d *DB) CalendarFeed(ctx context.Context, projectID string) (Time, bool, error) {
	var at Time
	err := d.QueryRow(ctx, `SELECT created_at FROM calendar_feeds WHERE tenant_id = ? AND project_id = ?`, TenantOf(ctx), projectID).Scan(&at)
	if errors.Is(err, sql.ErrNoRows) {
		return at, false, nil
	}
	return at, err == nil, err
}

// CalendarFeedByToken finds the tenant and project of a feed token (across
// tenants: the token is the only credential of a feed URL).
func (d *DB) CalendarFeedByToken(ctx context.Context, tokenHash string) (tenant, project, by string, err error) {
	err = d.QueryRow(System(ctx), `SELECT tenant_id, project_id, created_by FROM calendar_feeds WHERE token_hash = ?`, tokenHash).Scan(&tenant, &project, &by)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return
}

// ProjectByID returns a live project without a user's role (background
// work such as calendar syncs and feeds).
func (d *DB) ProjectByID(ctx context.Context, id string) (Project, error) {
	p, err := scanProject(d.QueryRow(ctx, `SELECT `+projectCols+projectFrom+` WHERE p.tenant_id = ? AND p.id = ? AND p.deleted_at IS NULL`, TenantOf(ctx), id), false)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}

// AllCalendars lists the external calendars of all tenants with their
// tenant (background syncs).
func (d *DB) AllCalendars(ctx context.Context) (map[string][]Calendar, error) {
	rows, err := d.Query(System(ctx), `SELECT `+calendarCols+`, tenant_id FROM calendars`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]Calendar{}
	for rows.Next() {
		var c Calendar
		var tenant string
		if err := rows.Scan(&c.ID, &c.ProjectID, &c.Name, &c.Kind, &c.URL, &c.Username, &c.Secret, &c.Color, &c.WriteBack, &c.SyncedAt, &c.LastError, &c.CreatedBy, &c.CreatedAt, &tenant); err != nil {
			return nil, err
		}
		out[tenant] = append(out[tenant], c)
	}
	return out, rows.Err()
}
