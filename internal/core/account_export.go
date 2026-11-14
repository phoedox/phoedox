package core

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"
	"strings"
	"time"

	"github.com/phoedox/phoedox/internal/blob"
	"github.com/phoedox/phoedox/internal/jobs"
	"github.com/phoedox/phoedox/internal/mail"
	"github.com/phoedox/phoedox/internal/store"
	"github.com/phoedox/phoedox/resources"
)

// Personal data export (ADR-0055): a background job writes a ZIP with all
// data about the person (JSON and their files, without password hashes,
// secrets or tokens). The person gets a link by mail; the download needs
// their login, works for 7 days and is then deleted. One export per 24 h.

// JobUserExport builds one personal data export.
const JobUserExport = "user.export"

const (
	exportValid    = 7 * 24 * time.Hour
	exportInterval = 24 * time.Hour
	// exportFileLimit caps the files copied into one export; the rest is listed.
	exportFileLimit = int64(1) << 30
)

// ExportStatus is what the account page shows about exports.
type ExportStatus struct {
	// MailEnabled is false when the server cannot send mail; exports are
	// then off, because the link goes out by mail.
	MailEnabled bool              `json:"mailEnabled"`
	Latest      *store.UserExport `json:"latest,omitempty"`
	NextAt      *store.Time       `json:"nextAt,omitempty"` // earliest next export
}

func ownAccount(a Actor) error {
	if a.User.ID == "" || a.Via == "mcp" || a.Via == "token" || a.Plugin != "" {
		return ErrForbidden
	}
	return nil
}

// MyExport returns the export state of the signed-in person.
func (s *Service) MyExport(ctx context.Context, a Actor) (ExportStatus, error) {
	if err := ownAccount(a); err != nil {
		return ExportStatus{}, err
	}
	st := ExportStatus{MailEnabled: s.Mail != nil && s.Mail.Enabled() && s.PublicURL != ""}
	e, err := s.DB.LatestUserExport(ctx, a.User.ID)
	if errors.Is(err, store.ErrNotFound) {
		return st, nil
	}
	if err != nil {
		return st, err
	}
	if e.ExpiresAt.After(time.Now()) {
		st.Latest = &e
	}
	if next := e.CreatedAt.Add(exportInterval); next.After(time.Now()) {
		st.NextAt = &store.Time{Time: next}
	}
	return st, nil
}

// RequestExport queues a new export of the signed-in person's data.
func (s *Service) RequestExport(ctx context.Context, a Actor) (store.UserExport, error) {
	st, err := s.MyExport(ctx, a)
	if err != nil {
		return store.UserExport{}, err
	}
	if !st.MailEnabled {
		return store.UserExport{}, invalid("validation.export_needs_mail", nil, "exports need mail delivery on this server")
	}
	if st.NextAt != nil {
		return store.UserExport{}, invalid("validation.export_too_soon", map[string]any{"at": st.NextAt.UTC().Format(time.RFC3339)}, "one export per 24 hours")
	}
	now := store.Now()
	e := store.UserExport{ID: newID(), UserID: a.User.ID, Status: "queued", CreatedAt: now, ExpiresAt: store.Time{Time: now.Add(exportValid)}}
	if err := s.DB.CreateUserExport(ctx, e); err != nil {
		return e, err
	}
	s.event(ctx, a, "user.export", a.User.ID, map[string]string{"export": e.ID})
	if s.Jobs == nil {
		err = s.runExport(ctx, e.ID)
	} else {
		err = s.Jobs.Add(ctx, JobUserExport, map[string]string{"export": e.ID})
	}
	return e, err
}

func (s *Service) registerExportJobs(q *jobs.Queue) {
	q.Handle(JobUserExport, func(ctx context.Context, payload json.RawMessage) error {
		var p struct{ Export string }
		if err := json.Unmarshal(payload, &p); err != nil {
			return jobs.Permanent(err)
		}
		return s.runExport(ctx, p.Export)
	})
}

func (s *Service) runExport(ctx context.Context, id string) error {
	e, err := s.DB.GetUserExport(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if e.Status != "queued" {
		return nil
	}
	u, err := s.DB.UserByID(ctx, e.UserID)
	if err != nil || u.Deleted {
		return s.DB.FinishUserExport(ctx, id, "", 0, "account not found")
	}
	sha, size, err := s.writeExport(ctx, u)
	if err != nil {
		slog.Warn("personal data export failed", "export", id, "err", err)
		_ = s.DB.FinishUserExport(ctx, id, "", 0, err.Error())
		return jobs.Permanent(err)
	}
	if err := s.DB.FinishUserExport(ctx, id, sha, size, ""); err != nil {
		return err
	}
	s.mailExport(ctx, u)
	return nil
}

func (s *Service) mailExport(ctx context.Context, u store.User) {
	if s.Mail == nil || !s.Mail.Enabled() || s.PublicURL == "" {
		return
	}
	subject, body, err := resources.Mail("export", s.locale(u), map[string]any{
		"Name": u.Name, "Link": s.PublicURL + "/settings/account", "Days": int(exportValid.Hours() / 24),
	})
	if err == nil {
		err = s.Mail.Send(ctx, mail.Message{To: u.Email, Subject: subject, Body: body})
	}
	if err != nil {
		slog.Warn("export mail failed", "user", u.ID, "err", err)
	}
}

// exportWriter adds JSON files and copies to the export ZIP.
type exportWriter struct{ z *zip.Writer }

func (w exportWriter) json(name string, v any) error {
	f, err := w.z.Create(name)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// writeExport builds the ZIP for u in the temp folder and stores it.
func (s *Service) writeExport(ctx context.Context, u store.User) (string, int64, error) {
	if s.Blobs == nil {
		return "", 0, errors.New("no file storage configured")
	}
	if err := os.MkdirAll(s.TempDir, 0o700); err != nil {
		return "", 0, err
	}
	tmp, err := os.CreateTemp(s.TempDir, "export-*.zip")
	if err != nil {
		return "", 0, err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	h := sha256.New()
	cw := &countWriter{w: io.MultiWriter(tmp, h)}
	z := zip.NewWriter(cw)
	if err := s.fillExport(ctx, exportWriter{z}, u); err != nil {
		return "", 0, err
	}
	if err := z.Close(); err != nil {
		return "", 0, err
	}
	if err := tmp.Close(); err != nil {
		return "", 0, err
	}
	sum := hex.EncodeToString(h.Sum(nil))
	return sum, cw.n, s.Blobs.Put(ctx, sum, tmp.Name(), cw.n)
}

type countWriter struct {
	w io.Writer
	n int64
}

func (c *countWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

// exportQueries are the person's rows, without hashes, secrets or tokens.
var exportQueries = []struct{ file, query string }{
	{"account/sessions.json", `SELECT created_at, expires_at FROM sessions WHERE user_id = ?`},
	{"account/api-tokens.json", `SELECT id, name, prefix, access, projects, created_at, last_used_at, revoked_at FROM api_tokens WHERE user_id = ?`},
	{"account/two-factor.json", `SELECT confirmed_at, created_at FROM user_totp WHERE user_id = ?`},
	{"account/passkeys.json", `SELECT id, name, created_at, last_used_at FROM webauthn_credentials WHERE user_id = ?`},
	{"account/sign-in-links.json", `SELECT provider, subject, email, created_at, last_login_at FROM user_identities WHERE user_id = ?`},
	{"account/ai-clients.json", `SELECT g.id, c.name AS client, g.scope, g.resource, g.access, g.projects, g.created_at, g.last_used_at, g.revoked_at FROM oauth_grants g LEFT JOIN oauth_clients c ON c.id = g.client_id WHERE g.user_id = ?`},
	{"account/signing-keys.json", `SELECT id, name, public_key, created_at FROM signing_keys WHERE user_id = ?`},
	{"memberships/organizations.json", `SELECT o.id, o.slug, o.name, m.role, m.created_at FROM org_members m JOIN organizations o ON o.id = m.org_id WHERE m.user_id = ?`},
	{"memberships/groups.json", `SELECT g.id, g.name, o.name AS organization, m.source FROM group_members m JOIN groups g ON g.id = m.group_id LEFT JOIN organizations o ON o.id = g.org_id WHERE m.user_id = ?`},
	{"memberships/projects.json", `SELECT p.id, p.slug, p.name, m.role, m.can_control, m.expires_at FROM project_members m JOIN projects p ON p.id = m.project_id WHERE m.user_id = ?`},
	{"memberships/shared-pages.json", `SELECT d.id, d.title, p.slug AS project, g.role, g.created_at FROM document_grants g JOIN documents d ON d.id = g.document_id JOIN projects p ON p.id = d.project_id WHERE g.user_id = ?`},
	{"memberships/device-control.json", `SELECT c.id, p.slug AS project, e.name AS entry, c.granted_by, c.created_at, c.expires_at FROM control_grants c JOIN projects p ON p.id = c.project_id LEFT JOIN entities e ON e.id = c.entity_id WHERE c.user_id = ?`},
	{"settings/preferences.json", `SELECT shortcuts, pinned_projects, updated_at FROM user_prefs WHERE user_id = ?`},
	{"settings/notifications.json", `SELECT email, ntfy_url, muted, matrix_homeserver, matrix_room, last_digest, updated_at FROM notification_settings WHERE user_id = ?`},
	{"settings/push-subscriptions.json", `SELECT id, endpoint, created_at FROM push_subscriptions WHERE user_id = ?`},
	{"settings/favorites.json", `SELECT f.id, p.slug AS project, d.title, f.position, f.created_at FROM favorites f LEFT JOIN projects p ON p.id = f.project_id LEFT JOIN documents d ON d.id = f.document_id WHERE f.user_id = ?`},
	{"activity/recently-viewed.json", `SELECT r.kind, r.ref_id, p.slug AS project, r.viewed_at FROM recent_views r LEFT JOIN projects p ON p.id = r.project_id WHERE r.user_id = ?`},
	{"activity/notifications.json", `SELECT id, kind, title, body, link, actor_name, read_at, mailed_at, created_at FROM notifications WHERE user_id = ?`},
	{"activity/comments.json", `SELECT c.id, p.slug AS project, d.title AS page, c.body, c.created_at, c.updated_at, c.deleted_at FROM comments c JOIN projects p ON p.id = c.project_id LEFT JOIN documents d ON d.id = c.document_id WHERE c.author_id = ?`},
	{"activity/read-confirmations.json", `SELECT d.title AS page, p.slug AS project, r.version, r.confirmed_at FROM read_confirmations r JOIN documents d ON d.id = r.document_id JOIN projects p ON p.id = d.project_id WHERE r.user_id = ?`},
	{"activity/tasks.json", `SELECT t.id, p.slug AS project, t.title, t.description, t.due, t.created_at, t.done_at, CASE WHEN t.created_by = ? THEN 'created' ELSE 'assigned' END AS relation FROM tasks t JOIN projects p ON p.id = t.project_id WHERE t.created_by = ? OR t.assignee_id = ?`},
	{"activity/pages-created.json", `SELECT d.id, p.slug AS project, d.title, d.doc_type, d.created_at, d.deleted_at FROM documents d JOIN projects p ON p.id = d.project_id WHERE d.created_by = ?`},
	{"activity/files-uploaded.json", `SELECT f.id, p.slug AS project, f.name, f.mime, f.size, f.created_at, f.deleted_at FROM files f JOIN projects p ON p.id = f.project_id WHERE f.created_by = ?`},
}

func (s *Service) fillExport(ctx context.Context, w exportWriter, u store.User) error {
	readme, err := resources.Text("export-readme.txt")
	if err != nil {
		return err
	}
	f, err := w.z.Create("README.txt")
	if err != nil {
		return err
	}
	if _, err := io.WriteString(f, readme); err != nil {
		return err
	}
	if err := w.json("account/profile.json", map[string]any{
		"id": u.ID, "email": u.Email, "name": u.Name, "locale": u.Locale, "systemAdmin": u.IsAdmin,
		"createdAt": u.CreatedAt, "lastLoginAt": u.LastLoginAt, "hasPassword": u.HasPassword, "exportedAt": store.Now(),
	}); err != nil {
		return err
	}
	if img, err := s.DB.GetImage(ctx, "user", u.ID); err == nil {
		ext := ".img"
		if exts := mimeExt(img.Mime); exts != "" {
			ext = exts
		}
		if f, err := w.z.Create("account/profile-picture" + ext); err == nil {
			_, _ = f.Write(img.Data)
		}
	}
	for _, q := range exportQueries {
		args := []any{u.ID}
		if n := strings.Count(q.query, "?"); n > 1 {
			args = []any{u.ID, u.ID, u.ID}[:n]
		}
		rows, err := s.DB.Rows(ctx, q.query, args...)
		if err != nil {
			return fmt.Errorf("%s: %w", q.file, err)
		}
		if err := w.json(q.file, rows); err != nil {
			return err
		}
	}
	events, err := s.DB.Rows(ctx, `SELECT created_at, kind, subject, payload FROM events WHERE tenant_id = ? AND (actor = ? OR actor LIKE ?) ORDER BY id`,
		store.TenantOf(ctx), "user:"+u.ID, "%:"+u.ID+":%")
	if err != nil {
		return err
	}
	if err := w.json("activity/audit-log.json", events); err != nil {
		return err
	}
	return s.exportFiles(ctx, w, u)
}

// exportFiles copies the current version of files the person uploaded.
func (s *Service) exportFiles(ctx context.Context, w exportWriter, u store.User) error {
	files, err := s.DB.Rows(ctx, `SELECT f.id, f.name, f.sha256, f.size, p.slug AS project FROM files f JOIN projects p ON p.id = f.project_id
		WHERE f.created_by = ? AND f.deleted_at IS NULL ORDER BY f.created_at`, u.ID)
	if err != nil {
		return err
	}
	var total int64
	var skipped []map[string]any
	for _, f := range files {
		size, _ := f["size"].(int64)
		name := path.Join("files", safeName(fmt.Sprint(f["project"])), safeName(fmt.Sprint(f["id"]))+"-"+safeName(fmt.Sprint(f["name"])))
		if total+size > exportFileLimit {
			skipped = append(skipped, map[string]any{"id": f["id"], "project": f["project"], "name": f["name"], "size": size})
			continue
		}
		if err := s.copyBlob(ctx, w, name, fmt.Sprint(f["sha256"])); err != nil {
			skipped = append(skipped, map[string]any{"id": f["id"], "project": f["project"], "name": f["name"], "error": "content missing"})
			continue
		}
		total += size
	}
	if len(skipped) > 0 {
		return w.json("files/not-included.json", skipped)
	}
	return nil
}

func (s *Service) copyBlob(ctx context.Context, w exportWriter, name, hash string) error {
	rc, err := s.Blobs.Open(ctx, hash)
	if err != nil {
		return err
	}
	defer rc.Close()
	f, err := w.z.Create(name)
	if err != nil {
		return err
	}
	_, err = io.Copy(f, rc)
	return err
}

func mimeExt(m string) string {
	switch m {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	}
	return ""
}

// ExportDownload opens a finished export of the signed-in person.
func (s *Service) ExportDownload(ctx context.Context, a Actor, id string) (store.UserExport, blob.ReadSeekCloser, error) {
	if err := ownAccount(a); err != nil {
		return store.UserExport{}, nil, err
	}
	e, err := s.DB.GetUserExport(ctx, id)
	if err != nil || e.UserID != a.User.ID || e.Status != "done" || !e.ExpiresAt.After(time.Now()) || s.Blobs == nil {
		return e, nil, ErrNotFound
	}
	rc, err := s.Blobs.Open(ctx, e.SHA256)
	if err != nil {
		return e, nil, err
	}
	s.event(ctx, a, "user.export_download", a.User.ID, map[string]string{"export": e.ID})
	return e, rc, nil
}

// PruneExports deletes expired exports and their files (housekeeping, all tenants).
func (s *Service) PruneExports(ctx context.Context, now time.Time) {
	list, err := s.DB.ExpiredUserExports(ctx, store.Time{Time: now})
	if err != nil {
		slog.Warn("listing expired exports failed", "err", err)
		return
	}
	for _, e := range list {
		s.dropExport(ctx, e)
	}
}

func (s *Service) dropExport(ctx context.Context, e store.UserExport) {
	if err := s.DB.DeleteUserExport(ctx, e.ID); err != nil {
		slog.Warn("deleting an export failed", "export", e.ID, "err", err)
		return
	}
	if e.SHA256 != "" {
		s.freeBlobs(ctx, []string{e.SHA256})
	}
}
