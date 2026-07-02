package agentauth

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRegistryHasExpectedProviders(t *testing.T) {
	got := map[string]Provider{}
	for _, p := range Providers() {
		got[p.ID] = p
	}
	for _, want := range []string{"opencode", "claude-code", "codex"} {
		p, ok := got[want]
		if !ok {
			t.Errorf("registry missing %q", want)
			continue
		}
		if p.Binary == "" || p.Label == "" {
			t.Errorf("provider %q missing binary/label: %+v", want, p)
		}
	}
	if _, ok := Get("opencode"); !ok {
		t.Error("Get(opencode) should resolve")
	}
	if _, ok := Get("nope"); ok {
		t.Error("Get(nope) should not resolve")
	}
}

// Connected = dir exists AND non-empty; contents are never read.
func TestStoreConnectedIsPresenceOnly(t *testing.T) {
	data := t.TempDir()
	s := NewStore(data)
	if err := s.EnsureRoot(); err != nil {
		t.Fatal(err)
	}
	if got := filepath.Base(s.Root()); got != "agent-auth" {
		t.Errorf("root = %s", s.Root())
	}

	// Absent provider dir => not connected (A0 never creates provider dirs).
	if s.Connected("claude-code") {
		t.Error("absent provider dir should be not-connected")
	}
	// Empty dir => not connected.
	if err := os.MkdirAll(s.Dir("claude-code"), 0o700); err != nil {
		t.Fatal(err)
	}
	if s.Connected("claude-code") {
		t.Error("empty provider dir should be not-connected")
	}
	// Non-empty (an opaque blob) => connected. We never open it.
	if err := os.WriteFile(filepath.Join(s.Dir("claude-code"), ".credentials.json"), []byte("opaque"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !s.Connected("claude-code") {
		t.Error("non-empty provider dir should be connected")
	}
}

// The store root is under the data dir, never inside a sandbox workspace.
func TestStoreRootOutsideWorkspaces(t *testing.T) {
	s := NewStore("/var/lib/sandboxd")
	if s.Root() != "/var/lib/sandboxd/agent-auth" {
		t.Errorf("root = %s", s.Root())
	}
	if filepath.Dir(s.Dir("opencode")) == filepath.Join("/var/lib/sandboxd", "workspaces") {
		t.Error("auth dir must not be under workspaces/")
	}
}
