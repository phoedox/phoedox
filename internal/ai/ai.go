// Package ai holds the model providers of the AI chat (ADR-0066).
//
// Chats are stored in the Messages API format (role + content blocks: text,
// thinking, tool_use, tool_result). The format is the storage format for every
// provider: an adapter for another API translates on the way in and out. The
// history is only appended to, so reasoning blocks of earlier turns stay valid.
package ai

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
)

// Tool is a function the model may call.
type Tool struct {
	Name        string
	Description string
	Schema      json.RawMessage // JSON schema of the input (an object)
}

// ToolCall is a call the model asked for.
type ToolCall struct {
	ID    string
	Name  string
	Input json.RawMessage
}

// ToolResult answers a ToolCall.
type ToolResult struct {
	ID      string
	Content string
	IsError bool
}

// Usage counts the tokens of one model call.
type Usage struct {
	Model      string
	Input      int64
	Output     int64
	CacheRead  int64
	CacheWrite int64
}

// Message is a stored message and the model that wrote it ("" for the
// person's messages). Adapters drop reasoning blocks of other models.
type Message struct {
	Raw   json.RawMessage
	Model string
}

// Request is one call of the model.
type Request struct {
	Model string
	// System holds the instructions: the first part is the same for every
	// chat (cached), the rest describes the situation (project, page, date).
	System    []string
	History   []Message
	Tools     []Tool
	MaxTokens int
}

// Event is streamed while the model answers.
type Event struct {
	Type string `json:"type"`           // text | tool
	Text string `json:"text,omitempty"` // text: the next piece of the answer
	Tool string `json:"tool,omitempty"` // tool: name of a tool the model starts to call
}

// Stop reasons that need attention.
const (
	StopToolUse   = "tool_use"
	StopMaxTokens = "max_tokens"
	StopRefusal   = "refusal"
)

// Turn is the complete answer of one call.
type Turn struct {
	Message json.RawMessage // assistant message to append to the history
	Calls   []ToolCall
	Stop    string
	Usage   Usage
}

// Provider calls the models of one API (Anthropic, or an OpenAI-compatible
// API such as OpenAI, Mistral, Kimi, Ollama or a gateway).
type Provider interface {
	Stream(ctx context.Context, req Request, emit func(Event)) (Turn, error)
	// Models lists the models the API offers (to choose from when an admin
	// sets up the provider).
	Models(ctx context.Context) ([]string, error)
}

// Kinds of providers.
const (
	KindAnthropic = "anthropic"
	KindOpenAI    = "openai" // OpenAI-compatible chat completions
)

// ErrNotConfigured means no provider is set up on this instance.
var ErrNotConfigured = errors.New("ai: no provider configured")

// Price is the cost of a model in US dollars per million tokens.
type Price struct {
	Input, Output, CacheRead, CacheWrite float64
}

// Prices of known models (USD per million tokens); cache writes cost 1.25×
// the input price (5-minute cache).
var Prices = map[string]Price{
	"claude-opus-5-5":   {Input: 4, Output: 20, CacheRead: 0.20, CacheWrite: 5},
	"claude-sonnet-5-5": {Input: 2, Output: 10, CacheRead: 0.10, CacheWrite: 2.5},
	"claude-haiku-5-5":  {Input: 0.10, Output: 0.50, CacheRead: 0.01, CacheWrite: 0.125},
}

// PriceOf returns the price of a model; unknown models cost like the most
// expensive known one, so budgets never run over.
func PriceOf(model string, override *Price) Price {
	if override != nil {
		return *override
	}
	if p, ok := Prices[model]; ok {
		return p
	}
	return Prices["claude-opus-5-5"]
}

// CostMicros returns the cost of a call in US dollars × 10⁶ (a price in
// dollars per million tokens is micro-dollars per token).
func CostMicros(u Usage, p Price) int64 {
	c := float64(u.Input)*p.Input + float64(u.Output)*p.Output + float64(u.CacheRead)*p.CacheRead + float64(u.CacheWrite)*p.CacheWrite
	return int64(math.Ceil(c))
}

// StripReasoning removes reasoning blocks (thinking) from an assistant
// message; they are only valid for the model that wrote them.
func StripReasoning(raw json.RawMessage) json.RawMessage {
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return raw
	}
	var blocks []map[string]json.RawMessage
	if json.Unmarshal(m["content"], &blocks) != nil {
		return raw
	}
	kept := blocks[:0]
	for _, b := range blocks {
		var typ string
		_ = json.Unmarshal(b["type"], &typ)
		if typ != "thinking" && typ != "redacted_thinking" {
			kept = append(kept, b)
		}
	}
	m["content"], _ = json.Marshal(kept)
	out, err := json.Marshal(m)
	if err != nil {
		return raw
	}
	return out
}

// UserMessage builds a user message: tool results first (they must answer
// the calls of the previous assistant message), then the text.
func UserMessage(text string, results []ToolResult) json.RawMessage {
	type block struct {
		Type      string `json:"type"`
		Text      string `json:"text,omitempty"`
		ToolUseID string `json:"tool_use_id,omitempty"`
		Content   string `json:"content,omitempty"`
		IsError   bool   `json:"is_error,omitempty"`
	}
	blocks := []block{}
	for _, r := range results {
		c := r.Content
		if c == "" {
			c = "ok"
		}
		blocks = append(blocks, block{Type: "tool_result", ToolUseID: r.ID, Content: c, IsError: r.IsError})
	}
	if strings.TrimSpace(text) != "" {
		blocks = append(blocks, block{Type: "text", Text: text})
	}
	b, _ := json.Marshal(map[string]any{"role": "user", "content": blocks})
	return b
}

// Part is a piece of a stored message prepared for display.
type Part struct {
	Type    string          `json:"type"` // text | tool_use | tool_result
	Text    string          `json:"text,omitempty"`
	ID      string          `json:"id,omitempty"`
	Name    string          `json:"name,omitempty"`
	Input   json.RawMessage `json:"input,omitempty"`
	IsError bool            `json:"isError,omitempty"`
}

type rawBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
}

func blocksOf(raw json.RawMessage) []rawBlock {
	var m struct {
		Content json.RawMessage `json:"content"`
	}
	if json.Unmarshal(raw, &m) != nil {
		return nil
	}
	var s string
	if json.Unmarshal(m.Content, &s) == nil {
		return []rawBlock{{Type: "text", Text: s}}
	}
	var blocks []rawBlock
	_ = json.Unmarshal(m.Content, &blocks)
	return blocks
}

// Parts returns the visible parts of a stored message. Reasoning blocks are
// left out; tool results keep only whether they failed and a short text.
func Parts(raw json.RawMessage) []Part {
	out := []Part{}
	for _, b := range blocksOf(raw) {
		switch b.Type {
		case "text":
			out = append(out, Part{Type: "text", Text: b.Text})
		case "tool_use":
			out = append(out, Part{Type: "tool_use", ID: b.ID, Name: b.Name, Input: b.Input})
		case "tool_result":
			var text string
			if json.Unmarshal(b.Content, &text) != nil {
				text = string(b.Content)
			}
			if len(text) > 300 {
				text = text[:300] + "…"
			}
			out = append(out, Part{Type: "tool_result", ID: b.ToolUseID, Text: text, IsError: b.IsError})
		}
	}
	return out
}

// OpenCalls returns the tool calls of an assistant message (to answer calls
// that were left open, e.g. after an aborted turn).
func OpenCalls(raw json.RawMessage) []ToolCall {
	var out []ToolCall
	for _, b := range blocksOf(raw) {
		if b.Type == "tool_use" {
			out = append(out, ToolCall{ID: b.ID, Name: b.Name, Input: b.Input})
		}
	}
	return out
}

// APIError is an error answer of the provider (e.g. 429 overloaded, 401
// wrong key); the chat shows a translated message instead of its text.
type APIError struct {
	Status int
	Err    error
}

func (e *APIError) Error() string { return e.Err.Error() }
func (e *APIError) Unwrap() error { return e.Err }
