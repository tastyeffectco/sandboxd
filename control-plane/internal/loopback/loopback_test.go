package loopback

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// recordingLchown swaps lchownFn for one that records the paths it would chown
// (and the uid/gid), without needing root. Returns the recorder + a restore fn.
func recordingLchown() (paths *[]string, uids *[]int, restore func()) {
	var mu sync.Mutex
	var ps []string
	var us []int
	orig := lchownFn
	lchownFn = func(p string, uid, gid int) error {
		mu.Lock()
		ps = append(ps, p)
		us = append(us, uid)
		mu.Unlock()
		return nil
	}
	return &ps, &us, func() { lchownFn = orig }
}

// normalizeOwnership chowns every entry (incl. the root dir) to the sandbox
// uid/gid — so a restored/forked workspace is writable by uid 1000.
func TestNormalizeOwnershipChownsWholeTree(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "app", "page.js"), "x")
	mustWrite(t, filepath.Join(dir, ".cache", "c"), "x")

	paths, uids, restore := recordingLchown()
	defer restore()
	m := &Manager{}
	if err := m.normalizeOwnership(dir); err != nil {
		t.Fatal(err)
	}
	// Root dir + both files + their parent dirs are all chowned to 1000:1000.
	got := map[string]bool{}
	for i, p := range *paths {
		got[p] = true
		if (*uids)[i] != sandboxUID {
			t.Errorf("%s chowned to uid %d; want %d", p, (*uids)[i], sandboxUID)
		}
	}
	for _, want := range []string{dir, filepath.Join(dir, "app", "page.js"), filepath.Join(dir, ".cache", "c")} {
		if !got[want] {
			t.Errorf("expected %s to be chowned", want)
		}
	}
}

// A symlink pointing OUTSIDE the workspace is chowned as a link (lchown) and
// never followed — its target outside the tree is never touched.
func TestNormalizeOwnershipDoesNotFollowSymlinkEscape(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret")
	mustWrite(t, secret, "TOP SECRET")
	mustWrite(t, filepath.Join(dir, "real.txt"), "x")
	if err := os.Symlink(outside, filepath.Join(dir, "escape")); err != nil {
		t.Fatal(err)
	}

	paths, _, restore := recordingLchown()
	defer restore()
	m := &Manager{}
	if err := m.normalizeOwnership(dir); err != nil {
		t.Fatal(err)
	}
	for _, p := range *paths {
		if p == secret || p == outside {
			t.Errorf("chown escaped the workspace via symlink: touched %s", p)
		}
	}
	// The symlink itself is chowned (harmless lchown), not its target.
	var sawLink bool
	for _, p := range *paths {
		if p == filepath.Join(dir, "escape") {
			sawLink = true
		}
	}
	if !sawLink {
		t.Error("expected the symlink entry itself to be lchowned")
	}
}

// ProvisionFromTemplate — the single funnel for app fork, app restore, and
// direct from_snapshot creation — copies the template then normalizes
// ownership of the whole workspace.
func TestProvisionFromTemplateNormalizesOwnership(t *testing.T) {
	tmpl := t.TempDir()
	mustWrite(t, filepath.Join(tmpl, "workspace", "app", "sandbox.yaml"), "version: 1\n")
	mustWrite(t, filepath.Join(tmpl, "workspace", "app", "src.js"), "// source")

	root := t.TempDir()
	m := &Manager{Root: root}

	paths, uids, restore := recordingLchown()
	defer restore()
	if err := m.ProvisionFromTemplate(context.Background(), "01ABCDEF", tmpl); err != nil {
		t.Fatalf("provision: %v", err)
	}
	ws := filepath.Join(root, "01ABCDEF")
	// Copied files are present...
	if _, err := os.Stat(filepath.Join(ws, "workspace", "app", "sandbox.yaml")); err != nil {
		t.Fatalf("sandbox.yaml not cloned: %v", err)
	}
	// ...and the workspace tree was normalized to the sandbox uid.
	var sawWS, sawFile bool
	for i, p := range *paths {
		if (*uids)[i] != sandboxUID {
			t.Errorf("%s chowned to %d; want %d", p, (*uids)[i], sandboxUID)
		}
		if p == ws {
			sawWS = true
		}
		if p == filepath.Join(ws, "workspace", "app", "src.js") {
			sawFile = true
		}
	}
	if !sawWS || !sawFile {
		t.Errorf("normalization missed workspace dir (%v) or cloned file (%v)", sawWS, sawFile)
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
