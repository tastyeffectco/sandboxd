package authproxy

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/tastyeffectco/sandboxd/control-plane/internal/agentauth"
)

func writeClaudeOAuth(t *testing.T, st *agentauth.Store, body string) {
	t.Helper()
	dir := filepath.Join(st.Dir("claude-code"), ".claude")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".credentials.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeAPIKey(t *testing.T, st *agentauth.Store, provider, key string) {
	t.Helper()
	if err := os.MkdirAll(st.Dir(provider), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(st.Dir(provider), agentauth.APIKeyFile), []byte(key), 0o600); err != nil {
		t.Fatal(err)
	}
}

// stubUpstream points a named upstream at a test server for the test's duration.
func stubUpstream(t *testing.T, name string, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(h)
	old := upstreams[name]
	upstreams[name] = s.URL
	t.Cleanup(func() { upstreams[name] = old; s.Close() })
	return s
}

func TestMergeBeta(t *testing.T) {
	if got := mergeBeta(""); got != oauthBeta {
		t.Errorf("empty => %q; want %q", got, oauthBeta)
	}
	if got := mergeBeta("foo-2024"); got != "foo-2024,"+oauthBeta {
		t.Errorf("append => %q", got)
	}
	if got := mergeBeta(oauthBeta); got != oauthBeta {
		t.Errorf("already present must not duplicate => %q", got)
	}
}

// Bad path (missing <agent>/<upstream>) → 400, never forwards.
func TestServeBadPath(t *testing.T) {
	p := New(agentauth.NewStore(t.TempDir()), nil)
	w := httptest.NewRecorder()
	p.ServeHTTP(w, httptest.NewRequest("POST", "/v1/messages", nil))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("got %d; want 400", w.Code)
	}
}

// A valid path but no usable credential → 401 with an actionable message.
func TestServeNoCredential(t *testing.T) {
	p := New(agentauth.NewStore(t.TempDir()), nil)
	w := httptest.NewRecorder()
	p.ServeHTTP(w, httptest.NewRequest("POST", "/claude-code/anthropic/v1/messages", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("got %d; want 401", w.Code)
	}
}

// claude-code OAuth: the proxy injects the real bearer + oauth beta, strips the
// sandbox's dummy key, and forwards the upstream path.
func TestInjectsClaudeBearer(t *testing.T) {
	var gotAuth, gotKey, gotBeta, gotPath string
	stubUpstream(t, "anthropic", func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotKey, gotBeta, gotPath = r.Header.Get("Authorization"), r.Header.Get("X-Api-Key"), r.Header.Get("anthropic-beta"), r.URL.Path
		w.WriteHeader(200)
	})
	st := agentauth.NewStore(t.TempDir())
	writeClaudeOAuth(t, st, `{"claudeAiOauth":{"accessToken":"REAL-BEARER"}}`)
	p := New(st, nil)

	req := httptest.NewRequest("POST", "/claude-code/anthropic/v1/messages", nil)
	req.Header.Set("X-Api-Key", "sandboxd-proxy-injected") // the dummy the sandbox sent
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)

	if gotAuth != "Bearer REAL-BEARER" {
		t.Errorf("Authorization = %q; want injected real bearer", gotAuth)
	}
	if gotKey != "" {
		t.Errorf("X-Api-Key leaked to upstream: %q (dummy must be stripped)", gotKey)
	}
	if gotBeta != oauthBeta {
		t.Errorf("anthropic-beta = %q; want %q", gotBeta, oauthBeta)
	}
	if gotPath != "/v1/messages" {
		t.Errorf("forwarded path = %q; want /v1/messages", gotPath)
	}
}

// opencode API key → Zen upstream, injected as a Bearer, dummy stripped.
func TestInjectsOpencodeZenKey(t *testing.T) {
	var gotAuth, gotPath string
	stubUpstream(t, "zen", func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotPath = r.Header.Get("Authorization"), r.URL.Path
		w.WriteHeader(200)
	})
	st := agentauth.NewStore(t.TempDir())
	writeAPIKey(t, st, "opencode", "zen-REAL-KEY")
	p := New(st, nil)

	req := httptest.NewRequest("POST", "/opencode/zen/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer sandboxd-proxy-injected")
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)

	if gotAuth != "Bearer zen-REAL-KEY" {
		t.Errorf("Authorization = %q; want the injected real key", gotAuth)
	}
	if gotPath != "/v1/chat/completions" {
		t.Errorf("forwarded path = %q", gotPath)
	}
}

// Anthropic API key uses the x-api-key header (not Bearer).
func TestInjectsAnthropicAPIKeyHeader(t *testing.T) {
	var gotKey string
	stubUpstream(t, "anthropic", func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("X-Api-Key")
		w.WriteHeader(200)
	})
	st := agentauth.NewStore(t.TempDir())
	writeAPIKey(t, st, "claude-code", "sk-ant-REAL")
	p := New(st, nil)

	req := httptest.NewRequest("POST", "/claude-code/anthropic/v1/messages", nil)
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)
	if gotKey != "sk-ant-REAL" {
		t.Errorf("X-Api-Key = %q; want the injected key", gotKey)
	}
}

// opencode with NO connected credential falls back to Zen's keyless free tier:
// the proxy forwards to the zen upstream with NO auth (the sandbox's dummy key
// stripped), rather than returning 401. This is what makes opencode work out of
// the box with zero setup. It is opencode-only — every other agent still 401s
// when unconnected (see TestServeNoCredential).
func TestOpencodeFreeTierKeyless(t *testing.T) {
	var gotAuth, gotKey, gotPath string
	zenHit, zengoHit := false, false
	stubUpstream(t, "zen", func(w http.ResponseWriter, r *http.Request) {
		zenHit = true
		gotAuth, gotKey, gotPath = r.Header.Get("Authorization"), r.Header.Get("X-Api-Key"), r.URL.Path
		w.WriteHeader(200)
	})
	stubUpstream(t, "zengo", func(w http.ResponseWriter, _ *http.Request) { zengoHit = true; w.WriteHeader(200) })
	p := New(agentauth.NewStore(t.TempDir()), nil) // nothing connected

	// The sandbox requests the `zengo` path (operator pinned it), but the free
	// models live on `zen` — the proxy must override the upstream to `zen`.
	req := httptest.NewRequest("POST", "/opencode/zengo/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer sandboxd-proxy-injected") // the dummy the sandbox sends
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)

	if !zenHit || zengoHit {
		t.Fatalf("free tier must route to zen, not zengo (zenHit=%v zengoHit=%v, status %d)", zenHit, zengoHit, w.Code)
	}
	if gotAuth != "" {
		t.Errorf("Authorization leaked to upstream: %q (free tier must send none)", gotAuth)
	}
	if gotKey != "" {
		t.Errorf("X-Api-Key leaked to upstream: %q (dummy must be stripped)", gotKey)
	}
	if gotPath != "/v1/chat/completions" {
		t.Errorf("forwarded path = %q; want /v1/chat/completions", gotPath)
	}
}

// A CONNECTED opencode key is NOT rerouted — it keeps the operator's chosen
// upstream (here zengo) and gets the real key injected.
func TestConnectedOpencodeKeepsZengo(t *testing.T) {
	var gotAuth string
	zengoHit := false
	stubUpstream(t, "zengo", func(w http.ResponseWriter, r *http.Request) {
		zengoHit = true
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(200)
	})
	st := agentauth.NewStore(t.TempDir())
	writeAPIKey(t, st, "opencode", "zen-REAL-KEY")
	p := New(st, nil)

	req := httptest.NewRequest("POST", "/opencode/zengo/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer sandboxd-proxy-injected")
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)

	if !zengoHit {
		t.Fatalf("connected opencode should keep zengo; status %d", w.Code)
	}
	if gotAuth != "Bearer zen-REAL-KEY" {
		t.Errorf("Authorization = %q; want the injected real key", gotAuth)
	}
}

// A NON-opencode agent with no credential still returns 401 (no keyless tier).
func TestCodexNoCredentialStill401(t *testing.T) {
	p := New(agentauth.NewStore(t.TempDir()), nil)
	w := httptest.NewRecorder()
	p.ServeHTTP(w, httptest.NewRequest("POST", "/codex/openai/v1/chat/completions", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("codex unconnected = %d; want 401 (only opencode has a free tier)", w.Code)
	}
}
