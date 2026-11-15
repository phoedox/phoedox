// Package anthropic is the Claude provider of the AI chat, using the
// official Go SDK.
package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

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
	effort string
}

// New returns a provider. baseURL may be empty (the public API); effort is
// low, medium, high, xhigh or max ("" = not sent, for models without it);
// client may be nil (the default HTTP client).
func New(apiKey, baseURL, effort string, client *http.Client) *Provider {
	opts := []option.RequestOption{option.WithAPIKey(apiKey), option.WithMaxRetries(2)}
	if baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}
	if client != nil {
		opts = append(opts, option.WithHTTPClient(client))
	}
	return &Provider{client: sdk.NewClient(opts...), effort: effort}
}

// Models lists the models of the API.
func (p *Provider) Models(ctx context.Context) ([]string, error) {
	page, err := p.client.Models.List(ctx, sdk.ModelListParams{Limit: sdk.Int(100)})
	if err != nil {
		return nil, apiError(err)
	}
	out := make([]string, 0, len(page.Data))
	for _, m := range page.Data {
		out = append(out, m.ID)
	}
	return out, nil
}

func apiError(err error) error {
	var apiErr *sdk.Error
	if errors.As(err, &apiErr) {
		return &ai.APIError{Status: apiErr.StatusCode, Err: err}
	}
	return err
}

func (p *Provider) params(req ai.Request) sdk.MessageNewParams {
	max := req.MaxTokens
	if max <= 0 {
		max = 16000
	}
	model := req.Model
	if model == "" {
		model = DefaultModel
	}
	params := sdk.MessageNewParams{
		Model:     sdk.Model(model),
		MaxTokens: int64(max),
		// One breakpoint at the end of the request: system prompt, tools and
		// the history so far are read from the cache in the next turn.
		CacheControl: sdk.NewCacheControlEphemeralParam(),
	}
	if p.effort != "" {
		params.OutputConfig = sdk.OutputConfigParam{Effort: sdk.OutputConfigEffort(p.effort)}
	}
	for _, s := range req.System {
		params.System = append(params.System, sdk.TextBlockParam{Text: s})
	}
	for _, m := range req.History {
		raw := m.Raw
		if m.Model != "" && m.Model != model {
			// Reasoning of another model cannot be sent back.
			raw = ai.StripReasoning(raw)
		}
		params.Messages = append(params.Messages, param.Override[sdk.MessageParam](raw))
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
	params := p.params(req)
	stream := p.client.Messages.NewStreaming(ctx, params)
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
		return ai.Turn{}, apiError(err)
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
		Model: string(params.Model), Input: msg.Usage.InputTokens, Output: msg.Usage.OutputTokens,
		CacheRead: msg.Usage.CacheReadInputTokens, CacheWrite: msg.Usage.CacheCreationInputTokens,
	}}
	for _, b := range msg.Content {
		if tu, ok := b.AsAny().(sdk.ToolUseBlock); ok {
			turn.Calls = append(turn.Calls, ai.ToolCall{ID: tu.ID, Name: tu.Name, Input: tu.Input})
		}
	}
	return turn, nil
}
