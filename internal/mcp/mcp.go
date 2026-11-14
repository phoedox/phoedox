// Package mcp exposes Phoedox as an MCP server (Streamable HTTP at /mcp).
//
// Clients authenticate with a personal API token (Authorization: Bearer phx_…)
// or an OAuth access token from Phoedox's authorization server. A 401 points
// clients to the protected resource metadata, so they can start OAuth on
// their own. Every tool runs with the rights of the token owner. In projects with
// ai_write_mode "suggest" (default) writes become suggestions that a human
// accepts in Phoedox.
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	phxauth "github.com/phoedox/phoedox/internal/auth"
	"github.com/phoedox/phoedox/internal/core"
	"github.com/phoedox/phoedox/internal/store"
	"github.com/phoedox/phoedox/resources"
)

// Handler returns the HTTP handler for /mcp. baseURL returns the public
// origin of a request (used for the resource metadata URL).
func Handler(svc *core.Service, a *phxauth.Auth, version string, baseURL func(*http.Request) string) http.Handler {
	servers := &serverCache{byTenant: map[string]cachedServer{}}
	build := func(ctx context.Context) *sdk.Server {
		server := sdk.NewServer(&sdk.Implementation{Name: "phoedox", Title: "Phoedox", Version: version}, &sdk.ServerOptions{Instructions: resources.Prompt("mcp-instructions")})
		registerTools(server, svc)
		registerResources(server, svc)
		registerPluginTools(ctx, server, svc)
		return server
	}

	verify := func(ctx context.Context, token string, r *http.Request) (*auth.TokenInfo, error) {
		actor, exp, _, err := a.FromRequestWithExpiry(r, "mcp")
		if err != nil {
			return nil, auth.ErrInvalidToken
		}
		return &auth.TokenInfo{Expiration: exp, Extra: map[string]any{"actor": actor}}, nil
	}
	h := sdk.NewStreamableHTTPHandler(func(r *http.Request) *sdk.Server { return servers.get(r.Context(), svc.PluginGeneration(), build) },
		&sdk.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !svc.AIEnabled(r.Context()) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":"AI access (MCP) is disabled on this instance."}`))
			return
		}
		// Personal API tokens have no expiry (they are revoked in the UI).
		opts := &auth.RequireBearerTokenOptions{AllowMissingExpiration: true, ClockSkew: 30 * time.Second}
		if baseURL != nil {
			opts.ResourceMetadataURL = baseURL(r) + "/.well-known/oauth-protected-resource/mcp"
		}
		auth.RequireBearerToken(verify, opts)(h).ServeHTTP(w, r)
	})
}

// serverCache keeps one MCP server per tenant, rebuilt when plugins change
// (plugin tools differ between tenants).
type serverCache struct {
	mu       sync.Mutex
	byTenant map[string]cachedServer
}

type cachedServer struct {
	gen    int64
	server *sdk.Server
}

func (c *serverCache) get(ctx context.Context, gen int64, build func(context.Context) *sdk.Server) *sdk.Server {
	tenant := store.TenantOf(ctx)
	c.mu.Lock()
	defer c.mu.Unlock()
	if s, ok := c.byTenant[tenant]; ok && s.gen == gen {
		return s.server
	}
	s := build(ctx)
	c.byTenant[tenant] = cachedServer{gen: gen, server: s}
	return s
}

var toolNameRe = regexp.MustCompile(`[^a-zA-Z0-9_]+`)

// registerPluginTools adds the tools of the tenant's plugins. Each needs a
// project in which the plugin is enabled; it runs with the caller's rights.
func registerPluginTools(ctx context.Context, s *sdk.Server, svc *core.Service) {
	seen := map[string]bool{}
	for _, t := range svc.PluginTools(ctx) {
		name := "plugin_" + toolNameRe.ReplaceAllString(t.Name, "_")
		if seen[name] {
			continue
		}
		seen[name] = true
		schema := map[string]any{"type": "object", "properties": map[string]any{}}
		if len(t.Input) > 0 {
			var in map[string]any
			if json.Unmarshal(t.Input, &in) == nil && in["type"] == "object" {
				schema = in
			}
		}
		props, _ := schema["properties"].(map[string]any)
		if props == nil {
			props = map[string]any{}
			schema["properties"] = props
		}
		props["project"] = map[string]any{"type": "string", "description": "Project slug or id; the plugin must be enabled there."}
		req, _ := schema["required"].([]any)
		schema["required"] = append(req, "project")
		plugin, tool := t.Plugin, t.Name
		s.AddTool(&sdk.Tool{Name: name, Description: t.Description + " (plugin " + plugin + ")", InputSchema: schema},
			func(ctx context.Context, req *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
				a, err := actorFrom(req)
				if err != nil {
					return nil, err
				}
				var args map[string]any
				if len(req.Params.Arguments) > 0 {
					if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
						return nil, err
					}
				}
				project, _ := args["project"].(string)
				delete(args, "project")
				raw, _ := json.Marshal(args)
				out, err := svc.PluginTool(ctx, a, project, plugin, tool, raw)
				if err != nil {
					return nil, friendly(err)
				}
				return result(out), nil
			})
	}
}

var errNoActor = errors.New("not authenticated")

func actorFrom(req *sdk.CallToolRequest) (core.Actor, error) {
	return actorOfExtra(req.Extra)
}

func actorOfExtra(extra *sdk.RequestExtra) (core.Actor, error) {
	if extra == nil || extra.TokenInfo == nil {
		return core.Actor{}, errNoActor
	}
	a, ok := extra.TokenInfo.Extra["actor"].(core.Actor)
	if !ok {
		return core.Actor{}, errNoActor
	}
	return a, nil
}

// result renders any value as indented JSON text content.
func result(v any) *sdk.CallToolResult {
	b, _ := json.MarshalIndent(v, "", "  ")
	return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: string(b)}}}
}

// tool registers a handler that receives the authenticated actor.
func tool[In any](s *sdk.Server, name, desc string, fn func(ctx context.Context, a core.Actor, in In) (any, error)) {
	sdk.AddTool(s, &sdk.Tool{Name: name, Description: desc}, func(ctx context.Context, req *sdk.CallToolRequest, in In) (*sdk.CallToolResult, any, error) {
		a, err := actorFrom(req)
		if err != nil {
			return nil, nil, err
		}
		v, err := fn(ctx, a, in)
		if err != nil {
			return nil, nil, friendly(err)
		}
		return result(v), nil, nil
	})
}

func friendly(err error) error {
	var inv *core.InvalidError
	switch {
	case errors.As(err, &inv):
		return errors.New("invalid input: " + inv.Msg)
	case errors.Is(err, core.ErrForbidden):
		return errors.New("forbidden: the token owner lacks the required role (editor or owner)")
	case errors.Is(err, core.ErrNotFound):
		return errors.New("not found (or not visible to the token owner)")
	case errors.Is(err, core.ErrConflict):
		return errors.New("conflict: slug already exists or the document changed since expect_version")
	}
	return err
}
