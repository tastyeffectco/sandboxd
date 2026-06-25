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
