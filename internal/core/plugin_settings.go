package core

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/phoedox/phoedox/internal/plugins"
	"github.com/phoedox/phoedox/internal/store"
)

// Plugin settings: options per project that a plugin declares as a JSON
// Schema (contributes.settings). Project owners set them; the plugin's
// backend reads them with settings.get (saved values over the defaults).

// PluginSetting is one option as the settings form shows it.
type PluginSetting struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"` // boolean | string | integer | number
	Title       string   `json:"title,omitempty"`
	Description string   `json:"description,omitempty"`
	Enum        []string `json:"enum,omitempty"`
	Default     any      `json:"default,omitempty"`
	Min         *float64 `json:"minimum,omitempty"`
	Max         *float64 `json:"maximum,omitempty"`
	// I18n translates title, description and enum labels by language.
	I18n map[string]schemaI18n `json:"i18n,omitempty"`
}

// pluginSettingsSchema reads the settings a package declares (nil without).
func pluginSettingsSchema(pkg *plugins.Package) []PluginSetting {
	if pkg == nil || pkg.Manifest.Contributes.Settings == "" {
		return nil
	}
	raw, ok := pkg.File(pkg.Manifest.Contributes.Settings)
	if !ok {
		return nil
	}
	var sch struct {
		Properties json.RawMessage `json:"properties"`
	}
	if json.Unmarshal(raw, &sch) != nil {
		return nil
	}
	out := []PluginSetting{}
	for _, kv := range orderedObject(sch.Properties) {
		var p struct {
			Type        string          `json:"type"`
			Title       string          `json:"title"`
			Description string          `json:"description"`
			Enum        []any           `json:"enum"`
			Default     json.RawMessage `json:"default"`
			Minimum     *float64        `json:"minimum"`
			Maximum     *float64        `json:"maximum"`
			I18n        schemaI18nMap   `json:"x-i18n"`
		}
		if json.Unmarshal(kv.value, &p) != nil || !validName(kv.key) {
			continue
		}
		if !slices.Contains([]string{"boolean", "string", "integer", "number"}, p.Type) {
			continue
		}
		st := PluginSetting{Name: kv.key, Type: p.Type, Title: p.Title, Description: p.Description, Min: p.Minimum, Max: p.Maximum}
		for lang, tr := range p.I18n {
			if validLang(lang) {
				if st.I18n == nil {
					st.I18n = map[string]schemaI18n{}
				}
				st.I18n[lang] = tr
			}
		}
		if p.Type == "string" {
			for _, e := range p.Enum {
				st.Enum = append(st.Enum, fmt.Sprint(e))
			}
		}
		if len(p.Default) > 0 {
			var d any
			if json.Unmarshal(p.Default, &d) == nil {
				if v, err := st.check(d); err == nil {
					st.Default = v
				}
			}
		}
		out = append(out, st)
		if len(out) == 50 {
			break
		}
	}
	return out
}

// check validates a value against the option and returns it normalized.
func (st PluginSetting) check(v any) (any, error) {
	switch st.Type {
	case "boolean":
		if b, ok := v.(bool); ok {
			return b, nil
		}
	case "string":
		s, ok := v.(string)
		if !ok {
			break
		}
		if len(s) > 2000 {
			return nil, fmt.Errorf("longer than 2000 characters")
		}
		if len(st.Enum) > 0 && !slices.Contains(st.Enum, s) {
			return nil, fmt.Errorf("must be one of %s", strings.Join(st.Enum, ", "))
		}
		return s, nil
	case "integer", "number":
		f, ok := v.(float64)
		if !ok || math.IsNaN(f) || math.IsInf(f, 0) {
			break
		}
		if st.Type == "integer" && f != math.Trunc(f) {
			return nil, fmt.Errorf("must be a whole number")
		}
		if (st.Min != nil && f < *st.Min) || (st.Max != nil && f > *st.Max) {
			return nil, fmt.Errorf("out of range")
		}
		return f, nil
	}
	return nil, fmt.Errorf("must be a %s", st.Type)
}

// pluginSettingValues are the saved values over the defaults.
func pluginSettingValues(schema []PluginSetting, saved string) map[string]any {
	out := map[string]any{}
	var cur map[string]any
	_ = json.Unmarshal([]byte(saved), &cur)
	for _, st := range schema {
		if v, ok := cur[st.Name]; ok {
			if n, err := st.check(v); err == nil {
				out[st.Name] = n
				continue
			}
		}
		if st.Default != nil {
			out[st.Name] = st.Default
		}
	}
	return out
}

// SetPluginSettings stores options of a plugin in a project (owners). Only
// declared options are accepted; null resets one to its default.
func (s *Service) SetPluginSettings(ctx context.Context, a Actor, ref, id string, values map[string]any) (map[string]any, error) {
	p, err := s.project(ctx, a, ref, store.RoleOwner)
	if err != nil {
		return nil, err
	}
	if a.Via == "mcp" || a.Plugin != "" {
		return nil, ErrForbidden
	}
	pp, err := s.DB.GetProjectPlugin(ctx, p.ID, id)
	if err != nil {
		return nil, err
	}
	row, err := s.DB.GetPlugin(ctx, id)
	if err != nil {
		return nil, err
	}
	pkg, err := s.pkg(ctx, row.SHA256)
	if err != nil {
		return nil, err
	}
	schema := pluginSettingsSchema(pkg)
	cur := map[string]any{}
	_ = json.Unmarshal([]byte(pp.Settings), &cur)
	for name, v := range values {
		i := slices.IndexFunc(schema, func(st PluginSetting) bool { return st.Name == name })
		if i < 0 {
			return nil, invalid("validation.plugin_setting", map[string]any{"name": name}, "the plugin has no option %q", name)
		}
		if v == nil {
			delete(cur, name)
			continue
		}
		n, err := schema[i].check(v)
		if err != nil {
			return nil, invalid("validation.plugin_setting_value", map[string]any{"name": name}, "option %q: %v", name, err)
		}
		cur[name] = n
	}
	raw, _ := json.Marshal(cur)
	if err := s.DB.SetPluginSettings(ctx, p.ID, id, string(raw)); err != nil {
		return nil, err
	}
	s.event(ctx, a, "plugin.settings", p.ID, map[string]any{"plugin": id, "settings": cur})
	blockCache.clear()
	return pluginSettingValues(schema, string(raw)), nil
}

// Settings answers settings.get.
func (e *pluginEnv) Settings(ctx context.Context) (any, error) {
	pp, err := e.s.DB.GetProjectPlugin(ctx, e.p.ID, e.ap.row.ID)
	if err != nil {
		return nil, err
	}
	return pluginSettingValues(pluginSettingsSchema(e.ap.pkg), pp.Settings), nil
}

// maxSubgraph is the most entities one graph.subgraph call returns.
const maxSubgraph = 5000

// GraphSubgraph answers graph.subgraph: entities of the kinds and the
// edges between them. Block renders read without a user (readAll).
func (e *pluginEnv) GraphSubgraph(ctx context.Context, kinds []string) (any, error) {
	type subgraph struct {
		Entities  []store.Entity `json:"entities"`
		Edges     []store.Edge   `json:"edges"`
		Truncated bool           `json:"truncated,omitempty"`
	}
	if !e.readAll {
		if _, err := e.s.project(ctx, e.a, e.p.ID, store.RoleReader); err != nil {
			return nil, err
		}
	}
	var list []store.Entity
	if len(kinds) == 0 {
		all, err := e.s.DB.ListEntities(ctx, e.p.ID, "")
		if err != nil {
			return nil, err
		}
		list = all
	}
	for _, k := range kinds {
		part, err := e.s.DB.ListEntities(ctx, e.p.ID, k)
		if err != nil {
			return nil, err
		}
		list = append(list, part...)
	}
	out := subgraph{Entities: []store.Entity{}, Edges: []store.Edge{}}
	if len(list) > maxSubgraph {
		list, out.Truncated = list[:maxSubgraph], true
	}
	in := make(map[string]bool, len(list))
	for _, ent := range list {
		in[ent.ID] = true
	}
	out.Entities = append(out.Entities, list...)
	edges, err := e.s.DB.EdgesOfProject(ctx, e.p.ID)
	if err != nil {
		return nil, err
	}
	for _, ed := range edges {
		if in[ed.From] && in[ed.To] {
			out.Edges = append(out.Edges, ed)
		}
	}
	return out, nil
}
