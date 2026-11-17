// Package openai is the provider for OpenAI-compatible chat completion APIs:
// OpenAI, Mistral, Kimi (Moonshot), Ollama, OpenRouter and self-hosted
// gateways. It translates the stored history (Messages format, see package
// ai) to chat completions and the streamed answer back.
package openai

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/phoedox/phoedox/internal/ai"
)

// Provider calls one OpenAI-compatible API.
type Provider struct {
	base   string // e.g. https://api.openai.com/v1
	key    string
	client *http.Client
}

// New returns a provider for the API at base (with its version path, e.g.
// https://api.mistral.ai/v1). key may be empty (local servers).
func New(base, key string, client *http.Client) *Provider {
	if client == nil {
		client = http.DefaultClient
	}
	return &Provider{base: strings.TrimRight(base, "/"), key: key, client: client}
}

func (p *Provider) do(ctx context.Context, method, path string, body any) (*http.Response, error) {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, p.base+path, r)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if p.key != "" {
		req.Header.Set("Authorization", "Bearer "+p.key)
	}
	res, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		res.Body.Close()
		return nil, &ai.APIError{Status: res.StatusCode, Err: fmt.Errorf("%s %s: %d %s", method, path, res.StatusCode, strings.TrimSpace(string(msg)))}
	}
	return res, nil
}

// Models lists the models of the API (GET /models).
func (p *Provider) Models(ctx context.Context) ([]string, error) {
	res, err := p.do(ctx, http.MethodGet, "/models", nil)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	var out struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(&out); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(out.Data))
	for _, m := range out.Data {
		ids = append(ids, m.ID)
	}
	return ids, nil
}

type message struct {
	Role       string     `json:"role"`
	Content    *string    `json:"content"`
	ToolCalls  []toolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type toolCall struct {
	Index    *int   `json:"index,omitempty"`
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type block struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
}

var idRe = regexp.MustCompile(`^[a-zA-Z0-9]{9}$`)

// callID makes a tool call id acceptable to strict APIs (Mistral wants nine
// letters or digits); ids that fit stay as they are, others map stably.
func callID(id string) string {
	if idRe.MatchString(id) {
		return id
	}
	sum := sha256.Sum256([]byte(id))
	const abc = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	out := make([]byte, 9)
	for i := range out {
		out[i] = abc[int(sum[i])%len(abc)]
	}
	return string(out)
}

func str(s string) *string { return &s }

// messages translates the stored history to chat completion messages.
func messages(system []string, history []ai.Message) []message {
	out := []message{}
	if len(system) > 0 {
		out = append(out, message{Role: "system", Content: str(strings.Join(system, "\n\n"))})
	}
	for _, m := range history {
		var msg struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		}
		if json.Unmarshal(m.Raw, &msg) != nil {
			continue
		}
		var blocks []block
		var plain string
		if json.Unmarshal(msg.Content, &plain) == nil {
			blocks = []block{{Type: "text", Text: plain}}
		} else {
			_ = json.Unmarshal(msg.Content, &blocks)
		}
		var text []string
		switch msg.Role {
		case "user":
			for _, b := range blocks {
				switch b.Type {
				case "tool_result":
					var c string
					if json.Unmarshal(b.Content, &c) != nil {
						c = string(b.Content)
					}
					if b.IsError {
						c = "Error: " + c
					}
					out = append(out, message{Role: "tool", ToolCallID: callID(b.ToolUseID), Content: str(c)})
				case "text":
					text = append(text, b.Text)
				}
			}
			if len(text) > 0 {
				out = append(out, message{Role: "user", Content: str(strings.Join(text, "\n\n"))})
			}
		case "assistant":
			am := message{Role: "assistant"}
			for _, b := range blocks {
				switch b.Type {
				case "text":
					text = append(text, b.Text)
				case "tool_use":
					tc := toolCall{ID: callID(b.ID), Type: "function"}
					tc.Function.Name = b.Name
					tc.Function.Arguments = string(b.Input)
					if tc.Function.Arguments == "" {
						tc.Function.Arguments = "{}"
					}
					am.ToolCalls = append(am.ToolCalls, tc)
				}
			}
			am.Content = str(strings.Join(text, "\n\n"))
			out = append(out, am)
		}
	}
	return out
}

type chunk struct {
	Choices []struct {
		Delta struct {
			Content   string     `json:"content"`
			ToolCalls []toolCall `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
		Usage        *usage `json:"usage"` // some APIs (Kimi) put it here
	} `json:"choices"`
	Usage *usage `json:"usage"`
}

type usage struct {
	PromptTokens        int64 `json:"prompt_tokens"`
	CompletionTokens    int64 `json:"completion_tokens"`
	PromptTokensDetails struct {
		CachedTokens int64 `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
}

// Stream runs one call with streaming.
func (p *Provider) Stream(ctx context.Context, req ai.Request, emit func(ai.Event)) (ai.Turn, error) {
	body := map[string]any{
		"model":          req.Model,
		"stream":         true,
		"stream_options": map[string]any{"include_usage": true},
		"messages":       messages(req.System, req.History),
	}
	if len(req.Tools) > 0 {
		tools := make([]map[string]any, 0, len(req.Tools))
		for _, t := range req.Tools {
			tools = append(tools, map[string]any{"type": "function", "function": map[string]any{"name": t.Name, "description": t.Description, "parameters": t.Schema}})
		}
		body["tools"] = tools
	}
	res, err := p.do(ctx, http.MethodPost, "/chat/completions", body)
	var apiErr *ai.APIError
	if errors.As(err, &apiErr) && (apiErr.Status == 400 || apiErr.Status == 422) && strings.Contains(apiErr.Error(), "stream_options") {
		// Some compatible APIs do not know stream_options.
		delete(body, "stream_options")
		res, err = p.do(ctx, http.MethodPost, "/chat/completions", body)
	}
	if err != nil {
		return ai.Turn{}, err
	}
	defer res.Body.Close()

	var text strings.Builder
	type call struct {
		id, name string
		args     strings.Builder
	}
	var calls []*call
	byIndex := map[int]*call{}
	finish := ""
	var u usage
	sc := bufio.NewScanner(res.Body)
	sc.Buffer(make([]byte, 64<<10), 8<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		var c chunk
		if json.Unmarshal([]byte(data), &c) != nil {
			continue
		}
		if c.Usage != nil {
			u = *c.Usage
		}
		for _, ch := range c.Choices {
			if ch.Usage != nil {
				u = *ch.Usage
			}
			if ch.Delta.Content != "" {
				text.WriteString(ch.Delta.Content)
				emit(ai.Event{Type: "text", Text: ch.Delta.Content})
			}
			for i, tc := range ch.Delta.ToolCalls {
				idx := i
				if tc.Index != nil {
					idx = *tc.Index
				}
				cl, ok := byIndex[idx]
				if !ok {
					cl = &call{}
					byIndex[idx] = cl
					calls = append(calls, cl)
				}
				if tc.ID != "" {
					cl.id = tc.ID
				}
				if tc.Function.Name != "" && cl.name == "" {
					cl.name = tc.Function.Name
					emit(ai.Event{Type: "tool", Tool: cl.name})
				}
				cl.args.WriteString(tc.Function.Arguments)
			}
			if ch.FinishReason != "" {
				finish = ch.FinishReason
			}
		}
	}
	if err := sc.Err(); err != nil {
		return ai.Turn{}, err
	}

	turn := ai.Turn{Usage: ai.Usage{Model: req.Model, Input: u.PromptTokens - u.PromptTokensDetails.CachedTokens,
		Output: u.CompletionTokens, CacheRead: u.PromptTokensDetails.CachedTokens}}
	blocks := []map[string]any{}
	if text.Len() > 0 {
		blocks = append(blocks, map[string]any{"type": "text", "text": text.String()})
	}
	for i, cl := range calls {
		if cl.id == "" {
			cl.id = fmt.Sprintf("call%05d", i)
		}
		input := json.RawMessage(cl.args.String())
		if !json.Valid(input) {
			input = json.RawMessage("{}")
		}
		blocks = append(blocks, map[string]any{"type": "tool_use", "id": cl.id, "name": cl.name, "input": input})
		turn.Calls = append(turn.Calls, ai.ToolCall{ID: cl.id, Name: cl.name, Input: input})
	}
	switch {
	case finish == "length":
		turn.Stop = ai.StopMaxTokens
	case finish == "content_filter":
		turn.Stop = ai.StopRefusal
	case len(turn.Calls) > 0:
		turn.Stop = ai.StopToolUse
	default:
		turn.Stop = "end_turn"
	}
	turn.Message, err = json.Marshal(map[string]any{"role": "assistant", "content": blocks})
	return turn, err
}

// Embed returns the embeddings of texts (POST /embeddings), in input order.
func (p *Provider) Embed(ctx context.Context, model string, texts []string) ([][]float32, error) {
	res, err := p.do(ctx, http.MethodPost, "/embeddings", map[string]any{"model": model, "input": texts})
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	var out struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 64<<20)).Decode(&out); err != nil {
		return nil, err
	}
	vecs := make([][]float32, len(texts))
	for _, d := range out.Data {
		if d.Index >= 0 && d.Index < len(vecs) {
			vecs[d.Index] = d.Embedding
		}
	}
	for i, v := range vecs {
		if len(v) == 0 {
			return nil, fmt.Errorf("embeddings: no vector for input %d", i)
		}
	}
	return vecs, nil
}
