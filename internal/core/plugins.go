package core

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/phoedox/phoedox/internal/plugins"
	"github.com/phoedox/phoedox/internal/store"
)

// PluginConfig switches plugins on (Runtime != nil) and says whom to trust.
type PluginConfig struct {
	Runtime *plugins.Runtime
	// Keys are the trusted signing keys of the store (and of authors).
	Keys map[string]ed25519.PublicKey
	// AllowUnsigned accepts uploaded packages without a valid signature
	// (development only).
	AllowUnsigned bool
	// Version is the server version checked against engines.phoedox.
	Version string
	// Store is the plugin store (nil = no store).
	Store PluginStore
}

// PluginStore is the client side of the plugin store (internal/storeclient).
type PluginStore interface {
	Catalog(ctx context.Context, version, hostAPI string) ([]CatalogEntry, error)
	Download(ctx context.Context, id, version string) ([]byte, plugins.Signature, error)
	Redeem(ctx context.Context, code, pluginID, orgID, instanceID string) (json.RawMessage, error)
	// Recalls reports recalls of installed versions (id -> version).
	Recalls(ctx context.Context, installed map[string]string) (map[string]Recall, error)
}

// Recall marks a plugin version the store pulled back.
type Recall struct {
	Level string `json:"level"` // warning | critical
	Note  string `json:"note,omitempty"`
}

// CatalogEntry is a plugin offered by the store (newest compatible version).
type CatalogEntry struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Version     string          `json:"version"`
	Summary     string          `json:"summary"`
	Author      string          `json:"author"`
	Verified    bool            `json:"verified"` // also signed by the author
	Icon        string          `json:"icon,omitempty"`
	Permissions []string        `json:"permissions"`
	License     json.RawMessage `json:"license,omitempty"`
	SHA256      string          `json:"sha256"`
	Recall      string          `json:"recall,omitempty"` // warning | critical
	RecallNote  string          `json:"recallNote,omitempty"`
	// Filled by the instance:
	Installed string `json:"installed,omitempty"` // installed version
}

const (
	pluginKVKeys  = 1000
	settingInstID = "instance_id"
	// JobPluginEvent delivers an event to a plugin backend.
	JobPluginEvent = "plugin.event"
)

// pluginCache keeps parsed packages by content hash.
type pluginCache struct {
	mu   sync.Mutex
	pkgs map[string]*plugins.Package
}

func (s *Service) pluginsOn() bool { return s.Plugins.Runtime != nil }

func errPluginsOff() error {
	return invalid("validation.plugins_off", nil, "plugins are switched off on this server")
}

// pkg loads a package from the blob store (cached).
func (s *Service) pkg(ctx context.Context, sha string) (*plugins.Package, error) {
	s.plugCache.mu.Lock()
	defer s.plugCache.mu.Unlock()
	if p, ok := s.plugCache.pkgs[sha]; ok {
		return p, nil
	}
	if s.Blobs == nil {
		return nil, errors.New("no blob store")
	}
	rc, err := s.Blobs.Open(ctx, sha)
	if err != nil {
		return nil, fmt.Errorf("plugin package %s: %w", sha, err)
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, plugins.MaxPackage+1))
	if err != nil {
		return nil, err
	}
	p, err := plugins.ReadPackage(data)
	if err != nil {
		return nil, err
	}
	if p.SHA256 != sha {
		return nil, fmt.Errorf("plugin package %s: content does not match its hash", sha)
	}
	if s.plugCache.pkgs == nil {
		s.plugCache.pkgs = map[string]*plugins.Package{}
	}
	s.plugCache.pkgs[sha] = p
	return p, nil
}

func (s *Service) adminOnly(a Actor) error {
	if !a.User.IsAdmin || a.Via == "mcp" || a.Restricted() || a.Plugin != "" {
		return ErrForbidden
	}
	return nil
}

// PluginInfo is an installed plugin as admins see it.
type PluginInfo struct {
	store.Plugin
	Description  string                `json:"description,omitempty"`
	Permissions  []string              `json:"permissions"`
	Kinds        []string              `json:"kinds"`
	Tools        []string              `json:"tools"`
	Events       []string              `json:"events"`
	Secrets      []string              `json:"secrets"`
	Frontend     bool                  `json:"frontend"`
	Backend      bool                  `json:"backend"`
	CriticalPath bool                  `json:"criticalPath"`
	License      LicenseStatus         `json:"license"`
	Projects     int                   `json:"projects"`
	Problem      string                `json:"problem,omitempty"` // manifest | incompatible
	Needs        string                `json:"needs,omitempty"`   // engines.phoedox when incompatible
	Terms        json.RawMessage       `json:"terms,omitempty"`
	Signed       bool                  `json:"signed"`
	Update       *CatalogEntry         `json:"update,omitempty"`
	ProjectsList []store.ProjectPlugin `json:"-"`
}

func (s *Service) pluginInfo(ctx context.Context, row store.Plugin) PluginInfo {
	info := PluginInfo{Plugin: row, Permissions: []string{}, Kinds: []string{}, Tools: []string{}, Events: []string{}, Secrets: []string{}, Signed: row.SignedBy != ""}
	m, err := plugins.ParseManifest([]byte(row.Manifest))
	if err != nil {
		info.Problem = "manifest"
		return info
	}
	info.Description, info.CriticalPath, info.Terms = m.Description, m.CriticalPath, m.License
	info.Permissions = append(info.Permissions, m.Permissions...)
	for _, k := range m.Contributes.Kinds {
		info.Kinds = append(info.Kinds, k.ID)
	}
	for _, t := range m.Contributes.MCPTools {
		info.Tools = append(info.Tools, t.Name)
	}
	info.Events = append(info.Events, m.Contributes.Events...)
	info.Secrets = append(info.Secrets, m.Secrets()...)
	info.Frontend, info.Backend = m.Frontend != nil, m.Backend != nil
	if err := m.Supported(s.Plugins.Version); err != nil {
		info.Problem, info.Needs = "incompatible", m.Engines.Phoedox
	}
	info.License = s.licenseStatus(ctx, row, m)
	if pp, err := s.DB.PluginProjects(ctx, row.ID); err == nil {
		info.Projects = len(pp)
	}
	return info
}

// InstalledPlugins lists the plugins of the instance (admins).
func (s *Service) InstalledPlugins(ctx context.Context, a Actor) ([]PluginInfo, error) {
	if err := s.adminOnly(a); err != nil {
		return nil, err
	}
	rows, err := s.DB.Plugins(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]PluginInfo, 0, len(rows))
	for _, r := range rows {
		out = append(out, s.pluginInfo(ctx, r))
	}
	return out, nil
}

// InstallPlugin installs or updates a plugin from a package. Uploaded
// packages need a trusted signature unless AllowUnsigned is set.
func (s *Service) InstallPlugin(ctx context.Context, a Actor, data []byte, sig *plugins.Signature, source string) (PluginInfo, error) {
	if err := s.adminOnly(a); err != nil {
		return PluginInfo{}, err
	}
	if !s.pluginsOn() {
		return PluginInfo{}, errPluginsOff()
	}
	p, err := plugins.ReadPackage(data)
	if err != nil {
		return PluginInfo{}, invalid("validation.plugin_package", map[string]any{"reason": strings.TrimPrefix(err.Error(), plugins.ErrPackage.Error()+": ")}, "%v", err)
	}
	m := p.Manifest
	if err := m.Supported(s.Plugins.Version); err != nil {
		return PluginInfo{}, invalid("validation.plugin_incompatible", map[string]any{"reason": err.Error()}, "%v", err)
	}
	signedBy := ""
	if sig != nil && sig.Sig != "" {
		if err := plugins.Verify(s.Plugins.Keys, m.ID, m.Version, p.SHA256, *sig); err != nil {
			return PluginInfo{}, invalid("validation.plugin_signature", nil, "the package signature is not valid")
		}
		signedBy = sig.KeyID
	} else if !s.Plugins.AllowUnsigned {
		return PluginInfo{}, invalid("validation.plugin_unsigned", nil, "the package is not signed by the store; unsigned plugins need PHOEDOX_PLUGINS_ALLOW_UNSIGNED=true")
	}
	if err := s.putBlob(ctx, p.SHA256, data); err != nil {
		return PluginInfo{}, err
	}
	old, err := s.DB.GetPlugin(ctx, m.ID)
	row := store.Plugin{ID: m.ID, Version: m.Version, Name: m.Name, Manifest: string(p.Files[plugins.ManifestFile]), SHA256: p.SHA256, Size: p.Size,
		Source: source, SignedBy: signedBy, Enabled: true, InstalledBy: a.User.ID}
	switch {
	case err == nil:
		row.Enabled, row.License, row.Latest, row.InstalledAt = old.Enabled, old.License, old.Latest, old.InstalledAt
		if old.SHA256 != p.SHA256 {
			defer s.freePluginBlob(ctx, old.SHA256)
		}
	case !errors.Is(err, store.ErrNotFound):
		return PluginInfo{}, err
	}
	if err := s.DB.SavePlugin(ctx, row); err != nil {
		return PluginInfo{}, err
	}
	s.plugCache.mu.Lock()
	if s.plugCache.pkgs == nil {
		s.plugCache.pkgs = map[string]*plugins.Package{}
	}
	s.plugCache.pkgs[p.SHA256] = p
	s.plugCache.mu.Unlock()
	s.event(ctx, a, "plugin.install", m.ID, map[string]any{"version": m.Version, "source": source, "signedBy": signedBy, "permissions": m.Permissions})
	s.pluginsChanged()
	row, _ = s.DB.GetPlugin(ctx, m.ID)
	return s.pluginInfo(ctx, row), nil
}

func (s *Service) putBlob(ctx context.Context, sha string, data []byte) error {
	if s.Blobs == nil {
		return errors.New("no blob store")
	}
	f, err := os.CreateTemp(s.TempDir, "plugin-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return s.Blobs.Put(ctx, sha, f.Name(), int64(len(data)))
}

func (s *Service) freePluginBlob(ctx context.Context, sha string) {
	sys := store.System(ctx)
	if used, err := s.DB.PluginHashInUse(sys, sha); err != nil || used {
		return
	}
	if used, err := s.DB.HashInUse(sys, sha); err != nil || used {
		return
	}
	s.plugCache.mu.Lock()
	delete(s.plugCache.pkgs, sha)
	s.plugCache.mu.Unlock()
	if s.Plugins.Runtime != nil {
		s.Plugins.Runtime.Forget(ctx, sha)
	}
	if err := s.Blobs.Delete(ctx, sha); err != nil {
		slog.Warn("deleting a plugin package failed", "sha256", sha, "err", err)
	}
}

// SetPluginEnabled switches a plugin on or off for the whole instance.
func (s *Service) SetPluginEnabled(ctx context.Context, a Actor, id string, on bool) (PluginInfo, error) {
	if err := s.adminOnly(a); err != nil {
		return PluginInfo{}, err
	}
	row, err := s.DB.GetPlugin(ctx, id)
	if err != nil {
		return PluginInfo{}, err
	}
	row.Enabled = on
	if err := s.DB.SavePlugin(ctx, row); err != nil {
		return PluginInfo{}, err
	}
	s.event(ctx, a, "plugin.enable", id, map[string]bool{"enabled": on})
	s.pluginsChanged()
	return s.pluginInfo(ctx, row), nil
}

// UninstallPlugin removes a plugin with its project settings and data.
func (s *Service) UninstallPlugin(ctx context.Context, a Actor, id string) error {
	if err := s.adminOnly(a); err != nil {
		return err
	}
	row, err := s.DB.GetPlugin(ctx, id)
	if err != nil {
		return err
	}
	if err := s.DB.DeletePlugin(ctx, id); err != nil {
		return err
	}
	s.freePluginBlob(ctx, row.SHA256)
	s.event(ctx, a, "plugin.uninstall", id, map[string]string{"version": row.Version})
	s.pluginsChanged()
	return nil
}

// PluginLogs returns recent log lines of a plugin (admins).
func (s *Service) PluginLogs(ctx context.Context, a Actor, id string) ([]plugins.LogLine, error) {
	if err := s.adminOnly(a); err != nil {
		return nil, err
	}
	if _, err := s.DB.GetPlugin(ctx, id); err != nil {
		return nil, err
	}
	if s.Plugins.Runtime == nil {
		return []plugins.LogLine{}, nil
	}
	return s.Plugins.Runtime.Logs(logKey(ctx, id)), nil
}

func logKey(ctx context.Context, id string) string { return store.TenantOf(ctx) + "/" + id }

// pluginsChanged tells listeners (MCP tools) that the set of plugins changed.
func (s *Service) pluginsChanged() {
	s.pluginGen.Add(1)
}

// PluginGeneration changes whenever plugins are installed, removed or
// switched; the MCP server rebuilds its tool list when it changes.
func (s *Service) PluginGeneration() int64 { return s.pluginGen.Load() }

// --- per project ---

// ProjectPluginInfo is a plugin as a project sees it.
type ProjectPluginInfo struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Version     string          `json:"version"`
	Description string          `json:"description,omitempty"`
	Enabled     bool            `json:"enabled"`
	Permissions []string        `json:"permissions,omitempty"`
	Kinds       []string        `json:"kinds"`
	Tools       []string        `json:"tools"`
	Frontend    string          `json:"frontend,omitempty"` // module URL path
	Secrets     map[string]bool `json:"secrets,omitempty"`  // name -> set
	LastRun     store.Time      `json:"lastRun"`
	LastError   string          `json:"lastError,omitempty"`
	Problem     string          `json:"problem,omitempty"`
	Recall      string          `json:"recall,omitempty"`
}

type activePlugin struct {
	row store.Plugin
	pp  store.ProjectPlugin
	pkg *plugins.Package
}

// usable tells why an installed plugin may not run ("" = it may).
func (s *Service) usable(ctx context.Context, row store.Plugin, m plugins.Manifest) string {
	switch {
	case !row.Enabled:
		return "disabled"
	case row.Recall == "critical" && !m.CriticalPath:
		return "recalled"
	}
	if err := m.Supported(s.Plugins.Version); err != nil {
		return "incompatible"
	}
	if st := s.licenseStatus(ctx, row, m); st.Required && !st.Valid {
		return "license"
	}
	return ""
}

// activePlugins returns the plugins enabled for a project that may run.
func (s *Service) activePlugins(ctx context.Context, projectID string) ([]activePlugin, error) {
	if !s.pluginsOn() {
		return nil, nil
	}
	pps, err := s.DB.ProjectPlugins(ctx, projectID)
	if err != nil || len(pps) == 0 {
		return nil, err
	}
	var out []activePlugin
	for _, pp := range pps {
		row, err := s.DB.GetPlugin(ctx, pp.PluginID)
		if err != nil {
			continue
		}
		p, err := s.pkg(ctx, row.SHA256)
		if err != nil {
			slog.Warn("loading a plugin failed", "plugin", row.ID, "err", err)
			continue
		}
		if s.usable(ctx, row, p.Manifest) != "" {
			continue
		}
		out = append(out, activePlugin{row: row, pp: pp, pkg: p})
	}
	return out, nil
}

func (s *Service) activePlugin(ctx context.Context, projectID, id string) (activePlugin, error) {
	list, err := s.activePlugins(ctx, projectID)
	if err != nil {
		return activePlugin{}, err
	}
	for _, ap := range list {
		if ap.row.ID == id {
			return ap, nil
		}
	}
	return activePlugin{}, ErrNotFound
}

// ProjectPlugins lists the plugins a project can use. Owners see all
// installed plugins with their settings; others see the enabled ones.
func (s *Service) ProjectPlugins(ctx context.Context, a Actor, ref string) ([]ProjectPluginInfo, error) {
	p, err := s.project(ctx, a, ref, store.RoleReader)
	if err != nil {
		return nil, err
	}
	out := []ProjectPluginInfo{}
	if !s.pluginsOn() {
		return out, nil
	}
	rows, err := s.DB.Plugins(ctx)
	if err != nil {
		return nil, err
	}
	pps, err := s.DB.ProjectPlugins(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	owner := p.Role == store.RoleOwner
	for _, row := range rows {
		i := slices.IndexFunc(pps, func(pp store.ProjectPlugin) bool { return pp.PluginID == row.ID })
		pkg, err := s.pkg(ctx, row.SHA256)
		if err != nil {
			continue
		}
		m := pkg.Manifest
		problem := s.usable(ctx, row, m)
		if !owner && (i < 0 || problem != "") {
			continue
		}
		if owner && problem == "disabled" && i < 0 {
			continue // switched off by the admin: not offered
		}
		info := ProjectPluginInfo{ID: row.ID, Name: row.Name, Version: row.Version, Description: m.Description, Enabled: i >= 0,
			Kinds: []string{}, Tools: []string{}, Problem: problem, Recall: row.Recall}
		for _, k := range m.Contributes.Kinds {
			info.Kinds = append(info.Kinds, k.ID)
		}
		for _, t := range m.Contributes.MCPTools {
			info.Tools = append(info.Tools, t.Name)
		}
		if m.Frontend != nil && problem == "" {
			info.Frontend = "/api/v1/plugins/" + row.ID + "/" + row.Version + "/files/" + strings.TrimPrefix(m.Frontend.Module, "./")
		}
		if owner {
			info.Permissions = m.Permissions
			info.Secrets = map[string]bool{}
			var set map[string]string
			if i >= 0 {
				set, _ = s.pluginSecrets(pps[i])
				info.LastRun, info.LastError = pps[i].LastRun, pps[i].LastError
			}
			for _, name := range m.Secrets() {
				info.Secrets[name] = set[name] != ""
			}
		}
		out = append(out, info)
	}
	return out, nil
}

// SetProjectPlugin enables or disables a plugin for a project (owners).
func (s *Service) SetProjectPlugin(ctx context.Context, a Actor, ref, id string, on bool) error {
	p, err := s.project(ctx, a, ref, store.RoleOwner)
	if err != nil {
		return err
	}
	if a.Via == "mcp" || a.Plugin != "" {
		return ErrForbidden
	}
	if !s.pluginsOn() {
		return errPluginsOff()
	}
	row, err := s.DB.GetPlugin(ctx, id)
	if err != nil {
		return err
	}
	if on {
		pkg, err := s.pkg(ctx, row.SHA256)
		if err != nil {
			return err
		}
		switch s.usable(ctx, row, pkg.Manifest) {
		case "":
		case "license":
			return invalid("validation.plugin_license", nil, "this plugin needs a license; the administrator can add one")
		default:
			return invalid("validation.plugin_unusable", nil, "this plugin cannot be enabled right now")
		}
		if err := s.DB.EnablePlugin(ctx, p.ID, id, a.User.ID); err != nil {
			return err
		}
	} else if err := s.DB.DisablePlugin(ctx, p.ID, id); err != nil {
		return err
	}
	s.event(ctx, a, "plugin.project", p.ID, map[string]any{"plugin": id, "enabled": on})
	s.pluginsChanged()
	return nil
}

func (s *Service) pluginSecrets(pp store.ProjectPlugin) (map[string]string, error) {
	out := map[string]string{}
	if pp.Secrets == "" || s.Box == nil {
		return out, nil
	}
	raw, err := s.Box.Open(pp.Secrets, "plugin:"+pp.ProjectID+":"+pp.PluginID)
	if err != nil {
		return out, err
	}
	return out, json.Unmarshal(raw, &out)
}

// SetPluginSecrets stores secrets of a plugin in a project (owners). Only
// declared names are accepted; an empty value removes a secret. Values are
// never returned.
func (s *Service) SetPluginSecrets(ctx context.Context, a Actor, ref, id string, values map[string]string) error {
	p, err := s.project(ctx, a, ref, store.RoleOwner)
	if err != nil {
		return err
	}
	if a.Via == "mcp" || a.Plugin != "" {
		return ErrForbidden
	}
	if s.Box == nil {
		return errors.New("no master key configured")
	}
	pp, err := s.DB.GetProjectPlugin(ctx, p.ID, id)
	if err != nil {
		return err
	}
	row, err := s.DB.GetPlugin(ctx, id)
	if err != nil {
		return err
	}
	pkg, err := s.pkg(ctx, row.SHA256)
	if err != nil {
		return err
	}
	cur, _ := s.pluginSecrets(pp)
	for name, v := range values {
		if !pkg.Manifest.CanSecret(name) {
			return invalid("validation.plugin_secret", map[string]any{"name": name}, "the plugin does not ask for a secret %q", name)
		}
		if len(v) > 4096 {
			return invalid("validation.too_long", map[string]any{"field": name, "max": 4096}, "secret too long")
		}
		if v == "" {
			delete(cur, name)
		} else {
			cur[name] = v
		}
	}
	raw, _ := json.Marshal(cur)
	sealed, err := s.Box.Seal(raw, "plugin:"+p.ID+":"+id)
	if err != nil {
		return err
	}
	if err := s.DB.SetPluginSecrets(ctx, p.ID, id, sealed); err != nil {
		return err
	}
	names := make([]string, 0, len(values))
	for n := range values {
		names = append(names, n)
	}
	s.event(ctx, a, "plugin.secrets", p.ID, map[string]any{"plugin": id, "names": names})
	return nil
}

// PluginFile serves a file of an installed plugin's package (frontend
// modules). Every signed-in user may load the files of enabled plugins.
func (s *Service) PluginFile(ctx context.Context, a Actor, id, version, name string) ([]byte, error) {
	if !s.pluginsOn() || a.User.ID == "" {
		return nil, ErrNotFound
	}
	row, err := s.DB.GetPlugin(ctx, id)
	if err != nil || row.Version != version || !row.Enabled {
		return nil, ErrNotFound
	}
	pkg, err := s.pkg(ctx, row.SHA256)
	if err != nil {
		return nil, err
	}
	if name == plugins.ManifestFile || (pkg.Manifest.Backend != nil && strings.TrimPrefix(name, "./") == strings.TrimPrefix(pkg.Manifest.Backend.Wasm, "./")) {
		return nil, ErrNotFound
	}
	b, ok := pkg.File(name)
	if !ok {
		return nil, ErrNotFound
	}
	return b, nil
}

// --- calls ---

// PluginTool runs a tool a plugin contributes, for the calling user.
func (s *Service) PluginTool(ctx context.Context, a Actor, ref, pluginID, tool string, args json.RawMessage) (json.RawMessage, error) {
	p, err := s.project(ctx, a, ref, store.RoleReader)
	if err != nil {
		return nil, err
	}
	ap, err := s.activePlugin(ctx, p.ID, pluginID)
	if err != nil {
		return nil, err
	}
	i := slices.IndexFunc(ap.pkg.Manifest.Contributes.MCPTools, func(t plugins.ToolDef) bool { return t.Name == tool })
	if i < 0 {
		return nil, ErrNotFound
	}
	def := ap.pkg.Manifest.Contributes.MCPTools[i]
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	if def.Control {
		// Acting on the physical world needs edit rights; AI clients only
		// ever propose it, a person confirms in the suggestions.
		if _, err := s.project(ctx, a, ref, store.RoleEditor); err != nil {
			return nil, err
		}
		if a.Via == "mcp" {
			if p.AIWriteMode == AIOff || p.AIWriteMode == AIRead {
				return nil, ErrForbidden
			}
			summary := fmt.Sprintf("%s: %s %s", ap.pkg.Manifest.Name, tool, compactJSON(args, 160))
			b, _ := json.Marshal(opPayload{Project: p.ID, Plugin: pluginID, Tool: tool, Args: args})
			sug, err := s.DB.CreateSuggestion(ctx, store.Suggestion{ID: newID(), ProjectID: p.ID, Actor: a.String(), Op: "plugin.call", Summary: summary, Payload: b})
			if err != nil {
				return nil, err
			}
			s.event(ctx, a, "suggestion.create", sug.ID, map[string]string{"op": "plugin.call", "tool": tool})
			return json.Marshal(map[string]any{"applied": false, "suggestion": sug,
				"note": "This tool acts on real devices. A person must confirm it in Phoedox (Suggestions) before it runs."})
		}
	}
	pa := a
	pa.Plugin = pluginID
	out, err := s.Plugins.Runtime.CallWithin(ctx, ap.pkg, logKey(ctx, pluginID), "tool", map[string]any{"name": tool, "args": args, "project": p.Slug}, s.pluginEnv(pa, p, ap),
		time.Duration(def.TimeoutSec)*time.Second)
	if def.Control {
		s.event(ctx, a, "plugin.control", pluginID, map[string]any{"tool": tool, "args": args, "ok": err == nil})
	}
	if err != nil {
		return nil, invalid("validation.plugin_failed", map[string]any{"plugin": pluginID}, "%v", err)
	}
	return out, nil
}

// compactJSON shortens arguments for a one-line summary.
func compactJSON(raw json.RawMessage, max int) string {
	var buf bytes.Buffer
	if json.Compact(&buf, raw) != nil {
		return ""
	}
	out := buf.String()
	if len(out) > max {
		out = out[:max] + "…"
	}
	return out
}

// PluginEvent is what on_event receives.
type PluginEvent struct {
	Type    string          `json:"type"` // "schedule" or an event type like entity.saved
	Project string          `json:"project"`
	Subject string          `json:"subject,omitempty"`
	Actor   string          `json:"actor,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
	At      time.Time       `json:"at"`
}

type pluginJob struct {
	Project string      `json:"project"`
	Plugin  string      `json:"plugin"`
	Event   PluginEvent `json:"event"`
}

// emitPluginEvents queues events for plugins of the project that subscribed.
// Changes a plugin made itself are not sent back to it.
func (s *Service) emitPluginEvents(ctx context.Context, a Actor, kind, subject string, payload any) {
	typ, ok := webhookEvents[kind]
	if !ok || !s.pluginsOn() {
		return
	}
	p, err := s.DB.ProjectOfObject(ctx, subject)
	if err != nil {
		return
	}
	list, err := s.activePlugins(ctx, p.ID)
	if err != nil || len(list) == 0 {
		return
	}
	data, _ := json.Marshal(payload)
	for _, ap := range list {
		if ap.row.ID == a.Plugin || ap.pkg.Manifest.Backend == nil || !ap.pkg.Manifest.Subscribes(typ) {
			continue
		}
		s.queuePluginEvent(ctx, pluginJob{Project: p.ID, Plugin: ap.row.ID, Event: PluginEvent{Type: typ, Project: p.Slug, Subject: subject, Actor: a.String(), Data: data, At: time.Now().UTC()}})
	}
}

func (s *Service) queuePluginEvent(ctx context.Context, j pluginJob) {
	if s.Jobs == nil {
		if err := s.runPluginEvent(ctx, j); err != nil {
			slog.Warn("plugin event failed", "plugin", j.Plugin, "err", err)
		}
		return
	}
	if err := s.Jobs.Add(ctx, JobPluginEvent, j); err != nil {
		slog.Warn("queueing a plugin event failed", "plugin", j.Plugin, "err", err)
	}
}

// runPluginEvent calls on_event as the user who enabled the plugin; if that
// user lost access, the event is dropped.
func (s *Service) runPluginEvent(ctx context.Context, j pluginJob) error {
	ap, err := s.activePlugin(ctx, j.Project, j.Plugin)
	if err != nil {
		return nil // disabled meanwhile
	}
	u, err := s.DB.UserByID(ctx, ap.pp.EnabledBy)
	if err != nil || u.Disabled {
		_ = s.DB.PluginRan(ctx, j.Project, j.Plugin, "the user who enabled the plugin no longer exists")
		return nil
	}
	a := Actor{User: u, Via: "plugin", Plugin: j.Plugin}
	p, err := s.project(ctx, a, j.Project, store.RoleReader)
	if err != nil {
		_ = s.DB.PluginRan(ctx, j.Project, j.Plugin, "the user who enabled the plugin no longer has access")
		return nil
	}
	_, err = s.Plugins.Runtime.Call(ctx, ap.pkg, logKey(ctx, j.Plugin), "on_event", j.Event, s.pluginEnv(a, p, ap))
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	_ = s.DB.PluginRan(ctx, j.Project, j.Plugin, msg)
	return nil // plugin errors are logged, not retried
}

// RunPluginSchedules queues "schedule" events that are due. Call it every
// few seconds with a System context; it covers all tenants.
func (s *Service) RunPluginSchedules(ctx context.Context, now time.Time) {
	if !s.pluginsOn() {
		return
	}
	all, err := s.DB.AllProjectPlugins(ctx)
	if err != nil {
		slog.Warn("plugin schedules", "err", err)
		return
	}
	for _, tp := range all {
		tctx := store.WithTenant(ctx, tp.TenantID)
		row, err := s.DB.GetPlugin(tctx, tp.PluginID)
		if err != nil {
			continue
		}
		pkg, err := s.pkg(tctx, row.SHA256)
		if err != nil || pkg.Manifest.Backend == nil {
			continue
		}
		every := pkg.Manifest.Schedule()
		if every == 0 || s.usable(tctx, row, pkg.Manifest) != "" {
			continue
		}
		key := tp.ProjectID + "/" + tp.PluginID
		s.schedMu.Lock()
		if s.schedLast == nil {
			s.schedLast = map[string]time.Time{}
		}
		last, ok := s.schedLast[key]
		if !ok && !tp.LastRun.IsZero() {
			last = tp.LastRun.Time
		}
		due := now.Sub(last) >= every
		if due {
			s.schedLast[key] = now
		}
		s.schedMu.Unlock()
		if !due {
			continue
		}
		p, err := s.DB.ProjectFor(tctx, tp.ProjectID, "")
		if err != nil {
			continue
		}
		s.queuePluginEvent(tctx, pluginJob{Project: tp.ProjectID, Plugin: tp.PluginID, Event: PluginEvent{Type: "schedule", Project: p.Slug, At: now.UTC()}})
	}
}

func (s *Service) registerPluginJobs() {
	s.Jobs.Handle(JobPluginEvent, func(ctx context.Context, payload json.RawMessage) error {
		var j pluginJob
		if err := json.Unmarshal(payload, &j); err != nil {
			return err
		}
		return s.runPluginEvent(ctx, j)
	})
}

// --- licenses ---

// LicenseStatus tells whether a plugin needs and has a license.
type LicenseStatus struct {
	Required bool       `json:"required"` // the plugin cannot run without one
	Valid    bool       `json:"valid"`
	Plan     string     `json:"plan,omitempty"`
	Expires  *time.Time `json:"expires,omitempty"`
	TrialEnd *time.Time `json:"trialEnd,omitempty"`
	Problem  string     `json:"problem,omitempty"`
	Hint     string     `json:"hint,omitempty"` // "commercial": commercial use needs a license
}

type licenseTerms struct {
	Paid *struct {
		Plan      string `json:"plan"`
		TrialDays int    `json:"trialDays"`
	} `json:"paid"`
	Commercial *struct {
		Plan      string `json:"plan"`
		TrialDays int    `json:"trialDays"`
	} `json:"commercial"`
}

// LicenseFile is a signed license from the store (phoedox-store:
// internal/license).
type LicenseFile struct {
	License struct {
		ID         string    `json:"id"`
		PluginID   string    `json:"pluginId"`
		Plan       string    `json:"plan"`
		Kind       string    `json:"kind"`
		OrgID      string    `json:"orgId"`
		InstanceID string    `json:"instanceId"`
		IssuedAt   time.Time `json:"issuedAt"`
		Expires    time.Time `json:"expires"`
	} `json:"license"`
	KeyID     string `json:"keyId"`
	Signature string `json:"signature"`
}

func (s *Service) licenseStatus(ctx context.Context, row store.Plugin, m plugins.Manifest) LicenseStatus {
	var terms licenseTerms
	_ = json.Unmarshal(m.License, &terms)
	st := LicenseStatus{Required: terms.Paid != nil}
	if terms.Commercial != nil {
		st.Hint = "commercial"
	}
	if row.License != "" {
		var f LicenseFile
		if err := json.Unmarshal([]byte(row.License), &f); err != nil {
			st.Problem = "invalid"
		} else if err := s.verifyLicense(ctx, f, row.ID); err != nil {
			st.Problem = err.Error()
		} else {
			st.Valid, st.Plan = true, f.License.Plan
			if !f.License.Expires.IsZero() {
				st.Expires = &f.License.Expires
			}
		}
	}
	if st.Required && !st.Valid && terms.Paid.TrialDays > 0 {
		end := row.InstalledAt.Add(time.Duration(terms.Paid.TrialDays) * 24 * time.Hour)
		st.TrialEnd = &end
		if time.Now().Before(end) {
			st.Valid = true
		}
	}
	return st
}

func (s *Service) verifyLicense(ctx context.Context, f LicenseFile, pluginID string) error {
	pub, ok := s.Plugins.Keys[f.KeyID]
	if !ok {
		return errors.New("signature")
	}
	sig, err := decodeB64(f.Signature)
	if err != nil {
		return errors.New("signature")
	}
	msg, _ := json.Marshal(f.License)
	if !ed25519.Verify(pub, msg, sig) {
		return errors.New("signature")
	}
	inst, err := s.InstanceID(ctx)
	if err != nil {
		return err
	}
	if f.License.PluginID != pluginID || f.License.OrgID != store.TenantOf(ctx) || f.License.InstanceID != inst {
		return errors.New("binding")
	}
	if !f.License.Expires.IsZero() && !time.Now().Before(f.License.Expires) {
		return errors.New("expired")
	}
	return nil
}

// InstanceID identifies this installation (per tenant) for licenses.
func (s *Service) InstanceID(ctx context.Context) (string, error) {
	id, err := s.DB.Setting(ctx, settingInstID, "")
	if err != nil || id != "" {
		return id, err
	}
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	id = hex.EncodeToString(b)
	if err := s.DB.SetSetting(ctx, settingInstID, id, "system"); err != nil {
		return "", err
	}
	return id, nil
}

// SetPluginLicense stores a license file (pasted or redeemed) after
// checking it.
func (s *Service) SetPluginLicense(ctx context.Context, a Actor, id string, file json.RawMessage) (PluginInfo, error) {
	if err := s.adminOnly(a); err != nil {
		return PluginInfo{}, err
	}
	row, err := s.DB.GetPlugin(ctx, id)
	if err != nil {
		return PluginInfo{}, err
	}
	var f LicenseFile
	if err := json.Unmarshal(file, &f); err != nil {
		return PluginInfo{}, invalid("validation.plugin_license_file", map[string]any{"reason": "json"}, "not a license file")
	}
	if err := s.verifyLicense(ctx, f, id); err != nil {
		return PluginInfo{}, invalid("validation.plugin_license_file", map[string]any{"reason": err.Error()}, "license not valid: %v", err)
	}
	row.License = string(file)
	if err := s.DB.SavePlugin(ctx, row); err != nil {
		return PluginInfo{}, err
	}
	s.event(ctx, a, "plugin.license", id, map[string]string{"license": f.License.ID, "plan": f.License.Plan})
	return s.pluginInfo(ctx, row), nil
}

// RedeemPluginLicense trades a license code at the store for a license file.
func (s *Service) RedeemPluginLicense(ctx context.Context, a Actor, id, code string) (PluginInfo, error) {
	if err := s.adminOnly(a); err != nil {
		return PluginInfo{}, err
	}
	if s.Plugins.Store == nil {
		return PluginInfo{}, invalid("validation.store_off", nil, "no plugin store configured")
	}
	inst, err := s.InstanceID(ctx)
	if err != nil {
		return PluginInfo{}, err
	}
	file, err := s.Plugins.Store.Redeem(ctx, strings.TrimSpace(code), id, store.TenantOf(ctx), inst)
	if err != nil {
		return PluginInfo{}, invalid("validation.store_failed", map[string]any{"reason": err.Error()}, "store: %v", err)
	}
	return s.SetPluginLicense(ctx, a, id, file)
}

// --- store ---

// PluginCatalog lists what the store offers for this server.
func (s *Service) PluginCatalog(ctx context.Context, a Actor) ([]CatalogEntry, error) {
	if err := s.adminOnly(a); err != nil {
		return nil, err
	}
	if s.Plugins.Store == nil {
		return []CatalogEntry{}, nil
	}
	list, err := s.Plugins.Store.Catalog(ctx, s.Plugins.Version, plugins.HostAPI)
	if err != nil {
		return nil, invalid("validation.store_failed", map[string]any{"reason": err.Error()}, "store: %v", err)
	}
	rows, _ := s.DB.Plugins(ctx)
	for i := range list {
		for _, r := range rows {
			if r.ID == list[i].ID {
				list[i].Installed = r.Version
			}
		}
	}
	return list, nil
}

// InstallFromStore downloads, checks and installs a plugin version.
func (s *Service) InstallFromStore(ctx context.Context, a Actor, id, version string) (PluginInfo, error) {
	if err := s.adminOnly(a); err != nil {
		return PluginInfo{}, err
	}
	if s.Plugins.Store == nil {
		return PluginInfo{}, invalid("validation.store_off", nil, "no plugin store configured")
	}
	data, sig, err := s.Plugins.Store.Download(ctx, id, version)
	if err != nil {
		return PluginInfo{}, invalid("validation.store_failed", map[string]any{"reason": err.Error()}, "store: %v", err)
	}
	if sig.Sig == "" {
		return PluginInfo{}, invalid("validation.plugin_signature", nil, "the store sent no signature")
	}
	return s.InstallPlugin(ctx, a, data, &sig, "store")
}

// SyncPluginStore asks the store for updates and recalls of installed
// plugins (call it with a tenant context; the scheduler loops over tenants).
// Critically recalled plugins stop running unless they are on the critical
// path; admins decide on the rest.
func (s *Service) SyncPluginStore(ctx context.Context) error {
	if s.Plugins.Store == nil || !s.pluginsOn() {
		return nil
	}
	rows, err := s.DB.Plugins(ctx)
	if err != nil {
		return err
	}
	installed := map[string]string{}
	for _, r := range rows {
		if r.Source == "store" {
			installed[r.ID] = r.Version
		}
	}
	if len(installed) == 0 {
		return nil
	}
	list, err := s.Plugins.Store.Catalog(ctx, s.Plugins.Version, plugins.HostAPI)
	if err != nil {
		return err
	}
	recalls, err := s.Plugins.Store.Recalls(ctx, installed)
	if err != nil {
		return err
	}
	for _, r := range rows {
		if r.Source != "store" {
			continue
		}
		changed := false
		for _, e := range list {
			if e.ID == r.ID && e.Version != r.Latest && plugins.CompareVersions(e.Version, r.Version) > 0 {
				r.Latest, changed = e.Version, true
			}
		}
		rc := recalls[r.ID]
		if rc.Level != r.Recall || rc.Note != r.RecallNote {
			r.Recall, r.RecallNote, changed = rc.Level, rc.Note, true
			_ = s.DB.AddEvent(ctx, "system", "plugin.recall", r.ID, map[string]string{"version": r.Version, "level": rc.Level, "note": rc.Note})
		}
		if changed {
			if err := s.DB.SavePlugin(ctx, r); err != nil {
				return err
			}
			s.pluginsChanged()
		}
	}
	return nil
}

// --- plugin kinds in the data model ---

// pluginKinds returns kinds and relations contributed by active plugins.
func (s *Service) pluginKinds(ctx context.Context, projectID string) []Kind {
	list, err := s.activePlugins(ctx, projectID)
	if err != nil {
		return nil
	}
	var out []Kind
	for _, ap := range list {
		m := ap.pkg.Manifest
		for _, kd := range m.Contributes.Kinds {
			k := Kind{ID: kd.ID, Name: kd.ID, Group: "plugin", Icon: pascalIcon(kd.Icon), Source: "plugin:" + m.ID, Attrs: []Attr{}}
			if b, ok := ap.pkg.File(kd.Schema); ok && kd.Schema != "" {
				k.Name, k.Description, k.Attrs = schemaAttrs(b, kd.ID)
			}
			for _, r := range m.Contributes.Relations {
				if slices.Contains(r.From, kd.ID) {
					k.Relations = append(k.Relations, Relation{Rel: r.ID, To: r.To})
				}
			}
			out = append(out, k)
		}
	}
	return out
}

// pascalIcon turns a Lucide name ("battery-charging") into the export name.
func pascalIcon(name string) string {
	if name == "" {
		return "Puzzle"
	}
	var b strings.Builder
	for _, part := range strings.Split(name, "-") {
		if part != "" {
			b.WriteString(strings.ToUpper(part[:1]) + part[1:])
		}
	}
	return b.String()
}

// schemaAttrs reads attributes from a JSON Schema (top-level properties,
// in their order). Units come from "x-unit".
func schemaAttrs(raw []byte, fallback string) (name, desc string, attrs []Attr) {
	var sch struct {
		Title       string          `json:"title"`
		Description string          `json:"description"`
		Required    []string        `json:"required"`
		Properties  json.RawMessage `json:"properties"`
	}
	attrs = []Attr{}
	if json.Unmarshal(raw, &sch) != nil {
		return fallback, "", attrs
	}
	name, desc = sch.Title, sch.Description
	if name == "" {
		name = fallback
	}
	for _, kv := range orderedObject(sch.Properties) {
		var p struct {
			Type   any      `json:"type"`
			Format string   `json:"format"`
			Enum   []any    `json:"enum"`
			Title  string   `json:"title"`
			Unit   string   `json:"x-unit"`
			Items  struct{} `json:"items"`
		}
		if json.Unmarshal(kv.value, &p) != nil || !validName(kv.key) {
			continue
		}
		typ, _ := p.Type.(string)
		at := Attr{Name: kv.key, Label: p.Title, Unit: p.Unit, Required: slices.Contains(sch.Required, kv.key), Type: "text"}
		switch {
		case len(p.Enum) > 0:
			at.Type = "select"
			for _, e := range p.Enum {
				at.Options = append(at.Options, fmt.Sprint(e))
			}
		case typ == "number" || typ == "integer":
			at.Type = "number"
		case typ == "boolean":
			at.Type = "bool"
		case p.Format == "date": // date-time stays text (no time type yet)
			at.Type = "date"
		case p.Format == "ipv4" || p.Format == "ipv6":
			at.Type = "ip"
		case p.Format == "uri" || p.Format == "url":
			at.Type = "url"
		case typ == "array" || typ == "object":
			continue // not representable as a simple attribute
		}
		attrs = append(attrs, at)
	}
	return name, desc, attrs
}

type keyValue struct {
	key   string
	value json.RawMessage
}

// orderedObject returns the members of a JSON object in document order.
func orderedObject(raw json.RawMessage) []keyValue {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil
	}
	var out []keyValue
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return out
		}
		k, _ := t.(string)
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return out
		}
		out = append(out, keyValue{k, v})
	}
	return out
}

// PluginToolSpec is a tool a plugin offers to AI clients.
type PluginToolSpec struct {
	Plugin      string
	Name        string
	Description string
	Input       json.RawMessage // JSON Schema of the arguments (object) or nil
}

// PluginTools lists the tools of the tenant's usable plugins. Whether a
// plugin is enabled for a project is checked when a tool runs.
func (s *Service) PluginTools(ctx context.Context) []PluginToolSpec {
	if !s.pluginsOn() {
		return nil
	}
	rows, err := s.DB.Plugins(ctx)
	if err != nil {
		return nil
	}
	var out []PluginToolSpec
	for _, row := range rows {
		pkg, err := s.pkg(ctx, row.SHA256)
		if err != nil || s.usable(ctx, row, pkg.Manifest) != "" || pkg.Manifest.Backend == nil {
			continue
		}
		for _, t := range pkg.Manifest.Contributes.MCPTools {
			spec := PluginToolSpec{Plugin: row.ID, Name: t.Name, Description: t.Description}
			if spec.Description == "" {
				spec.Description = "Tool of the plugin " + row.Name + "."
			}
			if t.Control {
				spec.Description += " Acts on real devices: your call becomes a suggestion a person must confirm in Phoedox."
			}
			if b, ok := pkg.File(t.Input); ok && t.Input != "" {
				spec.Input = b
			}
			out = append(out, spec)
		}
	}
	return out
}
