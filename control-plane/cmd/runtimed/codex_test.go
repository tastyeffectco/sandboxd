package main

import (
	"strings"
	"testing"
)

// Happy path: thread start, a non-fatal item warning, tool activity, agent text,
// and a usage-bearing turn.completed.
func TestParseCodex_HappyPath(t *testing.T) {
	const stream = `
{"type":"thread.started","thread_id":"019f-abc"}
{"type":"turn.started"}
{"type":"item.completed","item":{"id":"i0","type":"error","message":"Model metadata for gpt-5 not found; using fallback."}}
{"type":"item.completed","item":{"id":"i1","type":"command_execution","command":"pnpm install"}}
{"type":"item.completed","item":{"id":"i2","type":"file_change","changes":[{"path":"src/App.tsx"},{"path":"src/main.tsx"}]}}
{"type":"item.completed","item":{"id":"i3","type":"agent_message","text":"Done — "}}
{"type":"item.completed","item":{"id":"i4","type":"agent_message","text":"created the todo app."}}
{"type":"turn.completed","usage":{"input_tokens":1200,"cached_input_tokens":200,"output_tokens":340}}
`
	c := &captured{}
	pr := parseCodexStream(strings.NewReader(stream), c.sink)

	if !pr.SawText {
		t.Fatal("expected SawText")
	}
	if pr.FinalMessage != "Done — created the todo app." {
		t.Fatalf("final message = %q", pr.FinalMessage)
	}
	if got := pr.Usage.Input; got != 1200 {
		t.Errorf("input tokens = %d", got)
	}
	if got := pr.Usage.Output; got != 340 {
		t.Errorf("output tokens = %d", got)
	}
	if pr.Usage.Total != 1200+340+200 {
		t.Errorf("total tokens = %d", pr.Usage.Total)
	}
	// thread.started → a session status event carrying the thread id.
	if st := c.ofType("status"); len(st) != 1 || st[0]["thread_id"] != "019f-abc" {
		t.Errorf("expected one session status with thread_id; got %v", st)
	}
	// two agent messages + one non-fatal agent_error surfaced.
	msgs := c.ofType("message")
	var agent, agentErr int
	for _, m := range msgs {
		switch m["role"] {
		case "agent":
			agent++
		case "agent_error":
			agentErr++
		}
	}
	if agent != 2 || agentErr != 1 {
		t.Errorf("agent=%d agent_error=%d (want 2,1)", agent, agentErr)
	}
	// tool events: one command + two file changes.
	if tools := c.ofType("tool"); len(tools) != 3 {
		t.Errorf("expected 3 tool events, got %d", len(tools))
	}
	if pr.ErrMsg != "" {
		t.Errorf("no top-level error expected, got %q", pr.ErrMsg)
	}
}

// A top-level error with no agent text is a real failure signal (the classifier
// uses ErrMsg only when SawText is false).
func TestParseCodex_FatalNoOutput(t *testing.T) {
	const stream = `
{"type":"thread.started","thread_id":"t1"}
{"type":"error","message":"401 Unauthorized: missing bearer"}
`
	c := &captured{}
	pr := parseCodexStream(strings.NewReader(stream), c.sink)
	if pr.SawText {
		t.Fatal("did not expect text")
	}
	if pr.ErrMsg == "" {
		t.Fatal("expected a captured top-level error")
	}
}
