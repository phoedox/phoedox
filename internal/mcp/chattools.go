package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	"github.com/google/jsonschema-go/jsonschema"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/phoedox/phoedox/internal/core"
)

// ChatTool is an MCP tool the AI chat calls directly, without the MCP
// transport (ADR-0066). It runs with the same rules as for MCP clients.
type ChatTool struct {
	Name        string
	Description string
	Schema      json.RawMessage
	// Call validates the input against the schema, runs the tool and returns
	// its result with "url" fields, as MCP clients get it.
	Call func(ctx context.Context, a core.Actor, input json.RawMessage) (any, error)
}

var (
	chatMu    sync.Mutex
	chatTools = map[string]ChatTool{}
	chatOnce  sync.Once
)

// registerChatTool keeps a tool for the chat; called from tool().
func registerChatTool[In any](name, desc string, fn func(ctx context.Context, a core.Actor, in In) (any, error)) {
	chatMu.Lock()
	defer chatMu.Unlock()
	if _, ok := chatTools[name]; ok {
		return
	}
	schema, err := jsonschema.For[In](nil)
	if err != nil {
		return
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		return
	}
	raw, _ := json.Marshal(schema)
	chatTools[name] = ChatTool{Name: name, Description: desc, Schema: raw, Call: func(ctx context.Context, a core.Actor, input json.RawMessage) (any, error) {
		var generic any
		if len(input) == 0 {
			input = json.RawMessage("{}")
		}
		if err := json.Unmarshal(input, &generic); err != nil {
			return nil, errors.New("invalid input: not valid JSON")
		}
		if err := resolved.Validate(generic); err != nil {
			return nil, errors.New("invalid input: " + err.Error())
		}
		var in In
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, errors.New("invalid input: " + err.Error())
		}
		v, err := fn(ctx, a, in)
		if err != nil {
			return nil, friendly(err)
		}
		return withURLs(ctx, v), nil
	}}
}

// ChatTools returns the named MCP tools for the AI chat (unknown names are
// left out).
func ChatTools(svc *core.Service, names []string) []ChatTool {
	chatOnce.Do(func() {
		linkSvc = svc
		registerTools(sdk.NewServer(&sdk.Implementation{Name: "phoedox-chat"}, nil), svc)
	})
	chatMu.Lock()
	defer chatMu.Unlock()
	var out []ChatTool
	for _, n := range names {
		if t, ok := chatTools[n]; ok {
			out = append(out, t)
		}
	}
	return out
}
