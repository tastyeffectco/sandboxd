package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"fmt"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/modelprovider"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/secrets"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/store"
	"path/filepath"
)

func agentTestServer(t *testing.T) (*Server, *store.Store, string) {
	t.Helper()
	st, err := store.Open(context.Background(), "file::memory:?_fk=1", "../../migrations")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{Store: st, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	s.Handler() // sets the inner router the tools call
	const secret = "agent-secret-for-tests"
	if err := st.CreateAgentInstance(context.Background(), &store.AgentInstance{ID: "h1", OwnerToken: "alice", Package: "hermes",
		Name: "Hermes", ConnectionID: "c1", Model: "m1", SecretHash: hashSecret(secret), CreatedAt: "now"}); err != nil {
		t.Fatal(err)
	}
	return s, st, secret
}

func mcpCall(t *testing.T, h http.Handler, path, secret, method string, params any) (int, map[string]any) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(body)))
	if secret != "" {
		req.Header.Set("Authorization", "Bearer "+secret)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func TestAgentProxyNeedsTheAgentsOwnSecret(t *testing.T) {
	s, _, secret := agentTestServer(t)
	p := s.AgentProxy()
	for _, c := range []struct{ path, secret string }{
		{"/agents/h1/mcp", ""},
		{"/agents/h1/mcp", "wrong"},
		{"/agents/other/mcp", secret}, // a secret only opens its own agent
	} {
		if code, _ := mcpCall(t, p, c.path, c.secret, "ping", nil); code != http.StatusUnauthorized {
			t.Errorf("%s with %q: HTTP %d; want 401", c.path, c.secret, code)
		}
	}
	if code, _ := mcpCall(t, p, "/agents/h1/mcp", secret, "ping", nil); code != http.StatusOK {
		t.Fatalf("own secret: HTTP %d", code)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/agents/h1/v1/models", nil)
	req.Header.Set("Authorization", "Bearer "+secret)
	p.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), `"m1"`) {
		t.Errorf("models: %s", rec.Body.String())
	}
}

func TestAgentMCPHandshakeAndTools(t *testing.T) {
	s, st, secret := agentTestServer(t)
	p := s.AgentProxy()
	_, out := mcpCall(t, p, "/agents/h1/mcp", secret, "initialize", map[string]any{"protocolVersion": "2025-03-26"})
	res, _ := out["result"].(map[string]any)
	if res["protocolVersion"] != "2025-03-26" || res["capabilities"] == nil {
		t.Fatalf("initialize: %v", out)
	}
	_, out = mcpCall(t, p, "/agents/h1/mcp", secret, "tools/list", nil)
	tools, _ := out["result"].(map[string]any)["tools"].([]any)
	names := []string{}
	for _, tl := range tools {
		names = append(names, tl.(map[string]any)["name"].(string))
	}
	for _, tl := range tools {
		schema := tl.(map[string]any)["inputSchema"].(map[string]any)
		if r, present := schema["required"]; present && r == nil {
			t.Errorf("%v: required is null (strict MCP clients reject the tool list)", tl.(map[string]any)["name"])
		}
	}
	if strings.Join(names, ",") != "list_apps,create_app,ask_app,app_status" {
		t.Fatalf("tools: %v (an agent must not get delete or other tools)", names)
	}

	// Apps are listed for the agent's owner only.
	ctx := context.Background()
	for _, a := range []*store.App{{ID: "a1", OwnerToken: "alice", Name: "Alice shop"}, {ID: "a2", OwnerToken: "bob", Name: "Bob blog"}} {
		if err := st.CreateApp(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	_, out = mcpCall(t, p, "/agents/h1/mcp", secret, "tools/call", map[string]any{"name": "list_apps"})
	text := toolText(out)
	if !strings.Contains(text, "Alice shop") || strings.Contains(text, "Bob blog") {
		t.Fatalf("list_apps: %q", text)
	}

	// The daily limit is enforced before anything is created.
	day := time.Now().UTC().Format("2006-01-02")
	for i := 0; i < agentAppsPerDay; i++ {
		if ok, _ := st.TakeAgentAppSlot(ctx, "h1", day, agentAppsPerDay); !ok {
			t.Fatalf("slot %d refused", i)
		}
	}
	_, out = mcpCall(t, p, "/agents/h1/mcp", secret, "tools/call",
		map[string]any{"name": "create_app", "arguments": map[string]string{"idea": "a todo app"}})
	if r := out["result"].(map[string]any); r["isError"] != true || !strings.Contains(toolText(out), "daily limit") {
		t.Fatalf("create_app over the limit: %v", out)
	}
}

func TestAgentAppSlotResetsDaily(t *testing.T) {
	_, st, _ := agentTestServer(t)
	ctx := context.Background()
	if ok, _ := st.TakeAgentAppSlot(ctx, "h1", "2026-10-30", 1); !ok {
		t.Fatal("first slot refused")
	}
	if ok, _ := st.TakeAgentAppSlot(ctx, "h1", "2026-10-30", 1); ok {
		t.Fatal("over the limit allowed")
	}
	if ok, _ := st.TakeAgentAppSlot(ctx, "h1", "2026-10-31", 1); !ok {
		t.Fatal("the next day did not reset the limit")
	}
}

func toolText(out map[string]any) string {
	r, _ := out["result"].(map[string]any)
	c, _ := r["content"].([]any)
	if len(c) == 0 {
		return ""
	}
	s, _ := c[0].(map[string]any)["text"].(string)
	return s
}

// An agent counts as running only once its chat API accepts connections.
func TestAgentAnsweringProbe(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	if !agentAnswering(context.Background(), "127.0.0.1", port) {
		t.Fatal("listening agent reported as not answering")
	}
	l.Close()
	if agentAnswering(context.Background(), "127.0.0.1", port) {
		t.Fatal("closed port reported as answering")
	}
}

// An agent always thinks with the model it was given: a model name it kept
// from before the person changed its model is replaced, not refused.
func TestAgentModelRequestUsesTheAgentsModel(t *testing.T) {
	s, st, _ := agentTestServer(t)
	cipher, err := secrets.Load("", filepath.Join(t.TempDir(), "key"))
	if err != nil {
		t.Fatal(err)
	}
	if s.ModelProviders, err = modelprovider.New(st, cipher); err != nil {
		t.Fatal(err)
	}
	s.AgentProxyURL = "http://sandboxd:9100"
	got := make(chan string, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b map[string]any
		_ = json.NewDecoder(r.Body).Decode(&b)
		got <- fmt.Sprint(b["model"])
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"choices":[{"message":{"content":"ok"}}]}`)
	}))
	defer upstream.Close()
	key := "k"
	c, err := s.ModelProviders.Save(context.Background(), "", modelprovider.Input{Name: "Pro", Protocol: "openai-completions",
		BaseURL: upstream.URL + "/v1", Models: []modelprovider.Model{{ID: "deepseek/pro"}}, APIKey: &key})
	if err != nil {
		t.Fatal(err)
	}
	const secret = "agent-secret-3"
	if err := st.CreateAgentInstance(context.Background(), &store.AgentInstance{ID: "h3", OwnerToken: "alice", Package: "openmausbot",
		Name: "OpenMausBot", ConnectionID: c.ID, Model: "deepseek/pro", SecretHash: hashSecret(secret), CreatedAt: "now"}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/agents/h3/v1/chat/completions",
		strings.NewReader(`{"model":"deepseek/flash-it-remembered","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Authorization", "Bearer "+secret)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.AgentProxy().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("HTTP %d %s", rec.Code, rec.Body.String())
	}
	if m := <-got; m != "deepseek/pro" {
		t.Errorf("upstream got model %q", m)
	}
}
