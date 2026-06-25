package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sandboxd/control-plane/internal/agentauth"
)

// With no session manager configured, every connect endpoint is nil-safe (503).
func TestAgentConnectNilSafe(t *testing.T) {
	s := &Server{} // AgentSessions nil
	cases := []struct {
		method, path string
		fn           http.HandlerFunc
	}{
		{"POST", "/v1/agents/claude-code/connect", s.v1AgentConnect},
		{"GET", "/v1/agents/claude-code/connect/x", s.v1AgentConnectStatus},
		{"POST", "/v1/agents/claude-code/connect/x/code", s.v1AgentConnectCode},
		{"POST", "/v1/agents/claude-code/disconnect", s.v1AgentDisconnect},
	}
	for _, c := range cases {
		w := httptest.NewRecorder()
		c.fn(w, httptest.NewRequest(c.method, c.path, strings.NewReader(`{}`)))
		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("%s %s: got %d; want 503", c.method, c.path, w.Code)
		}
	}
}

// An unknown session id returns 404 (no token, no leak).
func TestAgentConnectStatusUnknown(t *testing.T) {
	store := agentauth.NewStore(t.TempDir())
	s := &Server{AgentSessions: agentauth.NewSessionManager(store, "img", "true", "")}
	r := httptest.NewRequest("GET", "/v1/agents/claude-code/connect/nope", nil)
	r.SetPathValue("id", "nope")
	w := httptest.NewRecorder()
	s.v1AgentConnectStatus(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("got %d; want 404", w.Code)
	}
}
