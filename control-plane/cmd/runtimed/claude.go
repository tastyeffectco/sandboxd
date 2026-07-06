package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/tastyeffectco/sandboxd/control-plane/internal/runtime"
)

// claudeCodeAgent drives the official Claude Code CLI:
//
//	claude -p <prompt> --output-format stream-json --verbose --dangerously-skip-permissions
//
// It runs in the sandbox with HOME pointed at the mounted agent-auth dir
// (/run/agent-home), so the CLI authenticates with the owner's imported Claude
// credentials. This is the claude-code provider's runner — it is NOT used for
// opencode, and claude credentials are only meaningful here.
type claudeCodeAgent struct{ log *slog.Logger }

func (c *claudeCodeAgent) name() string { return "claude-code" }

// claudeEvent is one line of `claude … --output-format stream-json`. Only the
// fields runtimed maps are declared; the rest is treated as opaque.
type claudeEvent struct {
	Type    string  `json:"type"`    // system | assistant | user | result
	Subtype string  `json:"subtype"` // on result: success | error_* …
	Model   string  `json:"model"`   // on system/init: the RESOLVED model id
	Result  string  `json:"result"`  // on result: the final assistant text
	IsError bool    `json:"is_error"`
	Cost    float64 `json:"total_cost_usd"`
	Usage   struct {
		Input       int `json:"input_tokens"`
		Output      int `json:"output_tokens"`
		CacheRead   int `json:"cache_read_input_tokens"`
		CacheCreate int `json:"cache_creation_input_tokens"`
	} `json:"usage"`
	Message struct {
		Content []struct {
			Type  string          `json:"type"` // text | tool_use | tool_result
			Text  string          `json:"text"`
			Name  string          `json:"name"`  // tool name on tool_use
			Input json.RawMessage `json:"input"` // tool args
		} `json:"content"`
	} `json:"message"`
	// Error is tolerated as string | object | null: real claude puts a bare
	// string here on the assistant turn (e.g. "authentication_failed"), so a
	// strongly-typed struct would fail the whole line's unmarshal.
	Error json.RawMessage `json:"error"`
}

// hasError reports whether a raw `error` field is present and non-null.
func hasError(raw json.RawMessage) bool {
	s := strings.TrimSpace(string(raw))
	return s != "" && s != "null" && s != `""`
}

// errString renders a raw `error` (string or {message:…} object) as a message.
func errString(raw json.RawMessage) string {
	if !hasError(raw) {
		return "claude error"
	}
	var s string
	if json.Unmarshal(raw, &s) == nil && s != "" {
		return s
	}
	var obj struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(raw, &obj) == nil && obj.Message != "" {
		return obj.Message
	}
	return "claude error"
}

type claudeParseResult struct {
	FinalMessage string
	Usage        runtime.TokenUsage
	SawText      bool
	SawTool      bool
	APIErr       string
}

// parseClaudeStream consumes NDJSON from r, dispatches canonical events, and
// returns a structured summary. Pure — unit-testable without spawning claude.
func parseClaudeStream(r io.Reader, emit eventSink) claudeParseResult {
	var pr claudeParseResult
	var acc strings.Builder

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		raw := sc.Bytes()
		var ev claudeEvent
		if json.Unmarshal(raw, &ev) != nil {
			// Non-JSON line. claude prints "Not logged in · Please run /login"
			// (and exits 0) when unauthenticated — capture it as an error so
			// the task is classified as failed, not silently empty.
			s := strings.TrimSpace(string(raw))
			if pr.APIErr == "" && s != "" && (strings.Contains(s, "Not logged in") || strings.Contains(s, "Invalid API key")) {
				pr.APIErr = s
			}
			continue
		}
		switch ev.Type {
		case "system":
			// The init event reports the RESOLVED model (an alias like "sonnet"
			// becomes e.g. "claude-sonnet-5"). Surface it so the user sees which
			// model actually ran — the model's own "what model are you" answer is
			// unreliable (it reports its system-prompt identity).
			if ev.Subtype == "init" && ev.Model != "" {
				emit(runtime.EventStatus, map[string]any{"phase": "model", "model": ev.Model})
			}
		case "assistant":
			// An assistant turn carrying a top-level error (e.g. auth failure)
			// is NOT real output: capture its text as the failure reason and do
			// not emit it as a normal agent message.
			errTurn := hasError(ev.Error)
			for _, blk := range ev.Message.Content {
				switch blk.Type {
				case "text":
					if blk.Text == "" {
						continue
					}
					if errTurn {
						if pr.APIErr == "" {
							pr.APIErr = blk.Text
						}
						continue
					}
					pr.SawText = true
					acc.WriteString(blk.Text)
					emit(runtime.EventMessage, map[string]any{"role": "agent", "text": blk.Text})
				case "tool_use":
					if blk.Name != "" {
						pr.SawTool = true
						emit(runtime.EventTool, map[string]any{
							"name":   blk.Name,
							"status": "running",
							"path":   toolTarget(blk.Input),
						})
					}
				}
			}
		case "result":
			if ev.Result != "" {
				pr.FinalMessage = ev.Result
			}
			pr.Usage.Input += ev.Usage.Input
			pr.Usage.Output += ev.Usage.Output
			pr.Usage.CacheRead += ev.Usage.CacheRead
			pr.Usage.CacheWrite += ev.Usage.CacheCreate
			pr.Usage.Cost += ev.Cost
			if ev.IsError || (ev.Subtype != "" && ev.Subtype != "success") {
				if pr.APIErr == "" {
					switch {
					case ev.Result != "":
						pr.APIErr = ev.Result
					case ev.Subtype != "":
						pr.APIErr = ev.Subtype
					default:
						pr.APIErr = "claude reported an error"
					}
				}
			}
		case "error":
			if pr.APIErr == "" {
				pr.APIErr = errString(ev.Error)
			}
		}
	}
	if pr.FinalMessage == "" {
		pr.FinalMessage = acc.String()
	}
	pr.Usage.Total = pr.Usage.Input + pr.Usage.Output + pr.Usage.Reasoning +
		pr.Usage.CacheRead + pr.Usage.CacheWrite
	return pr
}

func (c *claudeCodeAgent) run(ctx context.Context, spec agentSpec, emit eventSink) (string, runtime.TokenUsage, error) {
	var usage runtime.TokenUsage
	args := []string{"-p", "--output-format", "stream-json", "--verbose", "--dangerously-skip-permissions"}
	// Per-task model (claude accepts an alias like "sonnet"/"opus" or a full id).
	if spec.model != "" {
		args = append(args, "--model", spec.model)
	}
	// Platform briefing → appended to claude's default system prompt (out of the
	// workspace, so it can't be committed or edited by the agent).
	if spec.systemPrompt != "" {
		args = append(args, "--append-system-prompt", spec.systemPrompt)
	}
	args = append(args, spec.prompt)
	cmd := exec.Command("claude", args...)
	cmd.Dir = spec.workDir
	// Scrub secret-shaped vars and point HOME at THIS agent's mounted auth dir
	// (/run/agent-auth/claude-code = the imported Claude creds), keyed on the
	// agent name so it works even when the sandbox default is opencode.
	cmd.Env = agentEnv(c.name(), spec.env)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", usage, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", usage, err
	}
	if err := cmd.Start(); err != nil {
		return "", usage, fmt.Errorf("start claude: %w", err)
	}
	pgid := cmd.Process.Pid

	finished := make(chan struct{})
	go func() {
		select {
		case <-finished:
		case <-ctx.Done():
			_ = syscall.Kill(-pgid, syscall.SIGTERM)
			t := time.NewTimer(5 * time.Second)
			defer t.Stop()
			select {
			case <-finished:
			case <-t.C:
				_ = syscall.Kill(-pgid, syscall.SIGKILL)
			}
		}
	}()
	defer close(finished)

	stderrDone := make(chan struct{})
	go func() {
		_, _ = io.Copy(spec.rawLog, stderr)
		close(stderrDone)
	}()

	pr := parseClaudeStream(stdout, emit)
	waitErr := cmd.Wait()
	<-stderrDone

	switch {
	case ctx.Err() != nil:
		return pr.FinalMessage, pr.Usage, nil
	case pr.APIErr != "":
		return pr.FinalMessage, pr.Usage, fmt.Errorf("agent error: %s", pr.APIErr)
	case waitErr != nil:
		return pr.FinalMessage, pr.Usage, fmt.Errorf("claude exited: %w", waitErr)
	case !pr.SawText && !pr.SawTool && pr.FinalMessage == "":
		return pr.FinalMessage, pr.Usage,
			fmt.Errorf("agent produced no output (claude exited with zero events)")
	}
	return pr.FinalMessage, pr.Usage, nil
}
