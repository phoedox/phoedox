package openai

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

func chunks(w http.ResponseWriter, lines ...string) {
	w.Header().Set("Content-Type", "text/event-stream")
	for _, l := range lines {
		fmt.Fprintf(w, "data: %s\n\n", l)
	}
	fmt.Fprint(w, "data: [DONE]\n\n")
}

func TestStreamTranslatesBothWays(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer sk-test" {
			t.Errorf("request: %s %s", r.URL.Path, r.Header.Get("Authorization"))
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &body)
		chunks(w,
			`{"choices":[{"delta":{"content":"Let me "}}]}`,
			`{"choices":[{"delta":{"content":"search."}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"abcDEF123","type":"function","function":{"name":"search","arguments":""}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"query\":"}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"nas\"}"}}]},"finish_reason":"tool_calls"}]}`,
			`{"choices":[],"usage":{"prompt_tokens":1000,"completion_tokens":50,"prompt_tokens_details":{"cached_tokens":400}}}`)
	}))
	defer srv.Close()

	// History with reasoning, a Claude tool call and its result.
	hist := []ai.Message{
		{Raw: ai.UserMessage("Where is the NAS?", nil)},
		{Raw: json.RawMessage(`{"role":"assistant","content":[{"type":"thinking","thinking":"x","signature":"s"},{"type":"text","text":"Checking."},{"type":"tool_use","id":"toolu_01ABC","name":"get_entity","input":{"entity":"nas"}}]}`), Model: "claude-opus-5-5"},
		{Raw: ai.UserMessage("", []ai.ToolResult{{ID: "toolu_01ABC", Content: "not found", IsError: true}})},
		{Raw: ai.UserMessage("Search for it", nil)},
	}
	var text strings.Builder
	var tools []string
	turn, err := New(srv.URL+"/v1/", "sk-test", nil).Stream(context.Background(), ai.Request{Model: "mistral-large-latest", System: []string{"Rules.", "Situation."}, History: hist,
		Tools: []ai.Tool{{Name: "search", Description: "Search", Schema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}}}`)}}},
		func(e ai.Event) {
			if e.Type == "text" {
				text.WriteString(e.Text)
			} else {
				tools = append(tools, e.Tool)
			}
		})
	if err != nil {
		t.Fatal(err)
	}
	if text.String() != "Let me search." || len(tools) != 1 || tools[0] != "search" {
		t.Fatalf("events: %q %v", text.String(), tools)
	}
	if turn.Stop != ai.StopToolUse || len(turn.Calls) != 1 || turn.Calls[0].ID != "abcDEF123" || string(turn.Calls[0].Input) != `{"query":"nas"}` {
		t.Fatalf("turn: %+v", turn)
	}
	if turn.Usage.Input != 600 || turn.Usage.CacheRead != 400 || turn.Usage.Output != 50 || turn.Usage.Model != "mistral-large-latest" {
		t.Fatalf("usage: %+v", turn.Usage)
	}
	// The stored answer is in Messages format.
	calls := ai.OpenCalls(turn.Message)
	if len(calls) != 1 || calls[0].Name != "search" {
		t.Fatalf("message: %s", turn.Message)
	}

	// Request: system, user, assistant with tool_calls (no reasoning), tool, user.
	msgs := body["messages"].([]any)
	roles := []string{}
	for _, m := range msgs {
		roles = append(roles, m.(map[string]any)["role"].(string))
	}
	if strings.Join(roles, ",") != "system,user,assistant,tool,user" {
		t.Fatalf("roles: %v", roles)
	}
	asst := msgs[2].(map[string]any)
	tc := asst["tool_calls"].([]any)[0].(map[string]any)
	tool := msgs[3].(map[string]any)
	if asst["content"] != "Checking." || tc["id"] != tool["tool_call_id"] || len(tc["id"].(string)) != 9 || !strings.HasPrefix(tool["content"].(string), "Error: ") {
		t.Fatalf("assistant/tool: %v %v", asst, tool)
	}
	if body["model"] != "mistral-large-latest" || body["tools"].([]any)[0].(map[string]any)["type"] != "function" {
		t.Fatalf("body: %v", body)
	}
}

func TestStreamWithoutStreamOptions(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		b, _ := io.ReadAll(r.Body)
		if strings.Contains(string(b), "stream_options") {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"unknown field stream_options"}`))
			return
		}
		chunks(w, `{"choices":[{"delta":{"content":"Hi"},"finish_reason":"stop","usage":{"prompt_tokens":10,"completion_tokens":2}}]}`)
	}))
	defer srv.Close()
	turn, err := New(srv.URL, "", nil).Stream(context.Background(), ai.Request{Model: "kimi-k2", History: []ai.Message{{Raw: ai.UserMessage("x", nil)}}}, func(ai.Event) {})
	if err != nil || calls != 2 || turn.Stop != "end_turn" || turn.Usage.Input != 10 {
		t.Fatalf("retry: %v %d %+v", err, calls, turn)
	}
}

func TestErrorsAndModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			_, _ = w.Write([]byte(`{"data":[{"id":"gpt-5"},{"id":"gpt-5-mini"}]}`))
			return
		}
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	p := New(srv.URL, "k", nil)
	ms, err := p.Models(context.Background())
	if err != nil || strings.Join(ms, ",") != "gpt-5,gpt-5-mini" {
		t.Fatalf("models: %v %v", err, ms)
	}
	_, err = p.Stream(context.Background(), ai.Request{Model: "gpt-5", History: []ai.Message{{Raw: ai.UserMessage("x", nil)}}}, func(ai.Event) {})
	var apiErr *ai.APIError
	if !errorsAs(err, &apiErr) || apiErr.Status != 429 {
		t.Fatalf("error: %v", err)
	}
}

func TestCallID(t *testing.T) {
	if callID("abcDEF123") != "abcDEF123" || callID("toolu_01X") != callID("toolu_01X") || len(callID("call_very_long_id")) != 9 {
		t.Fatal("callID")
	}
}

func errorsAs(err error, target **ai.APIError) bool { return errors.As(err, target) }
