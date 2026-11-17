package core

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"slices"
	"strings"

	"github.com/phoedox/phoedox/internal/store"
)

// Block kinds that left the core for official plugins (ADR-0087). Content
// that uses them keeps working (ADR-0088): a background job installs the
// plugin from the store and enables it where the kind is used; without the
// store the block shows which plugin it needs.

// MovedBlock is a block kind an official plugin draws now.
type MovedBlock struct {
	Plugin string `json:"plugin"`
	Label  string `json:"label"`
}

// movedBlocks maps each moved kind to its plugin.
var movedBlocks = map[string]MovedBlock{
	"gantt": {Plugin: "com.phoedox.gantt", Label: "Schedule (Gantt)"},
}

// MissingPlugin tells a client which plugin a block needs.
type MissingPlugin struct {
	Plugin string `json:"plugin"`
	// Installed: the plugin is on the instance, so project owners can enable it.
	Installed bool `json:"installed"`
	// Store: the instance has a plugin store, so admins can install it.
	Store bool `json:"store"`
}

const movedBlocksSetting = "plugins.moved_blocks"

// movedState remembers what the job did, so that an admin's later choice
// (disabling or uninstalling the plugin) is not undone.
type movedState struct {
	Installed []string `json:"installed,omitempty"` // plugins installed by the job
	Enabled   []string `json:"enabled,omitempty"`   // "plugin|project" pairs enabled by the job
}

// missingPlugin is set when kind moved to a plugin that is not active in
// the project (nil otherwise).
func (s *Service) missingPlugin(ctx context.Context, kind string) *MissingPlugin {
	mb, ok := movedBlocks[kind]
	if !ok {
		return nil
	}
	m := &MissingPlugin{Plugin: mb.Plugin, Store: s.Plugins.Store != nil && s.pluginsOn()}
	if s.pluginsOn() {
		if row, err := s.DB.GetPlugin(ctx, mb.Plugin); err == nil {
			if pkg, err := s.pkg(ctx, row.SHA256); err == nil && s.usable(ctx, row, pkg.Manifest) == "" {
				m.Installed = true
			}
		}
	}
	return m
}

// missingBlockResult is what drawing a moved kind gives without its plugin;
// older clients show the problem.
func (s *Service) missingBlockResult(ctx context.Context, kind string) PluginBlockResult {
	m := s.missingPlugin(ctx, kind)
	return PluginBlockResult{Missing: m, Problems: []BlockProblem{{Kind: kind, Line: 1, Severity: "error",
		Message: "this block needs the plugin " + movedBlocks[kind].Label + " (" + m.Plugin + ")"}}}
}

// usedKinds returns the moved kinds used anywhere in a document's content:
// fenced blocks in any text (pages, translations, decisions) and block
// elements of whiteboards.
func usedMovedKinds(content []byte) map[string]bool {
	var v any
	if json.Unmarshal(content, &v) != nil {
		return nil
	}
	out := map[string]bool{}
	var walk func(any)
	walk = func(x any) {
		switch t := x.(type) {
		case string:
			if strings.Contains(t, "```") || strings.Contains(t, "~~~") {
				for _, b := range fencedBlocks(t) {
					if _, ok := movedBlocks[b.Lang]; ok {
						out[b.Lang] = true
					}
				}
			}
		case map[string]any:
			if t["type"] == "block" {
				if k, _ := t["kind"].(string); k != "" {
					if _, ok := movedBlocks[k]; ok {
						out[k] = true
					}
				}
			}
			for _, y := range t {
				walk(y)
			}
		case []any:
			for _, y := range t {
				walk(y)
			}
		}
	}
	walk(v)
	return out
}

// InstallMovedBlockPlugins installs and enables the plugins of moved block
// kinds where content uses them (call with a tenant context; the scheduler
// runs it with the store sync). Failing store requests leave the work for
// the next run; meanwhile the blocks show what they need.
func (s *Service) InstallMovedBlockPlugins(ctx context.Context) error {
	if !s.pluginsOn() {
		return nil
	}
	var words []string
	for k := range movedBlocks {
		words = append(words, k)
	}
	docs, err := s.DB.DocumentsContaining(ctx, words)
	if err != nil || len(docs) == 0 {
		return err
	}
	need := map[string]map[string]bool{} // plugin → projects
	for _, d := range docs {
		for k := range usedMovedKinds(d.Content) {
			pl := movedBlocks[k].Plugin
			if need[pl] == nil {
				need[pl] = map[string]bool{}
			}
			need[pl][d.ProjectID] = true
		}
	}
	if len(need) == 0 {
		return nil
	}
	var st movedState
	raw, err := s.DB.Setting(ctx, movedBlocksSetting, "{}")
	if err != nil {
		return err
	}
	_ = json.Unmarshal([]byte(raw), &st)
	changed := false
	sys := Actor{Via: "system"}
	ids := make([]string, 0, len(need))
	for id := range need {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	var errs []error
	for _, id := range ids {
		row, err := s.DB.GetPlugin(ctx, id)
		switch {
		case errors.Is(err, store.ErrNotFound):
			if slices.Contains(st.Installed, id) || s.Plugins.Store == nil {
				continue // an admin removed it, or there is no store: the placeholder stays
			}
			if ok, err := s.installMovedPlugin(ctx, sys, id); err != nil || !ok {
				if err != nil {
					errs = append(errs, err)
				}
				continue
			}
			st.Installed, changed = append(st.Installed, id), true
			if row, err = s.DB.GetPlugin(ctx, id); err != nil {
				return err
			}
		case err != nil:
			return err
		}
		pkg, err := s.pkg(ctx, row.SHA256)
		if err != nil || s.usable(ctx, row, pkg.Manifest) != "" {
			continue // switched off or not usable: the admin decides
		}
		pps := map[string]bool{}
		for p := range need[id] {
			list, err := s.DB.ProjectPlugins(ctx, p)
			if err != nil {
				return err
			}
			if slices.ContainsFunc(list, func(pp store.ProjectPlugin) bool { return pp.PluginID == id }) {
				pps[p] = true
			}
		}
		projects := make([]string, 0, len(need[id]))
		for p := range need[id] {
			projects = append(projects, p)
		}
		slices.Sort(projects)
		for _, p := range projects {
			pair := id + "|" + p
			if pps[p] || slices.Contains(st.Enabled, pair) {
				continue // already on, or an owner turned it off after we turned it on
			}
			if err := s.DB.EnablePlugin(ctx, p, id, ""); err != nil {
				return err
			}
			st.Enabled, changed = append(st.Enabled, pair), true
			s.event(ctx, sys, "plugin.project", p, map[string]any{"plugin": id, "enabled": true, "auto": "moved_block"})
			s.pluginsChanged()
		}
	}
	if changed {
		b, _ := json.Marshal(st)
		if err := s.DB.SetSetting(ctx, movedBlocksSetting, string(b), "system"); err != nil {
			return err
		}
	}
	return errors.Join(errs...)
}

// installMovedPlugin installs the store's newest version of an official
// plugin; only signed packages are accepted.
func (s *Service) installMovedPlugin(ctx context.Context, a Actor, id string) (bool, error) {
	version, err := s.latestStoreVersion(ctx, id)
	if err != nil {
		return false, err
	}
	if version == "" {
		slog.Info("moved block plugin not in the store", "plugin", id)
		return false, nil
	}
	data, sig, err := s.Plugins.Store.Download(ctx, id, version)
	if err != nil {
		return false, err
	}
	if sig.Sig == "" {
		return false, errors.New("the store sent no signature for " + id)
	}
	if _, err = s.installPackage(ctx, a, data, &sig, "store"); err != nil {
		return false, err
	}
	return true, nil
}
