package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tastyeffectco/sandboxd/control-plane/internal/modelprovider"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/secrets"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/store"
)

// These tests execute the real installed CLIs and their tools against a local
// protocol fixture through the real credential proxy. No paid provider, Docker
// daemon, production sandbox or host credentials are used.
//
// SANDBOXD_TEST_REAL_AGENTS=1 go test ./cmd/runtimed -run TestRealProviderAgents -v
func TestRealProviderAgents(t *testing.T) {
	if os.Getenv("SANDBOXD_TEST_REAL_AGENTS") != "1" {
		t.Skip("set SANDBOXD_TEST_REAL_AGENTS=1 with the pinned OpenCode and Pi binaries on PATH")
	}
	for binary, version := range map[string]string{"opencode": "1.19.3", "pi": "1.3.0"} {
		output, err := exec.Command(binary, "--version").CombinedOutput()
		if err != nil || strings.TrimSpace(string(output)) != version {
			t.Fatalf("%s: expected %s, got %q (%v)", binary, version, output, err)
		}
	}
	for _, agentName := range []string{"opencode", "pi"} {
		for _, protocol := range []string{modelprovider.OpenAI, modelprovider.Anthropic} {
			t.Run(agentName+"/"+protocol, func(t *testing.T) {
				root := t.TempDir()
				work := filepath.Join(root, "workspace")
				if err := os.Mkdir(work, 0o700); err != nil {
					t.Fatal(err)
				}
				// OpenCode's config/cache discovery also stays inside the test.
				t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
				t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
				t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
				t.Setenv("OPENCODE_DISABLE_MODELS_FETCH", "true")
				t.Setenv("OPENCODE_DISABLE_AUTOUPDATE", "true")
				// The daemon's inherited PWD may differ from the task workspace.
				t.Setenv("PWD", filepath.Join(root, "wrong-workspace"))
				st, err := store.Open(context.Background(), filepath.Join(root, "test.sqlite"), "../../migrations")
				if err != nil {
					t.Fatal(err)
				}
				defer st.Close()
				cipher, err := secrets.Load("", filepath.Join(root, "key"))
				if err != nil {
					t.Fatal(err)
				}
				service, err := modelprovider.New(st, cipher)
				if err != nil {
					t.Fatal(err)
				}
				key := "fixture-upstream-key-must-never-enter-workspace"
				model := "team/local-coder:latest"
				var mu sync.Mutex
				calls := 0
				mode := "success"
				expectHistory := false
				requestStarted := make(chan struct{})
				var startedOnce sync.Once
				var fixtureErrors []string
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					mu.Lock()
					defer mu.Unlock()
					calls++
					if mode == "reject" {
						http.Error(w, key, http.StatusForbidden)
						return
					}
					if mode == "wait" {
						io.Copy(io.Discard, r.Body)
						startedOnce.Do(func() { close(requestStarted) })
						select {
						case <-r.Context().Done():
						case <-time.After(10 * time.Second):
							fixtureErrors = append(fixtureErrors, "upstream request was not cancelled")
						}
						return
					}
					bad := func(message string) { fixtureErrors = append(fixtureErrors, message); http.Error(w, message, 400) }
					if protocol == modelprovider.OpenAI && r.Header.Get("Authorization") != "Bearer "+key {
						bad("missing real OpenAI key")
						return
					}
					if protocol == modelprovider.Anthropic && r.Header.Get("X-Api-Key") != key {
						bad("missing real Anthropic key")
						return
					}
					var body map[string]json.RawMessage
					if json.NewDecoder(r.Body).Decode(&body) != nil {
						bad("bad JSON")
						return
					}
					var gotModel string
					json.Unmarshal(body["model"], &gotModel)
					if gotModel != model {
						bad("model alias changed: " + gotModel)
						return
					}
					var tools []struct {
						Name   string `json:"name"`
						Schema struct {
							Properties map[string]json.RawMessage `json:"properties"`
						} `json:"input_schema"`
						Function struct {
							Name       string `json:"name"`
							Parameters struct {
								Properties map[string]json.RawMessage `json:"properties"`
							} `json:"parameters"`
						} `json:"function"`
					}
					json.Unmarshal(body["tools"], &tools)
					if len(tools) > 0 && !strings.Contains(string(body["system"])+string(body["messages"]), work) {
						bad("agent system prompt did not identify the requested workspace")
						return
					}
					if expectHistory && len(tools) > 0 && !strings.Contains(string(body["messages"]), "real agent tool ran") {
						bad("agent did not resume its prior session")
						return
					}
					// Ask the actual agent to execute its actual write tool, then
					// return a final answer when its tool result reaches the model.
					hasResult := strings.Contains(string(body["messages"]), `"tool"`) || strings.Contains(string(body["messages"]), `"tool_result"`)
					name := ""
					args := map[string]string{}
					if !hasResult {
						for _, tool := range tools {
							n, props := tool.Name, tool.Schema.Properties
							if protocol == modelprovider.OpenAI {
								n, props = tool.Function.Name, tool.Function.Parameters.Properties
							}
							if n != "write" {
								continue
							}
							name = n
							pathKey := "path"
							if _, ok := props["filePath"]; ok {
								pathKey = "filePath"
							}
							args = map[string]string{pathKey: filepath.Join(work, "provider-smoke.txt"), "content": "real agent tool ran\n"}
						}
						if name == "" && len(tools) > 0 {
							bad("agent did not expose its write tool")
							return
						}
					}
					w.Header().Set("Content-Type", "text/event-stream")
					if protocol == modelprovider.OpenAI {
						writeOpenAIFixture(w, model, name, args)
					} else {
						writeAnthropicFixture(w, model, name, args)
					}
				}))
				defer upstream.Close()
				config, err := service.Save(context.Background(), "", modelprovider.Input{
					Name: "Fixture", Protocol: protocol, BaseURL: upstream.URL + "/v1", APIKey: &key,
					Models: []modelprovider.Model{{ID: model}},
				})
				if err != nil {
					t.Fatal(err)
				}
				var proxyErrors lockedTestBuffer
				proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					// Record rejected request bodies without recording capability headers.
					body, _ := io.ReadAll(r.Body)
					r.Body = io.NopCloser(bytes.NewReader(body))
					rec := &recordStatusWriter{ResponseWriter: w, status: 200}
					service.ServeHTTP(rec, r)
					if rec.status >= 400 {
						fmt.Fprintf(&proxyErrors, "%s %d %s\n", r.URL.Path, rec.status, body)
					}
				}))
				defer proxy.Close()
				connection, err := service.Connection(context.Background(), config.ID, model, proxy.URL, 2*time.Minute)
				if err != nil {
					t.Fatal(err)
				}
				ag, err := selectAgent(agentName, slog.Default())
				if err != nil {
					t.Fatal(err)
				}
				var log bytes.Buffer
				ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
				defer cancel()
				final, usage, err := ag.run(ctx, agentSpec{workDir: work, stateDir: filepath.Join(root, "runtime"),
					provider: connection, model: model, prompt: "Write provider-smoke.txt with exactly: real agent tool ran. Use the write tool, then confirm.",
					rawLog: &log}, func(string, any) {})
				if ctx.Err() != nil || err != nil {
					t.Fatalf("agent failed: ctx=%v err=%v\nstderr=%s\nproxy=%s", ctx.Err(), err, log.String(), proxyErrors.String())
				}
				content, err := os.ReadFile(filepath.Join(work, "provider-smoke.txt"))
				if err != nil || string(content) != "real agent tool ran\n" {
					t.Fatalf("real tool did not write file: %q %v; final=%q; stderr=%s", content, err, final, log.String())
				}
				if !strings.Contains(final, "Created provider-smoke.txt") || usage.Output == 0 {
					t.Fatalf("missing output or token accounting: final=%q usage=%+v", final, usage)
				}
				// Continue uses the same agent/connection/model session directory.
				mu.Lock()
				expectHistory = true
				mu.Unlock()
				resumed, _, resumeErr := ag.run(ctx, agentSpec{workDir: work, stateDir: filepath.Join(root, "runtime"),
					provider: connection, model: model, prompt: "Confirm the file you just wrote without changing it.", cont: true,
					rawLog: &log}, func(string, any) {})
				if resumeErr != nil || !strings.Contains(resumed, "Created provider-smoke.txt") {
					t.Fatalf("resume failed: %v %q %s", resumeErr, resumed, log.String())
				}
				// Permanent provider errors must fail promptly without leaking a key.
				mu.Lock()
				mode = "reject"
				mu.Unlock()
				errorCtx, errorCancel := context.WithTimeout(context.Background(), 20*time.Second)
				_, _, providerErr := ag.run(errorCtx, agentSpec{workDir: work, stateDir: filepath.Join(root, "runtime"),
					provider: connection, model: model, prompt: "Say hello.", rawLog: &log}, func(string, any) {})
				timedOut := errorCtx.Err() != nil
				errorCancel()
				if providerErr == nil || timedOut || strings.Contains(providerErr.Error(), key) {
					t.Fatalf("bad provider error handling: %v timeout=%v", providerErr, timedOut)
				}
				// Cancel only after an actual request has reached the upstream.
				mu.Lock()
				mode = "wait"
				mu.Unlock()
				cancelCtx, cancelRun := context.WithTimeout(context.Background(), 20*time.Second)
				go func() {
					select {
					case <-requestStarted:
						cancelRun()
					case <-cancelCtx.Done():
					}
				}()
				startCancel := time.Now()
				_, _, cancelErr := ag.run(cancelCtx, agentSpec{workDir: work, stateDir: filepath.Join(root, "runtime"),
					provider: connection, model: model, prompt: "Wait for a response.", rawLog: &log}, func(string, any) {})
				wasCancelled := cancelCtx.Err() == context.Canceled
				cancelRun()
				if !wasCancelled || cancelErr != nil || time.Since(startCancel) > 15*time.Second {
					t.Fatalf("cancellation failed: %v cancelled=%v", cancelErr, wasCancelled)
				}
				mu.Lock()
				defer mu.Unlock()
				if calls < 2 || len(fixtureErrors) > 0 {
					t.Fatalf("incomplete tool round trip: calls=%d errors=%v", calls, fixtureErrors)
				}
				if strings.Contains(log.String(), key) {
					t.Fatal("upstream credential entered agent log")
				}
				filepath.WalkDir(work, func(path string, d os.DirEntry, err error) error {
					if err != nil {
						t.Error(err)
						return nil
					}
					if !d.IsDir() {
						b, _ := os.ReadFile(path)
						if bytes.Contains(b, []byte(key)) {
							t.Errorf("key leaked to %s", path)
						}
					}
					return nil
				})
			})
		}
	}
}

type recordStatusWriter struct {
	http.ResponseWriter
	status int
}

func (w *recordStatusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}
func (w *recordStatusWriter) Flush() { w.ResponseWriter.(http.Flusher).Flush() }

func fixtureEvent(w io.Writer, event string, value any) {
	b, _ := json.Marshal(value)
	if event != "" {
		fmt.Fprintf(w, "event: %s\n", event)
	}
	fmt.Fprintf(w, "data: %s\n\n", b)
}

func writeOpenAIFixture(w io.Writer, model, name string, args map[string]string) {
	delta := map[string]any{"role": "assistant", "content": "Created provider-smoke.txt."}
	reason := "stop"
	if name != "" {
		raw, _ := json.Marshal(args)
		delta = map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{
			"index": 0, "id": "call_write_1", "type": "function", "function": map[string]any{"name": name, "arguments": string(raw)},
		}}}
		reason = "tool_calls"
	}
	fixtureEvent(w, "", map[string]any{"id": "chatcmpl-fixture", "object": "chat.completion.chunk", "created": 1, "model": model,
		"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": nil}}})
	fixtureEvent(w, "", map[string]any{"id": "chatcmpl-fixture", "object": "chat.completion.chunk", "created": 1, "model": model,
		"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": reason}},
		"usage":   map[string]int{"prompt_tokens": 50, "completion_tokens": 10, "total_tokens": 60}})
	fmt.Fprint(w, "data: [DONE]\n\n")
}

func writeAnthropicFixture(w io.Writer, model, name string, args map[string]string) {
	fixtureEvent(w, "message_start", map[string]any{"type": "message_start", "message": map[string]any{
		"id": "msg_fixture", "type": "message", "role": "assistant", "model": model, "content": []any{}, "stop_reason": nil, "stop_sequence": nil,
		"usage": map[string]int{"input_tokens": 50, "output_tokens": 0},
	}})
	block := map[string]any{"type": "text", "text": ""}
	delta := map[string]any{"type": "text_delta", "text": "Created provider-smoke.txt."}
	stop := "end_turn"
	if name != "" {
		raw, _ := json.Marshal(args)
		block = map[string]any{"type": "tool_use", "id": "call_write_1", "name": name, "input": map[string]any{}}
		delta = map[string]any{"type": "input_json_delta", "partial_json": string(raw)}
		stop = "tool_use"
	}
	fixtureEvent(w, "content_block_start", map[string]any{"type": "content_block_start", "index": 0, "content_block": block})
	fixtureEvent(w, "content_block_delta", map[string]any{"type": "content_block_delta", "index": 0, "delta": delta})
	fixtureEvent(w, "content_block_stop", map[string]any{"type": "content_block_stop", "index": 0})
	fixtureEvent(w, "message_delta", map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": stop, "stop_sequence": nil}, "usage": map[string]int{"output_tokens": 10}})
	fixtureEvent(w, "message_stop", map[string]any{"type": "message_stop"})
}

// lockedTestBuffer also supports concurrent reverse-proxy diagnostics.
type lockedTestBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *lockedTestBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}
func (b *lockedTestBuffer) String() string { b.mu.Lock(); defer b.mu.Unlock(); return b.b.String() }
