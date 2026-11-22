package aichat

import (
	"context"
	"encoding/json"
	"regexp"

	"github.com/phoedox/phoedox/internal/ai"
	"github.com/phoedox/phoedox/internal/core"
)

// The chat offers the tools of the plugins enabled in its project (named
// plugin_<tool> as on /mcp). The project is the chat's own, so the model
// does not pass it; every call goes through core.PluginTool with the same
// checks as MCP (control tools become suggestions a person confirms).

var pluginToolNameRe = regexp.MustCompile(`[^a-zA-Z0-9_]+`)

type pluginTool struct{ plugin, tool string }

type pluginTools struct {
	defs   []ai.Tool
	byName map[string]pluginTool
}

// pluginToolsFor lists the plugin tools of the project ref.
func (s *Service) pluginToolsFor(ctx context.Context, a core.Actor, ref string) pluginTools {
	out := pluginTools{byName: map[string]pluginTool{}}
	enabled := map[string]bool{}
	pps, err := s.Core.ProjectPlugins(ctx, a, ref)
	if err != nil {
		return out
	}
	for _, p := range pps {
		if p.Enabled {
			enabled[p.ID] = true
		}
	}
	if len(enabled) == 0 {
		return out
	}
	for _, t := range s.Core.PluginTools(ctx) {
		if !enabled[t.Plugin] {
			continue
		}
		name := "plugin_" + pluginToolNameRe.ReplaceAllString(t.Name, "_")
		if _, dup := out.byName[name]; dup || s.tools[name].call != nil {
			continue
		}
		schema := json.RawMessage(`{"type":"object","properties":{}}`)
		var in map[string]any
		if len(t.Input) > 0 && json.Unmarshal(t.Input, &in) == nil && in["type"] == "object" {
			schema = t.Input
		}
		out.defs = append(out.defs, ai.Tool{Name: name, Description: t.Description + " (plugin " + t.Plugin + ", runs in this chat's project)", Schema: schema})
		out.byName[name] = pluginTool{plugin: t.Plugin, tool: t.Name}
	}
	return out
}

// call runs a plugin tool in the chat's project.
func (p pluginTools) call(ctx context.Context, s *Service, a core.Actor, ref, name string, raw json.RawMessage) (any, bool, error) {
	t, ok := p.byName[name]
	if !ok {
		return nil, false, nil
	}
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	out, err := s.Core.PluginTool(ctx, a, ref, t.plugin, t.tool, raw)
	return out, true, err
}
