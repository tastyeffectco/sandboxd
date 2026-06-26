package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sandboxd/control-plane/internal/agentauth"
)

func newImportServer(t *testing.T) (*Server, *agentauth.Store) {
	t.Helper()
	st := agentauth.NewStore(t.TempDir())
	if err := st.EnsureRoot(); err != nil {
		t.Fatal(err)
	}
	return &Server{AgentAuth: st}, st
}

// Import writes the pasted credential opaquely, promotes it, and reports
// connected — without ever echoing the credential back.
func TestAgentImportStoresOpaquelyAndConnects(t *testing.T) {
	s, st := newImportServer(t)
	secret := `{"claudeAiOauth":{"accessToken":"SECRET-DO-NOT-LEAK"}}`
	w := httptest.NewRecorder()
	s.v1AgentImport(w, httptest.NewRequest("POST", "/v1/agents/claude-code/import",
		strings.NewReader(`{"credentials":`+jsonString(secret)+`}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("got %d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "SECRET-DO-NOT-LEAK") {
		t.Fatal("import response echoed the credential")
	}
	if !st.Connected("claude-code") {
		t.Fatal("claude-code should be connected after import")
	}
	// Stored at the provider's opaque credential path, verbatim.
	b, err := os.ReadFile(filepath.Join(st.Dir("claude-code"), ".claude", ".credentials.json"))
	if err != nil || string(b) != secret {
		t.Fatalf("credential not stored verbatim: err=%v", err)
	}
}

func TestAgentImportRejectsEmpty(t *testing.T) {
	s, _ := newImportServer(t)
	w := httptest.NewRecorder()
	s.v1AgentImport(w, httptest.NewRequest("POST", "/v1/agents/claude-code/import",
		strings.NewReader(`{"credentials":""}`)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("got %d; want 400", w.Code)
	}
}

func TestAgentDisconnectDeletes(t *testing.T) {
	s, st := newImportServer(t)
	_ = st.ImportCredential("claude-code", ".claude/.credentials.json", []byte("x"))
	if !st.Connected("claude-code") {
		t.Fatal("precondition: should be connected")
	}
	w := httptest.NewRecorder()
	s.v1AgentDisconnect(w, httptest.NewRequest("POST", "/v1/agents/claude-code/disconnect", nil))
	if w.Code != http.StatusNoContent {
		t.Fatalf("got %d; want 204", w.Code)
	}
	if st.Connected("claude-code") {
		t.Error("should be disconnected after delete")
	}
}

func TestAgentImportNilSafe(t *testing.T) {
	s := &Server{} // no AgentAuth
	for _, fn := range []http.HandlerFunc{s.v1AgentImport, s.v1AgentDisconnect} {
		w := httptest.NewRecorder()
		fn(w, httptest.NewRequest("POST", "/x", strings.NewReader(`{}`)))
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
