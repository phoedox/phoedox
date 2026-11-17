package core

import (
	"context"
	"encoding/json"
	"slices"
	"time"

	"github.com/phoedox/phoedox/internal/store"
)

// Public apps (apps-definition "Sharing"): an owner releases an app to
// people without an account. Visitors get level 0, or – in snapshot mode –
// level 1 answered from a snapshot of the app's sources, taken with the
// publisher's rights by hand or on a schedule. Nothing is read live and
// nothing is written; storage, writes and opening pages are refused. AI
// calls run on the publisher's budget within a monthly limit and behind
// the bot check of the "apps" place. Admins of the instance or the
// organization can forbid public apps (AppPolicy.NoPublic).

const (
	appShareStatic   = "static"
	appShareSnapshot = "snapshot"
	// maxAppSnapshot limits the stored data of one public app.
	maxAppSnapshot = 2 << 20
	maxAppAILimit  = 10_000
)

var appShareSchedules = map[string]time.Duration{"manual": 0, "hourly": time.Hour, "daily": 24 * time.Hour, "weekly": 7 * 24 * time.Hour}

// AppShareInput is what the owner chooses when publishing.
type AppShareInput struct {
	Mode     string `json:"mode"`     // static | snapshot
	Schedule string `json:"schedule"` // manual | hourly | daily | weekly
	AILimit  int    `json:"aiLimit"`  // AI calls per month, 0 = none
	// Confirm: the owner saw the warning (the app's script runs for anyone,
	// the snapshot becomes public).
	Confirm bool `json:"confirm"`
}

// AppShareView is the sharing state an owner sees.
type AppShareView struct {
	Shared bool            `json:"shared"`
	Share  *store.AppShare `json:"share,omitempty"`
	URL    string          `json:"url,omitempty"`
	// Allowed is false when the instance or organization forbids public apps.
	Allowed bool        `json:"allowed"`
	CanEdit bool        `json:"canEdit"`
	Level   int         `json:"level"`
	Sources []AppSource `json:"sources"`
	AI      bool        `json:"ai"`
	// Preview is what a snapshot would hold now (on request).
	Preview map[string]any `json:"preview,omitempty"`
	// PreviewErrors are sources that could not be read.
	PreviewErrors map[string]string `json:"previewErrors,omitempty"`
}

func (s *Service) appShareURL(token string) string { return s.PublicURL + "/shared/app/" + token }

// shareApp loads an app for its owner's sharing settings.
func (s *Service) shareApp(ctx context.Context, a Actor, ref, docRef string, need string) (store.Project, store.Document, App, AppPolicy, error) {
	p, doc, err := s.docAccess(ctx, a, ref, docRef, need)
	if err != nil {
		return p, doc, App{}, AppPolicy{}, err
	}
	if doc.Type != store.DocApp {
		return p, doc, App{}, AppPolicy{}, ErrNotFound
	}
	pol := s.effectiveAppPolicy(ctx, p)
	if pol.Off {
		return p, doc, App{}, pol, errAppsOff()
	}
	return p, doc, appOf(doc), pol, nil
}

// GetAppShare shows whether an app is public; preview adds what a snapshot
// would hold now.
func (s *Service) GetAppShare(ctx context.Context, a Actor, ref, docRef string, preview bool) (AppShareView, error) {
	p, doc, app, pol, err := s.shareApp(ctx, a, ref, docRef, store.RoleReader)
	if err != nil {
		return AppShareView{}, err
	}
	v := AppShareView{Allowed: !pol.NoPublic, CanEdit: p.Role == store.RoleOwner && s.fullUser(a) == nil, Level: pol.capLevel(app.Level), Sources: nonNil(app.Sources), AI: app.AI && !pol.NoAI}
	if sh, err := s.DB.AppShare(ctx, doc.ID); err == nil {
		v.Shared, v.Share, v.URL = true, &sh, s.appShareURL(sh.Token)
	}
	if preview && v.CanEdit && v.Level >= 1 {
		v.Preview, v.PreviewErrors = s.takeSnapshot(ctx, a, p, app)
	}
	return v, nil
}

// takeSnapshot reads every source with the actor's rights.
func (s *Service) takeSnapshot(ctx context.Context, a Actor, p store.Project, app App) (map[string]any, map[string]string) {
	data, errs := map[string]any{}, map[string]string{}
	for _, src := range app.Sources {
		v, err := s.readSource(ctx, a, p, src)
		if err != nil {
			errs[src.ID] = err.Error()
			continue
		}
		data[src.ID] = v
	}
	if len(errs) == 0 {
		errs = nil
	}
	return data, errs
}

func encodeSnapshot(data map[string]any) (string, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return "", err
	}
	if len(raw) > maxAppSnapshot {
		return "", invalid("validation.app_snapshot_size", map[string]any{"mb": maxAppSnapshot >> 20}, "the snapshot is larger than %d MB", maxAppSnapshot>>20)
	}
	return string(raw), nil
}

// PublishApp releases an app to people without an account (project
// owners), or changes the release. It takes a fresh snapshot.
func (s *Service) PublishApp(ctx context.Context, a Actor, ref, docRef string, in AppShareInput) (AppShareView, error) {
	if err := s.fullUser(a); err != nil {
		return AppShareView{}, err
	}
	p, doc, app, pol, err := s.shareApp(ctx, a, ref, docRef, store.RoleReader)
	if err != nil {
		return AppShareView{}, err
	}
	if p.Role != store.RoleOwner {
		return AppShareView{}, ErrForbidden
	}
	if pol.NoPublic {
		return AppShareView{}, invalid("validation.app_public_off", nil, "public apps are switched off here")
	}
	if !in.Confirm {
		return AppShareView{}, invalid("validation.app_public_confirm", nil, "confirm that the app and its snapshot become public")
	}
	if in.Mode != appShareSnapshot {
		in.Mode = appShareStatic
	}
	if _, ok := appShareSchedules[in.Schedule]; !ok || in.Mode == appShareStatic {
		in.Schedule = "manual"
	}
	if in.AILimit < 0 || in.AILimit > maxAppAILimit {
		return AppShareView{}, invalid("validation.app_ai_limit", map[string]any{"max": maxAppAILimit}, "the AI limit is 0 to %d calls a month", maxAppAILimit)
	}
	if !app.AI || pol.NoAI {
		in.AILimit = 0
	}
	snap := "{}"
	if in.Mode == appShareSnapshot && pol.capLevel(app.Level) >= 1 {
		data, errs := s.takeSnapshot(ctx, a, p, app)
		for id := range errs {
			return AppShareView{}, invalid("validation.app_snapshot_source", map[string]any{"id": id}, "the source %q cannot be read", id)
		}
		if snap, err = encodeSnapshot(data); err != nil {
			return AppShareView{}, err
		}
	}
	now := store.Now()
	sh, err := s.DB.AppShare(ctx, doc.ID)
	if err != nil {
		sh = store.AppShare{DocumentID: doc.ID, ProjectID: p.ID, Token: randomToken("")}
	}
	sh.Mode, sh.Schedule, sh.AILimit = in.Mode, in.Schedule, in.AILimit
	sh.Snapshot, sh.SnapshotAt, sh.SnapshotError = snap, now, ""
	sh.PublishedBy, sh.PublishedAt = a.User.ID, now
	if err := s.DB.SaveAppShare(ctx, sh); err != nil {
		return AppShareView{}, err
	}
	s.event(ctx, a, "app.publish", doc.ID, map[string]any{"mode": in.Mode, "schedule": in.Schedule, "aiLimit": in.AILimit})
	return s.GetAppShare(ctx, a, p.ID, doc.ID, false)
}

// RefreshAppShare takes a new snapshot of a public app now (owners).
func (s *Service) RefreshAppShare(ctx context.Context, a Actor, ref, docRef string) (AppShareView, error) {
	if err := s.fullUser(a); err != nil {
		return AppShareView{}, err
	}
	p, doc, app, pol, err := s.shareApp(ctx, a, ref, docRef, store.RoleReader)
	if err != nil {
		return AppShareView{}, err
	}
	if p.Role != store.RoleOwner {
		return AppShareView{}, ErrForbidden
	}
	sh, err := s.DB.AppShare(ctx, doc.ID)
	if err != nil {
		return AppShareView{}, err
	}
	if sh.Mode == appShareSnapshot && pol.capLevel(app.Level) >= 1 {
		data, errs := s.takeSnapshot(ctx, a, p, app)
		for id := range errs {
			return AppShareView{}, invalid("validation.app_snapshot_source", map[string]any{"id": id}, "the source %q cannot be read", id)
		}
		snap, err := encodeSnapshot(data)
		if err != nil {
			return AppShareView{}, err
		}
		if err := s.DB.SetAppShareSnapshot(ctx, doc.ID, snap, store.Now(), ""); err != nil {
			return AppShareView{}, err
		}
	}
	s.event(ctx, a, "app.snapshot", doc.ID, nil)
	return s.GetAppShare(ctx, a, p.ID, doc.ID, false)
}

// UnpublishApp withdraws the public release (owners).
func (s *Service) UnpublishApp(ctx context.Context, a Actor, ref, docRef string) error {
	p, doc, err := s.docAccess(ctx, a, ref, docRef, store.RoleReader)
	if err != nil {
		return err
	}
	if p.Role != store.RoleOwner || s.fullUser(a) != nil {
		return ErrForbidden
	}
	if err := s.DB.DeleteAppShare(ctx, doc.ID); err != nil {
		return err
	}
	s.event(ctx, a, "app.unpublish", doc.ID, nil)
	return nil
}

// RunAppShares refreshes the snapshots of public apps on a schedule, with
// the rights of the person who published them. A failed refresh keeps the
// old data and records the error.
func (s *Service) RunAppShares(ctx context.Context, now time.Time) {
	shares, err := s.DB.AppSharesDue(store.System(ctx))
	if err != nil {
		return
	}
	for _, sh := range shares {
		every := appShareSchedules[sh.Schedule]
		if every == 0 || now.Sub(sh.SnapshotAt.Time) < every {
			continue
		}
		tctx := store.WithTenant(ctx, sh.TenantID)
		u, err := s.DB.UserByID(tctx, sh.PublishedBy)
		if err != nil {
			_ = s.DB.SetAppShareSnapshot(tctx, sh.DocumentID, "", store.Time{}, "publisher_gone")
			continue
		}
		a := Actor{User: u, Via: "web"}
		p, doc, app, pol, err := s.shareApp(tctx, a, sh.ProjectID, sh.DocumentID, store.RoleReader)
		if err != nil || p.Role != store.RoleOwner || pol.NoPublic {
			_ = s.DB.SetAppShareSnapshot(tctx, sh.DocumentID, "", store.Time{}, "not_allowed")
			continue
		}
		data, errs := s.takeSnapshot(tctx, a, p, app)
		snap, err := encodeSnapshot(data)
		switch {
		case len(errs) > 0:
			_ = s.DB.SetAppShareSnapshot(tctx, doc.ID, "", store.Time{}, "source_failed")
		case err != nil:
			_ = s.DB.SetAppShareSnapshot(tctx, doc.ID, "", store.Time{}, "too_large")
		default:
			_ = s.DB.SetAppShareSnapshot(tctx, doc.ID, snap, store.Time{Time: now}, "")
		}
	}
}

// --- visitors --------------------------------------------------------------

// SharedApp is what the public page of an app needs.
type SharedApp struct {
	Title      string      `json:"title"`
	Project    string      `json:"project"`
	URL        string      `json:"url"`
	Isolated   bool        `json:"isolated"`
	Level      int         `json:"level"` // 0 or 1
	Sources    []AppSource `json:"sources"`
	SnapshotAt *time.Time  `json:"snapshotAt,omitempty"`
	AI         bool        `json:"ai"`
	Locale     string      `json:"locale"`
}

// sharedApp resolves a public address to its release, project and app.
func (s *Service) sharedApp(ctx context.Context, token string) (context.Context, store.AppShare, store.Project, store.Document, App, AppPolicy, error) {
	fail := func() (context.Context, store.AppShare, store.Project, store.Document, App, AppPolicy, error) {
		return ctx, store.AppShare{}, store.Project{}, store.Document{}, App{}, AppPolicy{}, ErrNotFound
	}
	if token == "" || len(token) > 100 {
		return fail()
	}
	sh, err := s.DB.AppShareByToken(store.System(ctx), token)
	if err != nil {
		return fail()
	}
	ctx = store.WithTenant(ctx, sh.TenantID)
	p, err := s.DB.ProjectFor(ctx, sh.ProjectID, "")
	if err != nil {
		return fail()
	}
	doc, err := s.DB.GetDocument(ctx, p.ID, sh.DocumentID)
	if err != nil || doc.Type != store.DocApp {
		return fail()
	}
	pol := s.effectiveAppPolicy(ctx, p)
	if pol.Off || pol.NoPublic {
		return fail()
	}
	return ctx, sh, p, doc, appOf(doc), pol, nil
}

// OpenSharedApp gives a visitor the frame of a public app.
func (s *Service) OpenSharedApp(ctx context.Context, token string) (SharedApp, error) {
	_, sh, p, doc, app, pol, err := s.sharedApp(ctx, token)
	if err != nil {
		return SharedApp{}, err
	}
	tok, err := s.sealFrame(appFrameClaims{Tenant: sh.TenantID, Project: p.ID, Doc: doc.ID, Share: sh.Token, Exp: time.Now().Add(appFrameTTL).Unix()})
	if err != nil {
		return SharedApp{}, err
	}
	url, isolated := s.frameURL("s"+doc.ID, tok)
	out := SharedApp{Title: doc.Title, Project: p.Name, URL: url, Isolated: isolated, Sources: []AppSource{}, AI: sh.AILimit > 0 && app.AI && !pol.NoAI, Locale: p.MainLocale}
	if sh.Mode == appShareSnapshot && pol.capLevel(app.Level) >= 1 {
		out.Level = 1
		for _, src := range app.Sources {
			out.Sources = append(out.Sources, AppSource{ID: src.ID, Kind: src.Kind, Label: src.Label})
		}
		at := sh.SnapshotAt.Time
		out.SnapshotAt = &at
	}
	return out, nil
}

// ReadSharedApp answers a visitor's read from the snapshot.
func (s *Service) ReadSharedApp(ctx context.Context, token, sourceID string) (json.RawMessage, error) {
	_, sh, _, _, app, pol, err := s.sharedApp(ctx, token)
	if err != nil {
		return nil, err
	}
	if sh.Mode != appShareSnapshot || pol.capLevel(app.Level) < 1 {
		return nil, needLevel(0, 1)
	}
	if !slices.ContainsFunc(app.Sources, func(x AppSource) bool { return x.ID == sourceID }) {
		return nil, invalid("validation.app_source_unknown", map[string]any{"id": sourceID}, "the app has no source %q", sourceID)
	}
	var data map[string]json.RawMessage
	if json.Unmarshal([]byte(sh.Snapshot), &data) != nil {
		return nil, ErrNotFound
	}
	v, ok := data[sourceID]
	if !ok {
		return json.RawMessage("null"), nil
	}
	return v, nil
}

// AskSharedApp answers a visitor's AI call on the publisher's budget,
// within the monthly limit of the release (the server checks bots first).
func (s *Service) AskSharedApp(ctx context.Context, token, prompt string, wantJSON bool, now time.Time) (AppAIAnswer, error) {
	ctx, sh, p, doc, app, pol, err := s.sharedApp(ctx, token)
	if err != nil {
		return AppAIAnswer{}, err
	}
	if !app.AI || pol.NoAI || sh.AILimit <= 0 {
		return AppAIAnswer{}, invalid("validation.app_ai_off", nil, "this app does not use the AI")
	}
	u, err := s.DB.UserByID(ctx, sh.PublishedBy)
	if err != nil {
		return AppAIAnswer{}, invalid("validation.app_ai_off", nil, "this app does not use the AI")
	}
	ok, err := s.DB.UseAppShareAI(ctx, doc.ID, now.UTC().Format("2006-01"))
	if err != nil {
		return AppAIAnswer{}, err
	}
	if !ok {
		return AppAIAnswer{}, invalid("validation.app_ai_limit_reached", nil, "the AI limit of this app is used up for this month")
	}
	return s.askAppAI(ctx, Actor{User: u, Via: "web"}, p.ID, doc.ID, prompt, wantJSON)
}
