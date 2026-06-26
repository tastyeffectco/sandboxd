package main

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type capturedEvent struct {
	typ  string
	data any
}

func collectSink() (eventSink, *[]capturedEvent) {
	var evs []capturedEvent
	return func(t string, d any) { evs = append(evs, capturedEvent{t, d}) }, &evs
}

func TestParseClaudeStreamSuccess(t *testing.T) {
	stream := strings.Join([]string{
		`{"type":"system","subtype":"init","session_id":"s"}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"Hello "},{"type":"tool_use","name":"Edit","input":{"file_path":"app.js"}}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"world"}]}}`,
		`{"type":"result","subtype":"success","result":"Hello world","is_error":false,"total_cost_usd":0.01,"usage":{"input_tokens":10,"output_tokens":5}}`,
	}, "\n")
	sink, evs := collectSink()
	pr := parseClaudeStream(strings.NewReader(stream), sink)

	if pr.FinalMessage != "Hello world" {
		t.Errorf("final = %q", pr.FinalMessage)
	}
	if !pr.SawText || !pr.SawTool {
		t.Errorf("sawText=%v sawTool=%v", pr.SawText, pr.SawTool)
	}
	if pr.APIErr != "" {
		t.Errorf("unexpected APIErr %q", pr.APIErr)
	}
	if pr.Usage.Input != 10 || pr.Usage.Output != 5 || pr.Usage.Total != 15 {
		t.Errorf("usage = %+v", pr.Usage)
	}
	if pr.Usage.Cost != 0.01 {
		t.Errorf("cost = %v", pr.Usage.Cost)
	}
	// events: 2 messages + 1 tool
	var msgs, tools int
	for _, e := range *evs {
		switch e.typ {
		case "message":
			msgs++
		case "tool":
			tools++
			if m, ok := e.data.(map[string]any); ok && m["path"] != "app.js" {
				t.Errorf("tool path = %v", m["path"])
			}
		}
	}
	if msgs != 2 || tools != 1 {
		t.Errorf("emitted msgs=%d tools=%d", msgs, tools)
	}
}

func TestParseClaudeStreamNotLoggedIn(t *testing.T) {
	// claude prints this (non-JSON) and exits 0 when unauthenticated.
	sink, _ := collectSink()
	pr := parseClaudeStream(strings.NewReader("Not logged in · Please run /login\n"), sink)
	if pr.APIErr == "" || !strings.Contains(pr.APIErr, "Not logged in") {
		t.Errorf("expected a Not-logged-in APIErr; got %q", pr.APIErr)
	}
	if pr.SawText || pr.SawTool {
		t.Error("should not have seen text/tool output")
	}
}

func TestParseClaudeStreamResultError(t *testing.T) {
	sink, _ := collectSink()
	pr := parseClaudeStream(strings.NewReader(
		`{"type":"result","subtype":"error_during_execution","is_error":true,"result":"boom"}`+"\n"), sink)
	if pr.APIErr != "boom" {
		t.Errorf("APIErr = %q; want boom", pr.APIErr)
	}
}

func TestSelectAgentClaudeCode(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	a, err := selectAgent("claude-code", log)
	if err != nil || a.name() != "claude-code" {
		t.Fatalf("selectAgent(claude-code) = %v, %v", a, err)
	}
	if _, err := selectAgent("codex", log); err == nil {
		t.Error("codex should be unsupported")
	}
}

// Fake `claude` binary: proves run() execs the CLI, injects HOME from
// RUNTIMED_AGENT_HOME, scrubs env, and parses the stream into a final message.
func TestClaudeAgentRunWithFakeBinary(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "claude")
	// Emits a result whose text echoes $HOME, proving HOME was injected.
	script := "#!/bin/sh\n" +
		`printf '{"type":"assistant","message":{"content":[{"type":"text","text":"hi"}]}}\n'` + "\n" +
		`printf '{"type":"result","subtype":"success","result":"home=%s","usage":{"input_tokens":1,"output_tokens":1}}\n' "$HOME"` + "\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	t.Setenv("RUNTIMED_AGENT_HOME", "/run/agent-home")
	// A secret in the inherited env must NOT reach the agent.
	t.Setenv("ANTHROPIC_API_KEY", "sk-should-be-scrubbed")

	sink, _ := collectSink()
	a := &claudeCodeAgent{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	final, usage, err := a.run(context.Background(),
		agentSpec{workDir: t.TempDir(), prompt: "do it", env: nil, rawLog: io.Discard}, sink)
	if err != nil {
		t.Fatalf("run err: %v", err)
	}
	if final != "home=/run/agent-home" {
		t.Errorf("HOME not injected into agent; final = %q", final)
	}
	if usage.Total == 0 {
		t.Error("usage not parsed")
	}
}
