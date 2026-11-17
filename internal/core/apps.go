package core

import (
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"path"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/evanw/esbuild/pkg/api"

	"github.com/phoedox/phoedox/internal/ai"
	"github.com/phoedox/phoedox/internal/store"
	"github.com/phoedox/phoedox/internal/usercontent"
	"github.com/phoedox/phoedox/resources"
)

// Apps (apps-definition): a small web program that lives in a project as a
// document. Its files are HTML, CSS and JavaScript or JSX/TypeScript, which
// Phoedox compiles on save. It runs in an isolated frame (own origin, no
// cookies, no network) and reaches project data only through the bridge,
// level by level as the person allows:
//
//	0 none · 1 read sources of its manifest · 2 own storage · 3 propose writes

// App is the content of an app document.
type App struct {
	Files map[string]string `json:"files"`
	// Level is the highest bridge level the app asks for (0–3).
	Level int `json:"level"`
	// Sources are what the app may read at level 1, with the viewer's rights.
	Sources []AppSource `json:"sources,omitempty"`
	// StorageMB limits the app's own storage (level 2), default 10.
	StorageMB int `json:"storageMb,omitempty"`
	// AI lets the app ask the AI on the viewer's budget.
	AI bool `json:"ai,omitempty"`
	// Build is the compiled result, made on save.
	Build *AppBuild `json:"build,omitempty"`
	// Origin is the store template the app was made from.
	Origin *AppOrigin `json:"origin,omitempty"`
	// Devices the app may ask for: camera, microphone, location, screen,
	// motion. People allow each one separately (appdata.go).
	Devices []string `json:"devices,omitempty"`
	// SelfEdit lets the app save its own files (people who may edit it,
	// after they confirm), e.g. a document edited in place.
	SelfEdit bool `json:"selfEdit,omitempty"`
}

// AppSource is something an app may read.
type AppSource struct {
	ID    string `json:"id"`              // what the app calls it, e.g. "servers"
	Kind  string `json:"kind"`            // query | board | checks | entities | request | readings
	Ref   string `json:"ref,omitempty"`   // saved query, board, kind, page#n (a request block) or entry[:metric]
	Label string `json:"label,omitempty"` // shown in the permission dialog
}

// AppBuild is the compiled app.
type AppBuild struct {
	// Files are compiled scripts by their source name (app.tsx → JS).
	Files  map[string]string `json:"files,omitempty"`
	Errors []AppDiag         `json:"errors"`
}

// AppDiag is a compile error.
type AppDiag struct {
	File   string `json:"file"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
	Text   string `json:"text"`
}

const (
	maxAppFiles   = 40
	maxAppBytes   = 1 << 20
	maxAppSources = 20
)

var (
	appFileRe = regexp.MustCompile(`^[A-Za-z0-9_-][A-Za-z0-9_.-]{0,60}$`)
	appExts   = []string{".html", ".css", ".js", ".mjs", ".ts", ".tsx", ".jsx", ".json", ".svg", ".md", ".txt"}
	appIDRe   = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,40}$`)
	appKinds  = []string{"query", "board", "checks", "entities", "request", "readings"}
)

//go:embed appbridge.js
var appBridgeJS string

// AppLibs are the libraries apps import by name; Phoedox serves them from
// /applib/ (version-pinned, see web/scripts/applibs.ts).
var AppLibs = map[string]string{
	"react":             "react.js",
	"react/jsx-runtime": "react-jsx-runtime.js",
	"react-dom/client":  "react-dom-client.js",
	"lit":               "lit.js",
	"chart.js":          "chart.js",
	"d3":                "d3.js",
}

func compiled(name string) bool {
	switch path.Ext(name) {
	case ".ts", ".tsx", ".jsx":
		return true
	}
	return false
}

// appEntry is the file the frame opens: index.html, or a page Phoedox
// makes around the main script.
func appEntry(files map[string]string) (string, string) {
	if _, ok := files["index.html"]; ok {
		return "index.html", ""
	}
	for _, n := range []string{"app.tsx", "app.jsx", "app.ts", "app.js", "main.tsx", "main.jsx", "main.ts", "main.js"} {
		if _, ok := files[n]; ok {
			return "", n
		}
	}
	return "", ""
}

func appContent(app *App) (store.JSON, string, error) {
	if app == nil {
		app = &App{}
	}
	if app.Files == nil || len(app.Files) == 0 {
		app.Files = map[string]string{"index.html": defaultAppHTML}
	}
	if len(app.Files) > maxAppFiles {
		return nil, "", invalid("validation.app_files", map[string]any{"max": maxAppFiles}, "an app has at most %d files", maxAppFiles)
	}
	total := 0
	for name, src := range app.Files {
		if !appFileRe.MatchString(name) || !slices.Contains(appExts, strings.ToLower(path.Ext(name))) {
			return nil, "", invalid("validation.app_file_name", map[string]any{"name": name}, "invalid file name %q", name)
		}
		total += len(src)
	}
	if total > maxAppBytes {
		return nil, "", invalid("validation.app_size", nil, "the app's files are larger than 1 MB")
	}
	if e, m := appEntry(app.Files); e == "" && m == "" {
		return nil, "", invalid("validation.app_entry", nil, "an app needs index.html or app.tsx/app.jsx/app.ts/app.js")
	}
	if app.Level < 0 || app.Level > 3 {
		return nil, "", invalid("validation.app_level", nil, "the level is 0 to 3")
	}
	if app.StorageMB == 0 {
		app.StorageMB = 10
	}
	if app.StorageMB < 1 || app.StorageMB > 100 {
		return nil, "", invalid("validation.app_storage", nil, "the app's storage is 1 to 100 MB")
	}
	if len(app.Sources) > maxAppSources {
		return nil, "", invalid("validation.app_sources", nil, "too many sources")
	}
	seen := map[string]bool{}
	for i, src := range app.Sources {
		src.ID = strings.TrimSpace(src.ID)
		if !appIDRe.MatchString(src.ID) || seen[src.ID] || !slices.Contains(appKinds, src.Kind) {
			return nil, "", invalid("validation.app_source", map[string]any{"id": src.ID}, "invalid source %q", src.ID)
		}
		if src.Kind != "checks" && strings.TrimSpace(src.Ref) == "" {
			return nil, "", invalid("validation.app_source", map[string]any{"id": src.ID}, "source %q needs ref", src.ID)
		}
		seen[src.ID] = true
		app.Sources[i] = src
	}
	devs := []string{}
	for _, d := range app.Devices {
		if !slices.Contains(appDevices, d) {
			return nil, "", invalid("validation.app_device", map[string]any{"device": d}, "unknown device %q (camera, microphone, location, screen, motion)", d)
		}
		if !slices.Contains(devs, d) {
			devs = append(devs, d)
		}
	}
	app.Devices = devs
	if len(app.Devices) == 0 {
		app.Devices = nil
	}
	app.Build = buildApp(app.Files)
	b, _ := json.Marshal(app)
	// Code is not text to mention or search.
	return b, "", nil
}

const defaultAppHTML = `<!doctype html>
<html>
<head>
  <meta charset="utf-8">
  <style>
    body { font-family: system-ui, sans-serif; margin: 24px; color: #1f1a17; }
    [data-theme=dark] body { background: #14110f; color: #eee7e1; }
  </style>
</head>
<body>
  <h1>Hello</h1>
  <p>Edit this app on the left.</p>
</body>
</html>
`

func sortedNames(files map[string]string) []string {
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// buildApp compiles JSX and TypeScript files to JavaScript modules.
func buildApp(files map[string]string) *AppBuild {
	b := &AppBuild{Files: map[string]string{}, Errors: []AppDiag{}}
	for _, name := range sortedNames(files) {
		if !compiled(name) {
			continue
		}
		loader := map[string]api.Loader{".ts": api.LoaderTS, ".tsx": api.LoaderTSX, ".jsx": api.LoaderJSX}[path.Ext(name)]
		r := api.Transform(files[name], api.TransformOptions{
			Loader: loader, Format: api.FormatESModule, Target: api.ES2022,
			JSX: api.JSXAutomatic, JSXImportSource: "react", Sourcefile: name, Sourcemap: api.SourceMapInline,
		})
		for _, m := range r.Errors {
			d := AppDiag{File: name, Text: m.Text}
			if m.Location != nil {
				d.Line, d.Column = m.Location.Line, m.Location.Column+1
			}
			b.Errors = append(b.Errors, d)
		}
		if len(r.Errors) == 0 {
			b.Files[name] = string(r.Code)
		}
	}
	return b
}

func appOf(doc store.Document) App {
	var a App
	_ = json.Unmarshal(doc.Content, &a)
	if a.Files == nil {
		a.Files = map[string]string{}
	}
	if a.StorageMB == 0 {
		a.StorageMB = 10
	}
	return a
}

// --- frames ---------------------------------------------------------------

// appFrameClaims is what a frame address carries (sealed).
type appFrameClaims struct {
	Tenant  string `json:"t"`
	Project string `json:"p"`
	Doc     string `json:"d,omitempty"`
	Preview string `json:"v,omitempty"`
	// Share is the address of a public release (the frame then serves it
	// only while the release exists).
	Share string `json:"s,omitempty"`
	Exp   int64  `json:"e"`
}

type appPreview struct {
	app     App
	project string
	doc     string // the app being edited ("" for a new one): its assets
	exp     time.Time
}

var (
	appPreviews sync.Map // id → appPreview
	appConsoles sync.Map // document id → *appConsole
	frameKey    = newID()
)

func (s *Service) sealFrame(c appFrameClaims) (string, error) {
	raw, _ := json.Marshal(c)
	if s.Box == nil {
		// Tests: an in-process key.
		return base64.RawURLEncoding.EncodeToString([]byte(frameKey + "|" + string(raw))), nil
	}
	sealed, err := s.Box.Seal(raw, "app-frame")
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString([]byte(sealed)), nil
}

func (s *Service) openFrame(tok string) (appFrameClaims, error) {
	var c appFrameClaims
	raw, err := base64.RawURLEncoding.DecodeString(tok)
	if err != nil {
		return c, ErrNotFound
	}
	var plain []byte
	if s.Box == nil {
		k, rest, ok := strings.Cut(string(raw), "|")
		if !ok || k != frameKey {
			return c, ErrNotFound
		}
		plain = []byte(rest)
	} else if plain, err = s.Box.Open(string(raw), "app-frame"); err != nil {
		return c, ErrNotFound
	}
	if json.Unmarshal(plain, &c) != nil || time.Now().Unix() > c.Exp {
		return c, ErrNotFound
	}
	return c, nil
}

// AppFrame is what the page around an app needs.
type AppFrame struct {
	URL       string      `json:"url"`
	Level     int         `json:"level"`
	Granted   int         `json:"granted"` // -1 = not asked yet
	Sources   []AppSource `json:"sources"`
	AI        bool        `json:"ai"`
	StorageMB int         `json:"storageMb"`
	Errors    []AppDiag   `json:"errors"`
	CanEdit   bool        `json:"canEdit"`
	// Isolated: the app runs on its own subdomain of the user content
	// domain (then it may keep browser storage); otherwise in a frame
	// without an origin.
	Isolated bool `json:"isolated"`
	// MaxLevel is the level the instance or organization allows (≤ Level).
	MaxLevel int `json:"maxLevel"`
	// Devices the app may ask for, and what this person answered.
	Devices      []string        `json:"devices"`
	DeviceGrants map[string]bool `json:"deviceGrants"`
	SelfEdit     bool            `json:"selfEdit,omitempty"`
	// NeedsPreview asks the page (of someone who may edit) for a picture of
	// the app: there is none of this version yet.
	NeedsPreview bool `json:"needsPreview,omitempty"`
}

const appFrameTTL = 12 * time.Hour

// OpenApp gives a viewer the frame address of an app and their grant.
func (s *Service) OpenApp(ctx context.Context, a Actor, ref, docRef string) (AppFrame, error) {
	p, doc, err := s.docAccess(ctx, a, ref, docRef, store.RoleReader)
	if err != nil {
		return AppFrame{}, err
	}
	if doc.Type != store.DocApp {
		return AppFrame{}, ErrNotFound
	}
	app := appOf(doc)
	pol := s.effectiveAppPolicy(ctx, p)
	if pol.Off {
		return AppFrame{}, errAppsOff()
	}
	tok, err := s.sealFrame(appFrameClaims{Tenant: store.TenantOf(ctx), Project: p.ID, Doc: doc.ID, Exp: time.Now().Add(appFrameTTL).Unix()})
	if err != nil {
		return AppFrame{}, err
	}
	granted, _ := s.DB.AppGrant(ctx, doc.ID, a.User.ID)
	errs := []AppDiag{}
	if app.Build != nil {
		errs = app.Build.Errors
	}
	url, isolated := s.frameURL(doc.ID, tok)
	level := pol.capLevel(app.Level)
	devs, grants := s.appDeviceState(ctx, a, doc.ID, app, pol)
	canEdit := rank[p.Role] >= rank[store.RoleEditor] && !s.readOnly(a, p)
	return AppFrame{URL: url, Isolated: isolated, Level: app.Level, MaxLevel: level, Granted: min(granted, level), Sources: nonNil(app.Sources), AI: app.AI && !pol.NoAI, StorageMB: app.StorageMB, Errors: errs,
		CanEdit: canEdit, Devices: devs, DeviceGrants: grants, SelfEdit: app.SelfEdit && canEdit, NeedsPreview: canEdit && a.Via == "web" && s.previewStale(ctx, p.ID, doc)}, nil
}

// frameURL is where a frame loads: <label>.<user content domain>, or the
// sandboxed path on the instance's own address.
func (s *Service) frameURL(label, tok string) (string, bool) {
	if d := s.appDomain(); d != "" {
		return "https://" + usercontent.Label(label) + "." + d + "/f/" + tok + "/", true
	}
	return "/app-frame/" + tok + "/", false
}

func nonNil[T any](v []T) []T {
	if v == nil {
		return []T{}
	}
	return v
}

// PreviewApp compiles unsaved files for the editor's live preview.
// docRef (optional) is the app being edited; its grants apply.
func (s *Service) PreviewApp(ctx context.Context, a Actor, ref, docRef string, app App) (AppFrame, error) {
	p, err := s.project(ctx, a, ref, store.RoleEditor)
	if err != nil {
		return AppFrame{}, err
	}
	granted := -1
	docID := ""
	grants := map[string]bool{}
	if docRef != "" {
		if _, doc, err := s.docAccess(ctx, a, ref, docRef, store.RoleEditor); err == nil && doc.Type == store.DocApp {
			granted, _ = s.DB.AppGrant(ctx, doc.ID, a.User.ID)
			docID = doc.ID
			if g, err := s.DB.AppDeviceGrants(ctx, doc.ID, a.User.ID); err == nil {
				grants = g
			}
		}
	}
	if _, _, err := appContent(&app); err != nil {
		return AppFrame{}, err
	}
	id := newID()
	appPreviews.Store(id, appPreview{app: app, project: p.ID, doc: docID, exp: time.Now().Add(30 * time.Minute)})
	// Old previews go away.
	appPreviews.Range(func(k, v any) bool {
		if time.Now().After(v.(appPreview).exp) {
			appPreviews.Delete(k)
		}
		return true
	})
	tok, err := s.sealFrame(appFrameClaims{Tenant: store.TenantOf(ctx), Project: p.ID, Preview: id, Exp: time.Now().Add(30 * time.Minute).Unix()})
	if err != nil {
		return AppFrame{}, err
	}
	url, isolated := s.frameURL("p"+id, tok)
	pol := s.effectiveAppPolicy(ctx, p)
	devs := nonNil(app.Devices)
	if pol.NoDevices {
		devs = []string{}
	}
	return AppFrame{URL: url, Isolated: isolated, Level: app.Level, Granted: min(granted, app.Level), Sources: nonNil(app.Sources), AI: app.AI, StorageMB: app.StorageMB, Errors: app.Build.Errors, CanEdit: true,
		Devices: devs, DeviceGrants: grants, SelfEdit: app.SelfEdit}, nil
}

// AppFile is one file the frame serves.
type AppFile struct {
	Body []byte
	Type string
	// Hosts the app may connect to (policy of the instance and organization).
	Hosts []string
}

// AppFrameFile serves a file of an app's frame by its sealed address.
// origin is the instance's address (scheme://host) for library imports;
// label is the subdomain on the user content domain ("" on the instance's
// own address), which must belong to the token's app.
func (s *Service) AppFrameFile(ctx context.Context, tok, name, origin, label string) (AppFile, error) {
	c, err := s.openFrame(tok)
	if err != nil {
		return AppFile{}, err
	}
	if label != "" {
		want := usercontent.Label(c.Doc)
		if c.Preview != "" {
			want = usercontent.Label("p" + c.Preview)
		} else if c.Share != "" {
			want = usercontent.Label("s" + c.Doc)
		}
		if label != want {
			return AppFile{}, ErrNotFound
		}
	}
	ctx = store.WithTenant(ctx, c.Tenant)
	var hosts []string
	if pr, err := s.DB.ProjectFor(ctx, c.Project, ""); err == nil {
		pol := s.effectiveAppPolicy(ctx, pr)
		if pol.Off || (c.Share != "" && pol.NoPublic) {
			return AppFile{}, ErrNotFound
		}
		hosts = pol.Hosts
	}
	if c.Share != "" {
		if sh, err := s.DB.AppShare(ctx, c.Doc); err != nil || sh.Token != c.Share {
			return AppFile{}, ErrNotFound
		}
	}
	var app App
	assetDoc := c.Doc
	if c.Preview != "" {
		v, ok := appPreviews.Load(c.Preview)
		if !ok {
			return AppFile{}, ErrNotFound
		}
		app = v.(appPreview).app
		assetDoc = v.(appPreview).doc
	} else {
		doc, err := s.DB.GetDocument(ctx, c.Project, c.Doc)
		if err != nil || doc.Type != store.DocApp {
			return AppFile{}, ErrNotFound
		}
		app = appOf(doc)
		if app.Build == nil {
			app.Build = buildApp(app.Files)
		}
	}
	entry, main := appEntry(app.Files)
	if name == "" || name == "index.html" {
		page := ""
		if entry != "" {
			page = app.Files[entry]
		} else {
			// A page around the main script, with every stylesheet of the app.
			var css strings.Builder
			for _, n := range sortedNames(app.Files) {
				if path.Ext(n) == ".css" {
					css.WriteString(`<link rel="stylesheet" href="` + html.EscapeString(n) + `">`)
				}
			}
			page = `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">` + css.String() + `</head><body><div id="root"></div><script type="module" src="` + html.EscapeString(main) + `"></script></body></html>`
		}
		return AppFile{Body: []byte(injectAppHead(page, origin, app)), Type: "text/html; charset=utf-8", Hosts: hosts}, nil
	}
	if asset, ok := strings.CutPrefix(name, "assets/"); ok {
		return s.appAssetFile(ctx, c.Project, assetDoc, asset, hosts)
	}
	src, ok := app.Files[name]
	if !ok {
		return AppFile{}, ErrNotFound
	}
	if compiled(name) {
		js, ok := app.Build.Files[name]
		if !ok {
			msg := "compile error"
			for _, e := range app.Build.Errors {
				if e.File == name {
					msg = fmt.Sprintf("%s:%d:%d: %s", e.File, e.Line, e.Column, e.Text)
					break
				}
			}
			js = "throw new Error(" + jsString(msg) + ");"
		}
		return AppFile{Body: []byte(js), Type: "text/javascript; charset=utf-8", Hosts: hosts}, nil
	}
	types := map[string]string{".html": "text/html; charset=utf-8", ".css": "text/css; charset=utf-8", ".js": "text/javascript; charset=utf-8", ".mjs": "text/javascript; charset=utf-8",
		".json": "application/json", ".svg": "image/svg+xml", ".md": "text/plain; charset=utf-8", ".txt": "text/plain; charset=utf-8"}
	return AppFile{Body: []byte(src), Type: types[path.Ext(name)], Hosts: hosts}, nil
}

func jsString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// injectAppHead puts the import map and the bridge first into the page.
func injectAppHead(page, origin string, app App) string {
	imports := map[string]string{}
	for k, v := range AppLibs {
		imports[k] = origin + "/applib/" + v
	}
	im, _ := json.Marshal(map[string]any{"imports": imports})
	head := `<script type="importmap">` + string(im) + `</script><script>` + appBridgeJS + `</script>`
	low := strings.ToLower(page)
	if i := strings.Index(low, "<head>"); i >= 0 {
		return page[:i+6] + head + page[i+6:]
	}
	if i := strings.Index(low, "<html"); i >= 0 {
		if j := strings.Index(low[i:], ">"); j >= 0 {
			return page[:i+j+1] + "<head>" + head + "</head>" + page[i+j+1:]
		}
	}
	return head + page
}

// --- the bridge --------------------------------------------------------------

func (s *Service) appFor(ctx context.Context, a Actor, ref, docRef string) (store.Project, store.Document, App, int, error) {
	p, doc, err := s.docAccess(ctx, a, ref, docRef, store.RoleReader)
	if err != nil {
		return p, doc, App{}, 0, err
	}
	if doc.Type != store.DocApp {
		return p, doc, App{}, 0, ErrNotFound
	}
	app := appOf(doc)
	pol := s.effectiveAppPolicy(ctx, p)
	if pol.Off {
		return p, doc, app, 0, errAppsOff()
	}
	app.AI = app.AI && !pol.NoAI
	g, err := s.DB.AppGrant(ctx, doc.ID, a.User.ID)
	return p, doc, app, min(g, pol.capLevel(app.Level)), err
}

// GrantApp stores the level a person allows an app (at most what it asks for).
func (s *Service) GrantApp(ctx context.Context, a Actor, ref, docRef string, level int) (int, error) {
	if err := s.fullUser(a); err != nil {
		return 0, err
	}
	_, doc, app, _, err := s.appFor(ctx, a, ref, docRef)
	if err != nil {
		return 0, err
	}
	level = max(0, min(level, app.Level))
	if err := s.DB.SetAppGrant(ctx, doc.ID, a.User.ID, level); err != nil {
		return 0, err
	}
	s.event(ctx, a, "app.grant", doc.ID, map[string]int{"level": level})
	return level, nil
}

func needLevel(have, need int) error {
	if have < need {
		return invalid("validation.app_not_allowed", map[string]any{"level": need}, "the app may not do this (needs level %d)", need)
	}
	return nil
}

// ReadAppSource reads a source of the app's manifest with the viewer's rights.
func (s *Service) ReadAppSource(ctx context.Context, a Actor, ref, docRef, sourceID string) (any, error) {
	p, _, app, granted, err := s.appFor(ctx, a, ref, docRef)
	if err != nil {
		return nil, err
	}
	if err := needLevel(granted, 1); err != nil {
		return nil, err
	}
	i := slices.IndexFunc(app.Sources, func(x AppSource) bool { return x.ID == sourceID })
	if i < 0 {
		return nil, invalid("validation.app_source_unknown", map[string]any{"id": sourceID}, "the app has no source %q", sourceID)
	}
	return s.readSource(ctx, a, p, app.Sources[i])
}

// readSource reads one source with the actor's rights.
func (s *Service) readSource(ctx context.Context, a Actor, p store.Project, src AppSource) (any, error) {
	switch src.Kind {
	case "query":
		return s.RunSavedQuery(ctx, a, p.Slug, src.Ref)
	case "board":
		return s.Tasks(ctx, a, p.Slug, src.Ref, "", "", false)
	case "checks":
		return s.Checks(ctx, a, p.Slug, src.Ref)
	case "entities":
		return s.RunQuery(ctx, a, p.Slug, GraphQuery{Kind: src.Ref, Limit: 500})
	case "request":
		return s.readRequestSource(ctx, a, p, src.Ref)
	case "readings":
		slug, metric, _ := strings.Cut(src.Ref, ":")
		return s.EntityReadings(ctx, a, p.Slug, slug, metric, 24)
	}
	return nil, ErrNotFound
}

var appKeyRe = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,120}$`)

func appOwner(a Actor, personal bool) string {
	if personal {
		return a.User.ID
	}
	return ""
}

// AppStorageGet reads a value of the app's storage.
func (s *Service) AppStorageGet(ctx context.Context, a Actor, ref, docRef, key string, personal bool) (json.RawMessage, error) {
	_, doc, _, granted, err := s.appFor(ctx, a, ref, docRef)
	if err != nil {
		return nil, err
	}
	if err := needLevel(granted, 2); err != nil {
		return nil, err
	}
	it, err := s.DB.AppItem(ctx, doc.ID, appOwner(a, personal), key)
	if err == store.ErrNotFound {
		return json.RawMessage("null"), nil
	}
	return json.RawMessage(it.Value), err
}

// AppStorageList lists the keys of the app's storage.
func (s *Service) AppStorageList(ctx context.Context, a Actor, ref, docRef string, personal bool) ([]string, error) {
	_, doc, _, granted, err := s.appFor(ctx, a, ref, docRef)
	if err != nil {
		return nil, err
	}
	if err := needLevel(granted, 2); err != nil {
		return nil, err
	}
	items, err := s.DB.AppItems(ctx, doc.ID, appOwner(a, personal))
	keys := make([]string, 0, len(items))
	for _, it := range items {
		keys = append(keys, it.Key)
	}
	return keys, err
}

// AppStorageSet stores a value (JSON) in the app's storage.
func (s *Service) AppStorageSet(ctx context.Context, a Actor, ref, docRef, key string, value json.RawMessage, personal bool) error {
	if err := s.fullUser(a); err != nil {
		return err
	}
	p, doc, app, granted, err := s.appFor(ctx, a, ref, docRef)
	if err != nil {
		return err
	}
	if err := needLevel(granted, 2); err != nil {
		return err
	}
	if !appKeyRe.MatchString(key) {
		return invalid("validation.app_key", nil, "keys are 1–120 letters, digits and _.:-")
	}
	if !json.Valid(value) || len(value) > 1<<20 {
		return invalid("validation.app_value", nil, "values are JSON up to 1 MB")
	}
	used, err := s.DB.AppStorageSize(ctx, doc.ID)
	if err != nil {
		return err
	}
	old, _ := s.DB.AppItem(ctx, doc.ID, appOwner(a, personal), key)
	if used-int64(old.Size)+int64(len(value)) > int64(app.StorageMB)<<20 {
		return invalid("validation.app_storage_full", map[string]any{"mb": app.StorageMB}, "the app's storage is full (%d MB)", app.StorageMB)
	}
	if err := s.checkQuota(ctx, p, int64(len(value)-old.Size)); err != nil {
		return err
	}
	return s.DB.SetAppItem(ctx, p.ID, doc.ID, appOwner(a, personal), key, string(value))
}

// AppStorageDelete removes a value.
func (s *Service) AppStorageDelete(ctx context.Context, a Actor, ref, docRef, key string, personal bool) error {
	_, doc, _, granted, err := s.appFor(ctx, a, ref, docRef)
	if err != nil {
		return err
	}
	if err := needLevel(granted, 2); err != nil {
		return err
	}
	return s.DB.DeleteAppItem(ctx, doc.ID, appOwner(a, personal), key)
}

// --- console --------------------------------------------------------------

// AppLog is a message from an app's console (errors and warnings).
type AppLog struct {
	Level string    `json:"level"`
	Text  string    `json:"text"`
	Line  int       `json:"line,omitempty"`
	Col   int       `json:"col,omitempty"`
	File  string    `json:"file,omitempty"`
	At    time.Time `json:"at"`
}

type appConsole struct {
	mu   sync.Mutex
	logs []AppLog
}

// LogApp keeps the last console messages of an app for the AI (get_app).
func (s *Service) LogApp(ctx context.Context, a Actor, ref, docRef string, logs []AppLog) error {
	_, doc, err := s.docAccess(ctx, a, ref, docRef, store.RoleReader)
	if err != nil {
		return err
	}
	v, _ := appConsoles.LoadOrStore(doc.ID, &appConsole{})
	c := v.(*appConsole)
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, l := range logs[:min(len(logs), 20)] {
		l.Text = excerpt(l.Text, 2000)
		l.File = path.Base(l.File)
		l.At = time.Now()
		c.logs = append(c.logs, l)
	}
	if len(c.logs) > 50 {
		c.logs = c.logs[len(c.logs)-50:]
	}
	return nil
}

func appLogs(docID string) []AppLog {
	v, ok := appConsoles.Load(docID)
	if !ok {
		return []AppLog{}
	}
	c := v.(*appConsole)
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.logs)
}

// AppView is an app for the AI: files, manifest, compile errors and the
// last console messages.
type AppView struct {
	Document store.Document `json:"document"`
	App      App            `json:"app"`
	Console  []AppLog       `json:"console"`
	Assets   []AppAsset     `json:"assets"` // served at assets/<name> next to index.html
	URL      string         `json:"url"`
}

// GetApp reads an app with its build errors and console.
func (s *Service) GetApp(ctx context.Context, a Actor, ref, docRef string) (AppView, error) {
	p, doc, err := s.docAccess(ctx, a, ref, docRef, store.RoleReader)
	if err != nil {
		return AppView{}, err
	}
	if doc.Type != store.DocApp {
		return AppView{}, invalid("validation.not_app", nil, "%s is not an app", docRef)
	}
	app := appOf(doc)
	if app.Build != nil {
		app.Build.Files = nil // compiled code is noise for readers
	}
	doc.Content = nil
	assets, err := s.AppAssets(ctx, a, p.Slug, doc.ID)
	if err != nil {
		return AppView{}, err
	}
	return AppView{Document: doc, App: app, Console: appLogs(doc.ID), Assets: assets, URL: s.PublicURL + "/p/" + p.Slug + "/d/" + doc.Slug}, nil
}

// AppEdit is one change of a file (exact text, like page edits).
type AppEdit struct {
	File string `json:"file"`
	Old  string `json:"old"`
	New  string `json:"new"`
}

// EditApp applies find-and-replace edits and whole files to an app.
func (s *Service) EditApp(ctx context.Context, a Actor, ref, docRef string, files map[string]*string, edits []AppEdit, manifest *App) (DocResult, error) {
	_, doc, err := s.docAccess(ctx, a, ref, docRef, store.RoleEditor)
	if err != nil {
		return DocResult{}, err
	}
	if doc.Type != store.DocApp {
		return DocResult{}, invalid("validation.not_app", nil, "%s is not an app", docRef)
	}
	app := appOf(doc)
	app.Build = nil
	for name, src := range files {
		if src == nil {
			delete(app.Files, name)
		} else {
			app.Files[name] = *src
		}
	}
	for i, e := range edits {
		src, ok := app.Files[e.File]
		if !ok {
			return DocResult{}, invalid("validation.app_edit_file", map[string]any{"file": e.File, "n": i + 1}, "edit %d: no file %q", i+1, e.File)
		}
		switch c := strings.Count(src, e.Old); {
		case e.Old == "" || c == 0:
			return DocResult{}, invalid("validation.edit_not_found", map[string]any{"n": i + 1}, "edit %d: old text not found in %s", i+1, e.File)
		case c > 1:
			return DocResult{}, invalid("validation.edit_ambiguous", map[string]any{"n": i + 1, "count": c}, "edit %d: old text found %d times in %s (needs exactly once)", i+1, c, e.File)
		}
		app.Files[e.File] = strings.Replace(src, e.Old, e.New, 1)
	}
	if manifest != nil {
		app.Level, app.Sources, app.StorageMB, app.AI = manifest.Level, manifest.Sources, manifest.StorageMB, manifest.AI
		app.Devices, app.SelfEdit = manifest.Devices, manifest.SelfEdit
	}
	return s.UpdateDocument(ctx, a, ref, doc.ID, DocumentPatch{App: &app})
}

// AppAIAnswer is the AI's answer to an app.
type AppAIAnswer struct {
	Text string          `json:"text"`
	JSON json.RawMessage `json:"json,omitempty"`
}

// AskAppAI answers an app's AI call on the viewer's chat budget (apps that
// enable AI, where the organization and project allow the AI chat).
func (s *Service) AskAppAI(ctx context.Context, a Actor, ref, docRef, prompt string, wantJSON bool) (AppAIAnswer, error) {
	_, doc, app, _, err := s.appFor(ctx, a, ref, docRef)
	if err != nil {
		return AppAIAnswer{}, err
	}
	if !app.AI {
		return AppAIAnswer{}, invalid("validation.app_ai_off", nil, "this app does not use the AI")
	}
	return s.askAppAI(ctx, a, ref, doc.ID, prompt, wantJSON)
}

// askAppAI runs one AI call of an app on the actor's chat budget.
func (s *Service) askAppAI(ctx context.Context, a Actor, ref, docID, prompt string, wantJSON bool) (AppAIAnswer, error) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" || len(prompt) > 20_000 {
		return AppAIAnswer{}, invalid("validation.app_ai_prompt", nil, "the prompt is empty or longer than 20,000 characters")
	}
	cs, err := s.ChatStatus(ctx, a, ref)
	if err != nil {
		return AppAIAnswer{}, err
	}
	if !cs.Available {
		return AppAIAnswer{}, invalid(cs.Reason, nil, "the AI is not available: %s", cs.Reason)
	}
	m, client, err := s.ResolveChatModel(ctx, a, ref, "")
	if err != nil {
		return AppAIAnswer{}, err
	}
	system := []string{resources.Prompt("app-ai")}
	if wantJSON {
		prompt += "\n\nAnswer with JSON only."
	}
	turn, err := client.Stream(ctx, ai.Request{Model: m.Model, System: system, MaxTokens: 2000, History: []ai.Message{{Raw: ai.UserMessage(prompt, nil)}}}, func(ai.Event) {})
	if err != nil {
		return AppAIAnswer{}, invalid("validation.app_ai_failed", nil, "the AI did not answer")
	}
	s.RecordAIUsage(ctx, a, cs, "app:"+docID, m, turn.Usage)
	var b strings.Builder
	for _, p := range ai.Parts(turn.Message) {
		if p.Type == "text" {
			b.WriteString(p.Text)
		}
	}
	out := AppAIAnswer{Text: b.String()}
	if wantJSON {
		t := strings.TrimSpace(out.Text)
		t = strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(t, "```json"), "```"), "```")
		if json.Valid([]byte(strings.TrimSpace(t))) {
			out.JSON = json.RawMessage(strings.TrimSpace(t))
		}
	}
	return out, nil
}
