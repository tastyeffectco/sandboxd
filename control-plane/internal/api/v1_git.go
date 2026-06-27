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
	Available bool        `json:"available"`
	Reason    string      `json:"reason,omitempty"` // no_sandbox|sandbox_not_running|not_a_git_repo|git_error|exec_failed
	Branch    string      `json:"branch,omitempty"`
	HeadSHA   string      `json:"head_sha,omitempty"`
	Clean     bool        `json:"clean"`
	Ahead     *int        `json:"ahead"`  // null unless an upstream is locally known (no network)
	Behind    *int        `json:"behind"` // null unless an upstream is locally known
	Files     []v1GitFile `json:"files"`
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

// resolveGitSandbox enforces owner scope + a running sandbox. On any not-ready
// condition it writes the response itself and returns ok=false.
func (s *Server) resolveGitSandbox(w http.ResponseWriter, r *http.Request) (*store.Sandbox, bool) {
	app, err := s.Store.GetAppForOwner(r.Context(), r.PathValue("id"), tenantToken(r))
	if errors.Is(err, store.ErrNotFound) {
		writeV1Err(w, http.StatusNotFound, "not_found", "no such app")
		return nil, false
	}
	if err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", "lookup failed")
		return nil, false
	}
	sb, err := s.Store.CurrentSandboxForApp(r.Context(), app.ID)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"available": false, "reason": "no_sandbox"})
		return nil, false
	}
	if sb.Status != "running" {
		writeJSON(w, http.StatusOK, map[string]any{"available": false, "reason": "sandbox_not_running"})
		return nil, false
	}
	return sb, true
}

// GET /v1/apps/{id}/git/status
func (s *Server) v1GitStatus(w http.ResponseWriter, r *http.Request) {
	sb, ok := s.resolveGitSandbox(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, gitStatus(r.Context(), s.Docker, sb.ID))
}

// GET /v1/apps/{id}/git/diff?path=<optional relative path>
func (s *Server) v1GitDiff(w http.ResponseWriter, r *http.Request) {
	sb, ok := s.resolveGitSandbox(w, r)
	if !ok {
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

// --- git logic (testable with a fake execer) --------------------------

func gitStatus(ctx context.Context, ex sandboxExecer, sbID string) v1GitStatusResp {
	st, err := ex.Exec(ctx, "s-"+sbID, gitArgs("status", "--porcelain=v1", "--branch", "-z"))
	if err != nil {
		return v1GitStatusResp{Available: false, Reason: "exec_failed", Files: []v1GitFile{}}
	}
	if st.ExitCode != 0 {
		return v1GitStatusResp{Available: false, Reason: gitErrReason(st.Stderr), Files: []v1GitFile{}}
	}
	branch, ahead, behind, files := parsePorcelainZ(st.Stdout)
	head := ""
	if h, err := ex.Exec(ctx, "s-"+sbID, gitArgs("rev-parse", "HEAD")); err == nil && h.ExitCode == 0 {
		head = strings.TrimSpace(h.Stdout)
	}
	return v1GitStatusResp{
		Available: true, Branch: branch, HeadSHA: head,
		Clean: len(files) == 0, Ahead: ahead, Behind: behind, Files: files,
	}
}

func gitDiff(ctx context.Context, ex sandboxExecer, sbID, path string) v1GitDiffResp {
	args := []string{"--no-ext-diff", "--no-textconv", "diff", "HEAD"}
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
