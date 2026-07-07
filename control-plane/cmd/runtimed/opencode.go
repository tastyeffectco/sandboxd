package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/tastyeffectco/sandboxd/control-plane/internal/runtime"
)

// agentSpec is the input to an agent adapter run.
type agentSpec struct {
	workDir string
	prompt  string
	model   string // per-task model (agent CLI --model); empty = agent default
	env     map[string]string
	rawLog  io.Writer // the agent's own diagnostics (stderr)
	// systemPrompt is the rendered platform briefing (agentprompt.Render). Each
	// adapter delivers it in its own supported way (claude: --append-system-prompt;
	// opencode/codex: a preamble on the user prompt). Empty = inject nothing.
	systemPrompt string
	// cont continues the sandbox's most recent agent session instead of starting
	// fresh (claude --continue, opencode --continue, codex `exec resume --last`).
	// Each sandbox is one workspace, so "most recent" is naturally per-sandbox.
	cont bool
}

// agent is the coding-agent adapter boundary. This slice implements
// opencode only; claude_code / codex slot in here later.
type agent interface {
	name() string
	run(ctx context.Context, spec agentSpec, emit eventSink) (finalMessage string, usage runtime.TokenUsage, err error)
}

// opencodeAgent drives `opencode run --format json`.
type opencodeAgent struct{ log *slog.Logger }

func (o *opencodeAgent) name() string { return "opencode" }

// opencodeEvent is the envelope of one `opencode run --format json`
// output line. Only the fields runtimed maps are declared; the schema
// is otherwise treated as opaque (provider behaviour is best-effort).
type opencodeEvent struct {
	Type string `json:"type"`
	Part struct {
		ID    string `json:"id"`
		Type  string `json:"type"`
		Text  string `json:"text"`
		Tool  string `json:"tool"` // tool name on a "tool" part: edit, write, bash, glob…
		State struct {
			Status string          `json:"status"` // pending | running | completed | error
			Input  json.RawMessage `json:"input"`  // tool args — shape varies per tool
		} `json:"state"`
		// step-finish parts carry per-step token accounting.
		Tokens struct {
			Input     int `json:"input"`
			Output    int `json:"output"`
			Reasoning int `json:"reasoning"`
			Cache     struct {
				Read  int `json:"read"`
				Write int `json:"write"`
			} `json:"cache"`
		} `json:"tokens"`
		Cost float64 `json:"cost"`
	} `json:"part"`
	// Error is present on top-level `{"type":"error", ...}` lines. The
	// API-side error shape is `{name, data:{message, statusCode}}`;
	// opencode also emits other error shapes. Capturing both Name and
	// Data.Message handles the observed cases.
	Error *struct {
		Name string `json:"name"`
		Data struct {
			Message    string `json:"message"`
			StatusCode int    `json:"statusCode"`
		} `json:"data"`
	} `json:"error,omitempty"`
}

// opencodeParseResult is the outcome of consuming an opencode stdout
// stream. Pulled out for unit testing without spawning opencode.
type opencodeParseResult struct {
	FinalMessage string
	Usage        runtime.TokenUsage
	SawText      bool // any text part observed (real model output)
	SawTool      bool // any tool part observed (real model output)
	APIErr       string
}

// parseOpencodeStream consumes NDJSON from r, dispatches canonical
// events through emit, and returns a structured summary. Pure — no
// process management — so it's exercisable from tests with a string
// reader.
func parseOpencodeStream(r io.Reader, emit eventSink) opencodeParseResult {
	var pr opencodeParseResult
	parts := map[string]string{}
	var order []string
	seenTool := map[string]bool{}

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		var ev opencodeEvent
		if json.Unmarshal(sc.Bytes(), &ev) != nil {
			continue // best-effort: skip lines we cannot parse
		}
		switch {
		case ev.Type == "error":
			// opencode reported a structured error. Capture the first
			// one's message so the agent.run caller can classify the
			// task as failed; also emit as a canonical `message` event
			// with role=agent_error so SSE consumers see it.
			msg := "agent error (no message)"
			if ev.Error != nil {
				switch {
				case ev.Error.Data.Message != "":
					msg = ev.Error.Data.Message
				case ev.Error.Name != "":
					msg = ev.Error.Name
				}
			}
			if pr.APIErr == "" {
				pr.APIErr = msg
			}
			emit(runtime.EventMessage, map[string]any{"role": "agent_error", "text": msg})

		case ev.Type == "text" && ev.Part.Text != "":
			pr.SawText = true
			if _, seen := parts[ev.Part.ID]; !seen {
				order = append(order, ev.Part.ID)
			}
			parts[ev.Part.ID] = ev.Part.Text
			emit(runtime.EventMessage, map[string]any{"role": "agent", "text": ev.Part.Text})

		case ev.Part.Type == "tool" && ev.Part.Tool != "":
			// A coding-agent sub-step — the live progress feed. Emitted
			// as structured, language-neutral data (a tool name + a
			// path/identifier); the consumer localises the wording.
			// Deduped per (part, status) so a status transition emits
			// at most once.
			pr.SawTool = true
			key := ev.Part.ID + "|" + ev.Part.State.Status
			if !seenTool[key] {
				seenTool[key] = true
				emit(runtime.EventTool, map[string]any{
					"name":   ev.Part.Tool,
					"status": ev.Part.State.Status,
					"path":   toolTarget(ev.Part.State.Input),
				})
			}

		case ev.Part.Type == "step-finish":
			// Per-step token accounting — summed across the session.
			tk := ev.Part.Tokens
			pr.Usage.Input += tk.Input
			pr.Usage.Output += tk.Output
			pr.Usage.Reasoning += tk.Reasoning
			pr.Usage.CacheRead += tk.Cache.Read
			pr.Usage.CacheWrite += tk.Cache.Write
			pr.Usage.Cost += ev.Part.Cost
		}
	}
	pr.Usage.Total = pr.Usage.Input + pr.Usage.Output + pr.Usage.Reasoning +
		pr.Usage.CacheRead + pr.Usage.CacheWrite

	var b strings.Builder
	for _, id := range order {
		b.WriteString(parts[id])
	}
	pr.FinalMessage = b.String()
	return pr
}

// toolTarget pulls the most user-meaningful argument out of a tool
// call's input — a file path, a glob, or a command — for the live
// progress feed. It is language-neutral by design (a path/identifier,
// not prose): the consumer localises the verb around it.
func toolTarget(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var in struct {
		FilePath  string `json:"filePath"`
		FilePathS string `json:"file_path"` // claude tools use snake_case
		Path      string `json:"path"`
		File      string `json:"file"`
		Pattern   string `json:"pattern"`
		Command   string `json:"command"`
	}
	if json.Unmarshal(raw, &in) != nil {
		return ""
	}
	t := in.FilePath
	for _, c := range []string{in.FilePathS, in.Path, in.File, in.Pattern, in.Command} {
		if t == "" {
			t = c
		}
	}
	if len(t) > 200 {
		t = t[:200]
	}
	return t
}

// defaultProxyModel is used only when the proxy is on but no model was picked;
// glm-5 is present in both the "zen" (pay-as-you-go) and "zengo" (subscription)
// catalogs, so it routes on either path.
const defaultProxyModel = "glm-5"

// zenUpstream is the proxy <upstream> segment opencode's Zen gateway routes
// through: "zen" (pay-as-you-go, full model catalog) or "zengo" (the Zen "go"
// subscription's included models). Set SANDBOXD_OPENCODE_ZEN_PATH to choose;
// defaults to "zen".
func zenUpstream() string {
	if p := os.Getenv("SANDBOXD_OPENCODE_ZEN_PATH"); p == "zengo" || p == "zen" {
		return p
	}
	return "zen"
}

// ipBaseURL rewrites a base URL's host to its resolved IP. opencode's Bun
// runtime rejects a bare single-label hostname (e.g. "sandboxd") in a provider
// baseURL with "fetch() URL is invalid", so we hand it an IP literal instead.
func ipBaseURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if host := u.Hostname(); net.ParseIP(host) == nil {
		if ips, err := net.LookupHost(host); err == nil && len(ips) > 0 {
			if port := u.Port(); port != "" {
				u.Host = net.JoinHostPort(ips[0], port)
			} else {
				u.Host = ips[0]
			}
		}
	}
	return strings.TrimRight(u.String(), "/"), nil
}

// writeOpencodeProxyConfig writes an OPENCODE_CONFIG that routes opencode through
// the credential-injecting proxy, and returns (config path, model id to pass to
// --model). It defines ONE custom openai-compatible provider ("proxy") pointing
// at the proxy's `opencode/<zen-path>` endpoint with a DUMMY key; the proxy holds
// the real credential and injects it on the wire, so nothing secret lands in the
// sandbox. The requested model is exposed as `proxy/<id>`.
//
// Returns ("","",nil) when no proxy is configured — the caller then falls back to
// opencode's own auth/model handling.
func writeOpencodeProxyConfig(model string) (string, string, error) {
	proxy := strings.TrimRight(os.Getenv("RUNTIMED_ANTHROPIC_PROXY"), "/")
	if proxy == "" {
		return "", "", nil
	}
	base, err := ipBaseURL(proxy)
	if err != nil {
		return "", "", err
	}
	// Bare model id — strip any "provider/" prefix the caller passed.
	id := model
	if i := strings.LastIndex(id, "/"); i >= 0 {
		id = id[i+1:]
	}
	if id == "" {
		id = defaultProxyModel
	}
	cfg := map[string]any{
		"provider": map[string]any{
			"proxy": map[string]any{
				"npm":  "@ai-sdk/openai-compatible",
				"name": "proxy",
				"options": map[string]any{
					"baseURL": base + "/opencode/" + zenUpstream(),
					"apiKey":  dummyKey,
				},
				"models": map[string]any{id: map[string]any{"name": id}},
			},
		},
	}
	f, err := os.CreateTemp("", "opencode-cfg-*.json")
	if err != nil {
		return "", "", err
	}
	defer f.Close()
	if err := json.NewEncoder(f).Encode(cfg); err != nil {
		os.Remove(f.Name())
		return "", "", err
	}
	return f.Name(), "proxy/" + id, nil
}

func (o *opencodeAgent) run(ctx context.Context, spec agentSpec, emit eventSink) (string, runtime.TokenUsage, error) {
	var usage runtime.TokenUsage
	// Model precedence: per-task (spec.model) > global default (RUNTIMED_OPENCODE_MODEL)
	// > opencode's own configured default. Model is "provider/model" for opencode.
	model := spec.model
	if model == "" {
		model = os.Getenv("RUNTIMED_OPENCODE_MODEL")
	}
	// Route opencode through the credential-injecting proxy via an OPENCODE_CONFIG
	// file: a custom openai-compatible "proxy" provider carrying a dummy key (the
	// proxy injects the real one). When it applies, the model is rewritten to
	// `proxy/<id>` so the run uses that provider. No credential enters the sandbox.
	cfgPath, proxyModel, cfgErr := writeOpencodeProxyConfig(model)
	if cfgErr != nil {
		return "", usage, fmt.Errorf("configure opencode proxy: %w", cfgErr)
	}
	if cfgPath != "" {
		defer os.Remove(cfgPath)
		model = proxyModel
	}
	args := []string{"run", "--format", "json", "--dangerously-skip-permissions"}
	if model != "" {
		args = append(args, "--model", model)
	}
	if spec.cont {
		args = append(args, "--continue") // continue the last session
	}
	// opencode `run` has no system-prompt flag, so deliver the platform briefing
	// as a clearly-delimited preamble on the prompt — on a FRESH run only (on
	// --continue it's already in the session).
	prompt := spec.prompt
	if spec.systemPrompt != "" && !spec.cont {
		prompt = spec.systemPrompt + "\n\n---\n\n# Your task\n\n" + spec.prompt
	}
	args = append(args, prompt)
	cmd := exec.Command("opencode", args...)
	cmd.Dir = spec.workDir
	// Phase 10B — scrub secret-shaped vars and point HOME at THIS agent's
	// mounted auth dir (/run/agent-auth/opencode), if any. Credentials come from
	// files under HOME, never from inherited container env.
	cmd.Env = agentEnv(o.name(), spec.env)
	if cfgPath != "" {
		cmd.Env = append(cmd.Env, "OPENCODE_CONFIG="+cfgPath)
	}
	// Own process group so cancel/timeout kills the whole agent tree.
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
		return "", usage, fmt.Errorf("start opencode: %w", err)
	}
	pgid := cmd.Process.Pid

	// ctx cancellation (cancel or timeout) → kill the process group.
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

	// stderr → the per-task agent log.
	stderrDone := make(chan struct{})
	go func() {
		_, _ = io.Copy(spec.rawLog, stderr)
		close(stderrDone)
	}()

	// Parse stdout (one JSON event per line) — pure function so it can
	// be unit-tested without spawning opencode.
	pr := parseOpencodeStream(stdout, emit)
	waitErr := cmd.Wait()
	<-stderrDone

	// Classification, in order of authority:
	//   1. ctx already errored (cancel / timeout) → caller decides; we
	//      return nil err and let the upper layer mark the task.
	//   2. opencode reported an error event → real failure regardless
	//      of exit code (it often exits 0 even after an auth failure;
	//      that was the original "succeeded with empty result" bug).
	//   3. process exited non-zero with a live ctx → real failure.
	//   4. exit 0 with NO text and NO tool events → opencode crashed
	//      silently or never produced output (`agent_no_output`). Catches
	//      the case where the error event shape changes underneath us.
	switch {
	case ctx.Err() != nil:
		return pr.FinalMessage, pr.Usage, nil
	case pr.APIErr != "":
		return pr.FinalMessage, pr.Usage, fmt.Errorf("agent error: %s", pr.APIErr)
	case waitErr != nil:
		return pr.FinalMessage, pr.Usage, fmt.Errorf("opencode exited: %w", waitErr)
	case !pr.SawText && !pr.SawTool:
		return pr.FinalMessage, pr.Usage,
			fmt.Errorf("agent produced no output (opencode exited 0 with zero events)")
	}
	return pr.FinalMessage, pr.Usage, nil
}
