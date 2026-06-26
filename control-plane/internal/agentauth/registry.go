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
// MUST be kept in sync with runtimed's selectAgent. claude-code joins this when
// its adapter lands (Phase 10B slice 3); until then a connected claude-code is
// "imported, runner not enabled yet".
var runnable = map[string]bool{
	"opencode":    true,
	"claude-code": true,
}

// Runnable reports whether a provider has a runtimed task adapter.
func Runnable(id string) bool { return runnable[id] }

// credentialFiles maps a provider to the HOME-relative file its login writes the
// long-lived token to. Used as the opaque target for credential import and the
// presence check. The file is never opened or parsed.
var credentialFiles = map[string]string{
	"claude-code": ".claude/.credentials.json",
}

// CredentialFile returns the provider's credential file path (relative to HOME).
func CredentialFile(id string) (string, bool) {
	f, ok := credentialFiles[id]
	return f, ok
}
