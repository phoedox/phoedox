// Package anthropic is the Claude provider of the AI chat, using the
// official Go SDK.
package anthropic

import (
	"context"
	"encoding/json"
	"errors"

	sdk "github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/option"
	"github.com/anthropics/anthropic-sdk-go/v2/packages/param"

	"github.com/phoedox/phoedox/internal/ai"
)

// DefaultModel is used when no model is configured.
const DefaultModel = "claude-opus-5-5"

// Provider calls Claude over the Messages API.
type Provider struct {
	client sdk.Client
	model  string
	effort string
}

// New returns a provider. baseURL may be empty (the public API); effort is
// low, medium, high, xhigh or max ("" = medium).
func New(apiKey, baseURL, model, effort string) *Provider {
	opts := []option.RequestOption{option.WithAPIKey(apiKey), option.WithMaxRetries(2)}
	if baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}
	if model == "" {
		model = DefaultModel
	}
	if effort == "" {
		effort = "medium"
	}
	return &Provider{client: sdk.NewClient(opts...), model: model, effort: effort}
}

// Model returns the configured model.
func (p *Provider) Model() string { return p.model }

func (p *Provider) params(req ai.Request) sdk.MessageNewParams {
	max := req.MaxTokens
	if max <= 0 {
		max = 16000
	}
	params := sdk.MessageNewParams{
		Model:     sdk.Model(p.model),
		MaxTokens: int64(max),
		// One breakpoint at the end of the request: system prompt, tools and
		// the history so far are read from the cache in the next turn.
		CacheControl: sdk.NewCacheControlEphemeralParam(),
		OutputConfig: sdk.OutputConfigParam{Effort: sdk.OutputConfigEffort(p.effort)},
	}
	for _, s := range req.System {
		params.System = append(params.System, sdk.TextBlockParam{Text: s})
	}
	for _, m := range req.History {
		params.Messages = append(params.Messages, param.Override[sdk.MessageParam](m))
	}
	for _, t := range req.Tools {
		var schema struct {
			Properties any      `json:"properties"`
			Required   []string `json:"required"`
		}
		_ = json.Unmarshal(t.Schema, &schema)
		if schema.Properties == nil {
			schema.Properties = map[string]any{}
		}
		tool := sdk.ToolParam{
			Name:        t.Name,
			Description: sdk.String(t.Description),
			InputSchema: sdk.ToolInputSchemaParam{Properties: schema.Properties, Required: schema.Required},
			// Large inputs (a whole page) stream as they are written; the
			// caller validates every input before running the tool.
			EagerInputStreaming: sdk.Bool(true),
		}
		params.Tools = append(params.Tools, sdk.ToolUnionParam{OfTool: &tool})
	}
	return params
}

// Stream runs one call and reports text and starting tool calls as they come.
func (p *Provider) Stream(ctx context.Context, req ai.Request, emit func(ai.Event)) (ai.Turn, error) {
	stream := p.client.Messages.NewStreaming(ctx, p.params(req))
	defer stream.Close()
	msg := sdk.Message{}
	for stream.Next() {
		ev := stream.Current()
		if err := msg.Accumulate(ev); err != nil {
			return ai.Turn{}, err
		}
		switch v := ev.AsAny().(type) {
		case sdk.ContentBlockStartEvent:
			if tu, ok := v.ContentBlock.AsAny().(sdk.ToolUseBlock); ok {
				emit(ai.Event{Type: "tool", Tool: tu.Name})
			}
		case sdk.ContentBlockDeltaEvent:
			if d, ok := v.Delta.AsAny().(sdk.TextDelta); ok {
				emit(ai.Event{Type: "text", Text: d.Text})
			}
		}
	}
	if err := stream.Err(); err != nil {
		var apiErr *sdk.Error
		if errors.As(err, &apiErr) {
			return ai.Turn{}, &ai.APIError{Status: apiErr.StatusCode, Err: err}
		}
		return ai.Turn{}, err
	}
	// A tool input cut off by max_tokens is not valid JSON: keep the call
	// in the history with an empty input (it is answered as not run).
	for i, b := range msg.Content {
		if tu, ok := b.AsAny().(sdk.ToolUseBlock); ok && !json.Valid(tu.Input) {
			msg.Content[i].Input = json.RawMessage("{}")
		}
	}
	raw, err := json.Marshal(msg.ToParam())
	if err != nil {
		return ai.Turn{}, err
	}
	turn := ai.Turn{Message: raw, Stop: string(msg.StopReason), Usage: ai.Usage{
		Model: p.model, Input: msg.Usage.InputTokens, Output: msg.Usage.OutputTokens,
		CacheRead: msg.Usage.CacheReadInputTokens, CacheWrite: msg.Usage.CacheCreationInputTokens,
	}}
	for _, b := range msg.Content {
		if tu, ok := b.AsAny().(sdk.ToolUseBlock); ok {
			turn.Calls = append(turn.Calls, ai.ToolCall{ID: tu.ID, Name: tu.Name, Input: tu.Input})
		}
	}
	return turn, nil
}
