package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// A pre-task checkpoint must be invisible to the user's git: it may not move
// HEAD, add commits to the branch history, or stage anything into the user's
// index — while still giving filesChanged an accurate baseline. This is what
// keeps an imported repo's `git log`/push clean.
func TestCheckpointIsolatedFromUserHistory(t *testing.T) {
	dir := t.TempDir()
	gitT(t, dir, "init", "-q")
	gitT(t, dir, "-c", "user.email=u@e", "-c", "user.name=u", "commit", "--allow-empty", "-q", "-m", "user: initial")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitT(t, dir, "add", "a.txt")
	gitT(t, dir, "-c", "user.email=u@e", "-c", "user.name=u", "commit", "-q", "-m", "user: add a")

	headBefore := gitT(t, dir, "rev-parse", "HEAD")
	logBefore := gitT(t, dir, "log", "--oneline")

	cp, err := checkpoint(dir, "task1")
	if err != nil {
		t.Fatalf("checkpoint: %v", err)
	}

	if got := gitT(t, dir, "rev-parse", "HEAD"); got != headBefore {
		t.Errorf("HEAD moved (%s -> %s) — checkpoint must not advance the branch", headBefore, got)
	}
	if got := gitT(t, dir, "log", "--oneline"); got != logBefore {
		t.Errorf("branch history changed by a checkpoint:\nbefore:\n%s\nafter:\n%s", logBefore, got)
	}
	if ref := gitT(t, dir, "rev-parse", checkpointRefPrefix+"task1"); ref != cp {
		t.Errorf("checkpoint not on its private ref: %s vs %s", ref, cp)
	}

	// A task changes one tracked file and adds an untracked one.
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	files, err := filesChanged(dir, cp)
	if err != nil {
		t.Fatalf("filesChanged: %v", err)
	}
	joined := strings.Join(files, ",")
	if !strings.Contains(joined, "a.txt") || !strings.Contains(joined, "b.txt") {
		t.Errorf("files_changed = %v; want both a.txt and b.txt", files)
	}

	// The user's index must be untouched: b.txt is still untracked (NOT staged
	// by our add -A), a.txt is a normal unstaged modification.
	status := gitT(t, dir, "status", "--porcelain")
	if !strings.Contains(status, "?? b.txt") {
		t.Errorf("checkpoint polluted the index — b.txt should still be untracked; status:\n%s", status)
	}
	if strings.Contains(status, "A  b.txt") {
		t.Errorf("checkpoint staged b.txt into the user's index; status:\n%s", status)
	}
}
