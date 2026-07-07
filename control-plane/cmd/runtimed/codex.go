package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/tastyeffectco/sandboxd/control-plane/internal/runtime"
)

// codexAgent drives the OpenAI Codex CLI non-interactively:
//
//	codex exec --json --skip-git-repo-check --dangerously-bypass-approvals-and-sandbox
//	           -C <workdir> -o <lastmsg> [-m <model>] [resume --last] <prompt>
//
// --json streams JSONL "thread item" events to stdout (we map them to task
// events); -o writes the final agent message to a file (the authoritative
// result). --dangerously-bypass-approvals-and-sandbox is correct here — the
// containment boundary is the throwaway sandbox container, not the agent (the
// same stance as claude's --dangerously-skip-permissions).
type codexAgent struct{ log *slog.Logger }

func (c *codexAgent) name() string { return "codex" }

// codexEvent is the subset of a `codex exec --json` JSONL line we map. The
// schema is otherwise treated as opaque.
type codexEvent struct {
	Type     string `json:"type"`      // thread.started | turn.started | item.completed | turn.completed | error
	ThreadID string `json:"thread_id"` // on thread.started
	Message  string `json:"message"`   // on top-level {"type":"error"}
	Item     *struct {
		ID      string `json:"id"`
		Type    string `json:"type"` // agent_message | reasoning | command_execution | file_change | error
		Text    string `json:"text"`
		Message string `json:"message"`
		Command string `json:"command"`
		Changes []struct {
			Path string `json:"path"`
		} `json:"changes"`
	} `json:"item"`
	Usage *struct {
		InputTokens       int `json:"input_tokens"`
		CachedInputTokens int `json:"cached_input_tokens"`
		OutputTokens      int `json:"output_tokens"`
	} `json:"usage"`
}

type codexParseResult struct {
	FinalMessage string
	Usage        runtime.TokenUsage
	SawText      bool
	SawTool      bool
	ErrMsg       string // last top-level error; only fatal if no text was produced
}

// parseCodexStream consumes `codex exec --json` NDJSON, dispatches canonical
// events through emit, and returns a structured summary. Pure — no process
// management — so it's unit-testable from a string reader.
func parseCodexStream(r io.Reader, emit eventSink) codexParseResult {
	var pr codexParseResult
	var b strings.Builder
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 8*1024*1024)
	for sc.Scan() {
		var ev codexEvent
		if json.Unmarshal(sc.Bytes(), &ev) != nil {
			continue // best-effort: skip lines we can't parse
		}
		switch ev.Type {
		case "thread.started":
			if ev.ThreadID != "" {
				emit(runtime.EventStatus, map[string]any{"phase": "session", "thread_id": ev.ThreadID})
			}
		case "error":
			// Top-level errors include transient reconnects; keep the last one but
			// only treat it as fatal if no agent text was ultimately produced.
			if ev.Message != "" {
				pr.ErrMsg = ev.Message
			}
		case "item.completed":
			if ev.Item == nil {
				continue
			}
			switch ev.Item.Type {
			case "agent_message":
				if ev.Item.Text != "" {
					pr.SawText = true
					b.WriteString(ev.Item.Text)
					emit(runtime.EventMessage, map[string]any{"role": "agent", "text": ev.Item.Text})
				}
			case "command_execution":
				pr.SawTool = true
				emit(runtime.EventTool, map[string]any{"name": "bash", "status": "completed", "path": trim(ev.Item.Command)})
			case "file_change":
				pr.SawTool = true
				for _, ch := range ev.Item.Changes {
					emit(runtime.EventTool, map[string]any{"name": "edit", "status": "completed", "path": ch.Path})
				}
			case "error":
				// A non-fatal item error (e.g. a model-metadata warning). Surface it,
				// but don't fail the task on it.
				if ev.Item.Message != "" {
					emit(runtime.EventMessage, map[string]any{"role": "agent_error", "text": ev.Item.Message})
				}
			}
		case "turn.completed":
			if ev.Usage != nil {
				pr.Usage.Input += ev.Usage.InputTokens
				pr.Usage.Output += ev.Usage.OutputTokens
				pr.Usage.CacheRead += ev.Usage.CachedInputTokens
			}
		}
	}
	pr.Usage.Total = pr.Usage.Input + pr.Usage.Output + pr.Usage.CacheRead
	pr.FinalMessage = b.String()
	return pr
}

func trim(s string) string {
	if len(s) > 200 {
		return s[:200]
	}
	return s
}

func (c *codexAgent) run(ctx context.Context, spec agentSpec, emit eventSink) (string, runtime.TokenUsage, error) {
	var usage runtime.TokenUsage

	// -o writes the final agent message to a file we read afterward (the clean
	// authoritative result); the JSONL stream drives live events.
	outFile, ferr := os.CreateTemp("", "codex-last-*.txt")
	if ferr != nil {
		return "", usage, fmt.Errorf("codex: temp file: %w", ferr)
	}
	outPath := outFile.Name()
	_ = outFile.Close()
	defer os.Remove(outPath)

	args := []string{"exec", "--json", "--skip-git-repo-check",
		"--dangerously-bypass-approvals-and-sandbox", "-C", spec.workDir, "-o", outPath}
	// Model applies to a fresh session; a resumed session keeps its recorded model.
	if spec.model != "" && !spec.cont {
		args = append(args, "-m", spec.model)
	}
	if spec.cont {
		args = append(args, "resume", "--last")
	}
	// Codex has no system-prompt flag; deliver the platform briefing as a prompt
	// preamble on a FRESH run only (on resume it's already in the session).
	prompt := spec.prompt
	if spec.systemPrompt != "" && !spec.cont {
		prompt = spec.systemPrompt + "\n\n---\n\n# Your task\n\n" + spec.prompt
	}
	args = append(args, prompt)

	cmd := exec.Command("codex", args...)
	cmd.Dir = spec.workDir
	// Scrub secret-shaped env, point HOME at the mounted codex auth dir
	// (/run/agent-auth/codex → $CODEX_HOME defaults to $HOME/.codex), and inject
	// OPENAI_API_KEY when the provider was connected by API key.
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
		return "", usage, fmt.Errorf("start codex: %w", err)
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

	pr := parseCodexStream(stdout, emit)
	waitErr := cmd.Wait()
	<-stderrDone

	// The -o file is the authoritative final message; fall back to the streamed
	// agent text.
	final := pr.FinalMessage
	if b, e := os.ReadFile(outPath); e == nil && len(strings.TrimSpace(string(b))) > 0 {
		final = strings.TrimSpace(string(b))
	}
	usage = pr.Usage

	// Classification mirrors the opencode adapter (codex can exit 0 after an
	// unrecoverable auth error, so an error with no output is a real failure).
	switch {
	case ctx.Err() != nil:
		return final, usage, nil
	case !pr.SawText && pr.ErrMsg != "":
		return final, usage, fmt.Errorf("codex: %s", pr.ErrMsg)
	case waitErr != nil:
		return final, usage, fmt.Errorf("codex exited: %w", waitErr)
	case !pr.SawText && !pr.SawTool:
		return final, usage, fmt.Errorf("agent_no_output")
	default:
		return final, usage, nil
	}
}
