// Package agentauth is the foundation for "managed agent auth" (Phase 10B):
// a static registry of the coding-agent CLI providers and a host-side store
// of opaque credential directories. This A0 slice is READ-ONLY — it knows the
// providers, where their auth would live, and whether they're "connected"
// (a non-empty dir). It never creates provider dirs, parses tokens, or injects
// anything into a sandbox. Connect/import and per-task injection come later.
package agentauth

// Provider is one coding-agent CLI we can run as a task agent.
type Provider struct {
	ID     string // stable id used in the API ("opencode", "claude-code", "codex")
	Label  string // human label for the console
	Binary string // the CLI binary name, probed for "installed"
}

// registry is the fixed set of supported providers (owner-operated mode).
var registry = []Provider{
	{ID: "opencode", Label: "OpenCode", Binary: "opencode"},
	{ID: "claude-code", Label: "Claude Code", Binary: "claude"},
	{ID: "codex", Label: "Codex", Binary: "codex"},
}

// Providers returns a copy of the registry in display order.
func Providers() []Provider {
	out := make([]Provider, len(registry))
	copy(out, registry)
	return out
}

// Get returns a provider by id.
func Get(id string) (Provider, bool) {
	for _, p := range registry {
		if p.ID == id {
			return p, true
		}
	}
	return Provider{}, false
}

// runnable is the set of providers runtimed can actually run as a task agent.
// MUST be kept in sync with runtimed's selectAgent. All three have adapters;
// codex is runnable here but parked in the UI (its ChatGPT-subscription auth
// isn't proxyable yet), so the console hides it from the run picker.
var runnable = map[string]bool{
	"opencode":    true,
	"claude-code": true,
	"codex":       true,
}

// Runnable reports whether a provider has a runtimed task adapter.
func Runnable(id string) bool { return runnable[id] }

// credentialFiles maps a provider to the HOME-relative file its login writes the
// long-lived token to. Used as the opaque target for credential import (the
// "connect subscription" path) and the presence check. Every provider's own
// `<cli> login` produces one of these on the owner's machine; the owner pastes
// its contents in. The file is never opened or parsed.
var credentialFiles = map[string]string{
	"claude-code": ".claude/.credentials.json",
	"codex":       ".codex/auth.json",
	"opencode":    ".local/share/opencode/auth.json",
}

// CredentialFile returns the provider's credential file path (relative to HOME).
func CredentialFile(id string) (string, bool) {
	f, ok := credentialFiles[id]
	return f, ok
}

// apiKeyEnv maps a provider to the single environment variable its CLI reads an
// API key from. This is the ONE deliberate exception to runtimed's secret-env
// scrub: when an owner connects a provider by API key, runtimed injects just
// this var (from the stored key file) into the agent process — nothing else
// secret-shaped survives the scrub. opencode connects with an OpenCode (Zen)
// key against api.opencode.ai, so it maps to OPENCODE_API_KEY — NOT an Anthropic
// key (Zen is opencode's own gateway; the key is issued at opencode.ai).
var apiKeyEnv = map[string]string{
	"claude-code": "ANTHROPIC_API_KEY",
	"codex":       "OPENAI_API_KEY",
	"opencode":    "OPENCODE_API_KEY",
}

// APIKeyEnv returns the env var name a provider's CLI reads its API key from.
func APIKeyEnv(id string) (string, bool) {
	e, ok := apiKeyEnv[id]
	return e, ok
}

// APIKeyFile is the HOME-relative file an API key is stored in (opaque, one line).
// Distinct from any credentialFile so the two auth methods never collide; each
// connect fully replaces the provider dir, so a provider holds exactly one method.
const APIKeyFile = ".sandboxd-apikey"
