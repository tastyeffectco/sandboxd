package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tastyeffectco/sandboxd/control-plane/internal/store"
)

func helperTestServer(t *testing.T) (*Server, *store.Store, string) {
	t.Helper()
	st, err := store.Open(context.Background(), "file::memory:?_fk=1", "../../migrations")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{Store: st, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	s.Handler() // sets the inner router the tools call
	const secret = "helper-secret-for-tests"
	if err := st.CreateHelper(context.Background(), &store.Helper{ID: "h1", OwnerToken: "alice", Package: "hermes",
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

func TestHelperProxyNeedsTheHelpersOwnSecret(t *testing.T) {
	s, _, secret := helperTestServer(t)
	p := s.HelperProxy()
	for _, c := range []struct{ path, secret string }{
		{"/helpers/h1/mcp", ""},
		{"/helpers/h1/mcp", "wrong"},
		{"/helpers/other/mcp", secret}, // a secret only opens its own helper
	} {
		if code, _ := mcpCall(t, p, c.path, c.secret, "ping", nil); code != http.StatusUnauthorized {
			t.Errorf("%s with %q: HTTP %d; want 401", c.path, c.secret, code)
		}
	}
	if code, _ := mcpCall(t, p, "/helpers/h1/mcp", secret, "ping", nil); code != http.StatusOK {
		t.Fatalf("own secret: HTTP %d", code)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/helpers/h1/v1/models", nil)
	req.Header.Set("Authorization", "Bearer "+secret)
	p.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), `"m1"`) {
		t.Errorf("models: %s", rec.Body.String())
	}
}

func TestHelperMCPHandshakeAndTools(t *testing.T) {
	s, st, secret := helperTestServer(t)
	p := s.HelperProxy()
	_, out := mcpCall(t, p, "/helpers/h1/mcp", secret, "initialize", map[string]any{"protocolVersion": "2025-03-26"})
	res, _ := out["result"].(map[string]any)
	if res["protocolVersion"] != "2025-03-26" || res["capabilities"] == nil {
		t.Fatalf("initialize: %v", out)
	}
	_, out = mcpCall(t, p, "/helpers/h1/mcp", secret, "tools/list", nil)
	tools, _ := out["result"].(map[string]any)["tools"].([]any)
	names := []string{}
	for _, tl := range tools {
		names = append(names, tl.(map[string]any)["name"].(string))
	}
	if strings.Join(names, ",") != "list_apps,create_app,ask_app,app_status" {
		t.Fatalf("tools: %v (a helper must not get delete or other tools)", names)
	}

	// Apps are listed for the helper's owner only.
	ctx := context.Background()
	for _, a := range []*store.App{{ID: "a1", OwnerToken: "alice", Name: "Alice shop"}, {ID: "a2", OwnerToken: "bob", Name: "Bob blog"}} {
		if err := st.CreateApp(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	_, out = mcpCall(t, p, "/helpers/h1/mcp", secret, "tools/call", map[string]any{"name": "list_apps"})
	text := toolText(out)
	if !strings.Contains(text, "Alice shop") || strings.Contains(text, "Bob blog") {
		t.Fatalf("list_apps: %q", text)
	}

	// The daily limit is enforced before anything is created.
	day := time.Now().UTC().Format("2006-01-02")
	for i := 0; i < helperAppsPerDay; i++ {
		if ok, _ := st.TakeHelperAppSlot(ctx, "h1", day, helperAppsPerDay); !ok {
			t.Fatalf("slot %d refused", i)
		}
	}
	_, out = mcpCall(t, p, "/helpers/h1/mcp", secret, "tools/call",
		map[string]any{"name": "create_app", "arguments": map[string]string{"idea": "a todo app"}})
	if r := out["result"].(map[string]any); r["isError"] != true || !strings.Contains(toolText(out), "daily limit") {
		t.Fatalf("create_app over the limit: %v", out)
	}
}

func TestHelperAppSlotResetsDaily(t *testing.T) {
	_, st, _ := helperTestServer(t)
	ctx := context.Background()
	if ok, _ := st.TakeHelperAppSlot(ctx, "h1", "2026-10-30", 1); !ok {
		t.Fatal("first slot refused")
	}
	if ok, _ := st.TakeHelperAppSlot(ctx, "h1", "2026-10-30", 1); ok {
		t.Fatal("over the limit allowed")
	}
	if ok, _ := st.TakeHelperAppSlot(ctx, "h1", "2026-10-31", 1); !ok {
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
