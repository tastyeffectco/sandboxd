package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func collect(t *testing.T, upstream string, contentType string) ([]agentEvent, error, *http.Request) {
	t.Helper()
	var got *http.Request
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r
		body, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", contentType)
		io.WriteString(w, upstream)
	}))
	defer srv.Close()
	var events []agentEvent
	req := v1AgentStreamReq{ConversationID: "conv-1"}
	req.Messages = append(req.Messages, struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}{"user", "hi"})
	err := streamAgentChat(context.Background(), srv.URL+"/v1/chat/completions", "k", "hermes-agent", req,
		func(e agentEvent) { events = append(events, e) })
	var sent map[string]any
	_ = json.Unmarshal(body, &sent)
	if sent["stream"] != true || sent["user"] != "conv-1" {
		t.Errorf("request body: %s", body)
	}
	return events, err, got
}

func names(events []agentEvent) string {
	var out []string
	for _, e := range events {
		b, _ := json.Marshal(e.data)
		out = append(out, e.name+" "+string(b))
	}
	return strings.Join(out, "\n")
}

// Hermes streams reasoning, its tool progress as its own events, then text.
func TestAgentStreamHermes(t *testing.T) {
	upstream := `data: {"choices":[{"delta":{"role":"assistant"}}]}

data: {"choices":[{"delta":{"reasoning_content":"Let me look"}}]}

event: hermes.tool.progress
data: {"tool":"mcp__sandboxd__list_apps","label":"mcp__sandboxd__list_apps","toolCallId":"c1","status":"running"}

event: hermes.tool.progress
data: {"tool":"mcp__sandboxd__list_apps","toolCallId":"c1","status":"completed"}

data: {"choices":[{"delta":{"content":"You have "}}]}

data: {"choices":[{"delta":{"content":"7 apps."}}]}

data: {"choices":[{"delta":{},"finish_reason":"stop"}]}

data: [DONE]
`
	events, err, r := collect(t, upstream, "text/event-stream")
	if err != nil {
		t.Fatal(err)
	}
	want := `thinking {"delta":"Let me look"}
step {"id":"c1","tool":"mcp__sandboxd__list_apps","label":"mcp__sandboxd__list_apps","status":"running"}
step {"id":"c1","tool":"mcp__sandboxd__list_apps","status":"done"}
text {"delta":"You have "}
text {"delta":"7 apps."}`
	if got := names(events); got != want {
		t.Fatalf("events:\n%s\nwant:\n%s", got, want)
	}
	if r.Header.Get("X-Hermes-Session-Id") != "conv-1" || r.Header.Get("Authorization") != "Bearer k" {
		t.Errorf("headers: %v", r.Header)
	}
}

// Standard OpenAI streams announce tool calls in pieces; each becomes one step.
func TestAgentStreamToolCalls(t *testing.T) {
	upstream := `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"t1","function":{"name":"sandboxd__"}}]}}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"name":"create_app"}}]}}]}

data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}

data: {"choices":[{"delta":{"content":"Done."}}]}

data: [DONE]
`
	events, err, _ := collect(t, upstream, "text/event-stream")
	if err != nil {
		t.Fatal(err)
	}
	got := names(events)
	if !strings.Contains(got, `step {"id":"t1","tool":"sandboxd__","status":"running"}`) ||
		!strings.Contains(got, `step {"id":"t1","tool":"sandboxd__create_app","status":"done"}`) ||
		!strings.HasSuffix(got, `text {"delta":"Done."}`) {
		t.Fatalf("events:\n%s", got)
	}
}

func TestAgentStreamPlainJSONAndErrors(t *testing.T) {
	events, err, _ := collect(t, `{"choices":[{"message":{"content":"Hello"}}]}`, "application/json")
	if err != nil || names(events) != `text {"delta":"Hello"}` {
		t.Fatalf("plain answer: %v %s", err, names(events))
	}
	_, err, _ = collect(t, "data: {\"error\":{\"message\":\"model refused\"}}\n\n", "text/event-stream")
	if err == nil || !strings.Contains(err.Error(), "model refused") {
		t.Fatalf("stream error not reported: %v", err)
	}
	if msg := plainAgentError("Hermes", agentUnavailable{io.EOF}); !strings.Contains(msg, "isn't answering") {
		t.Fatal(msg)
	}
}
