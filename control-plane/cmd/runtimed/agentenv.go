package main

import (
	"os"
	"strings"
)

// dummyKey is handed to every agent so its CLI skips the local "not logged in"
// gate; the real credential is injected by the proxy on the wire, never here.
const dummyKey = "sandboxd-proxy-injected"

// agentEnv builds the spawned agent's environment: it scrubs secret-shaped vars
// out of the inherited env, applies the task's env, and points every provider's
// base URL at the credential-injecting proxy with a dummy key. No credential is
// ever placed in the sandbox — the proxy holds and injects it on the wire.

func agentEnv(agentName string, specEnv map[string]string) []string {
	overlay := make(map[string]string, len(specEnv)+8)
	for k, v := range specEnv {
		overlay[k] = v
	}
	// EVERY agent reaches its provider through the credential-injecting proxy:
	// point each provider's base URL at `<proxy>/<agent>/<upstream>` and give the
	// CLI only a DUMMY key. No credential — API key or OAuth token — is mounted or
	// env-injected into the sandbox; the proxy holds and injects the real one.
	// HOME is left at the container default (writable, credential-free) so session
	// state (e.g. --continue) persists without any secret on disk.
	if proxy := os.Getenv("RUNTIMED_ANTHROPIC_PROXY"); proxy != "" {
		base := strings.TrimRight(proxy, "/") + "/" + agentName
		switch agentName {
		case "claude-code":
			overlay["ANTHROPIC_BASE_URL"] = base + "/anthropic"
			overlay["ANTHROPIC_API_KEY"] = dummyKey
		case "opencode":
			// opencode is multi-provider; its per-provider base URLs are written to
			// an OPENCODE_CONFIG file by the opencode adapter (env overrides don't
			// reach the Zen provider). Here we only supply dummy keys so the CLI
			// skips its local login gate.
			overlay["OPENCODE_API_KEY"] = dummyKey
			overlay["ANTHROPIC_API_KEY"] = dummyKey
			overlay["OPENAI_API_KEY"] = dummyKey
		case "codex":
			// codex is parked (its ChatGPT-subscription auth can't be proxied yet);
			// the API-key path would set model_providers.openai.base_url in the
			// adapter. No env credential either way.
		}
	}
	return buildAgentEnv(os.Environ(), overlay)
}

// buildAgentEnv constructs the environment for a spawned coding-agent process.
// It (1) SCRUBS secret-shaped variables out of the inherited process env so an
// agent never picks up credentials that happen to be in the container env
// (e.g. an ANTHROPIC_API_KEY set via `docker run --env`), and (2) applies an
// explicit overlay (notably HOME, pointed at the mounted agent-auth dir). The
// overlay always wins — a scrubbed/duplicate key is replaced by the overlay.
//
// Managed agent auth (Phase 10B) delivers credentials as opaque files under the
// agent's HOME (a bind mount), never as env vars — so the scrub is safe: the
// agent reads its creds from HOME, not from inherited env.
func buildAgentEnv(inherited []string, overlay map[string]string) []string {
	keep := make(map[string]string, len(inherited))
	for _, kv := range inherited {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || isSecretEnvKey(k) {
			continue
		}
		keep[k] = v
	}
	for k, v := range overlay {
		keep[k] = v
	}
	out := make([]string, 0, len(keep))
	for k, v := range keep {
		out = append(out, k+"="+v)
	}
	return out
}

// isSecretEnvKey reports whether an env var name looks like credential
// material that must not leak into the agent process. Conservative: matches
// runtimed's own control vars and the common secret suffixes (so ANTHROPIC_API_KEY,
// OPENAI_API_KEY, GITHUB_TOKEN, *_SECRET, *_PASSWORD, *_CREDENTIALS are dropped)
// while leaving non-secret config (PATH, HOME, LANG, *_MODEL, *_BASE_URL) intact.
func isSecretEnvKey(name string) bool {
	u := strings.ToUpper(name)
	if strings.HasPrefix(u, "RUNTIMED_") {
		return true
	}
	for _, suf := range []string{"_KEY", "_TOKEN", "_SECRET", "_PASSWORD", "_CREDENTIALS", "_APIKEY"} {
		if strings.HasSuffix(u, suf) {
			return true
		}
	}
	return false
}
