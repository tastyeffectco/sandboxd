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

// Codex on a model connection (OpenRouter...), end to end: the real Codex CLI
// started by runtimed's adapter, through the real connection proxy, to a local
// Responses API fixture, and calling a sandboxd tool from the workspace's
// .codex/config.toml (as the sandboxd agent's workspace has). No paid provider
// or host credentials are used.
//
// SANDBOXD_TEST_REAL_AGENTS=1 go test ./cmd/runtimed -run TestRealCodexConnection -v
func TestRealCodexConnection(t *testing.T) {
	if os.Getenv("SANDBOXD_TEST_REAL_AGENTS") != "1" {
		t.Skip("set SANDBOXD_TEST_REAL_AGENTS=1 with the pinned Codex binary on PATH")
	}
	if out, err := exec.Command("codex", "--version").CombinedOutput(); err != nil || !strings.Contains(string(out), "0.162.1") {
		t.Fatalf("codex 0.162.1 expected: %q %v", out, err)
	}
	root := t.TempDir()
	work := filepath.Join(root, "workspace")
	if err := os.MkdirAll(filepath.Join(work, ".codex"), 0o700); err != nil {
		t.Fatal(err)
	}
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
	key := "fixture-openrouter-key-must-never-enter-workspace"
	model := "deepseek/deepseek-v4.1-flash"

	// sandboxd's tools, as the sandboxd agent's workspace reaches them.
	var mu sync.Mutex
	var toolCalls []string
	var problems []string
	mcp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params struct {
				Name            string `json:"name"`
				ProtocolVersion string `json:"protocolVersion"`
			} `json:"params"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		mu.Lock()
		defer mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer tool-secret" {
			problems = append(problems, "tools called without the workspace secret")
		}
		if req.ID == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		var result any = map[string]any{}
		switch req.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": req.Params.ProtocolVersion, "capabilities": map[string]any{"tools": map[string]any{}},
				"serverInfo": map[string]any{"name": "sandboxd", "version": "1"}}
		case "tools/list":
			result = map[string]any{"tools": []any{map[string]any{"name": "list_apps", "description": "List the user's apps",
				"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}}}}}
		case "tools/call":
			toolCalls = append(toolCalls, req.Params.Name)
			result = map[string]any{"content": []any{map[string]any{"type": "text", "text": "- Shop (running)"}}}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}))
	defer mcp.Close()
	toml := "[mcp_servers.sandboxd]\nurl = \"" + mcp.URL + "/assistant/mcp\"\nhttp_headers = { Authorization = \"Bearer tool-secret\" }\n"
	if err := os.WriteFile(filepath.Join(work, ".codex", "config.toml"), []byte(toml), 0o600); err != nil {
		t.Fatal(err)
	}

	// OpenRouter's Responses API, as a fixture: first ask for the tool, then
	// answer with what it returned.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]json.RawMessage
		json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		if r.URL.Path != "/api/v1/responses" || r.Header.Get("Authorization") != "Bearer "+key || !strings.Contains(string(body["model"]), model) {
			problems = append(problems, fmt.Sprintf("upstream got %s auth=%t model=%s", r.URL.Path, r.Header.Get("Authorization") == "Bearer "+key, body["model"]))
		}
		mu.Unlock()
		var item any = map[string]any{"type": "function_call", "id": "fc1", "call_id": "call1", "namespace": "mcp__sandboxd",
			"name": "list_apps", "arguments": "{}", "status": "completed"}
		if strings.Contains(string(body["input"]), "function_call_output") {
			item = map[string]any{"type": "message", "id": "m1", "role": "assistant", "status": "completed",
				"content": []any{map[string]any{"type": "output_text", "text": "You have one app: Shop.", "annotations": []any{}}}}
		}
		usage := map[string]any{"input_tokens": 10, "output_tokens": 5, "total_tokens": 15,
			"input_tokens_details": map[string]any{"cached_tokens": 0}, "output_tokens_details": map[string]any{"reasoning_tokens": 0}}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, ev := range []map[string]any{
			{"type": "response.created", "response": map[string]any{"id": "r1"}},
			{"type": "response.output_item.done", "output_index": 0, "item": item},
			{"type": "response.completed", "response": map[string]any{"id": "r1", "status": "completed", "output": []any{item}, "usage": usage}},
		} {
			raw, _ := json.Marshal(ev)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev["type"], raw)
		}
	}))
	defer upstream.Close()
	config, err := service.Save(context.Background(), "", modelprovider.Input{Name: "OpenRouter", Protocol: modelprovider.OpenAI,
		BaseURL: upstream.URL + "/api/v1", APIKey: &key, Models: []modelprovider.Model{{ID: model}}})
	if err != nil {
		t.Fatal(err)
	}
	var proxyErrors bytes.Buffer
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	ag, err := selectAgent("codex", slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	var log bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	final, _, err := ag.run(ctx, agentSpec{workDir: work, stateDir: filepath.Join(root, "runtime"), provider: connection,
		model: model, prompt: "Which apps do I have?", rawLog: &log}, func(string, any) {})
	if err != nil {
		t.Fatalf("codex failed: %v\nstderr=%s\nproxy=%s", err, log.String(), proxyErrors.String())
	}
	mu.Lock()
	defer mu.Unlock()
	if len(problems) > 0 {
		t.Fatalf("problems: %v\nproxy=%s", problems, proxyErrors.String())
	}
	if !strings.Contains(final, "Shop") || len(toolCalls) != 1 || toolCalls[0] != "list_apps" {
		t.Fatalf("final=%q tools=%v\nstderr=%s\nproxy=%s", final, toolCalls, log.String(), proxyErrors.String())
	}
	// The real key stays in the control plane.
	filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && !strings.HasSuffix(path, ".sqlite") && !strings.Contains(path, "sqlite") {
			if raw, _ := os.ReadFile(path); bytes.Contains(raw, []byte(key)) {
				t.Errorf("upstream key written to %s", path)
			}
		}
		return nil
	})
}
