package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/phoedox/phoedox/internal/ai"
)

func sse(w http.ResponseWriter, events ...string) {
	w.Header().Set("Content-Type", "text/event-stream")
	for _, e := range events {
		var typ struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal([]byte(e), &typ)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", typ.Type, e)
	}
}

const start = `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-5-5","content":[],"stop_reason":null,"usage":{"input_tokens":120,"output_tokens":1,"cache_read_input_tokens":80,"cache_creation_input_tokens":10}}}`

func TestStreamTextAndToolUse(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("api key header missing: %v", r.Header)
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &body)
		sse(w, start,
			`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Let me "}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"look."}}`,
			`{"type":"content_block_stop","index":0}`,
			`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"tu_1","name":"search","input":{}}}`,
			`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"query\":"}}`,
			`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"\"nas\"}"}}`,
			`{"type":"content_block_stop","index":1}`,
			`{"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"output_tokens":42}}`,
			`{"type":"message_stop"}`)
	}))
	defer srv.Close()
	p := New("test-key", srv.URL, "medium", nil)
	var text strings.Builder
	var tools []string
	turn, err := p.Stream(context.Background(), ai.Request{
		System:  []string{"You help.", "Today is Monday."},
		History: []ai.Message{{Raw: ai.UserMessage("Where is the NAS?", nil)}},
		Tools:   []ai.Tool{{Name: "search", Description: "Search", Schema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}`)}},
	}, func(e ai.Event) {
		switch e.Type {
		case "text":
			text.WriteString(e.Text)
		case "tool":
			tools = append(tools, e.Tool)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if text.String() != "Let me look." || len(tools) != 1 || tools[0] != "search" {
		t.Fatalf("events: %q %v", text.String(), tools)
	}
	if turn.Stop != ai.StopToolUse || len(turn.Calls) != 1 || string(turn.Calls[0].Input) != `{"query":"nas"}` {
		t.Fatalf("turn: %+v", turn)
	}
	if turn.Usage.Model != DefaultModel || turn.Usage.Input != 120 || turn.Usage.Output != 42 || turn.Usage.CacheRead != 80 || turn.Usage.CacheWrite != 10 {
		t.Fatalf("usage: %+v", turn.Usage)
	}
	// The stored message can be sent back as it is.
	var stored struct {
		Role    string           `json:"role"`
		Content []map[string]any `json:"content"`
	}
	if err := json.Unmarshal(turn.Message, &stored); err != nil || stored.Role != "assistant" || len(stored.Content) != 2 || stored.Content[1]["id"] != "tu_1" {
		t.Fatalf("message: %v %s", err, turn.Message)
	}
	// Request: model, effort, cache control, system parts, eager input streaming.
	if body["model"] != DefaultModel || body["cache_control"] == nil || body["output_config"].(map[string]any)["effort"] != "medium" {
		t.Fatalf("request: %v", body)
	}
	if sys := body["system"].([]any); len(sys) != 2 {
		t.Fatalf("system: %v", sys)
	}
	tool := body["tools"].([]any)[0].(map[string]any)
	if tool["eager_input_streaming"] != true || tool["input_schema"].(map[string]any)["required"].([]any)[0] != "query" {
		t.Fatalf("tool: %v", tool)
	}
}

func TestStreamCutOffToolInput(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sse(w, start,
			`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"tu_9","name":"edit_page","input":{}}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"markdown\":\"long te"}}`,
			`{"type":"message_delta","delta":{"stop_reason":"max_tokens","stop_sequence":null},"usage":{"output_tokens":16000}}`,
			`{"type":"message_stop"}`)
	}))
	defer srv.Close()
	turn, err := New("k", srv.URL, "", nil).Stream(context.Background(), ai.Request{History: []ai.Message{{Raw: ai.UserMessage("x", nil)}}}, func(ai.Event) {})
	if err != nil {
		t.Fatal(err)
	}
	if turn.Stop != ai.StopMaxTokens || !json.Valid(turn.Message) {
		t.Fatalf("turn: %+v %s", turn, turn.Message)
	}
	calls := ai.OpenCalls(turn.Message)
	if len(calls) != 1 || string(calls[0].Input) != "{}" {
		t.Fatalf("calls: %+v", calls)
	}
}

func TestStreamAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`))
	}))
	defer srv.Close()
	_, err := New("bad", srv.URL, "", nil).Stream(context.Background(), ai.Request{History: []ai.Message{{Raw: ai.UserMessage("x", nil)}}}, func(ai.Event) {})
	var apiErr *ai.APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusUnauthorized {
		t.Fatalf("error: %v", err)
	}
}

func TestReasoningOfOtherModelsIsDropped(t *testing.T) {
	var body struct {
		Messages []struct {
			Content []map[string]any `json:"content"`
		} `json:"messages"`
		OutputConfig any `json:"output_config"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &body)
		sse(w, start, `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":1}}`, `{"type":"message_stop"}`)
	}))
	defer srv.Close()
	asst := json.RawMessage(`{"role":"assistant","content":[{"type":"thinking","thinking":"hm","signature":"sig"},{"type":"text","text":"Hi"}]}`)
	hist := []ai.Message{{Raw: ai.UserMessage("a", nil)}, {Raw: asst, Model: "claude-sonnet-5-5"}, {Raw: ai.UserMessage("b", nil)}}
	if _, err := New("k", srv.URL, "", nil).Stream(context.Background(), ai.Request{Model: "claude-opus-5-5", History: hist}, func(ai.Event) {}); err != nil {
		t.Fatal(err)
	}
	if n := len(body.Messages[1].Content); n != 1 || body.Messages[1].Content[0]["type"] != "text" {
		t.Fatalf("thinking of another model must be dropped: %+v", body.Messages[1].Content)
	}
	if body.OutputConfig != nil {
		t.Fatalf("no effort configured, none sent: %v", body.OutputConfig)
	}
	// The same model keeps its reasoning.
	hist[1].Model = "claude-opus-5-5"
	_, _ = New("k", srv.URL, "", nil).Stream(context.Background(), ai.Request{Model: "claude-opus-5-5", History: hist}, func(ai.Event) {})
	if len(body.Messages[1].Content) != 2 {
		t.Fatalf("own reasoning must stay: %+v", body.Messages[1].Content)
	}
}

func TestModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"claude-opus-5-5","type":"model","display_name":"Opus","created_at":"2026-01-01T00:00:00Z"}],"has_more":false,"first_id":"a","last_id":"a"}`))
	}))
	defer srv.Close()
	ms, err := New("k", srv.URL, "", nil).Models(context.Background())
	if err != nil || len(ms) != 1 || ms[0] != "claude-opus-5-5" {
		t.Fatalf("models: %v %v", err, ms)
	}
}
