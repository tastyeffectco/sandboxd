package main

import (
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

// Every agent is pointed at the proxy per provider with a dummy key; no
// credential and no auth-dir HOME is placed in the sandbox.
func TestAgentEnvProxyRouting(t *testing.T) {
	t.Setenv("RUNTIMED_ANTHROPIC_PROXY", "http://sandboxd:9100")

	cc := envMap(agentEnv("claude-code", nil))
	if cc["ANTHROPIC_BASE_URL"] != "http://sandboxd:9100/claude-code/anthropic" {
		t.Errorf("claude ANTHROPIC_BASE_URL = %q", cc["ANTHROPIC_BASE_URL"])
	}
	if cc["ANTHROPIC_API_KEY"] != dummyKey {
		t.Errorf("claude key = %q; want dummy", cc["ANTHROPIC_API_KEY"])
	}

	// opencode gets dummy keys; its per-provider base URLs are supplied by the
	// OPENCODE_CONFIG file the adapter writes, not env.
	oc := envMap(agentEnv("opencode", nil))
	if oc["OPENCODE_API_KEY"] != dummyKey || oc["ANTHROPIC_API_KEY"] != dummyKey || oc["OPENAI_API_KEY"] != dummyKey {
		t.Error("opencode should get dummy keys for its providers")
	}

	// No proxy configured → no base urls (fallback path).
	t.Setenv("RUNTIMED_ANTHROPIC_PROXY", "")
	if _, ok := envMap(agentEnv("claude-code", nil))["ANTHROPIC_BASE_URL"]; ok {
		t.Error("no proxy => no ANTHROPIC_BASE_URL")
	}
}

// No real credential reaches the agent: an inherited real key is scrubbed and
// replaced by the dummy, and HOME is never pointed at an auth dir.
func TestAgentEnvNoCredentialInSandbox(t *testing.T) {
	t.Setenv("RUNTIMED_ANTHROPIC_PROXY", "http://sandboxd:9100")
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-REAL-should-not-leak")

	got := envMap(agentEnv("claude-code", nil))
	if got["ANTHROPIC_API_KEY"] != dummyKey {
		t.Errorf("ANTHROPIC_API_KEY = %q; a real inherited key must be replaced by the dummy", got["ANTHROPIC_API_KEY"])
	}
	if strings.Contains(got["HOME"], "agent-auth") {
		t.Errorf("HOME must not point at an auth dir: %q", got["HOME"])
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
