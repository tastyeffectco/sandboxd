package agentauth

import (
	"os"
	"path/filepath"
)

// Store is the host-side auth store: one opaque directory per connected
// provider under <DataDir>/agent-auth/<provider>. A0 only reads it (existence +
// non-empty); provider dirs are created later by the Connect/import flow.
type Store struct {
	root string // <DataDir>/agent-auth
}

// NewStore roots the auth store under the sandboxd data dir. It is deliberately
// NOT inside any sandbox workspace, so credentials can never land in a
// workspace or a snapshot.
func NewStore(dataDir string) *Store {
	return &Store{root: filepath.Join(dataDir, "agent-auth")}
}

// Root is the auth-store directory.
func (s *Store) Root() string { return s.root }

// Dir is the (possibly absent) auth directory for a provider.
func (s *Store) Dir(provider string) string {
	return filepath.Join(s.root, provider)
}

// EnsureRoot creates the store root (0700). Best-effort; A0 does not create
// per-provider dirs.
func (s *Store) EnsureRoot() error {
	return os.MkdirAll(s.root, 0o700)
}

// Connected reports whether a provider's auth dir exists AND is non-empty. It
// treats the contents as opaque — it never opens or parses any file.
func (s *Store) Connected(provider string) bool {
	entries, err := os.ReadDir(s.Dir(provider))
	if err != nil {
		return false // absent (or unreadable) => not connected
	}
	return len(entries) > 0
}
