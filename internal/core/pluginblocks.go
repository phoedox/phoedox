package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/phoedox/phoedox/internal/blocks"
	"github.com/phoedox/phoedox/internal/store"
)

// Plugin blocks (ADR-0086): a plugin adds a content block kind; its backend
// export block_render draws a fenced block of that kind as SVG. The picture
// is used everywhere (web, mobile app, published pages, whiteboards) and its
// problems check AI writes; an optional frontend element makes the block
// interactive where plugin code runs.

// PluginBlockInfo is a block kind a project's plugins add.
type PluginBlockInfo struct {
	Kind    string `json:"kind"`
	Label   string `json:"label"`
	Icon    string `json:"icon,omitempty"`
	Plugin  string `json:"plugin"`
	Version string `json:"version"`
	// Element is the frontend element that shows the block interactively;
	// Frontend is the module that defines it.
	Element  string `json:"element,omitempty"`
	Frontend string `json:"frontend,omitempty"`
	Board    bool   `json:"board,omitempty"`
	Example  string `json:"example,omitempty"`
}

// PluginBlockResult is a drawn plugin block.
type PluginBlockResult struct {
	SVG      string         `json:"svg"`
	Problems []BlockProblem `json:"problems"`
}

const (
	pluginBlockTimeout = 5 * time.Second
	pluginBlockMaxSVG  = 1 << 20
	pluginBlockCache   = 512
)

// pluginBlockKind is the form of a plugin block's id (the manifest's rule).
var pluginBlockKind = regexp.MustCompile(`^[a-z][a-z0-9]{1,29}$`)

// fixedFences are fenced block kinds the core renders itself; plugins
// cannot take them over.
var fixedFences = map[string]bool{
	"request": true, "compare": true, "status": true, "entity": true, "include": true, "query": true, "board": true,
	"whiteboard": true, "app": true, "map": true, "mermaid": true, "math": true, "dbmodel": true, "embed": true, "iframe": true,
}

// pluginBlockDefs lists the block kinds of the active plugins; the first
// plugin (by id) wins when two add the same kind, core kinds always win.
func (s *Service) pluginBlockDefs(ctx context.Context, projectID string) ([]PluginBlockInfo, map[string]activePlugin, error) {
	list, err := s.activePlugins(ctx, projectID)
	if err != nil {
		return nil, nil, err
	}
	slices.SortFunc(list, func(a, b activePlugin) int { return strings.Compare(a.row.ID, b.row.ID) })
	var out []PluginBlockInfo
	by := map[string]activePlugin{}
	for _, ap := range list {
		for _, b := range ap.pkg.Manifest.Contributes.Blocks {
			if _, core := blocks.Get(b.ID); core || fixedFences[b.ID] || by[b.ID].pkg != nil {
				continue
			}
			by[b.ID] = ap
			info := PluginBlockInfo{Kind: b.ID, Label: b.Label, Icon: b.Icon, Plugin: ap.row.ID, Version: ap.row.Version, Board: b.Board, Example: b.Example}
			if b.Element != "" && ap.pkg.Manifest.Frontend != nil {
				info.Element = b.Element
				info.Frontend = pluginFrontendURL(ap)
			}
			out = append(out, info)
		}
	}
	return out, by, nil
}

// pluginFrontendURL is where the web app loads a plugin's frontend module.
func pluginFrontendURL(ap activePlugin) string {
	return "/api/v1/plugins/" + ap.row.ID + "/" + ap.row.Version + "/files/" + strings.TrimPrefix(ap.pkg.Manifest.Frontend.Module, "./")
}

// PluginBlocks lists the block kinds the project's plugins add.
func (s *Service) PluginBlocks(ctx context.Context, a Actor, ref string) ([]PluginBlockInfo, error) {
	p, err := s.project(ctx, a, ref, store.RoleDoc)
	if err != nil {
		return nil, err
	}
	out, _, err := s.pluginBlockDefs(ctx, p.ID)
	if out == nil {
		out = []PluginBlockInfo{}
	}
	return out, err
}

// RenderPluginBlock draws a plugin block of the project.
func (s *Service) RenderPluginBlock(ctx context.Context, a Actor, ref, kind, source, locale string, width int) (PluginBlockResult, error) {
	p, err := s.project(ctx, a, ref, store.RoleDoc)
	if err != nil {
		return PluginBlockResult{}, err
	}
	return s.renderPluginBlock(ctx, p, kind, source, locale, width)
}

func (s *Service) renderPluginBlock(ctx context.Context, p store.Project, kind, source, locale string, width int) (PluginBlockResult, error) {
	if len(source) > blocks.MaxSource {
		return PluginBlockResult{}, invalid("validation.block_source", nil, "the block is larger than %d bytes", blocks.MaxSource)
	}
	_, by, err := s.pluginBlockDefs(ctx, p.ID)
	if err != nil {
		return PluginBlockResult{}, err
	}
	ap, ok := by[kind]
	if !ok {
		return PluginBlockResult{}, ErrNotFound
	}
	if width <= 0 || width > 4000 {
		width = 720
	}
	if locale == "" {
		locale = "en"
	}
	h := sha256.Sum256([]byte(ap.row.SHA256 + "\x00" + kind + "\x00" + locale + "\x00" + fmt.Sprint(width) + "\x00" + p.Slug + "\x00" + source))
	key := hex.EncodeToString(h[:])
	if r, ok := blockCache.get(key); ok {
		return r, nil
	}
	pa := Actor{Plugin: ap.row.ID}
	raw, err := s.Plugins.Runtime.CallWithin(ctx, ap.pkg, logKey(ctx, ap.row.ID), "block_render",
		map[string]any{"kind": kind, "source": source, "project": p.Slug, "locale": locale, "width": width}, s.pluginEnv(pa, p, ap), pluginBlockTimeout)
	if err != nil {
		return PluginBlockResult{}, invalid("validation.plugin_failed", map[string]any{"plugin": ap.row.ID}, "%v", err)
	}
	var out struct {
		SVG      string `json:"svg"`
		Problems []struct {
			Line     int    `json:"line"`
			Message  string `json:"message"`
			Severity string `json:"severity"`
		} `json:"problems"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return PluginBlockResult{}, invalid("validation.plugin_failed", map[string]any{"plugin": ap.row.ID}, "block_render returned no result: %v", err)
	}
	res := PluginBlockResult{Problems: []BlockProblem{}}
	for _, pr := range out.Problems {
		sev := pr.Severity
		if sev != "warning" {
			sev = "error"
		}
		msg := pr.Message
		if len(msg) > 500 {
			msg = msg[:500]
		}
		res.Problems = append(res.Problems, BlockProblem{Kind: kind, Line: max(1, pr.Line), Severity: sev, Message: msg})
	}
	if out.SVG != "" {
		if len(out.SVG) > pluginBlockMaxSVG {
			return PluginBlockResult{}, invalid("validation.plugin_failed", map[string]any{"plugin": ap.row.ID}, "the picture is larger than 1 MB")
		}
		clean, err := cleanSVG([]byte(out.SVG))
		if err != nil {
			return PluginBlockResult{}, invalid("validation.plugin_failed", map[string]any{"plugin": ap.row.ID}, "block_render returned no SVG picture")
		}
		res.SVG = string(clean)
	}
	blockCache.put(key, res)
	return res, nil
}

// checkPluginBlocks adds the problems of plugin blocks to validate_blocks.
func (s *Service) checkPluginBlocks(ctx context.Context, p store.Project, md string) []BlockProblem {
	defs, _, err := s.pluginBlockDefs(ctx, p.ID)
	if err != nil || len(defs) == 0 {
		return nil
	}
	kinds := map[string]bool{}
	for _, d := range defs {
		kinds[d.Kind] = true
	}
	var out []BlockProblem
	for i, b := range fencedBlocks(md) {
		if !kinds[b.Lang] {
			continue
		}
		r, err := s.renderPluginBlock(ctx, p, b.Lang, b.Body, "en", 720)
		if err != nil {
			out = append(out, BlockProblem{Kind: b.Lang, Block: i + 1, Line: b.Line, Severity: "warning", Message: "the plugin could not draw the block: " + err.Error()})
			continue
		}
		for _, pr := range r.Problems {
			pr.Block, pr.Line = i+1, b.Line+pr.Line
			out = append(out, pr)
		}
	}
	return out
}

// PubPluginBlock draws a plugin block of a published project ("" if the
// kind is not a plugin block there or drawing fails).
func (s *Service) PubPluginBlock(ctx context.Context, p store.Project, kind, source, locale string) string {
	r, err := s.renderPluginBlock(ctx, p, kind, source, locale, 720)
	if err != nil {
		return ""
	}
	return r.SVG
}

// renderCache keeps drawn blocks by plugin version and source.
type renderCache struct {
	mu    sync.Mutex
	items map[string]PluginBlockResult
	order []string
}

var blockCache = &renderCache{items: map[string]PluginBlockResult{}}

func (c *renderCache) get(k string) (PluginBlockResult, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	r, ok := c.items[k]
	return r, ok
}

func (c *renderCache) put(k string, r PluginBlockResult) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.items[k]; ok {
		return
	}
	if len(c.order) >= pluginBlockCache {
		delete(c.items, c.order[0])
		c.order = c.order[1:]
	}
	c.items[k] = r
	c.order = append(c.order, k)
}
