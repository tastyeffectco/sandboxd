package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func envMap(kv []string) map[string]string {
	m := map[string]string{}
	for _, e := range kv {
		k, v, _ := strings.Cut(e, "=")
		m[k] = v
	}
	return m
}

// Secret-shaped vars are scrubbed; the HOME overlay points at the auth mount;
// ordinary config is preserved.
func TestBuildAgentEnvScrubAndOverlay(t *testing.T) {
	inherited := []string{
		"PATH=/usr/local/bin:/usr/bin",
		"HOME=/home/sandbox",
		"LANG=C.UTF-8",
		"ANTHROPIC_MODEL=claude-sonnet", // non-secret config — keep
		"ANTHROPIC_API_KEY=sk-secret-leak",
		"OPENAI_API_KEY=sk-other",
		"GITHUB_TOKEN=ghp_leak",
		"MY_PASSWORD=hunter2",
		"DB_SECRET=pw",
		"RUNTIMED_AGENT_HOME=/run/agent-home", // runtimed control var — never to the agent
	}
	got := envMap(buildAgentEnv(inherited, map[string]string{"HOME": "/run/agent-home"}))

	for _, leak := range []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY", "GITHUB_TOKEN", "MY_PASSWORD", "DB_SECRET", "RUNTIMED_AGENT_HOME"} {
		if _, ok := got[leak]; ok {
			t.Errorf("secret-shaped %q leaked into agent env", leak)
		}
	}
	if got["HOME"] != "/run/agent-home" {
		t.Errorf("HOME overlay not applied: %q", got["HOME"])
	}
	if got["PATH"] == "" || got["LANG"] != "C.UTF-8" || got["ANTHROPIC_MODEL"] != "claude-sonnet" {
		t.Errorf("non-secret config not preserved: %+v", got)
	}
}

// With no overlay and no secrets, the env is passed through unchanged in spirit
// (opencode behavior compatible) — HOME stays whatever was inherited.
func TestBuildAgentEnvNoOverlayKeepsHome(t *testing.T) {
	got := envMap(buildAgentEnv([]string{"HOME=/home/sandbox", "PATH=/usr/bin"}, nil))
	if got["HOME"] != "/home/sandbox" {
		t.Errorf("HOME = %q; want inherited", got["HOME"])
	}
}

// agentEnv keys HOME on the AGENT NAME, so each agent gets its own mounted auth
// dir — claude-code works even when opencode is also (or only) present.
func TestAgentEnvPerAgentHome(t *testing.T) {
	base := t.TempDir()
	for _, p := range []string{"opencode", "claude-code"} {
		if err := os.MkdirAll(filepath.Join(base, p), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("RUNTIMED_AGENT_AUTH_BASE", base)

	if got := envMap(agentEnv("claude-code", nil))["HOME"]; got != filepath.Join(base, "claude-code") {
		t.Errorf("claude-code HOME = %q", got)
	}
	if got := envMap(agentEnv("opencode", nil))["HOME"]; got != filepath.Join(base, "opencode") {
		t.Errorf("opencode HOME = %q", got)
	}
	// Unmounted agent → no HOME override (runs with default HOME).
	if got := envMap(agentEnv("codex", nil))["HOME"]; got == filepath.Join(base, "codex") {
		t.Error("codex has no mounted auth dir; HOME must not point there")
	}
}

// When a provider is connected by API key, agentEnv injects its one key env var
// from the stored file — the single allowlisted exception to the scrub. Other
// secret-shaped inherited vars are still dropped.
func TestAgentEnvInjectsAPIKey(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "claude-code")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	// APIKeyFile is agentauth.APIKeyFile ('.sandboxd-apikey'); write with trailing
	// newline to confirm it is trimmed.
	if err := os.WriteFile(filepath.Join(dir, ".sandboxd-apikey"), []byte("sk-ant-INJECTED\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RUNTIMED_AGENT_AUTH_BASE", base)
	t.Setenv("ANTHROPIC_API_KEY", "sk-inherited-should-be-overridden")

	got := envMap(agentEnv("claude-code", nil))
	if got["ANTHROPIC_API_KEY"] != "sk-ant-INJECTED" {
		t.Errorf("ANTHROPIC_API_KEY = %q; want injected+trimmed value", got["ANTHROPIC_API_KEY"])
	}
	if got["HOME"] != dir {
		t.Errorf("HOME = %q; want %q", got["HOME"], dir)
	}

	// A provider with a mounted dir but NO key file gets no injected key, and the
	// inherited secret is still scrubbed.
	oc := filepath.Join(base, "opencode")
	if err := os.MkdirAll(oc, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, ok := envMap(agentEnv("opencode", nil))["ANTHROPIC_API_KEY"]; ok {
		t.Error("opencode has no key file; ANTHROPIC_API_KEY must be scrubbed, not injected")
	}
}

func TestIsSecretEnvKey(t *testing.T) {
	secret := []string{"ANTHROPIC_API_KEY", "openai_api_key", "GITHUB_TOKEN", "X_SECRET", "Y_PASSWORD", "Z_CREDENTIALS", "RUNTIMED_X"}
	ok := []string{"PATH", "HOME", "LANG", "ANTHROPIC_MODEL", "API_BASE_URL", "NODE_ENV"}
	for _, k := range secret {
		if !isSecretEnvKey(k) {
			t.Errorf("%q should be scrubbed", k)
		}
	}
	for _, k := range ok {
		if isSecretEnvKey(k) {
			t.Errorf("%q should NOT be scrubbed", k)
		}
	}
}
