package api

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tastyeffectco/sandboxd/control-plane/internal/agentauth"
)

func connect(t *testing.T, st *agentauth.Store, provider string) {
	t.Helper()
	if err := os.MkdirAll(st.Dir(provider), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(st.Dir(provider), "cred"), []byte("OPAQUE"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// Every CONNECTED provider is mounted at /run/agent-auth/<provider>, outside the
// workspace; the env carries no token.
func TestAgentAuthMountsAllConnected(t *testing.T) {
	st := agentauth.NewStore(t.TempDir())
	if err := st.EnsureRoot(); err != nil {
		t.Fatal(err)
	}
	s := &Server{AgentAuth: st, DefaultAgent: "opencode"}

	// Nothing connected → no mounts.
	if v := s.agentAuthMounts(); len(v) != 0 {
		t.Fatalf("expected no mounts; got %v", v)
	}

	connect(t, st, "opencode")
	connect(t, st, "claude-code")
	vols := s.agentAuthMounts()
	joined := strings.Join(vols, " ")

	for _, p := range []string{"opencode", "claude-code"} {
		want := st.Dir(p) + ":/run/agent-auth/" + p
		if !strings.Contains(joined, want) {
			t.Errorf("missing mount for %s: %q", p, joined)
		}
	}
	// codex is NOT connected → not mounted.
	if strings.Contains(joined, "/run/agent-auth/codex") {
		t.Error("unconnected codex must not be mounted")
	}
	// Never targets the workspace, and never carries the opaque token.
	if strings.Contains(joined, ":/home/sandbox") {
		t.Error("auth mounts must not target the workspace")
	}
	if strings.Contains(joined, "OPAQUE") {
		t.Error("mount spec leaked credential contents")
	}
}

func TestAgentAuthMountsNilSafe(t *testing.T) {
	if v := (&Server{}).agentAuthMounts(); v != nil {
		t.Error("nil AgentAuth should mount nothing")
	}
}
