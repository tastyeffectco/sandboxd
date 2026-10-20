package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/tastyeffectco/sandboxd/control-plane/internal/runtime"
)

type piAgent struct{}

func (*piAgent) name() string { return "pi" }

type piMessage struct {
	Role    string `json:"role"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	StopReason   string `json:"stopReason"`
	ErrorMessage string `json:"errorMessage"`
	Usage        struct {
		Input      int `json:"input"`
		Output     int `json:"output"`
		CacheRead  int `json:"cacheRead"`
		CacheWrite int `json:"cacheWrite"`
		Total      int `json:"totalTokens"`
		Cost       struct {
			Total float64 `json:"total"`
		} `json:"cost"`
	} `json:"usage"`
}

func parsePiStream(r io.Reader, emit eventSink) (string, runtime.TokenUsage, error) {
	var usage runtime.TokenUsage
	var final, failure string
	var sawOutput, settled bool
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64<<10), 16<<20)
	for scanner.Scan() {
		var event struct {
			Type                  string          `json:"type"`
			Message               json.RawMessage `json:"message"`
			AssistantMessageEvent struct {
				Type  string `json:"type"`
				Delta string `json:"delta"`
			} `json:"assistantMessageEvent"`
			ToolName string          `json:"toolName"`
			Args     json.RawMessage `json:"args"`
			IsError  bool            `json:"isError"`
			Aborted  bool            `json:"aborted"`
		}
		if json.Unmarshal(scanner.Bytes(), &event) != nil {
			continue
		}
		switch event.Type {
		case "message_update":
			if event.AssistantMessageEvent.Type == "text_delta" {
				sawOutput = true
				emit(runtime.EventMessage, map[string]any{"role": "agent", "text": event.AssistantMessageEvent.Delta})
			}
		case "message_end":
			var role struct {
				Role string `json:"role"`
			}
			if json.Unmarshal(event.Message, &role) != nil || role.Role != "assistant" {
				continue
			}
			var m piMessage
			if json.Unmarshal(event.Message, &m) != nil {
				return final, usage, errors.New("invalid Pi assistant message")
			}
			var text strings.Builder
			for _, part := range m.Content {
				if part.Type == "text" {
					text.WriteString(part.Text)
				}
			}
			final = text.String()
			if final != "" {
				sawOutput = true
			}
			usage.Input += m.Usage.Input
			usage.Output += m.Usage.Output
			usage.CacheRead += m.Usage.CacheRead
			usage.CacheWrite += m.Usage.CacheWrite
			usage.Total += m.Usage.Total
			usage.Cost += m.Usage.Cost.Total
			failure = ""
			if m.StopReason == "error" || m.StopReason == "aborted" {
				failure = m.ErrorMessage
				if failure == "" {
					failure = "Pi model request " + m.StopReason
				}
			}
		case "tool_execution_start", "tool_execution_end":
			sawOutput = true
			status := "running"
			if event.Type == "tool_execution_end" {
				status = "completed"
				if event.IsError {
					status = "error"
				}
			}
			emit(runtime.EventTool, map[string]any{"name": event.ToolName, "status": status, "path": toolTarget(event.Args)})
		case "agent_settled":
			settled = true
			if event.Aborted {
				failure = "Pi run aborted"
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return final, usage, fmt.Errorf("read Pi events: %w", err)
	}
	if failure != "" {
		return final, usage, errors.New(failure)
	}
	if !settled || !sawOutput {
		return final, usage, errors.New("Pi exited without a completed agent response")
	}
	return final, usage, nil
}

func (p *piAgent) run(ctx context.Context, spec agentSpec, emit eventSink) (string, runtime.TokenUsage, error) {
	var usage runtime.TokenUsage
	if spec.provider == nil {
		return "", usage, errors.New("Pi requires a configured model provider connection")
	}
	dir, err := writePiConnection(spec.provider)
	if err != nil {
		return "", usage, fmt.Errorf("configure Pi: %w", err)
	}
	defer os.RemoveAll(dir)
	sessionDir := connectionSessionDir(spec, p.name())
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		return "", usage, err
	}
	args := []string{"--mode", "json", "--print", "--offline", "--no-approve", "--no-extensions",
		"--provider", "sandboxd", "--model", spec.provider.Model.ID, "--session-dir", sessionDir}
	if spec.cont {
		args = append(args, "--continue")
	}
	if spec.systemPrompt != "" {
		args = append(args, "--append-system-prompt", spec.systemPrompt)
	}
	args = append(args, "--", spec.prompt)
	cmd := exec.Command("pi", args...)
	cmd.Dir = spec.workDir
	cmd.Env = append(agentEnv(p.name(), spec.env), "PI_CODING_AGENT_DIR="+dir)
	if spec.workDir != "" {
		cmd.Env = append(cmd.Env, "PWD="+spec.workDir)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", usage, err
	}
	// os/exec drains stderr before Wait returns.
	cmd.Stderr = spec.rawLog
	if err = cmd.Start(); err != nil {
		return "", usage, fmt.Errorf("start Pi: %w", err)
	}
	finished := make(chan struct{})
	go func() {
		select {
		case <-finished:
		case <-ctx.Done():
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
			timer := time.NewTimer(5 * time.Second)
			defer timer.Stop()
			select {
			case <-finished:
			case <-timer.C:
				_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			}
		}
	}()
	defer close(finished)
	final, usage, parseErr := parsePiStream(stdout, emit)
	if parseErr != nil {
		// A malformed/oversized stream must not leave a child blocked writing.
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	waitErr := cmd.Wait()
	if ctx.Err() != nil {
		return final, usage, nil
	} // task layer classifies cancellation
	if parseErr != nil {
		return final, usage, parseErr
	}
	if waitErr != nil {
		return final, usage, fmt.Errorf("Pi exited: %w", waitErr)
	}
	return final, usage, nil
}
