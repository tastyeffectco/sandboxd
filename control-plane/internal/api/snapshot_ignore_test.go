package api

import (
	"os"
	"path/filepath"
	"testing"
)

// copyTreeExcluding drops generated/dependency dirs (at any depth), keeps
// user source + sandbox.yaml, and copies symlinks verbatim without escaping.
func TestCopyTreeExcluding(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	// user source + manifest
	mustWrite(t, filepath.Join(src, "app", "page.js"), "export default 1")
	mustWrite(t, filepath.Join(src, "sandbox.yaml"), "version: 1\n")
	mustWrite(t, filepath.Join(src, "src", "main.tsx"), "// source")
	// ignored dirs, including a NESTED one (monorepo case)
	mustWrite(t, filepath.Join(src, "node_modules", "react", "index.js"), "x")
	mustWrite(t, filepath.Join(src, ".next", "static", "chunks", "main.js"), "x")
	mustWrite(t, filepath.Join(src, ".venv", "bin", "python"), "x")
	mustWrite(t, filepath.Join(src, "__pycache__", "m.pyc"), "x")
	mustWrite(t, filepath.Join(src, ".cache", "c"), "x")
	mustWrite(t, filepath.Join(src, "packages", "ui", "node_modules", "dep", "i.js"), "x")
	// dist is NOT ignored (kept)
	mustWrite(t, filepath.Join(src, "dist", "keep.js"), "x")
	// a symlink that points OUTSIDE the tree — must be copied as a link,
	// never followed (no escape).
	outside := filepath.Join(t.TempDir(), "secret")
	mustWrite(t, outside, "TOP SECRET")
	if err := os.Symlink(outside, filepath.Join(src, "link-to-secret")); err != nil {
		t.Fatal(err)
	}

	if err := copyTreeExcluding(src, dst, snapshotIgnoreDirs); err != nil {
		t.Fatalf("copy: %v", err)
	}

	// kept
	for _, p := range []string{"app/page.js", "sandbox.yaml", "src/main.tsx", "dist/keep.js"} {
		if _, err := os.Stat(filepath.Join(dst, p)); err != nil {
			t.Errorf("expected %s kept: %v", p, err)
		}
	}
	// excluded (incl. nested)
	for _, p := range []string{"node_modules", ".next", ".venv", "__pycache__", ".cache", "packages/ui/node_modules"} {
		if _, err := os.Stat(filepath.Join(dst, p)); !os.IsNotExist(err) {
			t.Errorf("expected %s excluded, err=%v", p, err)
		}
	}
	// symlink copied verbatim, NOT followed — and the secret content never
	// materialized as a real file in dst.
	fi, err := os.Lstat(filepath.Join(dst, "link-to-secret"))
	if err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("symlink should be copied as a symlink: %v mode=%v", err, fi.Mode())
	}
	if got, _ := os.ReadFile(filepath.Join(dst, "link-to-secret")); string(got) == "TOP SECRET" {
		// reading through the link is fine; what matters is no traversal wrote
		// outside dst — assert the link target wasn't copied as a real file.
		if data, _ := os.Lstat(filepath.Join(dst, "link-to-secret")); data.Mode().IsRegular() {
			t.Error("symlink target was materialized as a regular file (escape)")
		}
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
