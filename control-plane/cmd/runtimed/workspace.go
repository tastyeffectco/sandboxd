package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const buildCheckTimeout = 120 * time.Second

func runGit(appDir string, args ...string) (string, error) {
	full := append([]string{"-C", appDir}, args...)
	out, err := exec.Command("git", full...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

func gitCommit(appDir, msg string) error {
	_, err := runGit(appDir,
		"-c", "user.email=runtimed@sandbox.local", "-c", "user.name=runtimed",
		"commit", "--allow-empty", "-q", "-m", msg)
	return err
}

// ensureRepo makes the app dir a git repo on first use, with one
// baseline commit. node_modules / dist / .vite are excluded by the
// app's committed .gitignore.
func ensureRepo(appDir string) error {
	if _, err := os.Stat(filepath.Join(appDir, ".git")); err == nil {
		return nil
	}
	if _, err := runGit(appDir, "init", "-q"); err != nil {
		return err
	}
	if _, err := runGit(appDir, "add", "-A"); err != nil {
		return err
	}
	return gitCommit(appDir, "runtimed: golden snapshot baseline")
}

// runGitIndex runs git with an ISOLATED index file, so `git add` never touches
// the user's real staging area.
func runGitIndex(appDir, indexFile string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", appDir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_INDEX_FILE="+indexFile)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// worktreeTree stages the whole worktree into a THROWAWAY index and writes a
// tree object — capturing the current state without disturbing the user's
// index, working tree, or branch.
func worktreeTree(appDir string) (string, error) {
	idx := filepath.Join(appDir, ".git", "sandboxd-ckpt.index")
	defer os.Remove(idx)
	if _, err := runGitIndex(appDir, idx, "add", "-A"); err != nil {
		return "", err
	}
	return runGitIndex(appDir, idx, "write-tree")
}

// checkpointRefPrefix is a PRIVATE ref namespace for pre-task snapshots. It
// lives OUTSIDE refs/heads and refs/remotes, so checkpoints never appear in the
// user's `git log`, `git status`, or a push — only runtimed sees them.
const checkpointRefPrefix = "refs/sandboxd/checkpoints/"

// checkpoint records the pre-task workspace state as a commit under a PRIVATE
// ref (never HEAD) and returns its SHA — the files_changed baseline and revert
// seam. It does NOT advance the branch or touch the user's index/working tree,
// so an imported repo's history and push stay clean.
func checkpoint(appDir, taskID string) (string, error) {
	if err := ensureRepo(appDir); err != nil {
		return "", err
	}
	tree, err := worktreeTree(appDir)
	if err != nil {
		return "", err
	}
	// A checkpoint is a commit off to the side (commit-tree), so HEAD/the branch
	// never move: parent = current HEAD when the repo has one, else a root commit.
	args := []string{
		"-c", "user.email=runtimed@sandbox.local", "-c", "user.name=runtimed",
		"commit-tree", tree, "-m", "runtimed: checkpoint before task " + taskID,
	}
	if head, herr := runGit(appDir, "rev-parse", "-q", "--verify", "HEAD"); herr == nil && head != "" {
		args = append(args, "-p", head)
	}
	sha, err := runGit(appDir, args...)
	if err != nil {
		return "", err
	}
	// Tasks run one at a time, so only the current checkpoint is needed — prune
	// prior ones to keep the private refs from accumulating.
	if refs, _ := runGit(appDir, "for-each-ref", "--format=%(refname)", checkpointRefPrefix); refs != "" {
		for _, r := range strings.Split(refs, "\n") {
			_, _ = runGit(appDir, "update-ref", "-d", r)
		}
	}
	if _, err := runGit(appDir, "update-ref", checkpointRefPrefix+taskID, sha); err != nil {
		return "", err
	}
	return sha, nil
}

// filesChanged lists workspace paths the task changed, relative to the app dir,
// by diffing the current worktree tree against the checkpoint commit — via a
// throwaway index, so the user's staging area is untouched. It is the
// authoritative files_changed source (provider-agnostic).
func filesChanged(appDir, checkpointID string) ([]string, error) {
	if checkpointID == "" {
		return nil, nil
	}
	tree, err := worktreeTree(appDir)
	if err != nil {
		return nil, err
	}
	out, err := runGit(appDir, "diff", "--name-only", checkpointID, tree)
	if err != nil {
		return nil, err
	}
	if out == "" {
		return []string{}, nil
	}
	return strings.Split(out, "\n"), nil
}

// buildCheck runs the project build and reports whether the app
// compiles — this is what makes a task's build_ok honest.
func buildCheck(appDir, command string, timeout time.Duration, log *slog.Logger) (ok bool, errMsg string) {
	if command == "" {
		return true, "" // no build command declared -> nothing to check
	}
	if timeout <= 0 {
		timeout = buildCheckTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", "-lc", command)
	cmd.Dir = appDir
	out, err := cmd.CombinedOutput()
	if err == nil {
		return true, ""
	}
	log.Warn("post-task build check failed", "err", err.Error())
	msg := strings.TrimSpace(string(out))
	if len(msg) > 2000 {
		msg = "...(truncated)...\n" + msg[len(msg)-2000:]
	}
	return false, msg
}
