package core

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"log/slog"
	"os"
	"path"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/phoedox/phoedox/internal/mail"
	"github.com/phoedox/phoedox/internal/scan"
	"github.com/phoedox/phoedox/internal/store"
)

// Feedback portals (feedback-concept, ADR-0075): projects run portals for
// what they build; people send bugs, ideas and questions; reports are
// grouped into topics; moderators turn topics into tickets on boards.

// PortalField is a typed field of the report form.
type PortalField struct {
	ID       string            `json:"id"`
	Label    map[string]string `json:"label"`
	Type     string            `json:"type"` // text | number | choice | multi | version | bool | url | date
	Options  []string          `json:"options,omitempty"`
	Required bool              `json:"required"`
	// ReportTypes limits the field to some report types (empty = all).
	ReportTypes []string `json:"reportTypes,omitempty"`
}

// AttachRules are what a portal accepts as attachments.
type AttachRules struct {
	Images   bool                `json:"images"`
	Videos   bool                `json:"videos"`
	Logs     bool                `json:"logs"`
	Other    bool                `json:"other"`
	Endings  map[string][]string `json:"endings"`  // kind → endings without dot
	MaxMB    map[string]int      `json:"maxMb"`    // kind → MB per file
	TextOnly bool                `json:"textOnly"` // logs must be UTF-8 text
	MaxFiles int                 `json:"maxFiles"`
}

// PortalTheme is how a portal looks.
type PortalTheme struct {
	Accent     string `json:"accent,omitempty"`
	AccentDark string `json:"accentDark,omitempty"`
	Logo       string `json:"logo,omitempty"` // https URL or /api/v1/files/… of the project
}

// PortalSettings are a portal's options.
type PortalSettings struct {
	Intro       map[string]string `json:"intro"`
	Privacy     map[string]string `json:"privacy"`
	Languages   []string          `json:"languages"`
	Types       []string          `json:"types"`
	Categories  []string          `json:"categories"`
	Fields      []PortalField     `json:"fields"`
	Attachments AttachRules       `json:"attachments"`
	// Who may report: anonymous visitors, Phoedox accounts.
	Anonymous bool `json:"anonymous"`
	Accounts  bool `json:"accounts"`
	// AskEmail offers anonymous reporters an optional e-mail for answers.
	AskEmail bool `json:"askEmail"`
	// Topic states; new topics get DefaultState, done topics FinishedState.
	States        []string `json:"states"`
	DefaultState  string   `json:"defaultState"`
	FinishedState string   `json:"finishedState"`
	// StorageFull: no_files (send without files), drop_oldest, pause.
	StorageFull string      `json:"storageFull"`
	Theme       PortalTheme `json:"theme"`
	// Intake ways the owner switched on: public key and server key.
	PublicKeyOn bool `json:"publicKeyOn"`
	ServerKeyOn bool `json:"serverKeyOn"`
	// RetentionMonths keeps closed anonymous reports this long (0 = 12).
	RetentionMonths int `json:"retentionMonths"`
	// Tags moderators and the AI may give topics.
	Tags []string `json:"tags,omitempty"`
	// AITriage sorts new reports with the project's background AI (ADR-0077).
	AITriage bool `json:"aiTriage,omitempty"`
}

var reportTypes = []string{"bug", "idea", "question", "praise"}
var fieldTypes = []string{"text", "number", "choice", "multi", "version", "bool", "url", "date"}
var handleRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,38}[a-z0-9]$`)
var reservedHandles = []string{"status", "api", "new", "admin", "embed", "widget"}

// DefaultPortalSettings are the settings of a new portal.
func DefaultPortalSettings() PortalSettings {
	return PortalSettings{
		Intro: map[string]string{}, Privacy: map[string]string{}, Languages: []string{"en", "de"},
		Types: []string{"bug", "idea", "question"}, Categories: []string{}, Fields: []PortalField{},
		Attachments: AttachRules{Images: true, Logs: true, TextOnly: true, MaxFiles: 5,
			Endings: map[string][]string{"images": {"png", "jpg", "jpeg", "webp", "gif"}, "videos": {"mp4", "webm"}, "logs": {"log", "txt", "json"}, "other": {}},
			MaxMB:   map[string]int{"images": 10, "videos": 100, "logs": 20, "other": 20}},
		Accounts: true, AskEmail: true,
		States: []string{"new", "planned", "in_progress", "done", "wont_fix"}, DefaultState: "new", FinishedState: "done",
		StorageFull: "no_files", RetentionMonths: 12,
	}
}

func portalSettings(p store.FeedbackPortal) PortalSettings {
	s := DefaultPortalSettings()
	_ = json.Unmarshal(p.Settings, &s)
	return s
}

func (ps *PortalSettings) validate() error {
	ps.Tags = cleanLabels(append(ps.Tags[:0:0], ps.Tags...))
	if len(ps.Types) == 0 {
		ps.Types = []string{"bug"}
	}
	for _, t := range ps.Types {
		if !slices.Contains(reportTypes, t) {
			return invalid("validation.feedback_type", map[string]any{"type": t}, "unknown report type %q", t)
		}
	}
	ids := map[string]bool{}
	for _, f := range ps.Fields {
		if f.ID == "" || ids[f.ID] || !slices.Contains(fieldTypes, f.Type) || ((f.Type == "choice" || f.Type == "multi") && len(f.Options) == 0) {
			return invalid("validation.feedback_field", map[string]any{"field": f.ID}, "invalid field %q", f.ID)
		}
		ids[f.ID] = true
	}
	if len(ps.States) < 2 || !slices.Contains(ps.States, ps.DefaultState) || !slices.Contains(ps.States, ps.FinishedState) || ps.DefaultState == ps.FinishedState {
		return invalid("validation.feedback_states", nil, "states need a default and a different finished state")
	}
	if !slices.Contains([]string{"no_files", "drop_oldest", "pause"}, ps.StorageFull) {
		ps.StorageFull = "no_files"
	}
	if !ps.Anonymous && !ps.Accounts && !ps.PublicKeyOn && !ps.ServerKeyOn {
		return invalid("validation.feedback_no_way", nil, "switch on at least one way to report")
	}
	a := &ps.Attachments
	if a.MaxFiles < 0 || a.MaxFiles > 20 {
		a.MaxFiles = 5
	}
	for k, mb := range a.MaxMB {
		if mb < 1 || mb > 2048 {
			return invalid("validation.feedback_size", map[string]any{"kind": k}, "file size for %s must be 1–2048 MB", k)
		}
	}
	for k, list := range a.Endings {
		for i, e := range list {
			list[i] = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(e)), ".")
		}
		a.Endings[k] = slices.DeleteFunc(list, func(e string) bool { return e == "" })
	}
	if ps.RetentionMonths <= 0 || ps.RetentionMonths > 120 {
		ps.RetentionMonths = 12
	}
	for _, c := range []string{ps.Theme.Accent, ps.Theme.AccentDark} {
		if c != "" && !hexColor.MatchString(c) {
			return invalid("validation.color", nil, "colors must be #rrggbb")
		}
	}
	if l := ps.Theme.Logo; l != "" && !strings.HasPrefix(l, "https://") && !strings.HasPrefix(l, "/api/v1/") {
		return invalid("validation.feedback_logo", nil, "the logo must be an https address or a file of the project")
	}
	return nil
}

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// PortalView is a portal as moderators see it.
type PortalView struct {
	store.FeedbackPortal
	Settings   PortalSettings    `json:"settings"`
	Moderators []store.OrgMember `json:"moderators"`
	// ServerKey is shown once, right after it was created.
	ServerKey string `json:"serverKey,omitempty"`
}

// PortalInput creates or changes a portal.
type PortalInput struct {
	Handle   string          `json:"handle"`
	Name     string          `json:"name"`
	Enabled  *bool           `json:"enabled,omitempty"`
	Settings *PortalSettings `json:"settings,omitempty"`
}

// feedbackProject returns the project for owners/editors.
func (s *Service) feedbackProject(ctx context.Context, a Actor, ref, need string) (store.Project, error) {
	if err := s.fullUser(a); err != nil && a.Via != "mcp" {
		return store.Project{}, err
	}
	return s.project(ctx, a, ref, need)
}

// moderation returns the project and the portals the actor moderates: all
// for editors and owners, or the ones they were made moderator of.
func (s *Service) moderation(ctx context.Context, a Actor, ref string) (store.Project, []store.FeedbackPortal, error) {
	p, err := s.project(ctx, a, ref, store.RoleEditor)
	if err == nil {
		ps, err := s.DB.FeedbackPortals(ctx, p.ID)
		return p, ps, err
	}
	// Moderators from outside the project see only their portals.
	mods, merr := s.DB.ModeratedPortals(ctx, a.User.ID)
	if merr != nil || len(mods) == 0 {
		return p, nil, err
	}
	proj, perr := s.DB.ProjectFor(ctx, ref, a.User.ID)
	if perr != nil {
		return p, nil, err
	}
	mine := []store.FeedbackPortal{}
	for _, m := range mods {
		if m.ProjectID == proj.ID {
			mine = append(mine, m)
		}
	}
	if len(mine) == 0 {
		return p, nil, err
	}
	return proj, mine, nil
}

func newSecret(prefix string) string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return prefix + base64.RawURLEncoding.EncodeToString(b)
}

func hashSecret(v string) string {
	h := sha256.Sum256([]byte(v))
	return hex.EncodeToString(h[:])
}

func (s *Service) portalView(ctx context.Context, p store.FeedbackPortal) PortalView {
	mods, _ := s.DB.FeedbackModerators(ctx, p.ID)
	if mods == nil {
		mods = []store.OrgMember{}
	}
	return PortalView{FeedbackPortal: p, Settings: portalSettings(p), Moderators: mods}
}

// FeedbackPortals lists a project's portals (editors and owners).
func (s *Service) FeedbackPortals(ctx context.Context, a Actor, ref string) ([]PortalView, error) {
	_, ps, err := s.moderation(ctx, a, ref)
	if err != nil {
		return nil, err
	}
	out := []PortalView{}
	for _, p := range ps {
		out = append(out, s.portalView(ctx, p))
	}
	return out, nil
}

// SaveFeedbackPortal creates (id "") or changes a portal (owners).
func (s *Service) SaveFeedbackPortal(ctx context.Context, a Actor, ref, id string, in PortalInput) (PortalView, error) {
	p, err := s.feedbackProject(ctx, a, ref, store.RoleOwner)
	if err != nil {
		return PortalView{}, err
	}
	now := store.Now()
	var portal store.FeedbackPortal
	settings := DefaultPortalSettings()
	if id == "" {
		portal = store.FeedbackPortal{ID: newID(), ProjectID: p.ID, Enabled: true, CreatedBy: a.User.ID, CreatedAt: now}
	} else {
		if portal, err = s.DB.FeedbackPortal(ctx, p.ID, id); err != nil {
			return PortalView{}, err
		}
		settings = portalSettings(portal)
	}
	if in.Handle != "" || id == "" {
		h := strings.ToLower(strings.TrimSpace(in.Handle))
		if !handleRe.MatchString(h) || slices.Contains(reservedHandles, h) {
			return PortalView{}, invalid("validation.feedback_handle", nil, "the handle must be 3–40 lower-case letters, digits or dashes")
		}
		portal.Handle = h
	}
	if n := strings.TrimSpace(in.Name); n != "" {
		portal.Name = excerpt(n, 100)
	} else if id == "" {
		return PortalView{}, invalid("validation.title_required", nil, "name is required")
	}
	if in.Enabled != nil {
		portal.Enabled = *in.Enabled
	}
	if in.Settings != nil {
		settings = *in.Settings
	}
	if err := settings.validate(); err != nil {
		return PortalView{}, err
	}
	var serverKey string
	if settings.PublicKeyOn && portal.PublicKey == "" {
		portal.PublicKey = newSecret("pfk_")
	}
	if !settings.PublicKeyOn {
		portal.PublicKey = ""
	}
	if settings.ServerKeyOn && portal.ServerKeyHash == "" {
		serverKey = newSecret("sfk_")
		portal.ServerKeyHash = hashSecret(serverKey)
	}
	if !settings.ServerKeyOn {
		portal.ServerKeyHash = ""
	}
	raw, _ := json.Marshal(settings)
	portal.Settings, portal.UpdatedAt = raw, now
	if err := s.DB.SaveFeedbackPortal(ctx, portal); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return PortalView{}, invalid("validation.feedback_handle_taken", nil, "this handle is taken")
		}
		return PortalView{}, err
	}
	s.event(ctx, a, "feedback.portal", portal.ID, map[string]any{"handle": portal.Handle, "name": portal.Name})
	v := s.portalView(ctx, portal)
	v.ServerKey = serverKey
	return v, nil
}

// RotateFeedbackKey replaces a portal's public or server key (owners).
func (s *Service) RotateFeedbackKey(ctx context.Context, a Actor, ref, id, kind string) (PortalView, error) {
	p, err := s.feedbackProject(ctx, a, ref, store.RoleOwner)
	if err != nil {
		return PortalView{}, err
	}
	portal, err := s.DB.FeedbackPortal(ctx, p.ID, id)
	if err != nil {
		return PortalView{}, err
	}
	var serverKey string
	switch kind {
	case "public":
		portal.PublicKey = newSecret("pfk_")
	case "server":
		serverKey = newSecret("sfk_")
		portal.ServerKeyHash = hashSecret(serverKey)
	default:
		return PortalView{}, invalid("validation.feedback_key", nil, "kind must be public or server")
	}
	portal.UpdatedAt = store.Now()
	if err := s.DB.SaveFeedbackPortal(ctx, portal); err != nil {
		return PortalView{}, err
	}
	s.event(ctx, a, "feedback.key", portal.ID, map[string]string{"kind": kind})
	v := s.portalView(ctx, portal)
	v.ServerKey = serverKey
	return v, nil
}

// DeleteFeedbackPortal removes a portal with its topics and reports (owners).
func (s *Service) DeleteFeedbackPortal(ctx context.Context, a Actor, ref, id string) error {
	p, err := s.feedbackProject(ctx, a, ref, store.RoleOwner)
	if err != nil {
		return err
	}
	if err := s.DB.DeleteFeedbackPortal(ctx, p.ID, id); err != nil {
		return err
	}
	s.event(ctx, a, "feedback.portal_delete", id, nil)
	return nil
}

// SetFeedbackModerators names extra moderators by e-mail (owners).
func (s *Service) SetFeedbackModerators(ctx context.Context, a Actor, ref, id string, emails []string) (PortalView, error) {
	p, err := s.feedbackProject(ctx, a, ref, store.RoleOwner)
	if err != nil {
		return PortalView{}, err
	}
	portal, err := s.DB.FeedbackPortal(ctx, p.ID, id)
	if err != nil {
		return PortalView{}, err
	}
	ids := []string{}
	for _, e := range emails {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		u, err := s.DB.UserByEmail(ctx, e)
		if err != nil {
			return PortalView{}, invalid("validation.user_unknown", map[string]any{"email": e}, "no account with %s", e)
		}
		if !slices.Contains(ids, u.ID) {
			ids = append(ids, u.ID)
		}
	}
	if err := s.DB.SetFeedbackModerators(ctx, portal.ID, ids); err != nil {
		return PortalView{}, err
	}
	s.event(ctx, a, "feedback.moderators", portal.ID, map[string]any{"count": len(ids)})
	return s.portalView(ctx, portal), nil
}

// --- public side -------------------------------------------------------------

// PublicPortal is what the portal page and widgets need.
type PublicPortal struct {
	Handle      string        `json:"handle"`
	Name        string        `json:"name"`
	Project     string        `json:"project"`
	Intro       string        `json:"intro"`
	Privacy     string        `json:"privacy"`
	Types       []string      `json:"types"`
	Fields      []PortalField `json:"fields"`
	Attachments AttachRules   `json:"attachments"`
	Anonymous   bool          `json:"anonymous"`
	Accounts    bool          `json:"accounts"`
	AskEmail    bool          `json:"askEmail"`
	Theme       PortalTheme   `json:"theme"`
	Languages   []string      `json:"languages"`
	// Accepting is false when the portal is off or paused (storage full).
	Accepting bool `json:"accepting"`
	// FilesFull: attachments are refused until storage is freed.
	FilesFull bool `json:"filesFull"`
}

func localized(m map[string]string, lang string) string {
	if v := m[lang]; v != "" {
		return v
	}
	for _, l := range []string{"en", "de"} {
		if v := m[l]; v != "" {
			return v
		}
	}
	for _, v := range m {
		return v
	}
	return ""
}

// PublicFeedbackPortal returns a portal for its public page.
func (s *Service) PublicFeedbackPortal(ctx context.Context, handle, lang string) (PublicPortal, error) {
	portal, err := s.DB.FeedbackPortalByHandle(ctx, handle)
	if err != nil {
		return PublicPortal{}, err
	}
	ps := portalSettings(portal)
	proj, err := s.DB.ProjectFor(ctx, portal.ProjectID, "")
	if err != nil {
		return PublicPortal{}, err
	}
	full := s.feedbackFull(ctx, proj, 0)
	out := PublicPortal{Handle: portal.Handle, Name: portal.Name, Project: proj.Name, Intro: localized(ps.Intro, lang), Privacy: localized(ps.Privacy, lang),
		Types: ps.Types, Fields: ps.Fields, Attachments: ps.Attachments, Anonymous: ps.Anonymous, Accounts: ps.Accounts, AskEmail: ps.AskEmail && ps.Anonymous,
		Theme: ps.Theme, Languages: ps.Languages, Accepting: portal.Enabled && !(full && ps.StorageFull == "pause"), FilesFull: full && ps.StorageFull == "no_files"}
	if out.Privacy == "" {
		out.Privacy = defaultPrivacy(lang, portal.Name)
	}
	return out, nil
}

func defaultPrivacy(lang, name string) string {
	if lang == "de" {
		return fmt.Sprintf("Gespeichert werden deine Meldung, Anhänge und – falls angegeben – deine E-Mail-Adresse, damit das Team von %s antworten kann. Moderatoren sehen keine IP-Adressen. Abgeschlossene anonyme Meldungen werden nach einiger Zeit gelöscht; über deinen Statuslink kannst du deine Meldung jederzeit löschen oder exportieren.", name)
	}
	return fmt.Sprintf("We store your report, attachments and – if given – your e-mail address so the %s team can answer. Moderators never see IP addresses. Closed anonymous reports are deleted after a while; with your status link you can delete or export your report at any time.", name)
}

// feedbackQuotaBytes is a project's feedback storage limit (0 = none).
func (s *Service) feedbackQuotaBytes(ctx context.Context, p store.Project) int64 {
	mb := s.intSetting(ctx, settingFeedbackQuota, 1024)
	if own, err := s.DB.FeedbackQuota(ctx, p.ID); err == nil && own != nil {
		mb = *own
	}
	return int64(mb) << 20
}

const settingFeedbackQuota = "feedback.quota_mb"

func (s *Service) feedbackFull(ctx context.Context, p store.Project, add int64) bool {
	limit := s.feedbackQuotaBytes(ctx, p)
	if limit <= 0 {
		return false
	}
	used, _ := s.DB.FeedbackUsage(ctx, p.ID)
	if add == 0 {
		return used >= limit
	}
	return used+add > limit
}

// ReportInput is a report from the form, widget, API or MCP.
type ReportInput struct {
	Type    string            `json:"type"`
	Title   string            `json:"title"`
	Body    string            `json:"body"`
	Fields  map[string]any    `json:"fields,omitempty"`
	Context map[string]string `json:"context,omitempty"`
	Email   string            `json:"email,omitempty"`
	Lang    string            `json:"lang,omitempty"`
}

// ReportFile is an uploaded attachment (spooled to a temporary file).
type ReportFile struct {
	Name string
	R    io.Reader
}

// ReportReceipt answers a submitted report.
type ReportReceipt struct {
	ID string `json:"id"`
	// StatusToken opens the status link (/feedback/status/<token>); only
	// returned once.
	StatusToken  string `json:"statusToken"`
	TopicID      string `json:"-"`
	FilesDropped bool   `json:"filesDropped,omitempty"`
}

// Intake tells how a report came in.
type Intake struct {
	Via   string // form | widget | public_key | server_key | mcp
	Check string // the bot check that passed ("" when none applied)
}

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(?:bearer|basic)\s+[a-z0-9._~+/=-]{12,}`),
	regexp.MustCompile(`(?i)\b(password|passwd|pwd|secret|token|api[_-]?key|access[_-]?key|authorization)\s*[=:]\s*[^\s&"']+`),
	regexp.MustCompile(`(?i)([?&](?:token|key|secret|password|sig|signature|code)=)[^&\s"']+`),
	regexp.MustCompile(`\b(?:sk|pk|ghp|gho|xox[abp]|AKIA)[_-]?[A-Za-z0-9]{16,}\b`),
}

// scrubSecrets removes things that look like secrets from text.
func scrubSecrets(s string) string {
	for _, re := range secretPatterns {
		s = re.ReplaceAllStringFunc(s, func(m string) string {
			low := strings.ToLower(m)
			if i := strings.IndexAny(m, "=:"); i > 0 && !strings.HasPrefix(low, "bearer") && !strings.HasPrefix(low, "basic") {
				return m[:i+1] + "[removed]"
			}
			return "[removed]"
		})
	}
	return s
}

func (ps PortalSettings) checkFields(typ string, in map[string]any) (map[string]any, error) {
	out := map[string]any{}
	for _, f := range ps.Fields {
		if len(f.ReportTypes) > 0 && !slices.Contains(f.ReportTypes, typ) {
			continue
		}
		v, ok := in[f.ID]
		empty := !ok || v == nil || v == ""
		if empty {
			if f.Required {
				return nil, invalid("validation.feedback_field_required", map[string]any{"field": localized(f.Label, "en")}, "%s is required", f.ID)
			}
			continue
		}
		bad := func() error {
			return invalid("validation.feedback_field_value", map[string]any{"field": localized(f.Label, "en")}, "invalid value for %s", f.ID)
		}
		switch f.Type {
		case "number":
			if _, ok := v.(float64); !ok {
				return nil, bad()
			}
		case "bool":
			if _, ok := v.(bool); !ok {
				return nil, bad()
			}
		case "choice":
			str, _ := v.(string)
			if !slices.Contains(f.Options, str) {
				return nil, bad()
			}
		case "multi":
			list, ok := v.([]any)
			if !ok {
				return nil, bad()
			}
			for _, x := range list {
				if str, _ := x.(string); !slices.Contains(f.Options, str) {
					return nil, bad()
				}
			}
		case "url":
			str, _ := v.(string)
			if !strings.HasPrefix(str, "http://") && !strings.HasPrefix(str, "https://") {
				return nil, bad()
			}
			v = scrubSecrets(str)
		case "date":
			str, _ := v.(string)
			if _, err := time.Parse("2006-01-02", str); err != nil {
				return nil, bad()
			}
		default:
			str, ok := v.(string)
			if !ok || len(str) > 500 {
				return nil, bad()
			}
			v = scrubSecrets(str)
		}
		out[f.ID] = v
	}
	return out, nil
}

// fileKind sorts an attachment by its content (not only its ending).
func fileKind(mimeType string) string {
	switch {
	case strings.HasPrefix(mimeType, "image/"):
		return "images"
	case strings.HasPrefix(mimeType, "video/"):
		return "videos"
	case strings.HasPrefix(mimeType, "text/"), mimeType == "application/json":
		return "logs"
	}
	return "other"
}

func (a AttachRules) allows(kind string) bool {
	switch kind {
	case "images":
		return a.Images
	case "videos":
		return a.Videos
	case "logs":
		return a.Logs
	}
	return a.Other
}

// stripImage re-encodes JPEG and PNG screenshots, which drops metadata
// such as the location.
func stripImage(path, mimeType string) (string, error) {
	if mimeType != "image/jpeg" && mimeType != "image/png" {
		return path, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return path, err
	}
	img, _, err := image.Decode(f)
	f.Close()
	if err != nil {
		return path, invalid("validation.feedback_file_type", nil, "the image could not be read")
	}
	var buf bytes.Buffer
	if mimeType == "image/png" {
		err = png.Encode(&buf, img)
	} else {
		err = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90})
	}
	if err != nil {
		return path, err
	}
	out := path + ".clean"
	return out, os.WriteFile(out, buf.Bytes(), 0o600)
}

type checkedFile struct {
	name, mime, path, hash string
	size                   int64
}

// checkAttachments spools and checks a report's files against the rules.
func (s *Service) checkAttachments(ctx context.Context, ps PortalSettings, files []ReportFile) ([]checkedFile, func(), error) {
	out := []checkedFile{}
	cleanup := func() {
		for _, f := range out {
			os.Remove(f.path)
		}
	}
	rules := ps.Attachments
	if len(files) > rules.MaxFiles {
		return nil, cleanup, invalid("validation.feedback_files", map[string]any{"max": rules.MaxFiles}, "at most %d files", rules.MaxFiles)
	}
	if len(files) > 0 && s.Blobs == nil {
		return nil, cleanup, errNoBlobs
	}
	for _, f := range files {
		name := cleanName(f.Name)
		sp, err := s.spool(f.R)
		if err != nil {
			cleanup()
			return nil, func() {}, err
		}
		out = append(out, checkedFile{name: name, path: sp.path, size: sp.size})
		cf := &out[len(out)-1]
		if sp.size == 0 {
			cleanup()
			return nil, func() {}, invalid("validation.file_empty", nil, "the file is empty")
		}
		mimeType := sniffMime(name, sp.head)
		kind := fileKind(mimeType)
		ending := strings.TrimPrefix(strings.ToLower(path.Ext(name)), ".")
		if !rules.allows(kind) || !slices.Contains(rules.Endings[kind], ending) {
			cleanup()
			return nil, func() {}, invalid("validation.feedback_file_type", map[string]any{"name": name}, "%s is not accepted here", name)
		}
		if kind == "logs" && rules.TextOnly {
			data, err := os.ReadFile(sp.path)
			if err != nil || !utf8.Valid(data) {
				cleanup()
				return nil, func() {}, invalid("validation.feedback_file_text", map[string]any{"name": name}, "%s must be text", name)
			}
			// Secrets in logs are removed like in the text.
			clean := scrubSecrets(string(data))
			if clean != string(data) {
				if err := os.WriteFile(sp.path, []byte(clean), 0o600); err != nil {
					cleanup()
					return nil, func() {}, err
				}
				sp.size = int64(len(clean))
			}
		}
		if max := int64(rules.MaxMB[kind]) << 20; max > 0 && sp.size > max {
			cleanup()
			return nil, func() {}, invalid("validation.file_too_large", map[string]any{"max": max >> 20}, "the file is larger than %d MB", max>>20)
		}
		if s.Scanner != nil {
			var inf scan.ErrInfected
			if err := s.Scanner.Scan(ctx, sp.path); errors.As(err, &inf) {
				cleanup()
				return nil, func() {}, invalid("validation.file_infected", map[string]any{"signature": inf.Signature}, "the file contains malware (%s)", inf.Signature)
			} else if err != nil {
				cleanup()
				return nil, func() {}, invalid("validation.file_scan_failed", nil, "the file could not be checked for malware")
			}
		}
		clean, err := stripImage(sp.path, mimeType)
		if err != nil {
			cleanup()
			return nil, func() {}, err
		}
		if clean != sp.path {
			os.Remove(sp.path)
			cf.path = clean
		}
		hs, err := hashFile(cf.path)
		if err != nil {
			cleanup()
			return nil, func() {}, err
		}
		cf.hash, cf.size, cf.mime = hs.hash, hs.size, mimeType
	}
	return out, cleanup, nil
}

// SubmitFeedback stores a report. a is nil for anonymous reporters.
func (s *Service) SubmitFeedback(ctx context.Context, a *Actor, handle string, in ReportInput, files []ReportFile, intake Intake) (ReportReceipt, error) {
	portal, err := s.DB.FeedbackPortalByHandle(ctx, handle)
	if err != nil {
		return ReportReceipt{}, err
	}
	ps := portalSettings(portal)
	proj, err := s.DB.ProjectFor(ctx, portal.ProjectID, "")
	if err != nil {
		return ReportReceipt{}, err
	}
	if !portal.Enabled {
		return ReportReceipt{}, invalid("validation.feedback_closed", nil, "this portal does not take reports right now")
	}
	switch {
	case intake.Via == "server_key", intake.Via == "public_key", intake.Via == "mcp":
	case a != nil && ps.Accounts:
	case a == nil && ps.Anonymous:
	case a != nil && ps.Anonymous:
	default:
		return ReportReceipt{}, invalid("validation.feedback_signin", nil, "sign in to send a report")
	}
	typ := in.Type
	if typ == "" {
		typ = ps.Types[0]
	}
	if !slices.Contains(ps.Types, typ) {
		return ReportReceipt{}, invalid("validation.feedback_type", map[string]any{"type": typ}, "unknown report type %q", typ)
	}
	title := strings.TrimSpace(scrubSecrets(in.Title))
	body := strings.TrimSpace(scrubSecrets(in.Body))
	if title == "" {
		return ReportReceipt{}, invalid("validation.title_required", nil, "title is required")
	}
	if utf8.RuneCountInString(title) > 200 || utf8.RuneCountInString(body) > 20_000 {
		return ReportReceipt{}, invalid("validation.feedback_too_long", nil, "the report is too long")
	}
	fields, err := ps.checkFields(typ, in.Fields)
	if err != nil {
		return ReportReceipt{}, err
	}
	ctxFields := map[string]string{}
	for k, v := range in.Context {
		if len(ctxFields) < 20 && len(k) <= 40 {
			ctxFields[k] = excerpt(scrubSecrets(v), 500)
		}
	}
	email := strings.TrimSpace(in.Email)
	if email != "" && (!strings.Contains(email, "@") || len(email) > 254 || !ps.AskEmail) {
		email = ""
	}
	if a != nil {
		email = ""
	}
	receipt := ReportReceipt{}
	// Attachments: the portal's rules, then the feedback storage quota.
	checked, cleanup, err := s.checkAttachments(ctx, ps, files)
	defer cleanup()
	if err != nil {
		return ReportReceipt{}, err
	}
	var add int64
	for _, f := range checked {
		add += f.size
	}
	if add > 0 && s.feedbackFull(ctx, proj, add) {
		switch ps.StorageFull {
		case "pause":
			return ReportReceipt{}, invalid("validation.feedback_paused", nil, "the portal is paused until storage is freed")
		case "drop_oldest":
			s.freeFeedbackStorage(ctx, proj, add)
			if s.feedbackFull(ctx, proj, add) {
				return ReportReceipt{}, invalid("validation.feedback_storage_full", nil, "attachments are full – send without files")
			}
		default:
			return ReportReceipt{}, invalid("validation.feedback_storage_full", nil, "attachments are full – send without files")
		}
	}
	token := newSecret("")
	r := store.FeedbackReport{ID: newID(), ProjectID: proj.ID, PortalID: portal.ID, Type: typ, Title: excerpt(title, 200), Body: body, Fields: fields,
		Context: ctxFields, Lang: excerpt(in.Lang, 10), Email: email, StatusHash: hashSecret(token), Via: intake.Via, CheckKind: intake.Check, CreatedAt: store.Now()}
	actor := Actor{User: store.User{ID: "anonymous:" + portal.Handle, Name: "anonymous"}, Via: intake.Via}
	if a != nil {
		r.UserID = a.User.ID
		actor = *a
	}
	topic, isNew, err := s.topicFor(ctx, portal, ps, r)
	if err != nil {
		return ReportReceipt{}, err
	}
	r.TopicID = topic.ID
	if err := s.DB.AddFeedbackReport(ctx, r); err != nil {
		return ReportReceipt{}, err
	}
	for _, f := range checked {
		if err := s.Blobs.Put(ctx, f.hash, f.path, f.size); err != nil {
			return ReportReceipt{}, err
		}
		if err := s.DB.AddFeedbackFile(ctx, proj.ID, store.FeedbackFile{ID: newID(), ReportID: r.ID, Name: f.name, Mime: f.mime, Size: f.size, SHA256: f.hash, CreatedAt: store.Now()}); err != nil {
			return ReportReceipt{}, err
		}
	}
	_ = s.DB.RecountTopic(ctx, topic.ID)
	s.event(ctx, actor, "feedback.report", r.ID, map[string]any{"portal": portal.Handle, "topic": topic.ID, "via": intake.Via, "files": len(checked)})
	// The AI sorts the report on the project's background budget.
	if ps.AITriage {
		tctx := context.WithoutCancel(ctx)
		if s.SyncNotify {
			s.triageReport(tctx, proj, portal, ps, topic.ID, isNew, r)
		} else {
			go s.triageReport(tctx, proj, portal, ps, topic.ID, isNew, r)
		}
	}
	if isNew {
		s.notifyModerators(ctx, portal, Note{Kind: "feedback.topic", ProjectID: proj.ID, Project: proj.Name, Subject: topic.ID, Title: topic.Title,
			Body: excerpt(body, 300), Link: "/p/" + proj.Slug + "/feedback?topic=" + topic.ID})
	}
	receipt.ID, receipt.StatusToken, receipt.TopicID = r.ID, token, topic.ID
	if email != "" {
		s.mailFeedbackReceipt(ctx, email, in.Lang, portal, token)
	}
	return receipt, nil
}

var wordRe = regexp.MustCompile(`[\p{L}\p{N}]+`)

var stopWords = map[string]bool{
	"the": true, "and": true, "when": true, "with": true, "for": true, "not": true, "this": true, "that": true, "from": true, "after": true, "does": true, "doesn": true, "can": true, "into": true,
	"der": true, "die": true, "das": true, "und": true, "wenn": true, "mit": true, "nicht": true, "ein": true, "eine": true, "beim": true, "bei": true, "nach": true, "ist": true, "auf": true, "von": true, "den": true, "dem": true,
}

// stem cuts common English and German endings so "crashes" and "crash"
// count as the same word.
func stem(w string) string {
	for _, suf := range []string{"ing", "ed", "es", "s", "en", "er", "e"} {
		if strings.HasSuffix(w, suf) && utf8.RuneCountInString(w)-len(suf) >= 3 {
			return strings.TrimSuffix(w, suf)
		}
	}
	return w
}

func words(s string) map[string]bool {
	out := map[string]bool{}
	for _, w := range wordRe.FindAllString(strings.ToLower(s), -1) {
		if utf8.RuneCountInString(w) > 2 && !stopWords[w] {
			out[stem(w)] = true
		}
	}
	return out
}

// similarity is the Jaccard index of two texts' words.
func similarity(a, b string) float64 {
	wa, wb := words(a), words(b)
	if len(wa) == 0 || len(wb) == 0 {
		return 0
	}
	inter := 0
	for w := range wa {
		if wb[w] {
			inter++
		}
	}
	return float64(inter) / float64(len(wa)+len(wb)-inter)
}

// topicFor finds an open topic that a report duplicates (by title words),
// or creates a new one. This is the rule-based triage; AI triage comes on
// top of it.
func (s *Service) topicFor(ctx context.Context, portal store.FeedbackPortal, ps PortalSettings, r store.FeedbackReport) (store.FeedbackTopic, bool, error) {
	open, err := s.DB.OpenFeedbackTopics(ctx, portal.ID, 500)
	if err != nil {
		return store.FeedbackTopic{}, false, err
	}
	best, score := -1, 0.0
	for i, t := range open {
		if t.Type != r.Type {
			continue
		}
		if sc := similarity(t.Title, r.Title); sc > score {
			best, score = i, sc
		}
	}
	if best >= 0 && score >= 0.5 {
		t := open[best]
		t.UpdatedAt = store.Now()
		return t, false, s.DB.SaveFeedbackTopic(ctx, t)
	}
	prio := 3
	switch r.Type {
	case "bug":
		prio = 2
	case "praise":
		prio = 4
	}
	now := store.Now()
	t := store.FeedbackTopic{ID: newID(), ProjectID: portal.ProjectID, PortalID: portal.ID, Title: r.Title, Summary: excerpt(r.Body, 600), Type: r.Type,
		Tags: []string{}, Priority: prio, State: ps.DefaultState, CreatedAt: now, UpdatedAt: now}
	return t, true, s.DB.SaveFeedbackTopic(ctx, t)
}

// freeFeedbackStorage drops attachments of the oldest closed reports.
func (s *Service) freeFeedbackStorage(ctx context.Context, p store.Project, need int64) {
	ids, err := s.DB.OldestClosedWithFiles(ctx, p.ID, 200)
	if err != nil {
		return
	}
	for _, id := range ids {
		if !s.feedbackFull(ctx, p, need) {
			return
		}
		hashes, err := s.DB.DeleteFeedbackFiles(ctx, id)
		if err == nil {
			s.freeBlobs(ctx, hashes)
		}
	}
}

func (s *Service) notifyModerators(ctx context.Context, portal store.FeedbackPortal, n Note) {
	seen := map[string]bool{}
	send := func(id string) {
		if seen[id] {
			return
		}
		seen[id] = true
		if u, err := s.DB.UserByID(ctx, id); err == nil {
			s.notify(ctx, u, n)
		}
	}
	if ms, err := s.DB.ListMembers(ctx, portal.ProjectID); err == nil {
		for _, m := range ms {
			if m.Role == store.RoleOwner || m.Role == store.RoleEditor {
				send(m.UserID)
			}
		}
	}
	if mods, err := s.DB.FeedbackModerators(ctx, portal.ID); err == nil {
		for _, m := range mods {
			send(m.UserID)
		}
	}
}

func (s *Service) mailFeedbackReceipt(ctx context.Context, email, lang string, portal store.FeedbackPortal, token string) {
	if s.Mail == nil || !s.Mail.Enabled() || s.PublicURL == "" {
		return
	}
	if lang != "de" && lang != "en" {
		lang = s.DefaultLocale
	}
	subject, body, err := renderMail("feedback", lang, map[string]any{"Portal": portal.Name, "Link": s.PublicURL + "/feedback/status/" + token})
	if err != nil {
		return
	}
	if err := s.Mail.Send(ctx, mail.Message{To: email, Subject: subject, Body: body}); err != nil {
		slog.Warn("feedback receipt mail", "err", err)
	}
}

// ReportStatus is what the reporter sees with the status link.
type ReportStatus struct {
	Portal    string     `json:"portal"`
	Handle    string     `json:"handle"`
	Type      string     `json:"type"`
	Title     string     `json:"title"`
	Body      string     `json:"body"`
	CreatedAt store.Time `json:"createdAt"`
	State     string     `json:"state"`
	Finished  bool       `json:"finished"`
	Answer    string     `json:"answer,omitempty"`
	Reports   int        `json:"reports"`
	Deleted   bool       `json:"deleted"`
	Files     int        `json:"files"`
}

func (s *Service) reportByToken(ctx context.Context, token string) (store.FeedbackReport, store.FeedbackPortal, error) {
	r, err := s.DB.FeedbackReportByStatus(ctx, hashSecret(strings.TrimSpace(token)))
	if err != nil {
		return r, store.FeedbackPortal{}, err
	}
	portal, err := s.DB.FeedbackPortal(ctx, "", r.PortalID)
	return r, portal, err
}

// FeedbackStatus reads a report by its status link.
func (s *Service) FeedbackStatus(ctx context.Context, token string) (ReportStatus, error) {
	r, portal, err := s.reportByToken(ctx, token)
	if err != nil {
		return ReportStatus{}, err
	}
	ps := portalSettings(portal)
	out := ReportStatus{Portal: portal.Name, Handle: portal.Handle, Type: r.Type, Title: r.Title, Body: r.Body, CreatedAt: r.CreatedAt, Deleted: r.Deleted}
	if r.TopicID != "" {
		if t, err := s.DB.FeedbackTopic(ctx, "", r.TopicID); err == nil {
			out.State, out.Answer, out.Reports, out.Finished = t.State, t.Answer, t.ReportCount, t.State == ps.FinishedState
		}
	}
	files, _ := s.DB.FeedbackFiles(ctx, []string{r.ID})
	out.Files = len(files[r.ID])
	return out, nil
}

// DeleteFeedbackByToken removes the reporter's data from a report: the
// attachments, the e-mail address and the context. The text stays,
// anonymized, so the topic's count stays right.
func (s *Service) DeleteFeedbackByToken(ctx context.Context, token string) error {
	r, portal, err := s.reportByToken(ctx, token)
	if err != nil {
		return err
	}
	hashes, err := s.DB.DeleteFeedbackFiles(ctx, r.ID)
	if err != nil {
		return err
	}
	s.freeBlobs(ctx, hashes)
	if err := s.DB.AnonymizeFeedbackReport(ctx, r.ID); err != nil {
		return err
	}
	s.event(ctx, Actor{User: store.User{ID: "anonymous:" + portal.Handle}, Via: "status"}, "feedback.report_delete", r.ID, nil)
	return nil
}

// FeedbackExport is everything stored about a report (for the reporter).
type FeedbackExport struct {
	Report store.FeedbackReport `json:"report"`
	Status ReportStatus         `json:"status"`
}

// ExportFeedbackByToken returns what is stored about a report.
func (s *Service) ExportFeedbackByToken(ctx context.Context, token string) (FeedbackExport, error) {
	r, _, err := s.reportByToken(ctx, token)
	if err != nil {
		return FeedbackExport{}, err
	}
	st, err := s.FeedbackStatus(ctx, token)
	if err != nil {
		return FeedbackExport{}, err
	}
	files, _ := s.DB.FeedbackFiles(ctx, []string{r.ID})
	r.Attachments = files[r.ID]
	r.TopicID = ""
	return FeedbackExport{Report: r, Status: st}, nil
}

// MyFeedback lists the signed-in person's reports with their state.
func (s *Service) MyFeedback(ctx context.Context, a Actor) ([]ReportStatus, error) {
	if err := s.fullUser(a); err != nil {
		return nil, err
	}
	rs, err := s.DB.FeedbackReportsOf(ctx, a.User.ID)
	if err != nil {
		return nil, err
	}
	out := []ReportStatus{}
	for _, r := range rs {
		portal, err := s.DB.FeedbackPortal(ctx, "", r.PortalID)
		if err != nil {
			continue
		}
		st := ReportStatus{Portal: portal.Name, Handle: portal.Handle, Type: r.Type, Title: r.Title, Body: r.Body, CreatedAt: r.CreatedAt}
		if t, err := s.DB.FeedbackTopic(ctx, "", r.TopicID); err == nil {
			st.State, st.Answer, st.Reports, st.Finished = t.State, t.Answer, t.ReportCount, t.State == portalSettings(portal).FinishedState
		}
		out = append(out, st)
	}
	return out, nil
}

// PortalByKey checks an intake key: a public key (still needs the bot
// check) or a server key (trusted backends).
func (s *Service) PortalByKey(ctx context.Context, handle, key string) (store.FeedbackPortal, string, error) {
	portal, err := s.DB.FeedbackPortalByHandle(ctx, handle)
	if err != nil {
		return portal, "", err
	}
	ps := portalSettings(portal)
	switch {
	case strings.HasPrefix(key, "pfk_") && ps.PublicKeyOn && portal.PublicKey != "" && key == portal.PublicKey:
		return portal, "public_key", nil
	case strings.HasPrefix(key, "sfk_") && ps.ServerKeyOn && portal.ServerKeyHash != "" && hashSecret(key) == portal.ServerKeyHash:
		return portal, "server_key", nil
	}
	return portal, "", ErrForbidden
}

// SpooledUpload is an upload saved to a temporary file.
type SpooledUpload struct {
	*os.File
}

// Remove closes and deletes the temporary file.
func (u *SpooledUpload) Remove() {
	u.Close()
	os.Remove(u.Name())
}

// SpoolUpload saves an upload stream to a temporary file (up to the
// instance's upload limit) so later parts of a form can be read.
func (s *Service) SpoolUpload(r io.Reader) (*SpooledUpload, error) {
	sp, err := s.spool(r)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(sp.path)
	if err != nil {
		os.Remove(sp.path)
		return nil, err
	}
	return &SpooledUpload{File: f}, nil
}
