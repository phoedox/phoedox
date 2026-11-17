package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
)

// Feedback portals (feedback-concept, ADR-0075).

// FeedbackPortal is one portal of a project.
type FeedbackPortal struct {
	ID            string          `json:"id"`
	ProjectID     string          `json:"projectId"`
	Handle        string          `json:"handle"`
	Name          string          `json:"name"`
	Enabled       bool            `json:"enabled"`
	Settings      json.RawMessage `json:"settings"`
	PublicKey     string          `json:"publicKey,omitempty"`
	ServerKeyHash string          `json:"-"`
	HasServerKey  bool            `json:"hasServerKey"`
	// PlayerKey is the sealed key products sign reporter tokens with.
	PlayerKeySealed string `json:"-"`
	HasPlayerKey    bool   `json:"hasPlayerKey"`
	// Domain is the portal's own domain; it serves the portal once the DNS
	// record (DomainToken) was verified.
	Domain           string `json:"domain,omitempty"`
	DomainToken      string `json:"domainToken,omitempty"`
	DomainVerifiedAt *Time  `json:"domainVerifiedAt,omitempty"`
	CreatedBy        string `json:"createdBy"`
	CreatedAt        Time   `json:"createdAt"`
	UpdatedAt        Time   `json:"updatedAt"`
}

const portalCols = `id, project_id, handle, name, enabled, settings, public_key, server_key_hash, created_by, created_at, updated_at, player_key_sealed, domain, domain_token, domain_verified_at`

func scanPortal(r interface{ Scan(...any) error }) (FeedbackPortal, error) {
	var p FeedbackPortal
	var settings string
	var verified Time
	err := r.Scan(&p.ID, &p.ProjectID, &p.Handle, &p.Name, &p.Enabled, &settings, &p.PublicKey, &p.ServerKeyHash, &p.CreatedBy, &p.CreatedAt, &p.UpdatedAt, &p.PlayerKeySealed,
		&p.Domain, &p.DomainToken, &verified)
	if !verified.IsZero() {
		p.DomainVerifiedAt = &verified
	}
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	p.Settings = json.RawMessage(settings)
	p.HasServerKey = p.ServerKeyHash != ""
	p.HasPlayerKey = p.PlayerKeySealed != ""
	return p, err
}

// SaveFeedbackPortal inserts or updates a portal.
func (d *DB) SaveFeedbackPortal(ctx context.Context, p FeedbackPortal) error {
	_, err := d.Exec(ctx, `INSERT INTO feedback_portals (id, tenant_id, project_id, handle, name, enabled, settings, public_key, server_key_hash, created_by, created_at, updated_at, player_key_sealed)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET handle = excluded.handle, name = excluded.name, enabled = excluded.enabled, settings = excluded.settings,
		public_key = excluded.public_key, server_key_hash = excluded.server_key_hash, updated_at = excluded.updated_at, player_key_sealed = excluded.player_key_sealed`,
		p.ID, TenantOf(ctx), p.ProjectID, p.Handle, p.Name, p.Enabled, string(p.Settings), p.PublicKey, p.ServerKeyHash, p.CreatedBy, p.CreatedAt, p.UpdatedAt, p.PlayerKeySealed)
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "unique") {
		return ErrConflict
	}
	return err
}

// FeedbackPortals lists a project's portals.
func (d *DB) FeedbackPortals(ctx context.Context, projectID string) ([]FeedbackPortal, error) {
	rows, err := d.Query(ctx, `SELECT `+portalCols+` FROM feedback_portals WHERE project_id = ? ORDER BY name`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FeedbackPortal{}
	for rows.Next() {
		p, err := scanPortal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// FeedbackPortal reads a portal by id (within a project when projectID is set).
func (d *DB) FeedbackPortal(ctx context.Context, projectID, id string) (FeedbackPortal, error) {
	return scanPortal(d.QueryRow(ctx, `SELECT `+portalCols+` FROM feedback_portals WHERE id = ? AND (? = '' OR project_id = ?)`, id, projectID, projectID))
}

// FeedbackPortalByHandle reads a portal by its public handle.
func (d *DB) FeedbackPortalByHandle(ctx context.Context, handle string) (FeedbackPortal, error) {
	return scanPortal(d.QueryRow(ctx, `SELECT `+portalCols+` FROM feedback_portals WHERE handle = ?`, strings.ToLower(handle)))
}

// FeedbackPortalByKey finds a portal by its public key.
func (d *DB) FeedbackPortalByKey(ctx context.Context, key string) (FeedbackPortal, error) {
	return scanPortal(d.QueryRow(ctx, `SELECT `+portalCols+` FROM feedback_portals WHERE public_key = ? AND public_key <> ''`, key))
}

// DeleteFeedbackPortal removes a portal with its topics and reports.
func (d *DB) DeleteFeedbackPortal(ctx context.Context, projectID, id string) error {
	res, err := d.Exec(ctx, `DELETE FROM feedback_portals WHERE project_id = ? AND id = ?`, projectID, id)
	return affected(res, err)
}

// FeedbackModerators lists the extra moderators of a portal.
func (d *DB) FeedbackModerators(ctx context.Context, portalID string) ([]OrgMember, error) {
	rows, err := d.Query(ctx, `SELECT u.id, u.email, u.name, 'moderator' FROM feedback_moderators m JOIN users u ON u.id = m.user_id WHERE m.portal_id = ? ORDER BY u.name`, portalID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []OrgMember{}
	for rows.Next() {
		var m OrgMember
		if err := rows.Scan(&m.UserID, &m.Email, &m.Name, &m.Role); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// SetFeedbackModerators replaces the extra moderators of a portal.
func (d *DB) SetFeedbackModerators(ctx context.Context, portalID string, userIDs []string) error {
	return d.InTx(ctx, func(tx *Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM feedback_moderators WHERE portal_id = ?`, portalID); err != nil {
			return err
		}
		for _, u := range userIDs {
			if _, err := tx.Exec(ctx, `INSERT INTO feedback_moderators (portal_id, user_id, tenant_id, created_at) VALUES (?, ?, ?, ?)`, portalID, u, TenantOf(ctx), Now()); err != nil {
				return err
			}
		}
		return nil
	})
}

// ModeratedPortals lists the portals a person moderates (outside project roles).
func (d *DB) ModeratedPortals(ctx context.Context, userID string) ([]FeedbackPortal, error) {
	rows, err := d.Query(ctx, `SELECT `+prefixed("p.", portalCols)+` FROM feedback_portals p JOIN feedback_moderators m ON m.portal_id = p.id WHERE m.user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FeedbackPortal{}
	for rows.Next() {
		p, err := scanPortal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// FeedbackTopic groups reports about the same thing.
type FeedbackTopic struct {
	ID          string   `json:"id"`
	ProjectID   string   `json:"projectId"`
	PortalID    string   `json:"portalId"`
	Title       string   `json:"title"`
	Summary     string   `json:"summary"`
	Type        string   `json:"type"`
	Category    string   `json:"category"`
	Tags        []string `json:"tags"`
	Priority    int      `json:"priority"`
	State       string   `json:"state"`
	Answer      string   `json:"answer"`
	ReportCount int      `json:"reportCount"`
	CreatedAt   Time     `json:"createdAt"`
	UpdatedAt   Time     `json:"updatedAt"`
	ClosedAt    *Time    `json:"closedAt,omitempty"`
	Tickets     int      `json:"tickets"`
	// AI triage: why it chose category and priority, a topic it takes for
	// the same issue, and whether the reports look like spam.
	AINote      string `json:"aiNote,omitempty"`
	DuplicateOf string `json:"duplicateOf,omitempty"`
	Spam        bool   `json:"spam,omitempty"`
	// FixedIn is the product version that fixed the topic (told reporters).
	FixedIn string `json:"fixedIn,omitempty"`
	// Question asks the reporters for more information (they answer on
	// their status link); AnswerAuto marks an answer Phoedox gave by itself.
	Question   string `json:"question,omitempty"`
	AnswerAuto bool   `json:"answerAuto,omitempty"`
	// Public: released on the portal's public page; Votes counts visitors'
	// votes there.
	Public bool `json:"public"`
	Votes  int  `json:"votes"`
}

const topicCols = `t.id, t.project_id, t.portal_id, t.title, t.summary, t.type, t.category, t.tags, t.priority, t.state, t.answer, t.report_count, t.created_at, t.updated_at, t.closed_at,
	(SELECT COUNT(*) FROM feedback_topic_links l WHERE l.topic_id = t.id), t.ai_note, t.duplicate_of, t.spam, t.fixed_in, t.question, t.answer_auto, t.public, t.votes`

func scanTopic(r interface{ Scan(...any) error }) (FeedbackTopic, error) {
	var t FeedbackTopic
	var tags string
	var closed sql.Null[Time]
	err := r.Scan(&t.ID, &t.ProjectID, &t.PortalID, &t.Title, &t.Summary, &t.Type, &t.Category, &tags, &t.Priority, &t.State, &t.Answer, &t.ReportCount, &t.CreatedAt, &t.UpdatedAt, &closed, &t.Tickets, &t.AINote, &t.DuplicateOf, &t.Spam, &t.FixedIn, &t.Question, &t.AnswerAuto, &t.Public, &t.Votes)
	if errors.Is(err, sql.ErrNoRows) {
		return t, ErrNotFound
	}
	t.Tags = []string{}
	_ = json.Unmarshal([]byte(tags), &t.Tags)
	if closed.Valid {
		t.ClosedAt = &closed.V
	}
	return t, err
}

// SaveFeedbackTopic inserts or updates a topic.
func (d *DB) SaveFeedbackTopic(ctx context.Context, t FeedbackTopic) error {
	tags, _ := json.Marshal(t.Tags)
	if t.Tags == nil {
		tags = []byte("[]")
	}
	var closed any
	if t.ClosedAt != nil {
		closed = *t.ClosedAt
	}
	_, err := d.Exec(ctx, `INSERT INTO feedback_topics (id, tenant_id, project_id, portal_id, title, summary, type, category, tags, priority, state, answer, report_count, created_at, updated_at, closed_at, ai_note, duplicate_of, spam, fixed_in, question, answer_auto, public)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET portal_id = excluded.portal_id, title = excluded.title, summary = excluded.summary, type = excluded.type, category = excluded.category,
		ai_note = excluded.ai_note, duplicate_of = excluded.duplicate_of, spam = excluded.spam, fixed_in = excluded.fixed_in, question = excluded.question, answer_auto = excluded.answer_auto, public = excluded.public,
		tags = excluded.tags, priority = excluded.priority, state = excluded.state, answer = excluded.answer, report_count = excluded.report_count,
		updated_at = excluded.updated_at, closed_at = excluded.closed_at`,
		t.ID, TenantOf(ctx), t.ProjectID, t.PortalID, t.Title, t.Summary, t.Type, t.Category, string(tags), t.Priority, t.State, t.Answer, t.ReportCount, t.CreatedAt, t.UpdatedAt, closed, t.AINote, t.DuplicateOf, t.Spam, t.FixedIn, t.Question, t.AnswerAuto, t.Public)
	return err
}

// TopicFilter narrows a topic list.
type TopicFilter struct {
	ProjectID string
	PortalIDs []string // empty = all portals of the project
	State     string
	Category  string
	Priority  int
	Query     string
	Limit     int
}

// FeedbackTopics lists topics, highest priority and newest first.
func (d *DB) FeedbackTopics(ctx context.Context, f TopicFilter) ([]FeedbackTopic, error) {
	q := `SELECT ` + topicCols + ` FROM feedback_topics t WHERE t.project_id = ?`
	args := []any{f.ProjectID}
	if len(f.PortalIDs) > 0 {
		q += ` AND t.portal_id IN (?` + strings.Repeat(", ?", len(f.PortalIDs)-1) + `)`
		for _, id := range f.PortalIDs {
			args = append(args, id)
		}
	}
	if f.State != "" {
		q += ` AND t.state = ?`
		args = append(args, f.State)
	}
	if f.Category != "" {
		q += ` AND t.category = ?`
		args = append(args, f.Category)
	}
	if f.Priority > 0 {
		q += ` AND t.priority = ?`
		args = append(args, f.Priority)
	}
	if f.Query != "" {
		q += ` AND (LOWER(t.title) LIKE ? OR LOWER(t.summary) LIKE ?)`
		like := "%" + strings.ToLower(f.Query) + "%"
		args = append(args, like, like)
	}
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 200
	}
	rows, err := d.Query(ctx, q+` ORDER BY t.priority ASC, t.updated_at DESC LIMIT ?`, append(args, f.Limit)...)
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

// OpenFeedbackTopics lists a portal's topics that are not closed.
func (d *DB) OpenFeedbackTopics(ctx context.Context, portalID string, limit int) ([]FeedbackTopic, error) {
	rows, err := d.Query(ctx, `SELECT `+topicCols+` FROM feedback_topics t WHERE t.portal_id = ? AND t.closed_at IS NULL ORDER BY t.updated_at DESC LIMIT ?`, portalID, limit)
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

// FeedbackTopic reads a topic.
func (d *DB) FeedbackTopic(ctx context.Context, projectID, id string) (FeedbackTopic, error) {
	return scanTopic(d.QueryRow(ctx, `SELECT `+topicCols+` FROM feedback_topics t WHERE t.id = ? AND (? = '' OR t.project_id = ?)`, id, projectID, projectID))
}

// DeleteFeedbackTopic removes a topic (its reports keep existing without one).
func (d *DB) DeleteFeedbackTopic(ctx context.Context, id string) error {
	_, err := d.Exec(ctx, `DELETE FROM feedback_topics WHERE id = ?`, id)
	return err
}

// RecountTopic sets a topic's report count from its reports.
func (d *DB) RecountTopic(ctx context.Context, id string) error {
	_, err := d.Exec(ctx, `UPDATE feedback_topics SET report_count = (SELECT COUNT(*) FROM feedback_reports r WHERE r.topic_id = ?), updated_at = ? WHERE id = ?`, id, Now(), id)
	return err
}

// FeedbackReport is one submission.
type FeedbackReport struct {
	ID          string            `json:"id"`
	ProjectID   string            `json:"projectId"`
	PortalID    string            `json:"portalId"`
	TopicID     string            `json:"topicId,omitempty"`
	Type        string            `json:"type"`
	Title       string            `json:"title"`
	Body        string            `json:"body"`
	Fields      map[string]any    `json:"fields"`
	Context     map[string]string `json:"context"`
	Lang        string            `json:"lang,omitempty"`
	UserID      string            `json:"userId,omitempty"`
	UserName    string            `json:"userName,omitempty"`
	Email       string            `json:"email,omitempty"`
	StatusHash  string            `json:"-"`
	Via         string            `json:"via"`
	CheckKind   string            `json:"check,omitempty"`
	Spam        bool              `json:"spam"`
	Deleted     bool              `json:"deleted"`
	CreatedAt   Time              `json:"createdAt"`
	Attachments []FeedbackFile    `json:"attachments,omitempty"`
	// Replies are the reporter's answers to the moderators' question.
	Replies []FeedbackReply `json:"replies,omitempty"`
}

const reportCols = `r.id, r.project_id, r.portal_id, COALESCE(r.topic_id, ''), r.type, r.title, r.body, r.fields, r.context, r.lang, r.user_id, COALESCE(u.name, ''), r.email, r.status_hash, r.via, r.check_kind, r.spam, r.deleted, r.created_at`

func scanReport(r interface{ Scan(...any) error }) (FeedbackReport, error) {
	var x FeedbackReport
	var fields, ctxs string
	err := r.Scan(&x.ID, &x.ProjectID, &x.PortalID, &x.TopicID, &x.Type, &x.Title, &x.Body, &fields, &ctxs, &x.Lang, &x.UserID, &x.UserName, &x.Email, &x.StatusHash, &x.Via, &x.CheckKind, &x.Spam, &x.Deleted, &x.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return x, ErrNotFound
	}
	x.Fields, x.Context = map[string]any{}, map[string]string{}
	_ = json.Unmarshal([]byte(fields), &x.Fields)
	_ = json.Unmarshal([]byte(ctxs), &x.Context)
	return x, err
}

// AddFeedbackReport stores a report.
func (d *DB) AddFeedbackReport(ctx context.Context, r FeedbackReport) error {
	fields, _ := json.Marshal(r.Fields)
	ctxs, _ := json.Marshal(r.Context)
	var topic any
	if r.TopicID != "" {
		topic = r.TopicID
	}
	_, err := d.Exec(ctx, `INSERT INTO feedback_reports (id, tenant_id, project_id, portal_id, topic_id, type, title, body, fields, context, lang, user_id, email, status_hash, via, check_kind, spam, deleted, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, TenantOf(ctx), r.ProjectID, r.PortalID, topic, r.Type, r.Title, r.Body, string(fields), string(ctxs), r.Lang, r.UserID, r.Email, r.StatusHash, r.Via, r.CheckKind, r.Spam, r.Deleted, r.CreatedAt)
	return err
}

// FeedbackReports lists a topic's reports, oldest first.
func (d *DB) FeedbackReports(ctx context.Context, topicID string) ([]FeedbackReport, error) {
	return d.reports(ctx, `r.topic_id = ? ORDER BY r.created_at`, topicID)
}

// FeedbackReportsOf lists a person's reports, newest first.
func (d *DB) FeedbackReportsOf(ctx context.Context, userID string) ([]FeedbackReport, error) {
	return d.reports(ctx, `r.user_id = ? ORDER BY r.created_at DESC`, userID)
}

func (d *DB) reports(ctx context.Context, where string, args ...any) ([]FeedbackReport, error) {
	rows, err := d.Query(ctx, `SELECT `+reportCols+` FROM feedback_reports r LEFT JOIN users u ON u.id = r.user_id WHERE `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FeedbackReport{}
	for rows.Next() {
		x, err := scanReport(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// FeedbackReport reads a report.
func (d *DB) FeedbackReport(ctx context.Context, projectID, id string) (FeedbackReport, error) {
	return scanReport(d.QueryRow(ctx, `SELECT `+reportCols+` FROM feedback_reports r LEFT JOIN users u ON u.id = r.user_id WHERE r.id = ? AND (? = '' OR r.project_id = ?)`, id, projectID, projectID))
}

// FeedbackReportByStatus reads a report by the hash of its status link.
func (d *DB) FeedbackReportByStatus(ctx context.Context, hash string) (FeedbackReport, error) {
	return scanReport(d.QueryRow(ctx, `SELECT `+reportCols+` FROM feedback_reports r LEFT JOIN users u ON u.id = r.user_id WHERE r.status_hash = ? AND r.status_hash <> ''`, hash))
}

// MoveFeedbackReport puts a report into another topic.
func (d *DB) MoveFeedbackReport(ctx context.Context, id, topicID string) error {
	res, err := d.Exec(ctx, `UPDATE feedback_reports SET topic_id = ? WHERE id = ?`, topicID, id)
	return affected(res, err)
}

// MoveTopicReports moves all reports of one topic into another.
func (d *DB) MoveTopicReports(ctx context.Context, from, to string) error {
	if _, err := d.Exec(ctx, `UPDATE feedback_reports SET topic_id = ? WHERE topic_id = ?`, to, from); err != nil {
		return err
	}
	_, err := d.Exec(ctx, `UPDATE feedback_topic_links SET topic_id = ? WHERE topic_id = ? AND task_id NOT IN (SELECT task_id FROM feedback_topic_links WHERE topic_id = ?)`, to, from, to)
	return err
}

// AnonymizeFeedbackReport removes the person from a report and marks it deleted.
func (d *DB) AnonymizeFeedbackReport(ctx context.Context, id string) error {
	_, err := d.Exec(ctx, `UPDATE feedback_reports SET user_id = '', email = '', context = '{}', fields = '{}', status_hash = '', deleted = ? WHERE id = ?`, true, id)
	return err
}

// FeedbackFile is an attachment of a report.
type FeedbackFile struct {
	ID        string `json:"id"`
	ReportID  string `json:"reportId"`
	Name      string `json:"name"`
	Mime      string `json:"mime"`
	Size      int64  `json:"size"`
	SHA256    string `json:"-"`
	CreatedAt Time   `json:"createdAt"`
}

// AddFeedbackFile stores an attachment's metadata.
func (d *DB) AddFeedbackFile(ctx context.Context, projectID string, f FeedbackFile) error {
	_, err := d.Exec(ctx, `INSERT INTO feedback_attachments (id, tenant_id, project_id, report_id, name, mime, size, sha256, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		f.ID, TenantOf(ctx), projectID, f.ReportID, f.Name, f.Mime, f.Size, f.SHA256, f.CreatedAt)
	return err
}

// FeedbackFiles lists the attachments of reports.
func (d *DB) FeedbackFiles(ctx context.Context, reportIDs []string) (map[string][]FeedbackFile, error) {
	out := map[string][]FeedbackFile{}
	if len(reportIDs) == 0 {
		return out, nil
	}
	args := make([]any, len(reportIDs))
	for i, id := range reportIDs {
		args[i] = id
	}
	rows, err := d.Query(ctx, `SELECT id, report_id, name, mime, size, sha256, created_at FROM feedback_attachments WHERE report_id IN (?`+strings.Repeat(", ?", len(reportIDs)-1)+`) ORDER BY created_at`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var f FeedbackFile
		if err := rows.Scan(&f.ID, &f.ReportID, &f.Name, &f.Mime, &f.Size, &f.SHA256, &f.CreatedAt); err != nil {
			return nil, err
		}
		out[f.ReportID] = append(out[f.ReportID], f)
	}
	return out, rows.Err()
}

// FeedbackFile reads one attachment of a project.
func (d *DB) FeedbackFile(ctx context.Context, projectID, id string) (FeedbackFile, error) {
	var f FeedbackFile
	err := d.QueryRow(ctx, `SELECT id, report_id, name, mime, size, sha256, created_at FROM feedback_attachments WHERE id = ? AND project_id = ?`, id, projectID).
		Scan(&f.ID, &f.ReportID, &f.Name, &f.Mime, &f.Size, &f.SHA256, &f.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return f, ErrNotFound
	}
	return f, err
}

// DeleteFeedbackFiles removes attachments of a report and returns their hashes.
func (d *DB) DeleteFeedbackFiles(ctx context.Context, reportID string) ([]string, error) {
	files, err := d.FeedbackFiles(ctx, []string{reportID})
	if err != nil {
		return nil, err
	}
	hashes := []string{}
	for _, f := range files[reportID] {
		hashes = append(hashes, f.SHA256)
	}
	_, err = d.Exec(ctx, `DELETE FROM feedback_attachments WHERE report_id = ?`, reportID)
	return hashes, err
}

// FeedbackUsage is the size of a project's feedback attachments.
func (d *DB) FeedbackUsage(ctx context.Context, projectID string) (int64, error) {
	var n sql.NullInt64
	err := d.QueryRow(ctx, `SELECT SUM(size) FROM feedback_attachments WHERE project_id = ?`, projectID).Scan(&n)
	return n.Int64, err
}

// OldestClosedWithFiles lists reports of closed topics that still have
// attachments, oldest first (to free feedback storage).
func (d *DB) OldestClosedWithFiles(ctx context.Context, projectID string, limit int) ([]string, error) {
	rows, err := d.Query(ctx, `SELECT DISTINCT r.id, r.created_at FROM feedback_reports r JOIN feedback_topics t ON t.id = r.topic_id
		JOIN feedback_attachments a ON a.report_id = r.id WHERE r.project_id = ? AND t.closed_at IS NOT NULL ORDER BY r.created_at LIMIT ?`, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		var at Time
		if err := rows.Scan(&id, &at); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// FeedbackQuota is a project's own feedback storage limit in MB (nil = default).
func (d *DB) FeedbackQuota(ctx context.Context, projectID string) (*int, error) {
	var n sql.NullInt64
	err := d.QueryRow(ctx, `SELECT feedback_quota_mb FROM projects WHERE id = ?`, projectID).Scan(&n)
	return nullInt(n), err
}

// SetFeedbackQuota sets a project's feedback storage limit (nil = default).
func (d *DB) SetFeedbackQuota(ctx context.Context, projectID string, mb *int) error {
	_, err := d.Exec(ctx, `UPDATE projects SET feedback_quota_mb = ? WHERE id = ?`, intOrNull(mb), projectID)
	return err
}

// TopicTicket is a card made from a topic.
type TopicTicket struct {
	TaskID    string `json:"taskId"`
	Title     string `json:"title"`
	BoardID   string `json:"boardId"`
	BoardSlug string `json:"boardSlug"`
	Board     string `json:"board"`
	Column    string `json:"column"`
	Done      bool   `json:"done"`
	CreatedAt Time   `json:"createdAt"`
}

// LinkTopicTask links a card to a topic.
func (d *DB) LinkTopicTask(ctx context.Context, topicID, taskID, by string) error {
	_, err := d.Exec(ctx, `INSERT INTO feedback_topic_links (topic_id, task_id, tenant_id, created_by, created_at, reports_at_link)
		VALUES (?, ?, ?, ?, ?, (SELECT report_count FROM feedback_topics WHERE id = ?)) ON CONFLICT DO NOTHING`,
		topicID, taskID, TenantOf(ctx), by, Now(), topicID)
	return err
}

// LinkTopicTaskAt links a card made when the topic had reports reports
// (later ones count as new on the card).
func (d *DB) LinkTopicTaskAt(ctx context.Context, topicID, taskID, by, reports string) error {
	n, _ := strconv.Atoi(reports)
	_, err := d.Exec(ctx, `INSERT INTO feedback_topic_links (topic_id, task_id, tenant_id, created_by, created_at, reports_at_link) VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`,
		topicID, taskID, TenantOf(ctx), by, Now(), n)
	return err
}

// TopicTickets lists the cards of a topic.
func (d *DB) TopicTickets(ctx context.Context, topicID string) ([]TopicTicket, error) {
	rows, err := d.Query(ctx, `SELECT k.id, k.title, k.board_id, b.slug, b.title, k.column_id, k.done_at IS NOT NULL, l.created_at
		FROM feedback_topic_links l JOIN tasks k ON k.id = l.task_id JOIN documents b ON b.id = k.board_id WHERE l.topic_id = ? ORDER BY l.created_at`, topicID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TopicTicket{}
	for rows.Next() {
		var t TopicTicket
		if err := rows.Scan(&t.TaskID, &t.Title, &t.BoardID, &t.BoardSlug, &t.Board, &t.Column, &t.Done, &t.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// TopicsOfTask lists the topics a card was made from.
func (d *DB) TopicsOfTask(ctx context.Context, taskID string) ([]string, error) {
	rows, err := d.Query(ctx, `SELECT topic_id FROM feedback_topic_links WHERE task_id = ?`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// PurgeFeedbackReports deletes anonymous reports of topics closed before a
// time and returns the hashes of their attachments.
func (d *DB) PurgeFeedbackReports(ctx context.Context, portalID string, before time.Time) ([]string, error) {
	rows, err := d.Query(ctx, `SELECT r.id FROM feedback_reports r JOIN feedback_topics t ON t.id = r.topic_id
		WHERE r.portal_id = ? AND r.user_id = '' AND t.closed_at IS NOT NULL AND t.closed_at < ?`, portalID, Time{before})
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	hashes := []string{}
	for _, id := range ids {
		h, err := d.DeleteFeedbackFiles(ctx, id)
		if err != nil {
			return hashes, err
		}
		hashes = append(hashes, h...)
		if _, err := d.Exec(ctx, `DELETE FROM feedback_reports WHERE id = ?`, id); err != nil {
			return hashes, err
		}
	}
	return hashes, nil
}

// AllFeedbackProjects lists the projects that have portals (housekeeping).
func (d *DB) AllFeedbackProjects(ctx context.Context) ([]string, error) {
	rows, err := d.Query(ctx, `SELECT DISTINCT project_id FROM feedback_portals`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// TaskFeedback is the feedback behind a card: its topic and how many
// reports came in since the card was linked.
type TaskFeedback struct {
	TopicID string `json:"topicId"`
	Title   string `json:"title"`
	Reports int    `json:"reports"`
	New     int    `json:"new"`
}

// TaskFeedbackOf maps the cards of a project to their feedback topics.
func (d *DB) TaskFeedbackOf(ctx context.Context, projectID string) (map[string]TaskFeedback, error) {
	rows, err := d.Query(ctx, `SELECT l.task_id, t.id, t.title, t.report_count, l.reports_at_link
		FROM feedback_topic_links l JOIN feedback_topics t ON t.id = l.topic_id WHERE t.project_id = ?`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]TaskFeedback{}
	for rows.Next() {
		var task string
		var f TaskFeedback
		var at int
		if err := rows.Scan(&task, &f.TopicID, &f.Title, &f.Reports, &at); err != nil {
			return nil, err
		}
		f.New = max(0, f.Reports-at)
		// A card linked to several topics shows the one with most new reports.
		if cur, ok := out[task]; !ok || f.New > cur.New {
			out[task] = f
		}
	}
	return out, rows.Err()
}

// SeenTaskFeedback resets the "+n new reports" of a card.
func (d *DB) SeenTaskFeedback(ctx context.Context, taskID string) error {
	_, err := d.Exec(ctx, `UPDATE feedback_topic_links SET reports_at_link = (SELECT report_count FROM feedback_topics t WHERE t.id = feedback_topic_links.topic_id) WHERE task_id = ?`, taskID)
	return err
}

// TopicEntity is an entry linked to a feedback topic.
type TopicEntity struct {
	ID     string `json:"id"`
	Slug   string `json:"slug"`
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Source string `json:"source"` // person | ai
}

// TopicEntities lists the entries of a topic.
func (d *DB) TopicEntities(ctx context.Context, topicID string) ([]TopicEntity, error) {
	rows, err := d.Query(ctx, `SELECT e.id, e.slug, e.name, e.kind, l.source FROM feedback_topic_entities l JOIN entities e ON e.id = l.entity_id
		WHERE l.topic_id = ? AND e.deleted_at IS NULL ORDER BY e.name`, topicID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TopicEntity{}
	for rows.Next() {
		var e TopicEntity
		if err := rows.Scan(&e.ID, &e.Slug, &e.Name, &e.Kind, &e.Source); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// SetTopicEntities replaces the entries of a topic (ids with their source).
func (d *DB) SetTopicEntities(ctx context.Context, projectID, topicID string, ids map[string]string) error {
	return d.InTx(ctx, func(tx *Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM feedback_topic_entities WHERE topic_id = ?`, topicID); err != nil {
			return err
		}
		for id, src := range ids {
			if _, err := tx.Exec(ctx, `INSERT INTO feedback_topic_entities (topic_id, entity_id, tenant_id, project_id, source, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
				topicID, id, TenantOf(ctx), projectID, src, Now()); err != nil {
				return err
			}
		}
		return nil
	})
}

// AddTopicEntity links one entry (keeps an existing link and its source).
func (d *DB) AddTopicEntity(ctx context.Context, projectID, topicID, entityID, source string) error {
	_, err := d.Exec(ctx, `INSERT INTO feedback_topic_entities (topic_id, entity_id, tenant_id, project_id, source, created_at) VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`,
		topicID, entityID, TenantOf(ctx), projectID, source, Now())
	return err
}

// EntityTopics lists the topics linked to an entry (open ones first).
func (d *DB) EntityTopics(ctx context.Context, entityID string) ([]FeedbackTopic, error) {
	rows, err := d.Query(ctx, `SELECT `+topicCols+` FROM feedback_topics t JOIN feedback_topic_entities l ON l.topic_id = t.id
		WHERE l.entity_id = ? ORDER BY (t.closed_at IS NULL) DESC, t.updated_at DESC LIMIT 100`, entityID)
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

// FeedbackReply is a reporter's answer to a moderator's question.
type FeedbackReply struct {
	ID        string `json:"id"`
	TopicID   string `json:"topicId"`
	ReportID  string `json:"reportId"`
	Body      string `json:"body"`
	CreatedAt Time   `json:"createdAt"`
}

// AddFeedbackReply stores a reporter's answer.
func (d *DB) AddFeedbackReply(ctx context.Context, projectID string, r FeedbackReply) error {
	_, err := d.Exec(ctx, `INSERT INTO feedback_replies (id, tenant_id, project_id, topic_id, report_id, body, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		r.ID, TenantOf(ctx), projectID, r.TopicID, r.ReportID, r.Body, r.CreatedAt)
	return err
}

// FeedbackReplies lists the answers of reporters on a topic (by report).
func (d *DB) FeedbackReplies(ctx context.Context, topicID string) (map[string][]FeedbackReply, error) {
	rows, err := d.Query(ctx, `SELECT id, topic_id, report_id, body, created_at FROM feedback_replies WHERE topic_id = ? ORDER BY created_at`, topicID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]FeedbackReply{}
	for rows.Next() {
		var r FeedbackReply
		if err := rows.Scan(&r.ID, &r.TopicID, &r.ReportID, &r.Body, &r.CreatedAt); err != nil {
			return nil, err
		}
		out[r.ReportID] = append(out[r.ReportID], r)
	}
	return out, rows.Err()
}

// RecentlyClosedTopics lists topics of a portal closed since a time.
func (d *DB) RecentlyClosedTopics(ctx context.Context, portalID string, since Time) ([]FeedbackTopic, error) {
	rows, err := d.Query(ctx, `SELECT `+topicCols+` FROM feedback_topics t WHERE t.portal_id = ? AND t.closed_at IS NOT NULL AND t.closed_at >= ? ORDER BY t.closed_at DESC LIMIT 200`, portalID, since)
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
