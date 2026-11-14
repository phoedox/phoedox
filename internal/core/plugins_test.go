package core

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/phoedox/phoedox/internal/blob"
	"github.com/phoedox/phoedox/internal/plugins"
	"github.com/phoedox/phoedox/internal/plugins/plugintest"
	"github.com/phoedox/phoedox/internal/secret"
	"github.com/phoedox/phoedox/internal/store"
)

func pluginSetup(t *testing.T) (*Service, Actor, Actor, Actor, ed25519.PrivateKey) {
	t.Helper()
	s, owner, reader, stranger := setup(t)
	dir := t.TempDir()
	s.Blobs = blob.Local{Dir: filepath.Join(dir, "files")}
	s.TempDir = dir
	s.Box, _ = secret.New(make([]byte, 32))
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	rt := plugins.NewRuntime(slog.New(slog.NewTextHandler(io.Discard, nil)), 4)
	rt.Code = PluginErrorCode
	t.Cleanup(func() { rt.Close(context.Background()) })
	s.Plugins = PluginConfig{Runtime: rt, Keys: map[string]ed25519.PublicKey{"store-1": pub}, Version: "1.4.0"}
	return s, owner, reader, stranger, priv
}

func signed(t *testing.T, priv ed25519.PrivateKey, data []byte) *plugins.Signature {
	p, err := plugins.ReadPackage(data)
	if err != nil {
		t.Fatal(err)
	}
	sig := plugins.Sign(priv, "store-1", p.Manifest.ID, p.Manifest.Version, p.SHA256)
	return &sig
}

func TestPlugins(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, stranger, priv := pluginSetup(t)
	admin := owner
	admin.User.IsAdmin = true
	data := plugintest.Package(t, plugintest.Manifest)

	// Installing needs an admin and a trusted signature.
	if _, err := s.InstallPlugin(ctx, owner, data, signed(t, priv, data), "upload"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("non-admins must not install: %v", err)
	}
	if _, err := s.InstallPlugin(ctx, admin, data, nil, "upload"); invalidKey(err) != "validation.plugin_unsigned" {
		t.Fatalf("unsigned: %v", err)
	}
	_, other, _ := ed25519.GenerateKey(rand.Reader)
	bad := plugins.Sign(other, "store-1", "de.example.ups", "1.2.0", "x")
	if _, err := s.InstallPlugin(ctx, admin, data, &bad, "upload"); invalidKey(err) != "validation.plugin_signature" {
		t.Fatalf("bad signature: %v", err)
	}
	old := strings.Replace(plugintest.Manifest, ">=0.1.0 <2.0.0", ">=2.0.0", 1)
	oldData := plugintest.Package(t, old)
	if _, err := s.InstallPlugin(ctx, admin, oldData, signed(t, priv, oldData), "upload"); invalidKey(err) != "validation.plugin_incompatible" {
		t.Fatalf("incompatible: %v", err)
	}
	gen := s.PluginGeneration()
	info, err := s.InstallPlugin(ctx, admin, data, signed(t, priv, data), "upload")
	if err != nil || !info.Signed || info.Problem != "" || info.Version != "1.2.0" || !slices.Equal(info.Kinds, []string{"ups"}) {
		t.Fatalf("install: %v %+v", err, info)
	}
	if s.PluginGeneration() == gen {
		t.Fatal("installing must bump the generation")
	}

	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Haus", AIWriteMode: "direct"})
	_ = s.SetMember(ctx, owner, p.Slug, reader.User.Email, store.RoleReader)

	// Only owners enable plugins; readers see enabled ones only.
	if err := s.SetProjectPlugin(ctx, reader, p.Slug, "de.example.ups", true); !errors.Is(err, ErrForbidden) {
		t.Fatalf("readers must not enable: %v", err)
	}
	if list, _ := s.ProjectPlugins(ctx, reader, p.Slug); len(list) != 0 {
		t.Fatalf("not enabled yet: %+v", list)
	}
	if err := s.SetProjectPlugin(ctx, owner, p.Slug, "de.example.ups", true); err != nil {
		t.Fatal(err)
	}
	list, err := s.ProjectPlugins(ctx, reader, p.Slug)
	if err != nil || len(list) != 1 || !list[0].Enabled || list[0].Frontend != "/api/v1/plugins/de.example.ups/1.2.0/files/frontend/index.js" || list[0].Secrets != nil {
		t.Fatalf("reader view: %v %+v", err, list)
	}
	if _, err := s.PluginFile(ctx, reader, "de.example.ups", "1.2.0", "frontend/index.js"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PluginFile(ctx, reader, "de.example.ups", "1.2.0", "backend.wasm"); !errors.Is(err, ErrNotFound) {
		t.Fatal("the backend is not served")
	}

	// The plugin's kind is part of the data model and cannot be redefined.
	m, _ := s.DataModel(ctx, reader, p.Slug)
	k, ok := m.Find("ups")
	if !ok || k.Source != "plugin:de.example.ups" || k.Name != "USV" || k.Icon != "BatteryCharging" || len(k.Attrs) != 3 ||
		!k.Attrs[0].Required || k.Attrs[0].Unit != "VA" || k.Attrs[0].Label != "Kapazität" || k.Relations[0].Rel != "powers" {
		t.Fatalf("plugin kind: %+v", k)
	}
	if _, err := s.DefineKind(ctx, owner, p.Slug, KindInput{ID: "ups", Name: "Meine USV"}); invalidKey(err) != "validation.kind_plugin" {
		t.Fatalf("redefine: %v", err)
	}

	// Secrets: only declared names, never returned.
	if err := s.SetPluginSecrets(ctx, owner, p.Slug, "de.example.ups", map[string]string{"other": "x"}); invalidKey(err) != "validation.plugin_secret" {
		t.Fatalf("undeclared secret: %v", err)
	}
	if err := s.SetPluginSecrets(ctx, owner, p.Slug, "de.example.ups", map[string]string{"nut_password": "geheim"}); err != nil {
		t.Fatal(err)
	}
	list, _ = s.ProjectPlugins(ctx, owner, p.Slug)
	if !list[0].Secrets["nut_password"] {
		t.Fatalf("secret flag: %+v", list[0])
	}
	ap, err := s.activePlugin(ctx, p.ID, "de.example.ups")
	if err != nil {
		t.Fatal(err)
	}
	if v, err := s.pluginEnv(owner, p, ap).Secret(ctx, "nut_password"); err != nil || v != "geheim" {
		t.Fatalf("secret: %q %v", v, err)
	}

	// Tools run with the caller's rights.
	out, err := s.PluginTool(ctx, reader, p.Slug, "de.example.ups", "ups.runtime", json.RawMessage(`{"ups":"keller"}`))
	if err != nil || !strings.Contains(string(out), `"keller"`) {
		t.Fatalf("tool: %s %v", out, err)
	}
	if _, err := s.PluginTool(ctx, stranger, p.Slug, "de.example.ups", "ups.runtime", nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("strangers: %v", err)
	}
	if _, err := s.PluginTool(ctx, reader, p.Slug, "de.example.ups", "nope", nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown tool: %v", err)
	}

	// Control tools: readers may not run them, AI clients only propose them.
	if _, err := s.PluginTool(ctx, reader, p.Slug, "de.example.ups", "ups.switch", json.RawMessage(`{"on":false}`)); !errors.Is(err, ErrForbidden) {
		t.Fatalf("reader must not switch: %v", err)
	}
	ai := Actor{User: owner.User, Via: "mcp"}
	out, err = s.PluginTool(ctx, ai, p.Slug, "de.example.ups", "ups.switch", json.RawMessage(`{"on":false}`))
	if err != nil || !strings.Contains(string(out), `"applied":false`) {
		t.Fatalf("AI switch must become a suggestion: %s %v", out, err)
	}
	sugs, _ := s.Suggestions(ctx, owner, p.Slug, "pending")
	if len(sugs) != 1 || sugs[0].Op != "plugin.call" {
		t.Fatalf("suggestion: %+v", sugs)
	}
	res, err := s.DecideSuggestion(ctx, owner, sugs[0].ID, true)
	if err != nil || !strings.Contains(fmt.Sprint(string(res.(json.RawMessage))), `"ups.switch"`) {
		t.Fatalf("accept runs the tool: %v %v", res, err)
	}
	if out, err := s.PluginTool(ctx, owner, p.Slug, "de.example.ups", "ups.switch", json.RawMessage(`{"on":true}`)); err != nil || !strings.Contains(string(out), `"on":true`) {
		t.Fatalf("owner switches directly: %s %v", out, err)
	}
	readerEnv := s.pluginEnv(Actor{User: reader.User, Via: "web", Plugin: "de.example.ups"}, p, ap)
	if _, err := readerEnv.GraphApply(ctx, plugins.Ops{Ops: []plugins.Op{{Op: "upsert", Kind: "ups", ID: "x"}}}, ap.pkg.Manifest.CanWriteKind); !errors.Is(err, ErrForbidden) {
		t.Fatalf("a plugin must not write more than a reader: %v", err)
	}
	if tools := s.PluginTools(ctx); len(tools) != 2 || tools[0].Name != "ups.runtime" || len(tools[0].Input) == 0 || !strings.Contains(tools[1].Description, "confirm") {
		t.Fatalf("tools: %+v", tools)
	}

	// Schedules run as the user who enabled the plugin.
	now := time.Now()
	s.RunPluginSchedules(store.System(ctx), now)
	e, err := s.Entity(ctx, owner, p.Slug, "usv-keller")
	if err != nil || e.Entity.Source != "plugin" || !strings.Contains(string(e.Entity.Attrs), `"ladung":80`) {
		pps, _ := s.DB.ProjectPlugins(ctx, p.ID)
		t.Fatalf("scheduled run: %v %+v %+v %+v", err, e.Entity, pps, s.Plugins.Runtime.Logs(logKey(ctx, "de.example.ups")))
	}
	s.RunPluginSchedules(store.System(ctx), now.Add(10*time.Second))
	if v, _ := s.DB.PluginKV(ctx, p.ID, "de.example.ups", "count"); v != "1" {
		t.Fatalf("not due yet, count %s", v)
	}
	s.RunPluginSchedules(store.System(ctx), now.Add(61*time.Second))
	if v, _ := s.DB.PluginKV(ctx, p.ID, "de.example.ups", "count"); v != "2" {
		t.Fatalf("count %s", v)
	}
	// User changes reach plugins that subscribed (entity.*); its own changes do not.
	if _, err := s.UpsertEntity(ctx, owner, p.Slug, EntityInput{Kind: "device", Name: "NAS"}); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.DB.PluginKV(ctx, p.ID, "de.example.ups", "count"); v != "3" {
		t.Fatalf("event count %s", v)
	}
	var actor string
	_ = s.DB.QueryRow(ctx, `SELECT actor FROM events WHERE subject = ? AND kind = 'entity.upsert' LIMIT 1`, e.Entity.ID).Scan(&actor)
	if actor != "plugin:"+owner.User.ID+":de.example.ups" {
		t.Fatalf("plugin writes need an actor: %q", actor)
	}
	pps, _ := s.DB.ProjectPlugins(ctx, p.ID)
	if pps[0].LastRun.IsZero() || pps[0].LastError != "" {
		t.Fatalf("last run: %+v", pps[0])
	}

	// Switched off or critically recalled plugins stop running.
	if _, err := s.SetPluginEnabled(ctx, admin, "de.example.ups", false); err != nil {
		t.Fatal(err)
	}
	if m, _ := s.DataModel(ctx, reader, p.Slug); slices.ContainsFunc(m.Kinds, func(k Kind) bool { return k.ID == "ups" }) {
		t.Fatal("kind of a disabled plugin")
	}
	if _, err := s.PluginTool(ctx, reader, p.Slug, "de.example.ups", "ups.runtime", nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("tool of a disabled plugin: %v", err)
	}
	_, _ = s.SetPluginEnabled(ctx, admin, "de.example.ups", true)
	row, _ := s.DB.GetPlugin(ctx, "de.example.ups")
	row.Recall = "critical"
	_ = s.DB.SavePlugin(ctx, row)
	if list, _ := s.ProjectPlugins(ctx, reader, p.Slug); len(list) != 0 {
		t.Fatal("recalled plugins are hidden from readers")
	}
	row.Recall = ""
	_ = s.DB.SavePlugin(ctx, row)

	// Uninstalling removes settings, data and the package.
	if err := s.UninstallPlugin(ctx, admin, "de.example.ups"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Blobs.Open(ctx, info.SHA256); !errors.Is(err, blob.ErrNotFound) {
		t.Fatal("package must be deleted")
	}
	if _, err := s.DB.PluginKV(ctx, p.ID, "de.example.ups", "count"); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("plugin data must be deleted")
	}
}

func TestPluginLicense(t *testing.T) {
	ctx := context.Background()
	s, owner, _, _, priv := pluginSetup(t)
	admin := owner
	admin.User.IsAdmin = true
	manifest := strings.Replace(plugintest.Manifest, `"description"`, `"license": {"paid": {"plan": "pro"}}, "description"`, 1)
	data := plugintest.Package(t, manifest)
	info, err := s.InstallPlugin(ctx, admin, data, signed(t, priv, data), "store")
	if err != nil || !info.License.Required || info.License.Valid {
		t.Fatalf("install: %v %+v", err, info.License)
	}
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Haus"})
	if err := s.SetProjectPlugin(ctx, owner, p.Slug, "de.example.ups", true); invalidKey(err) != "validation.plugin_license" {
		t.Fatalf("needs a license: %v", err)
	}
	inst, _ := s.InstanceID(ctx)
	sign := func(instance string) json.RawMessage {
		var f LicenseFile
		f.License.ID, f.License.PluginID, f.License.Plan, f.License.Kind = "l1", "de.example.ups", "pro", "purchase"
		f.License.OrgID, f.License.InstanceID, f.License.IssuedAt = store.TenantOf(ctx), instance, time.Now().UTC().Truncate(time.Second)
		msg, _ := json.Marshal(f.License)
		f.KeyID, f.Signature = "store-1", b64(ed25519.Sign(priv, msg))
		b, _ := json.Marshal(f)
		return b
	}
	if _, err := s.SetPluginLicense(ctx, admin, "de.example.ups", sign("other-instance")); invalidKey(err) != "validation.plugin_license_file" {
		t.Fatalf("license for another instance: %v", err)
	}
	info, err = s.SetPluginLicense(ctx, admin, "de.example.ups", sign(inst))
	if err != nil || !info.License.Valid || info.License.Plan != "pro" {
		t.Fatalf("license: %v %+v", err, info.License)
	}
	if err := s.SetProjectPlugin(ctx, owner, p.Slug, "de.example.ups", true); err != nil {
		t.Fatal(err)
	}
}

func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

type fakeStore struct {
	catalog []CatalogEntry
	recalls map[string]Recall
	pkg     []byte
	sig     plugins.Signature
	channel string
}

func (f *fakeStore) Catalog(_ context.Context, _, _, channel string) ([]CatalogEntry, error) {
	f.channel = channel
	return f.catalog, nil
}
func (f *fakeStore) Collections(context.Context) ([]PluginCollection, error) {
	return []PluginCollection{{Slug: "home-lab", Title: "Home lab", Plugins: []string{"de.example.ups"}}}, nil
}
func (f *fakeStore) Download(context.Context, string, string) ([]byte, plugins.Signature, error) {
	return f.pkg, f.sig, nil
}
func (f *fakeStore) Redeem(context.Context, string, string, string, string) (json.RawMessage, error) {
	return nil, errors.New("no")
}
func (f *fakeStore) Recalls(context.Context, map[string]string) (map[string]Recall, error) {
	return f.recalls, nil
}

func TestPluginStoreSync(t *testing.T) {
	ctx := context.Background()
	s, owner, _, _, priv := pluginSetup(t)
	admin := owner
	admin.User.IsAdmin = true
	data := plugintest.Package(t, plugintest.Manifest)
	fs := &fakeStore{pkg: data, sig: *signed(t, priv, data), catalog: []CatalogEntry{{ID: "de.example.ups", Version: "1.2.0"}}}
	s.Plugins.Store = fs
	if _, err := s.InstallFromStore(ctx, admin, "de.example.ups", "1.2.0"); err != nil {
		t.Fatal(err)
	}
	list, _ := s.PluginCatalog(ctx, admin)
	if list[0].Installed != "1.2.0" {
		t.Fatalf("catalog: %+v", list)
	}
	if fs.channel != "stable" {
		t.Fatalf("channel by default: %q", fs.channel)
	}
	yes := true
	if _, err := s.UpdateSettings(ctx, admin, SettingsPatch{PluginBeta: &yes}); err != nil {
		t.Fatal(err)
	}
	if _, _ = s.PluginCatalog(ctx, admin); fs.channel != "beta" {
		t.Fatalf("beta channel: %q", fs.channel)
	}
	if cols, err := s.PluginCollections(ctx, admin); err != nil || len(cols) != 1 || cols[0].Slug != "home-lab" {
		t.Fatalf("collections: %v %+v", err, cols)
	}
	if _, err := s.PluginCollections(ctx, owner); err == nil {
		t.Fatal("collections need an admin")
	}
	fs.catalog[0].Version = "1.3.0"
	fs.recalls = map[string]Recall{"de.example.ups": {Level: "critical", Note: "leak"}}
	if err := s.SyncPluginStore(ctx); err != nil {
		t.Fatal(err)
	}
	row, _ := s.DB.GetPlugin(ctx, "de.example.ups")
	if row.Latest != "1.3.0" || row.Recall != "critical" || row.RecallNote != "leak" {
		t.Fatalf("sync: %+v", row)
	}
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Haus"})
	if err := s.SetProjectPlugin(ctx, owner, p.Slug, "de.example.ups", true); invalidKey(err) != "validation.plugin_unusable" {
		t.Fatalf("recalled plugins cannot be enabled: %v", err)
	}
	fs.recalls = map[string]Recall{}
	_ = s.SyncPluginStore(ctx)
	if row, _ := s.DB.GetPlugin(ctx, "de.example.ups"); row.Recall != "" {
		t.Fatal("lifted recall")
	}
}
