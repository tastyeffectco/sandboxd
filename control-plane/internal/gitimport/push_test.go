package gitimport

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runAskpass writes the real askpass script and runs it with a given prompt +
// expected host, returning what it would feed git.
func runAskpass(t *testing.T, prompt, expectHost string) string {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "askpass.sh")
	tokenFile := filepath.Join(dir, "token")
	if err := os.WriteFile(script, []byte(askpassScript), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tokenFile, []byte("THE_TOKEN"), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("/bin/sh", script, prompt)
	cmd.Env = []string{"GIT_EXPECT_HOST=" + expectHost, "GIT_PUSH_USER=x-access-token", "GIT_PUSH_TOKEN_FILE=" + tokenFile}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("askpass run: %v", err)
	}
	return string(out)
}

func TestAskpassHostCheck(t *testing.T) {
	cases := []struct {
		prompt, expectHost, want string
	}{
		// realistic GitHub prompts — exact host match emits the secret
		{"Username for 'https://github.com':", "github.com", "x-access-token"},
		{"Password for 'https://user@github.com':", "github.com", "THE_TOKEN"},
		{"Password for 'https://github.com':", "github.com", "THE_TOKEN"},
		{"Password for 'https://github.com:443':", "github.com", "THE_TOKEN"}, // port stripped
		// hostile / mismatched hosts emit NOTHING
		{"Password for 'https://github.com.evil.com':", "github.com", ""},
		{"Password for 'https://evilgithub.com':", "github.com", ""},
		{"Password for 'https://github.com@evil.com':", "github.com", ""}, // userinfo trick -> host is evil.com
		{"Password for 'https://evil.com':", "github.com", ""},
		// malformed / unknown prompts emit NOTHING
		{"Password: ", "github.com", ""},
		{"enter your password please", "github.com", ""},
		{"", "github.com", ""},
	}
	for _, c := range cases {
		got := runAskpass(t, c.prompt, c.expectHost)
		if got != c.want {
			t.Errorf("prompt %q (expect %s): got %q want %q", c.prompt, c.expectHost, got, c.want)
		}
	}
}

func TestAuditConfig(t *testing.T) {
	repo := t.TempDir()
	mustGit := func(args ...string) {
		t.Helper()
		c := exec.Command("git", append([]string{"-C", repo}, args...)...)
		c.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "HOME="+repo)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	mustGit("init", "-q")
	// clean repo -> no flags
	if f, err := (Runner{}).AuditConfig(context.Background(), repo); err != nil || len(f) != 0 {
		t.Fatalf("clean repo flagged %v (err %v)", f, err)
	}
	// add dangerous keys
	mustGit("config", "url.https://evil.tld/.insteadOf", "https://github.com/")
	mustGit("config", "core.sshCommand", "ssh -i /tmp/x")
	mustGit("config", "core.fsmonitor", "true")
	mustGit("config", "core.hooksPath", "/tmp/hooks")
	mustGit("config", "http.proxy", "http://evil:8080")
	f, err := (Runner{}).AuditConfig(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.ToLower(strings.Join(f, " "))
	for _, want := range []string{"insteadof", "core.sshcommand", "core.fsmonitor", "core.hookspath", "http.proxy"} {
		if !strings.Contains(joined, want) {
			t.Errorf("audit missed %q; flagged=%v", want, f)
		}
	}
}

func TestAuditConfigEmptyFsmonitorOK(t *testing.T) {
	repo := t.TempDir()
	c := exec.Command("git", "-C", repo, "init", "-q")
	c.Env = append(os.Environ(), "HOME="+repo)
	c.Run()
	c2 := exec.Command("git", "-C", repo, "config", "core.fsmonitor", "")
	c2.Env = append(os.Environ(), "HOME="+repo)
	c2.Run()
	if f, _ := (Runner{}).AuditConfig(context.Background(), repo); len(f) != 0 {
		t.Errorf("empty fsmonitor should not be flagged: %v", f)
	}
}

// fakeGit records argv + env and exits 0, so we can assert the token never
// appears in the push command line or environment.
func TestPushTokenNotInArgvOrEnv(t *testing.T) {
	dir := t.TempDir()
	argvF := filepath.Join(dir, "argv")
	envF := filepath.Join(dir, "env")
	fake := filepath.Join(dir, "git")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + argvF + "\nenv > " + envF + "\nexit 0\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	old := gitBin
	gitBin = fake
	defer func() { gitBin = old }()

	const token = "ghp_SUPERSECRETTOKEN"
	out := (Runner{}).Push(context.Background(), PushSpec{
		RepoURL: "https://github.com/o/r", Target: "sandboxd/x-abc",
		Token: token, AppDir: dir, ExpectHost: "github.com",
	})
	if !out.OK {
		t.Fatalf("fake push should succeed: %+v", out)
	}
	argv, _ := os.ReadFile(argvF)
	env, _ := os.ReadFile(envF)
	if strings.Contains(string(argv), token) || strings.Contains(string(env), token) {
		t.Fatal("TOKEN LEAK: token found in push argv or env")
	}
	a := string(argv)
	for _, want := range []string{"push", "https://github.com/o/r", "HEAD:refs/heads/sandboxd/x-abc",
		"protocol.ext.allow=never", "protocol.file.allow=never", "core.hooksPath="} {
		if !strings.Contains(a, want) {
			t.Errorf("push argv missing %q:\n%s", want, a)
		}
	}
	if strings.Contains(a, "--force") || strings.Contains(a, "-f\n") {
		t.Error("push must never use --force")
	}
	e := string(env)
	for _, want := range []string{"GIT_ALLOW_PROTOCOL=https", "GIT_ASKPASS=", "GIT_PUSH_TOKEN_FILE=", "GIT_EXPECT_HOST=github.com"} {
		if !strings.Contains(e, want) {
			t.Errorf("push env missing %q", want)
		}
	}
}

func TestClassifyPushErr(t *testing.T) {
	cases := map[string]string{
		"! [remote rejected] (shallow update not allowed)":   "shallow_push_unsupported",
		"error: failed to push some refs ... already exists": "branch_exists",
		"Updates were rejected ... (non-fast-forward)":       "non_fast_forward",
		"fatal: Authentication failed for 'https://...'":     "auth_failed",
		"fatal: unable to access ... 403":                    "auth_failed",
		"some other failure":                                 "push_failed",
	}
	for in, want := range cases {
		if got := classifyPushErr(in); got != want {
			t.Errorf("classify(%q)=%q want %q", in, got, want)
		}
	}
}

func TestHostOf(t *testing.T) {
	if HostOf("https://github.com/o/r") != "github.com" {
		t.Error("HostOf github")
	}
	if HostOf("https://github.com:443/o/r") != "github.com" {
		t.Error("HostOf strips port")
	}
}
