// v1_git.go — A2: read-only Git status/diff for an imported app.
//
// Execution model: git runs IN THE SANDBOX via `docker exec` (uid 1000, locked
// container, NO network), never host-side — because the workspace .git /
// .gitattributes are agent-controlled and `git status`/`diff` can execute
// configured programs (core.fsmonitor, diff textconv/external drivers); keeping
// that inside the sandbox stays within the existing trust boundary. The
// invocation is hardened (-c core.fsmonitor=, --no-ext-diff --no-textconv,
// -c safe.directory=*) and argv-only (no shell). No token, no credential
// lookup, no network, no fetch/pull/commit. Diffs are NOT logged (they can
// contain secrets).
package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/sandboxd/control-plane/internal/docker"
	"github.com/sandboxd/control-plane/internal/store"
)

// sandboxAppDir is the FIXED in-container path of the app workspace (the mnt is
// bind-mounted at /home/sandbox). Never derived from user input.
const sandboxAppDir = "/home/sandbox/workspace/app"

// maxDiffBytes caps the diff response; output beyond it is dropped + truncated=true.
const maxDiffBytes = 256 << 10 // 256 KiB

// sandboxExecer is the slice of *docker.Client the git handlers need (argv exec
// inside the sandbox). Declared as an interface so the logic is unit-testable.
type sandboxExecer interface {
	Exec(ctx context.Context, name string, cmd []string) (docker.ExecResult, error)
}

type v1GitFile struct {
	Path   string `json:"path"`
	Status string `json:"status"` // modified|added|deleted|renamed|copied|untracked|unmerged
	Staged bool   `json:"staged"`
}

type v1GitStatusResp struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"` // no_sandbox|sandbox_not_running|not_a_git_repo|git_error|exec_failed
	Branch    string `json:"branch,omitempty"`
	HeadSHA   string `json:"head_sha,omitempty"`
	// Clean is the RAW repo state (no changes at all). UserClean ignores known
	// sandboxd/runtime-generated files (sandbox.yaml, lockfiles, framework caches)
	// — so a pristine import whose only "changes" are runtime artifacts reports
	// user_clean=true while clean stays truthfully false.
	Clean     bool `json:"clean"`
	UserClean bool `json:"user_clean"`
	Ahead     *int `json:"ahead"`  // null unless an upstream is locally known (no network)
	Behind    *int `json:"behind"` // null unless an upstream is locally known
	// Files are user/repo changes; RuntimeFiles are known runtime-generated ones.
	// Their union is the raw working-tree change set (status stays truthful).
	Files        []v1GitFile `json:"files"`
	RuntimeFiles []v1GitFile `json:"runtime_files"`
}

type v1GitDiffResp struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
	Diff      string `json:"diff,omitempty"`
	Truncated bool   `json:"truncated"`
}

// gitArgs builds the hardened `git -C <appdir> ...` argv.
func gitArgs(args ...string) []string {
	base := []string{
		"git", "-C", sandboxAppDir,
		"-c", "safe.directory=*", // workspace is uid 1000; defensive
		"-c", "core.fsmonitor=", // never run a configured fsmonitor program
	}
	return append(base, args...)
}

// --- handlers ---------------------------------------------------------

// gitSandbox enforces owner scope + a running sandbox. Three outcomes:
//   - handled=true  -> a 404/500 was already written; the caller must return.
//   - reason!=""    -> not ready (no_sandbox|sandbox_not_running); the caller
//     renders its own shape (available:false for read; committed:false for commit).
//   - else          -> sb is the running sandbox.
func (s *Server) gitSandbox(w http.ResponseWriter, r *http.Request) (sb *store.Sandbox, reason string, handled bool) {
	app, err := s.Store.GetAppForOwner(r.Context(), r.PathValue("id"), tenantToken(r))
	if errors.Is(err, store.ErrNotFound) {
		writeV1Err(w, http.StatusNotFound, "not_found", "no such app")
		return nil, "", true
	}
	if err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", "lookup failed")
		return nil, "", true
	}
	cur, err := s.Store.CurrentSandboxForApp(r.Context(), app.ID)
	if err != nil {
		return nil, "no_sandbox", false
	}
	if cur.Status != "running" {
		return nil, "sandbox_not_running", false
	}
	return cur, "", false
}

// GET /v1/apps/{id}/git/status
func (s *Server) v1GitStatus(w http.ResponseWriter, r *http.Request) {
	sb, reason, handled := s.gitSandbox(w, r)
	if handled {
		return
	}
	if reason != "" {
		writeJSON(w, http.StatusOK, map[string]any{"available": false, "reason": reason})
		return
	}
	writeJSON(w, http.StatusOK, gitStatus(r.Context(), s.Docker, sb.ID))
}

// GET /v1/apps/{id}/git/diff?path=<optional relative path>
func (s *Server) v1GitDiff(w http.ResponseWriter, r *http.Request) {
	sb, reason, handled := s.gitSandbox(w, r)
	if handled {
		return
	}
	if reason != "" {
		writeJSON(w, http.StatusOK, map[string]any{"available": false, "reason": reason})
		return
	}
	path := r.URL.Query().Get("path")
	if !validGitPath(path) {
		writeV1Err(w, http.StatusBadRequest, "invalid_request", "invalid path filter (no absolute paths or ..)")
		return
	}
	if path != "" {
		path = filepath.Clean(path)
	}
	// NOTE: do not log the diff body — it can contain secrets.
	writeJSON(w, http.StatusOK, gitDiff(r.Context(), s.Docker, sb.ID, path))
}

// --- commit (B1) ------------------------------------------------------

const (
	maxCommitMessageBytes = 4 << 10 // 4 KiB
	maxCommitPaths        = 1000    // bound argv length
)

type v1GitCommitReq struct {
	Message      string   `json:"message"`
	Paths        []string `json:"paths"`         // default = current user files[]
	RuntimePaths []string `json:"runtime_paths"` // explicit runtime/platform files to include
	AuthorName   string   `json:"author_name"`
	AuthorEmail  string   `json:"author_email"`
}

type v1GitCommitResp struct {
	Committed      bool     `json:"committed"`
	Reason         string   `json:"reason,omitempty"` // no_changes|sandbox_not_running|not_a_git_repo|empty_repo_unsupported|git_error|exec_failed
	SHA            string   `json:"sha,omitempty"`
	Branch         string   `json:"branch,omitempty"`
	FilesCommitted []string `json:"files_committed,omitempty"`
}

// POST /v1/apps/{id}/git/commit
func (s *Server) v1GitCommit(w http.ResponseWriter, r *http.Request) {
	sb, reason, handled := s.gitSandbox(w, r)
	if handled {
		return
	}
	if reason != "" {
		writeJSON(w, http.StatusOK, v1GitCommitResp{Committed: false, Reason: reason})
		return
	}
	var req v1GitCommitReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeV1Err(w, http.StatusBadRequest, "invalid_request", "invalid json: "+err.Error())
		return
	}
	if msg, ok := validCommitMessage(req.Message); !ok {
		writeV1Err(w, http.StatusBadRequest, "invalid_request", "message is required and must be <4KiB with no NUL")
		return
	} else {
		req.Message = msg
	}
	if !validAuthorField(req.AuthorName) || !validAuthorField(req.AuthorEmail) {
		writeV1Err(w, http.StatusBadRequest, "invalid_request", "invalid author name/email")
		return
	}
	for _, p := range append(append([]string{}, req.Paths...), req.RuntimePaths...) {
		if !validGitPath(p) || p == "" {
			writeV1Err(w, http.StatusBadRequest, "invalid_request", "invalid path (no absolute paths, .., or empty)")
			return
		}
	}
	if len(req.Paths)+len(req.RuntimePaths) > maxCommitPaths {
		writeV1Err(w, http.StatusBadRequest, "invalid_request", "too many paths; select a subset")
		return
	}
	// NOTE: do not log the message or path contents — they can carry secrets.
	writeJSON(w, http.StatusOK, gitCommit(r.Context(), s.Docker, sb.ID, req))
}

// gitCommit stages exactly the selected (and actually-changed) paths and makes a
// path-scoped commit — never more than selected, never `git add -A`. Creds-free,
// --no-verify, ephemeral author via -c (never written to .git/config).
func gitCommit(ctx context.Context, ex sandboxExecer, sbID string, req v1GitCommitReq) v1GitCommitResp {
	// 1. Resolve the actual change set (and the default user selection).
	st := gitStatus(ctx, ex, sbID)
	if !st.Available {
		return v1GitCommitResp{Committed: false, Reason: st.Reason}
	}
	// B1 does not support an empty repo (no HEAD) — a path-scoped partial commit
	// needs HEAD, and we must never widen to commit more than selected.
	if h, err := ex.Exec(ctx, "s-"+sbID, gitArgs("rev-parse", "--verify", "HEAD")); err != nil || h.ExitCode != 0 {
		return v1GitCommitResp{Committed: false, Reason: "empty_repo_unsupported"}
	}

	changed := map[string]bool{}
	for _, f := range st.Files {
		changed[f.Path] = true
	}
	for _, f := range st.RuntimeFiles {
		changed[f.Path] = true
	}

	// 2. Requested selection: explicit paths, or default to all user files; plus
	//    explicitly-opted-in runtime paths. Intersect with the real change set.
	requested := req.Paths
	if len(requested) == 0 {
		for _, f := range st.Files { // default: user changes only
			requested = append(requested, f.Path)
		}
	}
	requested = append(requested, req.RuntimePaths...)

	seen := map[string]bool{}
	var toStage []string
	for _, p := range requested {
		if changed[p] && !seen[p] {
			seen[p] = true
			toStage = append(toStage, p)
		}
	}
	if len(toStage) == 0 {
		return v1GitCommitResp{Committed: false, Reason: "no_changes"}
	}

	// 3. Stage exactly toStage (explicit pathspecs; NEVER -A/.).
	addArgs := append(gitArgs("add", "--"), toStage...)
	if a, err := ex.Exec(ctx, "s-"+sbID, addArgs); err != nil || a.ExitCode != 0 {
		return v1GitCommitResp{Committed: false, Reason: gitErrReasonOf(a, err)}
	}

	// 4. Path-scoped commit: --no-verify, ephemeral author via -c, trailing
	//    `-- toStage` so any pre-staged agent changes are NOT swept in.
	name := req.AuthorName
	if name == "" {
		name = "sandbox-agent"
	}
	email := req.AuthorEmail
	if email == "" {
		email = "agent@sandboxd.local"
	}
	commitArgs := gitArgs("-c", "user.name="+name, "-c", "user.email="+email,
		"commit", "--no-verify", "-m", req.Message, "--")
	commitArgs = append(commitArgs, toStage...)
	if c, err := ex.Exec(ctx, "s-"+sbID, commitArgs); err != nil || c.ExitCode != 0 {
		return v1GitCommitResp{Committed: false, Reason: gitErrReasonOf(c, err)}
	}

	// 5. Resolve sha + branch.
	sha, branch := "", ""
	if o, err := ex.Exec(ctx, "s-"+sbID, gitArgs("rev-parse", "HEAD")); err == nil && o.ExitCode == 0 {
		sha = strings.TrimSpace(o.Stdout)
	}
	if o, err := ex.Exec(ctx, "s-"+sbID, gitArgs("rev-parse", "--abbrev-ref", "HEAD")); err == nil && o.ExitCode == 0 {
		branch = strings.TrimSpace(o.Stdout)
	}
	return v1GitCommitResp{Committed: true, SHA: sha, Branch: branch, FilesCommitted: toStage}
}

func gitErrReasonOf(res docker.ExecResult, err error) string {
	if err != nil {
		return "exec_failed"
	}
	return gitErrReason(res.Stderr)
}

// validCommitMessage trims, requires non-empty, rejects NUL, caps length.
func validCommitMessage(m string) (string, bool) {
	m = strings.TrimSpace(m)
	if m == "" || len(m) > maxCommitMessageBytes || strings.ContainsRune(m, 0) {
		return "", false
	}
	return m, true
}

// validAuthorField rejects NUL/CR/LF and over-long values (empty is allowed →
// the caller substitutes a default).
func validAuthorField(v string) bool {
	if len(v) > 256 {
		return false
	}
	return !strings.ContainsAny(v, "\x00\r\n")
}

// --- git logic (testable with a fake execer) --------------------------

func gitStatus(ctx context.Context, ex sandboxExecer, sbID string) v1GitStatusResp {
	st, err := ex.Exec(ctx, "s-"+sbID, gitArgs("status", "--porcelain=v1", "--branch", "-z"))
	if err != nil {
		return v1GitStatusResp{Available: false, Reason: "exec_failed", Files: []v1GitFile{}}
	}
	if st.ExitCode != 0 {
		return v1GitStatusResp{Available: false, Reason: gitErrReason(st.Stderr), Files: []v1GitFile{}}
	}
	branch, ahead, behind, allFiles := parsePorcelainZ(st.Stdout)
	user, runtime := splitRuntimeFiles(allFiles)
	head := ""
	if h, err := ex.Exec(ctx, "s-"+sbID, gitArgs("rev-parse", "HEAD")); err == nil && h.ExitCode == 0 {
		head = strings.TrimSpace(h.Stdout)
	}
	return v1GitStatusResp{
		Available: true, Branch: branch, HeadSHA: head,
		Clean: len(allFiles) == 0, UserClean: len(user) == 0,
		Ahead: ahead, Behind: behind,
		Files: user, RuntimeFiles: runtime,
	}
}

// runtimeExactFiles + runtimeDirPrefixes are files sandboxd or the runtime
// toolchain generates (NOT user edits): the injected manifest, install lockfiles,
// and framework build/cache dirs. Conservative on purpose — only well-known
// artifacts, so a real user change is never hidden.
var runtimeExactFiles = map[string]bool{
	"sandbox.yaml":      true, // injected by sandboxd / written by runtimed
	"pnpm-lock.yaml":    true,
	"package-lock.json": true,
	"yarn.lock":         true,
	"bun.lockb":         true,
}

var runtimeDirPrefixes = []string{
	"node_modules/", ".astro/", ".next/", ".svelte-kit/", ".turbo/", ".cache/", ".vite/",
}

func isRuntimeFile(p string) bool {
	if runtimeExactFiles[p] {
		return true
	}
	for _, pre := range runtimeDirPrefixes {
		if strings.HasPrefix(p, pre) {
			return true
		}
	}
	return false
}

// splitRuntimeFiles partitions the raw change set into user vs runtime-generated.
func splitRuntimeFiles(all []v1GitFile) (user, runtime []v1GitFile) {
	user, runtime = []v1GitFile{}, []v1GitFile{}
	for _, f := range all {
		if isRuntimeFile(f.Path) {
			runtime = append(runtime, f)
		} else {
			user = append(user, f)
		}
	}
	return user, runtime
}

func gitDiff(ctx context.Context, ex sandboxExecer, sbID, path string) v1GitDiffResp {
	// --no-ext-diff / --no-textconv are `git diff` options, NOT top-level git
	// options — they must come AFTER the `diff` subcommand or git errors out.
	args := []string{"diff", "--no-ext-diff", "--no-textconv", "HEAD"}
	if path != "" {
		args = append(args, "--", path)
	}
	d, err := ex.Exec(ctx, "s-"+sbID, gitArgs(args...))
	if err != nil {
		return v1GitDiffResp{Available: false, Reason: "exec_failed"}
	}
	if d.ExitCode != 0 {
		// An empty repo (no HEAD) is not an error for our purposes — no diff.
		if strings.Contains(d.Stderr, "unknown revision") || strings.Contains(d.Stderr, "bad revision") {
			return v1GitDiffResp{Available: true, Diff: "", Truncated: false}
		}
		return v1GitDiffResp{Available: false, Reason: gitErrReason(d.Stderr)}
	}
	body, trunc := capDiff(d.Stdout, maxDiffBytes)
	return v1GitDiffResp{Available: true, Diff: body, Truncated: trunc}
}

func gitErrReason(stderr string) string {
	if strings.Contains(stderr, "not a git repository") {
		return "not_a_git_repo"
	}
	return "git_error"
}

// --- pure parsing/validation ------------------------------------------

var aheadRe = regexp.MustCompile(`ahead (\d+)`)
var behindRe = regexp.MustCompile(`behind (\d+)`)

// parsePorcelainZ parses `git status --porcelain=v1 --branch -z` output.
func parsePorcelainZ(out string) (branch string, ahead, behind *int, files []v1GitFile) {
	parts := strings.Split(out, "\x00")
	files = []v1GitFile{}
	for i := 0; i < len(parts); i++ {
		e := parts[i]
		if e == "" {
			continue
		}
		if strings.HasPrefix(e, "## ") {
			branch, ahead, behind = parseBranchLine(e[3:])
			continue
		}
		if len(e) < 3 {
			continue
		}
		xy := e[:2]
		path := e[3:] // skip the separating space at index 2
		cat, staged := classifyXY(xy)
		files = append(files, v1GitFile{Path: path, Status: cat, Staged: staged})
		// With -z, a rename/copy emits the source path as the NEXT field.
		if xy[0] == 'R' || xy[0] == 'C' {
			i++
		}
	}
	return branch, ahead, behind, files
}

func parseBranchLine(s string) (branch string, ahead, behind *int) {
	if m := aheadRe.FindStringSubmatch(s); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil {
			ahead = &n
		}
	}
	if m := behindRe.FindStringSubmatch(s); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil {
			behind = &n
		}
	}
	if strings.HasPrefix(s, "No commits yet on ") {
		return strings.TrimSpace(strings.TrimPrefix(s, "No commits yet on ")), ahead, behind
	}
	if i := strings.Index(s, " ["); i >= 0 {
		s = s[:i]
	}
	if i := strings.Index(s, "..."); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s), ahead, behind
}

func classifyXY(xy string) (string, bool) {
	if xy == "??" {
		return "untracked", false
	}
	x, y := xy[0], xy[1]
	if x != ' ' && x != '?' { // staged (index) change present
		return catOf(x), true
	}
	return catOf(y), false
}

func catOf(c byte) string {
	switch c {
	case 'A':
		return "added"
	case 'D':
		return "deleted"
	case 'R':
		return "renamed"
	case 'C':
		return "copied"
	case 'U':
		return "unmerged"
	default: // M, T, and anything else
		return "modified"
	}
}

// validGitPath rejects absolute paths, NUL, and traversal. Empty = no filter.
func validGitPath(p string) bool {
	if p == "" {
		return true
	}
	if strings.ContainsRune(p, 0) || strings.HasPrefix(p, "/") {
		return false
	}
	c := filepath.Clean(p)
	if c == ".." || strings.HasPrefix(c, "../") {
		return false
	}
	return true
}

// capDiff truncates s to max bytes, backing off to a UTF-8 boundary.
func capDiff(s string, max int) (string, bool) {
	if len(s) <= max {
		return s, false
	}
	cut := max
	for cut > 0 && !utf8Boundary(s, cut) {
		cut--
	}
	return s[:cut], true
}

// utf8Boundary reports whether index i is the start of a rune (or end of string).
func utf8Boundary(s string, i int) bool {
	if i <= 0 || i >= len(s) {
		return true
	}
	return s[i]&0xC0 != 0x80 // not a UTF-8 continuation byte
}
