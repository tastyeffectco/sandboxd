// Package agentauth manages the built-in coding-agent registry and legacy
// credential-file store. Configurable model connections live in modelprovider;
// Pi uses those connections rather than a built-in credential-file import.
package agentauth

// Provider is one coding-agent CLI we can run as a task agent.
type Provider struct {
	ID     string // stable id used in the API ("opencode", "pi", "claude-code", "codex")
	Label  string // human label for the console
	Binary string // the CLI binary name, probed for "installed"
}

// registry is the fixed set of supported providers (owner-operated mode). It
// holds the coding-agent CLIs runtimed can drive AND any credential-only
// provider whose key the auth proxy injects on the wire (a model gateway that
// is reached through an agent's proxy path, not run as its own task agent).
var registry = []Provider{
	{ID: "opencode", Label: "OpenCode", Binary: "opencode"},
	{ID: "pi", Label: "Pi", Binary: "pi"},
	{ID: "claude-code", Label: "Claude Code", Binary: "claude"},
	{ID: "codex", Label: "Codex", Binary: "codex"},
	// MiniMax is a credential-only provider: the owner connects a MiniMax API
	// key here, and the auth proxy injects it for the minimax upstreams. It has
	// no task-agent CLI and is never run by runtimed (Runnable=false), so the
	// console shows it as a credential-only entry, not a run picker option.
	{ID: "minimax", Label: "MiniMax", Binary: ""},
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
// MUST be kept in sync with runtimed's selectAgent. All four have adapters;
// codex is runnable here but parked in the UI (its ChatGPT-subscription auth
// isn't proxyable yet), so the console hides it from the run picker.
var runnable = map[string]bool{
	"opencode":    true,
	"pi":          true,
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

// apiKeyEnv names the credential kinds supported by built-in providers. With
// the auth proxy enabled, these keys are read and injected control-plane-side;
// runtimed receives dummy keys. The map also supports legacy credential import.
var apiKeyEnv = map[string]string{
	"claude-code": "ANTHROPIC_API_KEY",
	"codex":       "OPENAI_API_KEY",
	"opencode":    "OPENCODE_API_KEY",
	// MiniMax is a credential-only provider; its key is read from the stored
	// key file and injected by the auth proxy for the minimax upstreams. No CLI
	// reads this env var (MiniMax is never run as a task agent), so the name is
	// advisory only.
	"minimax": "MINIMAX_API_KEY",
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
