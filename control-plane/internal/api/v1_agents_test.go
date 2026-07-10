package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tastyeffectco/sandboxd/control-plane/internal/agentauth"
)

func newAgentsServer(t *testing.T, installed map[string]string) (*Server, *agentauth.Store) {
	t.Helper()
	st := agentauth.NewStore(t.TempDir())
	if err := st.EnsureRoot(); err != nil {
		t.Fatal(err)
	}
	s := &Server{
		Image:        "sandboxd-base:test",
		AgentAuth:    st,
		agentProbeFn: func(string) map[string]string { return installed },
	}
	return s, st
}

func getAgents(s *Server) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	s.v1ListAgents(w, httptest.NewRequest("GET", "/v1/agents", nil))
	return w
}

// Status reflects the installed probe + opaque connected check; no tokens.
func TestListAgentsShapeAndStatus(t *testing.T) {
	s, st := newAgentsServer(t, map[string]string{
		"opencode": "installed", "claude": "installed", "codex": "not_installed",
	})
	// Connect claude-code by dropping an opaque blob in its dir.
	secret := "OPAQUE-TOKEN-DO-NOT-LEAK"
	if err := os.MkdirAll(st.Dir("claude-code"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(st.Dir("claude-code"), ".credentials.json"), []byte(secret), 0o600); err != nil {
		t.Fatal(err)
	}

	w := getAgents(s)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d", w.Code)
	}
	body := w.Body.String()
	if strings.Contains(body, secret) {
		t.Fatal("agents response leaked credential contents")
	}
	var d struct {
		Providers []map[string]any `json:"providers"`
	}
	json.Unmarshal([]byte(body), &d)
	by := map[string]map[string]any{}
	for _, p := range d.Providers {
		by[p["id"].(string)] = p
	}
	want := map[string][2]string{
		"opencode":    {"installed", "needs_login"},
		"claude-code": {"installed", "connected"},
		"codex":       {"not_installed", "needs_login"},
	}
	for id, exp := range want {
		p, ok := by[id]
		if !ok {
			t.Errorf("missing provider %q", id)
			continue
		}
		if p["installed_state"] != exp[0] || p["status"] != exp[1] {
			t.Errorf("%s: got installed=%v status=%v; want %v", id, p["installed_state"], p["status"], exp)
		}
	}
}

// A failed/absent probe => installed_state "unknown", and the endpoint still works.
func TestListAgentsProbeUnknown(t *testing.T) {
	s, _ := newAgentsServer(t, nil) // probe returns nil (e.g. docker/image error)
	w := getAgents(s)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d", w.Code)
	}
	var d struct {
		Providers []map[string]any `json:"providers"`
	}
	json.Unmarshal(w.Body.Bytes(), &d)
	if len(d.Providers) != 3 {
		t.Fatalf("want 3 providers, got %d", len(d.Providers))
	}
	for _, p := range d.Providers {
		if p["installed_state"] != "unknown" {
			t.Errorf("%v: installed_state = %v; want unknown", p["id"], p["installed_state"])
		}
	}
}
