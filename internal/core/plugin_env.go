package core

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"strings"

	"github.com/phoedox/phoedox/internal/plugins"
	"github.com/phoedox/phoedox/internal/store"
)

func decodeB64(s string) ([]byte, error) { return base64.StdEncoding.DecodeString(s) }

// pluginEnv is what a plugin reaches during one call: the project, with the
// rights of the actor (the calling user, or the user who enabled the plugin
// for scheduled runs). All writes go through the normal checks.
type pluginEnv struct {
	s  *Service
	a  Actor
	p  store.Project
	ap activePlugin
}

func (s *Service) pluginEnv(a Actor, p store.Project, ap activePlugin) *pluginEnv {
	return &pluginEnv{s: s, a: a, p: p, ap: ap}
}

// pluginCode maps core errors to the codes plugins see.
func pluginCode(err error) string {
	var inv *InvalidError
	switch {
	case errors.Is(err, ErrForbidden):
		return "forbidden"
	case errors.Is(err, ErrNotFound):
		return "not_found"
	case errors.Is(err, ErrConflict):
		return "conflict"
	case errors.Is(err, store.ErrQuota):
		return "quota"
	case errors.As(err, &inv):
		return "invalid"
	}
	return ""
}

// PluginErrorCode is set as plugins.Runtime.Code.
func PluginErrorCode(err error) string { return pluginCode(err) }

func (e *pluginEnv) GraphGet(ctx context.Context, ref string) (any, error) {
	return e.s.Entity(ctx, e.a, e.p.ID, ref)
}

func (e *pluginEnv) GraphQuery(ctx context.Context, q plugins.Query) (any, error) {
	list, err := e.s.Entities(ctx, e.a, e.p.ID, q.Kind)
	if err != nil {
		return nil, err
	}
	limit := q.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	needle := strings.ToLower(strings.TrimSpace(q.Q))
	out := []store.Entity{}
	for _, ent := range list {
		if needle != "" && !strings.Contains(strings.ToLower(ent.Name+" "+ent.Slug), needle) {
			continue
		}
		out = append(out, ent)
		if len(out) == limit {
			break
		}
	}
	return out, nil
}

// GraphApply checks all ops first, then applies them in order. Upserts
// merge attributes into an existing entity; refs ("$0") name entities
// upserted earlier in the batch.
func (e *pluginEnv) GraphApply(ctx context.Context, ops plugins.Ops, allow func(string) bool) (any, error) {
	type result struct {
		Applied     int               `json:"applied"`
		Refs        map[string]string `json:"refs"`
		Suggestions int               `json:"suggestions,omitempty"`
	}
	res := result{Refs: map[string]string{}}
	for i, o := range ops.Ops {
		switch o.Op {
		case "upsert":
			if o.Kind == "" || o.ID == "" {
				return res, invalid("validation.plugin_op", map[string]any{"op": i}, "op %d: upsert needs kind and id", i)
			}
		case "link", "unlink":
			if o.From == "" || o.To == "" || o.Rel == "" {
				return res, invalid("validation.plugin_op", map[string]any{"op": i}, "op %d: %s needs from, rel and to", i, o.Op)
			}
		case "delete":
			if o.ID == "" {
				return res, invalid("validation.plugin_op", map[string]any{"op": i}, "op %d: delete needs id", i)
			}
		default:
			return res, invalid("validation.plugin_op", map[string]any{"op": i}, "op %d: unknown op %q", i, o.Op)
		}
	}
	resolve := func(ref string) (string, error) {
		if strings.HasPrefix(ref, "$") {
			slug, ok := res.Refs[ref]
			if !ok {
				return "", fmt.Errorf("%w: unknown reference %s", plugins.ErrInvalid, ref)
			}
			return slug, nil
		}
		return ref, nil
	}
	ref := e.p.ID
	for _, o := range ops.Ops {
		switch o.Op {
		case "upsert":
			in := EntityInput{Kind: o.Kind, Slug: o.ID, Name: o.Name, Attrs: o.Attrs}
			if cur, err := e.s.DB.GetEntity(ctx, e.p.ID, o.ID); err == nil {
				if cur.Kind != o.Kind {
					return res, fmt.Errorf("%w: %s is a %s", ErrConflict, o.ID, cur.Kind)
				}
				attrs := map[string]any{}
				_ = json.Unmarshal(cur.Attrs, &attrs)
				maps.Copy(attrs, o.Attrs)
				in.Attrs = attrs
				if in.Name == "" {
					in.Name = cur.Name
				}
			}
			if in.Name == "" {
				in.Name = o.ID
			}
			r, err := e.s.UpsertEntity(ctx, e.a, ref, in)
			if err != nil {
				return res, err
			}
			if r.Suggestion != nil {
				res.Suggestions++
				continue
			}
			res.Applied++
			if o.Ref != "" {
				res.Refs[o.Ref] = r.Entity.Slug
			}
		case "delete":
			cur, err := e.s.DB.GetEntity(ctx, e.p.ID, o.ID)
			if err != nil {
				return res, err
			}
			if !allow(cur.Kind) {
				return res, fmt.Errorf("%w: graph:write:%s", plugins.ErrDenied, cur.Kind)
			}
			r, err := e.s.DeleteEntity(ctx, e.a, ref, cur.ID)
			if err != nil {
				return res, err
			}
			if r.Suggestion != nil {
				res.Suggestions++
			} else {
				res.Applied++
			}
		case "link", "unlink":
			from, err := resolve(o.From)
			if err != nil {
				return res, err
			}
			to, err := resolve(o.To)
			if err != nil {
				return res, err
			}
			src, err := e.s.DB.GetEntity(ctx, e.p.ID, from)
			if err != nil {
				return res, err
			}
			if !allow(src.Kind) {
				return res, fmt.Errorf("%w: graph:write:%s", plugins.ErrDenied, src.Kind)
			}
			in := EdgeInput{From: from, Rel: o.Rel, To: to, Attrs: o.Attrs}
			var r EntityResult
			if o.Op == "link" {
				r, err = e.s.Link(ctx, e.a, ref, in)
			} else {
				r, err = e.s.Unlink(ctx, e.a, ref, in)
			}
			if err != nil {
				return res, err
			}
			if r.Suggestion != nil {
				res.Suggestions++
			} else {
				res.Applied++
			}
		}
	}
	return res, nil
}

func (e *pluginEnv) Secret(ctx context.Context, name string) (string, error) {
	pp, err := e.s.DB.GetProjectPlugin(ctx, e.p.ID, e.ap.row.ID)
	if err != nil {
		return "", err
	}
	all, err := e.s.pluginSecrets(pp)
	if err != nil {
		return "", err
	}
	v, ok := all[name]
	if !ok {
		return "", fmt.Errorf("%w: secret %s is not set in this project", plugins.ErrNotFound, name)
	}
	return v, nil
}

func (e *pluginEnv) KVGet(ctx context.Context, key string) (json.RawMessage, error) {
	v, err := e.s.DB.PluginKV(ctx, e.p.ID, e.ap.row.ID, key)
	if errors.Is(err, store.ErrNotFound) {
		return json.RawMessage("null"), nil
	}
	return json.RawMessage(v), err
}

func (e *pluginEnv) KVSet(ctx context.Context, key string, value json.RawMessage) error {
	if len(value) > 0 && !json.Valid(value) {
		return fmt.Errorf("%w: value is not JSON", plugins.ErrInvalid)
	}
	return e.s.DB.SetPluginKV(ctx, e.p.ID, e.ap.row.ID, key, string(value), pluginKVKeys)
}
