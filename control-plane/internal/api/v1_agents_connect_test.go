package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tastyeffectco/sandboxd/control-plane/internal/agentauth"
)

func newImportServer(t *testing.T) (*Server, *agentauth.Store) {
	t.Helper()
	st := agentauth.NewStore(t.TempDir())
	if err := st.EnsureRoot(); err != nil {
		t.Fatal(err)
	}
	return &Server{AgentAuth: st}, st
}

// req builds a request with {provider} set, as the mux would.
func agentReq(method, provider, body string) *http.Request {
	r := httptest.NewRequest(method, "/v1/agents/"+provider+"/x", strings.NewReader(body))
	r.SetPathValue("provider", provider)
	return r
}

// Import writes the pasted credential opaquely, promotes it, and reports
// connected — without ever echoing the credential back. Runs for every provider
// so the generic path is exercised across the registry.
func TestAgentImportStoresOpaquelyAndConnects(t *testing.T) {
	for _, prov := range []string{"claude-code", "codex", "opencode"} {
		t.Run(prov, func(t *testing.T) {
			s, st := newImportServer(t)
			secret := `{"oauth":{"accessToken":"SECRET-DO-NOT-LEAK"}}`
			w := httptest.NewRecorder()
			s.v1AgentImport(w, agentReq("POST", prov, `{"credentials":`+jsonString(secret)+`}`))
			if w.Code != http.StatusOK {
				t.Fatalf("got %d: %s", w.Code, w.Body.String())
			}
			if strings.Contains(w.Body.String(), "SECRET-DO-NOT-LEAK") {
				t.Fatal("import response echoed the credential")
			}
			if !st.Connected(prov) {
				t.Fatalf("%s should be connected after import", prov)
			}
			if st.Method(prov) != "oauth" {
				t.Fatalf("method = %q; want oauth", st.Method(prov))
			}
			rel, _ := agentauth.CredentialFile(prov)
			b, err := os.ReadFile(filepath.Join(st.Dir(prov), rel))
			if err != nil || string(b) != secret {
				t.Fatalf("credential not stored verbatim: err=%v", err)
			}
		})
	}
}

// API key is stored opaquely at the key file and reported as method=api_key.
func TestAgentAPIKeyStoresAndConnects(t *testing.T) {
	s, st := newImportServer(t)
	w := httptest.NewRecorder()
	s.v1AgentAPIKey(w, agentReq("POST", "codex", `{"api_key":"sk-KEY-DO-NOT-LEAK"}`))
	if w.Code != http.StatusOK {
		t.Fatalf("got %d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "sk-KEY-DO-NOT-LEAK") {
		t.Fatal("api-key response echoed the key")
	}
	if st.Method("codex") != "api_key" {
		t.Fatalf("method = %q; want api_key", st.Method("codex"))
	}
	b, err := os.ReadFile(filepath.Join(st.Dir("codex"), agentauth.APIKeyFile))
	if err != nil || string(b) != "sk-KEY-DO-NOT-LEAK" {
		t.Fatalf("api key not stored verbatim: err=%v", err)
	}
}

// Connecting a second method fully replaces the first (dir is atomically swapped).
func TestAgentConnectReplacesMethod(t *testing.T) {
	s, st := newImportServer(t)
	w := httptest.NewRecorder()
	s.v1AgentImport(w, agentReq("POST", "claude-code", `{"credentials":"{\"x\":1}"}`))
	if st.Method("claude-code") != "oauth" {
		t.Fatalf("precondition: want oauth, got %q", st.Method("claude-code"))
	}
	w = httptest.NewRecorder()
	s.v1AgentAPIKey(w, agentReq("POST", "claude-code", `{"api_key":"sk-x"}`))
	if st.Method("claude-code") != "api_key" {
		t.Fatalf("method = %q; want api_key after replace", st.Method("claude-code"))
	}
	// The old oauth credential file must be gone.
	rel, _ := agentauth.CredentialFile("claude-code")
	if _, err := os.Stat(filepath.Join(st.Dir("claude-code"), rel)); err == nil {
		t.Fatal("old credential file survived the method switch")
	}
}

func TestAgentImportRejectsEmpty(t *testing.T) {
	s, _ := newImportServer(t)
	w := httptest.NewRecorder()
	s.v1AgentImport(w, agentReq("POST", "claude-code", `{"credentials":""}`))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("got %d; want 400", w.Code)
	}
}

func TestAgentUnknownProvider(t *testing.T) {
	s, _ := newImportServer(t)
	w := httptest.NewRecorder()
	s.v1AgentImport(w, agentReq("POST", "not-a-provider", `{"credentials":"x"}`))
	if w.Code != http.StatusNotFound {
		t.Fatalf("got %d; want 404", w.Code)
	}
}

func TestAgentDisconnectDeletes(t *testing.T) {
	s, st := newImportServer(t)
	_ = st.ImportCredential("claude-code", ".claude/.credentials.json", []byte("x"))
	if !st.Connected("claude-code") {
		t.Fatal("precondition: should be connected")
	}
	w := httptest.NewRecorder()
	s.v1AgentDisconnect(w, agentReq("POST", "claude-code", ""))
	if w.Code != http.StatusNoContent {
		t.Fatalf("got %d; want 204", w.Code)
	}
	if st.Connected("claude-code") {
		t.Error("should be disconnected after delete")
	}
}

func TestAgentImportNilSafe(t *testing.T) {
	s := &Server{} // no AgentAuth
	for _, fn := range []http.HandlerFunc{s.v1AgentImport, s.v1AgentAPIKey, s.v1AgentDisconnect} {
		w := httptest.NewRecorder()
		fn(w, agentReq("POST", "claude-code", `{}`))
		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("got %d; want 503", w.Code)
		}
	}
}

// jsonString quotes a string as a JSON literal for embedding in a request body.
func jsonString(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return `"` + r.Replace(s) + `"`
}
