package main

import "strings"

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
