// Package mcp exposes Phoedox as an MCP server (Streamable HTTP at /mcp).
//
// Clients authenticate with a personal API token (Authorization: Bearer phx_…).
// Every tool runs with the rights of the token owner. In projects with
// ai_write_mode "suggest" (default) writes become suggestions that a human
// accepts in Phoedox.
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/auth"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	phxauth "github.com/phoedox/phoedox/internal/auth"
	"github.com/phoedox/phoedox/internal/core"
	"github.com/phoedox/phoedox/resources"
)

// Handler returns the HTTP handler for /mcp.
func Handler(svc *core.Service, a *phxauth.Auth, version string) http.Handler {
	server := sdk.NewServer(&sdk.Implementation{Name: "phoedox", Title: "Phoedox", Version: version}, &sdk.ServerOptions{Instructions: resources.Prompt("mcp-instructions")})
	registerTools(server, svc)

	verify := func(ctx context.Context, token string, r *http.Request) (*auth.TokenInfo, error) {
		actor, _, err := a.FromRequest(r, "mcp")
		if err != nil {
			return nil, auth.ErrInvalidToken
		}
		return &auth.TokenInfo{Extra: map[string]any{"actor": actor}}, nil
	}
	h := sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return server }, &sdk.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	// Personal API tokens have no expiry (they are revoked in the UI).
	return auth.RequireBearerToken(verify, &auth.RequireBearerTokenOptions{AllowMissingExpiration: true})(h)
}

var errNoActor = errors.New("not authenticated")

func actorFrom(req *sdk.CallToolRequest) (core.Actor, error) {
	if req.Extra == nil || req.Extra.TokenInfo == nil {
		return core.Actor{}, errNoActor
	}
	a, ok := req.Extra.TokenInfo.Extra["actor"].(core.Actor)
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
