// Package gitimport performs host-side, control-plane-only clones of private
// HTTPS Git repos into a sandbox workspace. The access token is delivered to
// git ONLY via a GIT_ASKPASS helper that reads it from a 0600 temp file — so the
// token never appears in argv, in the git process environment, in .git/config,
// or anywhere the sandbox can see. The cloned remote is rewritten tokenless.
//
// Scope (Git A1): HTTPS only, --depth=1, --no-recurse-submodules, a single
// validated branch. No SSH, no submodule creds, no push.
package gitimport

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Spec is a single clone request. Token is the DECRYPTED PAT — the caller
// decrypts it (owner-scoped) and never logs it; gitimport keeps it only in a
// 0600 temp file for the lifetime of the clone.
type Spec struct {
	RepoURL  string // validated HTTPS, tokenless
	Branch   string // validated ref name
	Username string // optional; defaults to x-access-token
	Token    string // decrypted PAT (never logged)
	DestDir  string // control-plane-derived host path to clone INTO (never user input)
}

var (
	// branch/ref: letters, digits, and ._-/ ; no leading '-', no '..', no control chars.
	reBranch = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,254}$`)
	gitBin   = "git" // overridable in tests
)

// ValidateRepoURL accepts ONLY a plain https:// URL with a host and no embedded
// credentials. Everything else (http, ssh, git, file, scp-style, userinfo) is
// rejected.
func ValidateRepoURL(raw string) error {
	if raw == "" || len(raw) > 2048 || strings.ContainsAny(raw, " \t\r\n\x00") {
		return fmt.Errorf("invalid repo_url")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid repo_url")
	}
	if u.Scheme != "https" {
		return fmt.Errorf("repo_url must be https://")
	}
	if u.Host == "" {
		return fmt.Errorf("repo_url has no host")
	}
	if u.User != nil {
		return fmt.Errorf("repo_url must not contain credentials")
	}
	return nil
}

// ValidateBranch enforces a safe ref name (also prevents argv option injection).
func ValidateBranch(b string) error {
	if !reBranch.MatchString(b) || strings.Contains(b, "..") {
		return fmt.Errorf("invalid branch")
	}
	return nil
}

// Clone fetches Spec.RepoURL@Branch into Spec.DestDir using a tokenless URL +
// GIT_ASKPASS. On success the origin remote is tokenless and .git/config holds
// no secret. Returns an error whose message is SAFE to log/emit (never the token).
func Clone(ctx context.Context, spec Spec) error {
	if err := ValidateRepoURL(spec.RepoURL); err != nil {
		return err
	}
	if err := ValidateBranch(spec.Branch); err != nil {
		return err
	}
	if spec.Token == "" {
		return fmt.Errorf("missing token")
	}
	if !filepath.IsAbs(spec.DestDir) {
		return fmt.Errorf("dest must be absolute")
	}
	username := spec.Username
	if username == "" {
		username = "x-access-token"
	}

	// Token + askpass live in a 0700 control-plane-only temp dir, removed after.
	tmp, err := os.MkdirTemp("", "gitimport-")
	if err != nil {
		return fmt.Errorf("scratch dir: %w", err)
	}
	defer os.RemoveAll(tmp)
	tokenFile := filepath.Join(tmp, "token")
	if err := os.WriteFile(tokenFile, []byte(spec.Token), 0o600); err != nil {
		return fmt.Errorf("write token: %w", err)
	}
	askpass := filepath.Join(tmp, "askpass.sh")
	// $1 is git's prompt ("Username for ..." / "Password for ..."). Username is
	// not secret (returned inline); the token is only ever read from the file.
	script := "#!/bin/sh\ncase \"$1\" in\n  Username*) printf %s \"$GIT_IMPORT_USER\" ;;\n  *) cat \"$GIT_IMPORT_TOKEN_FILE\" ;;\nesac\n"
	if err := os.WriteFile(askpass, []byte(script), 0o700); err != nil {
		return fmt.Errorf("write askpass: %w", err)
	}

	env := append(os.Environ(),
		"GIT_ASKPASS="+askpass,
		"GIT_TERMINAL_PROMPT=0",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_IMPORT_USER="+username,        // not secret
		"GIT_IMPORT_TOKEN_FILE="+tokenFile, // the PATH, never the token
		"GIT_LFS_SKIP_SMUDGE=1",
	)

	cctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	// URL is TOKENLESS; creds come from askpass. Args are exec argv (no shell).
	clone := exec.CommandContext(cctx, gitBin, "clone",
		"--depth=1", "--single-branch", "--no-recurse-submodules",
		"--branch", spec.Branch, spec.RepoURL, spec.DestDir)
	clone.Env = env
	if out, err := clone.CombinedOutput(); err != nil {
		return fmt.Errorf("git clone failed: %s", sanitize(string(out), spec.Token))
	}

	// Belt-and-suspenders: ensure the persisted remote is tokenless.
	setURL := exec.CommandContext(cctx, gitBin, "-C", spec.DestDir, "remote", "set-url", "origin", spec.RepoURL)
	setURL.Env = env
	if out, err := setURL.CombinedOutput(); err != nil {
		return fmt.Errorf("git remote set-url failed: %s", sanitize(string(out), spec.Token))
	}
	return nil
}

// sanitize removes any accidental token occurrence from a message before it is
// logged or returned (defense in depth; the token should never be in git output).
func sanitize(msg, token string) string {
	if token != "" {
		msg = strings.ReplaceAll(msg, token, "[redacted]")
	}
	return strings.TrimSpace(msg)
}
