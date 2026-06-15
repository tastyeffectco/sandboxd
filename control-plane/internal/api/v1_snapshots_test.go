package api

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestCaptureImageCopiesDirectory is the regression guard for the
// directory-storage snapshot bug: captureImage used to `cp` the
// workspace without -r, which fails on a directory ("cp: -r not
// specified; omitting directory"). It must now copy the whole tree.
func TestCaptureImageCopiesDirectory(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	if err := os.MkdirAll(filepath.Join(src, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "sub", "b.txt"), []byte("world"), 0o644); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(root, "snap")
	size, err := captureImage(context.Background(), src, dst, root)
	if err != nil {
		t.Fatalf("captureImage on a directory failed: %v", err)
	}
	if size <= 0 {
		t.Errorf("expected positive captured size, got %d", size)
	}

	// The whole tree must round-trip (contents, not the source dir itself).
	if got, err := os.ReadFile(filepath.Join(dst, "a.txt")); err != nil || string(got) != "hello" {
		t.Errorf("a.txt = %q, %v; want \"hello\", nil", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(dst, "sub", "b.txt")); err != nil || string(got) != "world" {
		t.Errorf("sub/b.txt = %q, %v; want \"world\", nil", got, err)
	}

	// No .tmp staging dir must survive a successful capture.
	if _, err := os.Stat(dst + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("staging dir %s.tmp should not exist after capture", dst)
	}
}
