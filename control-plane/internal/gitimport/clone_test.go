package gitimport

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const aToken = "ghp_TOKEN_must_never_appear_in_argv_or_env_0123456789"

func TestValidateRepoURL(t *testing.T) {
	ok := []string{"https://github.com/org/repo.git", "https://gitlab.com/a/b"}
	bad := []string{
		"", "http://github.com/o/r", "ssh://git@github.com/o/r", "git@github.com:o/r.git",
		"file:///etc/passwd", "https://user:pass@github.com/o/r", "https://", "https://x/ r",
		"https://x/\nrepo",
	}
	for _, u := range ok {
		if err := ValidateRepoURL(u); err != nil {
			t.Errorf("ValidateRepoURL(%q) = %v; want ok", u, err)
		}
	}
	for _, u := range bad {
		if ValidateRepoURL(u) == nil {
			t.Errorf("ValidateRepoURL(%q) = nil; want error", u)
		}
	}
}

func TestValidateBranch(t *testing.T) {
	ok := []string{"main", "release/v0.4", "feature_x", "v1.2.3"}
	bad := []string{"", "-x", "--upload-pack=evil", "a..b", "a b", "a\nb", "a;rm -rf", strings.Repeat("x", 300)}
	for _, b := range ok {
		if err := ValidateBranch(b); err != nil {
			t.Errorf("ValidateBranch(%q) = %v; want ok", b, err)
		}
	}
	for _, b := range bad {
		if ValidateBranch(b) == nil {
			t.Errorf("ValidateBranch(%q) = nil; want error", b)
		}
	}
}

// fakeGit installs a recording fake `git` and returns the paths where it logs
// argv, the secret-bearing env subset, and the password the askpass returned.
func fakeGit(t *testing.T) (argvFile, envFile, passFile string) {
	t.Helper()
	dir := t.TempDir()
	argvFile = filepath.Join(dir, "argv")
	envFile = filepath.Join(dir, "env")
	passFile = filepath.Join(dir, "pass")
	script := "#!/bin/sh\n" +
		"echo \"$@\" >> '" + argvFile + "'\n" +
		"env >> '" + envFile + "'\n" +
		// prove the askpass mechanism actually yields the token from the file
		"if [ \"$1\" = clone ]; then \"$GIT_ASKPASS\" \"Password for 'https://x':\" > '" + passFile + "'; fi\n" +
		// create the dest dir (last arg) so the follow-up `-C dest` step is valid
		"for a in \"$@\"; do dest=\"$a\"; done\n" +
		"mkdir -p \"$dest/.git\" 2>/dev/null || true\n" +
		"exit 0\n"
	fake := filepath.Join(dir, "git")
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	old := gitBin
	gitBin = fake
	t.Cleanup(func() { gitBin = old })
	return argvFile, envFile, passFile
}

func TestCloneTokenNeverInArgvOrEnv_ReachesGitViaAskpass(t *testing.T) {
	argvFile, envFile, passFile := fakeGit(t)
	dest := filepath.Join(t.TempDir(), "app")

	err := Clone(context.Background(), Spec{
		RepoURL:  "https://github.com/org/repo.git",
		Branch:   "main",
		Username: "x-access-token",
		Token:    aToken,
		DestDir:  dest,
	})
	if err != nil {
		t.Fatalf("clone: %v", err)
	}
	argv, _ := os.ReadFile(argvFile)
	env, _ := os.ReadFile(envFile)
	pass, _ := os.ReadFile(passFile)

	// (sec) the token is NOT in argv and NOT in the git environment
	if strings.Contains(string(argv), aToken) {
		t.Fatal("token leaked into git argv")
	}
	if strings.Contains(string(env), aToken) {
		t.Fatal("token leaked into git environment")
	}
	// the clone used a TOKENLESS url + the required flags
	a := string(argv)
	for _, want := range []string{"clone", "--depth=1", "--single-branch", "--no-recurse-submodules", "--branch main", "https://github.com/org/repo.git", dest} {
		if !strings.Contains(a, want) {
			t.Errorf("argv missing %q; got %q", want, a)
		}
	}
	// askpass is wired and only the token FILE PATH is in env (not the token)
	if !strings.Contains(string(env), "GIT_ASKPASS=") || !strings.Contains(string(env), "GIT_IMPORT_TOKEN_FILE=") {
		t.Error("askpass env not wired")
	}
	// (sec) but the token DOES reach git through the askpass file
	if strings.TrimSpace(string(pass)) != aToken {
		t.Fatalf("askpass did not deliver the token: %q", string(pass))
	}
}

func TestCloneRejectsBadInput(t *testing.T) {
	fakeGit(t)
	bad := []Spec{
		{RepoURL: "http://x/r", Branch: "main", Token: "t", DestDir: "/tmp/a"},
		{RepoURL: "https://github.com/o/r", Branch: "-evil", Token: "t", DestDir: "/tmp/a"},
		{RepoURL: "https://github.com/o/r", Branch: "main", Token: "t", DestDir: "relative"},
	}
	for i, sp := range bad {
		if err := Clone(context.Background(), sp); err == nil {
			t.Errorf("case %d: expected error", i)
		}
	}
}

// A missing token is VALID: public repos/starters clone tokenless (no askpass).
func TestCloneTokenlessPublic(t *testing.T) {
	fakeGit(t)
	dest := filepath.Join(t.TempDir(), "app")
	if err := Clone(context.Background(), Spec{
		RepoURL: "https://github.com/o/r", Branch: "main", Token: "", DestDir: dest,
	}); err != nil {
		t.Fatalf("tokenless clone should succeed on valid input: %v", err)
	}
}
