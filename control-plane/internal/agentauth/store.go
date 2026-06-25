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

// Delete removes a provider's auth dir (Disconnect). Opaque; no parsing.
func (s *Store) Delete(provider string) error {
	return os.RemoveAll(s.Dir(provider))
}

// NewStaging creates a fresh, isolated staging dir under the store root for an
// in-progress login. It is chowned to the sandbox uid (best-effort) so the
// ephemeral auth container (uid 1000) can write its credential files there.
func (s *Store) NewStaging() (string, error) {
	if err := s.EnsureRoot(); err != nil {
		return "", err
	}
	dir, err := os.MkdirTemp(s.root, ".staging-")
	if err != nil {
		return "", err
	}
	_ = os.Chmod(dir, 0o700)
	_ = os.Chown(dir, 1000, 1000) // best-effort; sandboxd runs as root in prod
	return dir, nil
}

// Promote atomically replaces a provider's auth dir with the staging dir. Same
// filesystem (both under the store root), so the rename is atomic.
func (s *Store) Promote(staging, provider string) error {
	final := s.Dir(provider)
	_ = os.RemoveAll(final)
	return os.Rename(staging, final)
}

// CredentialPresent reports whether a non-empty file exists at rel within dir —
// presence only, never opened/parsed. Used as the success signal for a login
// (the CLI writes its long-lived token there), since exit codes are unreliable.
func (s *Store) CredentialPresent(dir, rel string) bool {
	fi, err := os.Stat(filepath.Join(dir, rel))
	return err == nil && !fi.IsDir() && fi.Size() > 0
}
