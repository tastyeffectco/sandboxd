package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tastyeffectco/sandboxd/control-plane/internal/store"
)

// The owner opens OpenClaw's own app through sandboxd: signed in by
// sandboxd, with none of sandboxd's own credentials passed on.
func TestAgentUISignsTheOwnerInThroughSandboxd(t *testing.T) {
	s, st, _ := agentTestServer(t)
	s.AgentRoot = t.TempDir()
	s.AgentProxyURL = "http://172.18.0.4:9100"
	if err := st.CreateAgentInstance(context.Background(), &store.AgentInstance{ID: "o1", OwnerToken: "alice",
		Package: "openclaw", Name: "OpenClaw", ConnectionID: "c1", Model: "m1", CreatedAt: "now"}); err != nil {
		t.Fatal(err)
	}
	// An agent made before proxy sign-in: its config is brought up to date.
	cfg := filepath.Join(s.AgentRoot, "o1", "openclaw.json")
	_ = os.MkdirAll(filepath.Dir(cfg), 0o700)
	_ = os.WriteFile(cfg, []byte(`{"gateway":{"mode":"local"}}`), 0o600)

	var got *http.Request
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Clone(context.Background())
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'none'; img-src *")
		w.Write([]byte("<h1>Control</h1>"))
	}))
	defer agent.Close()
	u, _ := url.Parse(agent.URL)
	old := agentUIHost
	agentUIHost = func(string, int) string { return u.Host }
	defer func() { agentUIHost = old }()

	req := asAlice(httptest.NewRequest(http.MethodGet, "/v1/agent-instances/o1/ui/chat?session=main", nil))
	req.Header.Set("Authorization", "Bearer sandboxd-api-token")
	req.Header.Set("Cookie", "sandboxd_session=abc")
	req.Header.Set("X-Sandboxd-User", "mallory")
	req.Header.Set("X-Forwarded-For", "127.0.0.1")
	req.Header.Set("Origin", "http://ui-o1.localhost:5555")
	req.SetPathValue("id", "o1")
	req.SetPathValue("path", "chat")
	rec := httptest.NewRecorder()
	s.v1AgentUI(rec, req)

	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Control") {
		t.Fatalf("HTTP %d %s", rec.Code, rec.Body.String())
	}
	if got.URL.Path != "/chat" || got.URL.RawQuery != "session=main" {
		t.Errorf("forwarded to %s", got.URL)
	}
	for k, want := range map[string]string{"X-Sandboxd-User": "owner", "X-Forwarded-For": "192.0.2.1",
		"Authorization": "", "Cookie": "", "Origin": "http://sandboxd-agent-ui"} {
		if v := got.Header.Get(k); v != want {
			t.Errorf("%s = %q; want %q", k, v, want)
		}
	}
	if rec.Header().Get("X-Frame-Options") != "" || strings.Contains(rec.Header().Get("Content-Security-Policy"), "frame-ancestors") {
		t.Errorf("the app must be allowed inside the agent window: %v", rec.Header())
	}
	b, _ := os.ReadFile(cfg)
	if !strings.Contains(string(b), `"trusted-proxy"`) || !strings.Contains(string(b), "172.18.0.4") {
		t.Errorf("config not brought up to date:\n%s", b)
	}
}

func TestAgentUIOnlyForItsOwnerAndAgentsWithAnApp(t *testing.T) {
	s, _, _ := agentTestServer(t)
	for _, c := range []struct {
		actor bool
		id    string
		want  int
	}{{true, "h1", http.StatusNotFound}, {true, "nope", http.StatusNotFound}} {
		req := httptest.NewRequest(http.MethodGet, "/v1/agent-instances/"+c.id+"/ui/", nil)
		if c.actor {
			req = asAlice(req)
		}
		req.SetPathValue("id", c.id)
		rec := httptest.NewRecorder()
		s.v1AgentUI(rec, req)
		if rec.Code != c.want {
			t.Errorf("%s: HTTP %d; want %d", c.id, rec.Code, c.want)
		}
	}
	// Someone else's agent is not found either.
	req := httptest.NewRequest(http.MethodGet, "/v1/agent-instances/h1/ui/", nil)
	req.SetPathValue("id", "h1")
	rec := httptest.NewRecorder()
	s.v1AgentUI(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("anonymous: HTTP %d", rec.Code)
	}
}
