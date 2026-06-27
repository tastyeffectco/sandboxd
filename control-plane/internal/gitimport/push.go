// push.go — Git B2: host-side, control-plane-only PUSH of locally-committed
// workspace changes to the app's remote. Like clone (A1) the token reaches git
// ONLY via a GIT_ASKPASS helper reading a 0600 file — but the push reads an
// agent-controlled .git, so it adds defenses clone didn't need:
//
//   - HOST-CHECKING askpass: the token is emitted ONLY when git's credential
//     prompt resolves to the EXACT expected host (parsed from app.git_repo_url);
//     any mismatch or unparsable prompt emits nothing → token never leaves.
//   - the push target is the validated app repo URL passed on argv, NEVER the
//     repo's .git/config origin.
//   - AuditConfig refuses dangerous repo-local config (insteadOf / sshCommand /
//     fsmonitor / hooksPath / proxy) BEFORE any network op.
//   - hardened invocation: hooks off, ext/file protocols off, GIT_ALLOW_PROTOCOL
//     =https, clean HOME/config, no credential.helper, argv-only.
//
// Scope (B2 MVP): push HEAD to a NEW branch, no force, no fetch/pull, no deepen
// (a shallow-update rejection returns shallow_push_unsupported — B2.1 later).
package gitimport

import (
	"context"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// HostOf returns the bare hostname (no port) of a validated https repo URL.
func HostOf(repoURL string) string {
	u, err := url.Parse(repoURL)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

// askpassScript is host-checking and CONSERVATIVE: it parses the host out of
// git's prompt and emits the username/token ONLY on an exact match. If the host
// can't be parsed, or the prompt isn't a recognized Username/Password prompt, it
// emits nothing (the token never leaves for an unexpected/rewritten host).
const askpassScript = `#!/bin/sh
p="$1"
host=$(printf '%s' "$p" | sed -n "s|^[^']*'[a-zA-Z][a-zA-Z0-9+.-]*://\([^@'/]*@\)\{0,1\}\([^/:']*\).*|\2|p")
[ -n "$host" ] || exit 0
[ "$host" = "$GIT_EXPECT_HOST" ] || exit 0
case "$p" in
  Username\ *) printf '%s' "$GIT_PUSH_USER" ;;
  Password\ *) cat "$GIT_PUSH_TOKEN_FILE" ;;
  *) exit 0 ;;
esac
`

// Runner runs the host-side git read + push operations over real git.
type Runner struct{}

// roEnv strips inherited GIT_*/HOME, neutralizes system/global config, and
// restricts git to https only.
func roEnv() []string {
	var env []string
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "GIT_") || strings.HasPrefix(e, "HOME=") {
			continue
		}
		env = append(env, e)
	}
	return append(env,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"HOME=/nonexistent",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_ALLOW_PROTOCOL=https",
	)
}

// roArgs is the hardened `git -C <dir> ...` prefix (safe for read-only + push).
func roArgs(appDir string, extra ...string) []string {
	base := []string{"-C", appDir,
		"-c", "safe.directory=*",
		"-c", "core.fsmonitor=",
		"-c", "protocol.ext.allow=never",
		"-c", "protocol.file.allow=never",
		"-c", "credential.helper=",
	}
	return append(base, extra...)
}

func runGit(ctx context.Context, env []string, args ...string) (out string, code int, err error) {
	cctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(cctx, gitBin, args...)
	cmd.Env = env
	b, e := cmd.CombinedOutput()
	if e == nil {
		return string(b), 0, nil
	}
	if ee, ok := e.(*exec.ExitError); ok {
		return string(b), ee.ExitCode(), nil
	}
	return string(b), -1, e
}

// dangerousConfigKey reports a repo-local config key we refuse to push with.
func dangerousConfigKey(key, val string) bool {
	k := strings.ToLower(key)
	switch {
	case strings.HasPrefix(k, "url.") && (strings.HasSuffix(k, ".insteadof") || strings.HasSuffix(k, ".pushinsteadof")):
		return true
	case k == "core.sshcommand", k == "core.hookspath":
		return true
	case k == "core.fsmonitor":
		return val != "" // empty (disabled) is fine
	case k == "http.proxy", strings.HasPrefix(k, "http.") && strings.HasSuffix(k, ".proxy"):
		return true
	}
	return false
}

// AuditConfig returns the dangerous repo-local config keys present (empty = safe).
// Read-only — never strips or rewrites.
func (Runner) AuditConfig(ctx context.Context, appDir string) ([]string, error) {
	out, code, err := runGit(ctx, roEnv(), roArgs(appDir, "config", "--local", "--list", "-z")...)
	if err != nil {
		return nil, err
	}
	if code != 0 {
		return nil, nil // no local config / not a repo (caught elsewhere)
	}
	var flagged []string
	for _, rec := range strings.Split(out, "\x00") {
		if rec == "" {
			continue
		}
		key, val := rec, ""
		if i := strings.IndexByte(rec, '\n'); i >= 0 {
			key, val = rec[:i], rec[i+1:]
		}
		if dangerousConfigKey(key, val) {
			flagged = append(flagged, key)
		}
	}
	return flagged, nil
}

// RepoState is the local git state the push pre-flight needs.
type RepoState struct {
	IsRepo        bool
	HasHEAD       bool
	Detached      bool
	IsShallow     bool
	CurrentBranch string
	HeadShort     string
}

func (Runner) RepoState(ctx context.Context, appDir string) (RepoState, error) {
	var st RepoState
	out, code, err := runGit(ctx, roEnv(), roArgs(appDir, "rev-parse", "--is-inside-work-tree")...)
	if err != nil {
		return st, err
	}
	if code != 0 || strings.TrimSpace(out) != "true" {
		return st, nil // not a repo
	}
	st.IsRepo = true
	if _, code, _ := runGit(ctx, roEnv(), roArgs(appDir, "rev-parse", "--verify", "HEAD")...); code == 0 {
		st.HasHEAD = true
	}
	if o, code, _ := runGit(ctx, roEnv(), roArgs(appDir, "rev-parse", "--abbrev-ref", "HEAD")...); code == 0 {
		if b := strings.TrimSpace(o); b == "HEAD" {
			st.Detached = true
		} else {
			st.CurrentBranch = b
		}
	}
	if o, code, _ := runGit(ctx, roEnv(), roArgs(appDir, "rev-parse", "--short", "HEAD")...); code == 0 {
		st.HeadShort = strings.TrimSpace(o)
	}
	if o, code, _ := runGit(ctx, roEnv(), roArgs(appDir, "rev-parse", "--is-shallow-repository")...); code == 0 {
		st.IsShallow = strings.TrimSpace(o) == "true"
	}
	return st, nil
}

// Unpushed counts commits on HEAD beyond the import baseline
// (refs/remotes/origin/<importBranch>). baselineKnown is false when that ref is
// absent (reset .git / fork) — the caller then treats HEAD as unpushed work.
func (Runner) Unpushed(ctx context.Context, appDir, importBranch string) (count int, baselineKnown bool, err error) {
	baseline := "refs/remotes/origin/" + importBranch
	if _, code, _ := runGit(ctx, roEnv(), roArgs(appDir, "rev-parse", "--verify", baseline)...); code != 0 {
		out, code2, e := runGit(ctx, roEnv(), roArgs(appDir, "rev-list", "--count", "HEAD")...)
		if e != nil || code2 != 0 {
			return 0, false, e
		}
		return atoiTrim(out), false, nil
	}
	out, code, e := runGit(ctx, roEnv(), roArgs(appDir, "rev-list", "--count", baseline+"..HEAD")...)
	if e != nil || code != 0 {
		return 0, true, e
	}
	return atoiTrim(out), true, nil
}

func atoiTrim(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

// PushSpec is one push request. Token is the DECRYPTED PAT (never logged).
type PushSpec struct {
	RepoURL    string // validated https, tokenless
	Target     string // NEW branch name (validated ref)
	Username   string // credential username; defaults x-access-token
	Token      string // decrypted PAT
	AppDir     string // host workspace path
	ExpectHost string // host the askpass will release the token to
}

// PushOutcome is the classified result. Output is sanitized (token-redacted) and
// must not be logged by default (it can echo repo content).
type PushOutcome struct {
	OK     bool
	Reason string // branch_exists|non_fast_forward|auth_failed|shallow_push_unsupported|push_failed
	Output string
}

// Push pushes HEAD to refs/heads/<Target> at the validated URL, tokenless, with
// the host-checking askpass. No --force, no deepen.
func (Runner) Push(ctx context.Context, spec PushSpec) PushOutcome {
	if ValidateRepoURL(spec.RepoURL) != nil || ValidateBranch(spec.Target) != nil ||
		spec.Token == "" || spec.ExpectHost == "" {
		return PushOutcome{Reason: "push_failed"}
	}
	username := spec.Username
	if username == "" {
		username = "x-access-token"
	}
	tmp, err := os.MkdirTemp("", "gitpush-")
	if err != nil {
		return PushOutcome{Reason: "push_failed"}
	}
	defer os.RemoveAll(tmp)
	tokenFile := tmp + "/token"
	if err := os.WriteFile(tokenFile, []byte(spec.Token), 0o600); err != nil {
		return PushOutcome{Reason: "push_failed"}
	}
	hooksDir := tmp + "/hooks" // empty dir => hooks disabled
	if err := os.Mkdir(hooksDir, 0o700); err != nil {
		return PushOutcome{Reason: "push_failed"}
	}
	askpass := tmp + "/askpass.sh"
	if err := os.WriteFile(askpass, []byte(askpassScript), 0o700); err != nil {
		return PushOutcome{Reason: "push_failed"}
	}
	env := append(roEnv(),
		"GIT_ASKPASS="+askpass,
		"GIT_PUSH_USER="+username,          // not secret
		"GIT_PUSH_TOKEN_FILE="+tokenFile,   // the PATH, never the token
		"GIT_EXPECT_HOST="+spec.ExpectHost, // exact-host gate in askpass
	)
	args := roArgs(spec.AppDir, "-c", "core.hooksPath="+hooksDir,
		"push", spec.RepoURL, "HEAD:refs/heads/"+spec.Target)
	out, code, runErr := runGit(ctx, env, args...)
	msg := sanitize(out, spec.Token)
	if runErr == nil && code == 0 {
		return PushOutcome{OK: true, Output: msg}
	}
	return PushOutcome{Reason: classifyPushErr(out), Output: msg}
}

func classifyPushErr(out string) string {
	o := strings.ToLower(out)
	switch {
	case strings.Contains(o, "shallow update not allowed"):
		return "shallow_push_unsupported"
	case strings.Contains(o, "already exists"):
		return "branch_exists"
	case strings.Contains(o, "non-fast-forward"), strings.Contains(o, "fetch first"):
		return "non_fast_forward"
	case strings.Contains(o, "authentication failed"),
		strings.Contains(o, "could not read username"),
		strings.Contains(o, "could not read password"),
		strings.Contains(o, "403"):
		return "auth_failed"
	}
	return "push_failed"
}
