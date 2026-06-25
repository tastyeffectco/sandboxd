package api

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sandboxd/control-plane/internal/agentauth"
)

// agentAuthMount mounts ONLY the connected default provider's dir, at a path
// outside the workspace, and never exposes the token via env (only a path).
func TestAgentAuthMountConnectedOnly(t *testing.T) {
	data := t.TempDir()
	st := agentauth.NewStore(data)
	if err := st.EnsureRoot(); err != nil {
		t.Fatal(err)
	}
	s := &Server{AgentAuth: st, DefaultAgent: "opencode"}

	// Not connected (no dir) => no mount.
	if vol, env := s.agentAuthMount(); vol != "" || env != "" {
		t.Fatalf("unconnected provider should not mount: vol=%q env=%q", vol, env)
	}

	// Connect opencode with an opaque blob.
	token := "OPAQUE-DO-NOT-LEAK"
	if err := os.MkdirAll(st.Dir("opencode"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(st.Dir("opencode"), "auth.json"), []byte(token), 0o600); err != nil {
		t.Fatal(err)
	}

	vol, env := s.agentAuthMount()
	if vol != st.Dir("opencode")+":/run/agent-home" {
		t.Errorf("vol = %q", vol)
	}
	if env != "RUNTIMED_AGENT_HOME=/run/agent-home" {
		t.Errorf("env = %q", env)
	}
	// The mount target is OUTSIDE the workspace (/home/sandbox).
	if strings.Contains(vol, ":/home/sandbox") {
		t.Error("auth mount must not target the workspace")
	}
	// The env carries only a path — never the opaque token.
	if strings.Contains(env, token) {
		t.Error("auth env leaked the token")
	}

	// Mounts ONLY the default provider, even if another is connected.
	if err := os.MkdirAll(st.Dir("claude-code"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(st.Dir("claude-code"), "c"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if vol, _ := s.agentAuthMount(); !strings.Contains(vol, "agent-auth/opencode:") {
		t.Errorf("should mount only the default provider (opencode); got %q", vol)
	}
}

// nil-safe: no store / empty default => no mount (sandbox runs as before).
func TestAgentAuthMountDisabled(t *testing.T) {
	if vol, _ := (&Server{}).agentAuthMount(); vol != "" {
		t.Error("nil AgentAuth should not mount")
	}
	st := agentauth.NewStore(t.TempDir())
	if vol, _ := (&Server{AgentAuth: st, DefaultAgent: ""}).agentAuthMount(); vol != "" {
		t.Error("empty DefaultAgent should not mount")
	}
}
