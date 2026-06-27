package api

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sandboxd/control-plane/internal/docker"
	"github.com/sandboxd/control-plane/internal/store"
)

// fakeExecer returns canned ExecResults keyed by the git subcommand.
type fakeExecer struct {
	status docker.ExecResult
	head   docker.ExecResult
	diff   docker.ExecResult
	err    error
	gotCmd [][]string
}

func (f *fakeExecer) Exec(_ context.Context, _ string, cmd []string) (docker.ExecResult, error) {
	f.gotCmd = append(f.gotCmd, cmd)
	if f.err != nil {
		return docker.ExecResult{}, f.err
	}
	joined := strings.Join(cmd, " ")
	switch {
	case strings.Contains(joined, "status"):
		return f.status, nil
	case strings.Contains(joined, "rev-parse"):
		return f.head, nil
	case strings.Contains(joined, "diff"):
		return f.diff, nil
	}
	return docker.ExecResult{}, nil
}

const nul = "\x00"

func indexOf(ss []string, want string) int {
	for i, s := range ss {
		if s == want {
			return i
		}
	}
	return -1
}

func TestParsePorcelainZ(t *testing.T) {
	// branch w/ upstream + ahead/behind, then: modified(unstaged), staged-add,
	// deleted(unstaged), untracked, renamed (consumes old path).
	out := "## main...origin/main [ahead 2, behind 1]" + nul +
		" M src/a.ts" + nul +
		"A  src/new.ts" + nul +
		" D gone.txt" + nul +
		"?? note.md" + nul +
		"R  dst.ts" + nul + "old.ts" + nul

	branch, ahead, behind, files := parsePorcelainZ(out)
	if branch != "main" {
		t.Errorf("branch=%q", branch)
	}
	if ahead == nil || *ahead != 2 || behind == nil || *behind != 1 {
		t.Errorf("ahead/behind = %v/%v", ahead, behind)
	}
	want := []v1GitFile{
		{"src/a.ts", "modified", false},
		{"src/new.ts", "added", true},
		{"gone.txt", "deleted", false},
		{"note.md", "untracked", false},
		{"dst.ts", "renamed", true},
	}
	if len(files) != len(want) {
		t.Fatalf("files=%+v", files)
	}
	for i, w := range want {
		if files[i] != w {
			t.Errorf("file[%d]=%+v want %+v", i, files[i], w)
		}
	}
}

func TestParsePorcelainCleanAndNoUpstream(t *testing.T) {
	branch, ahead, behind, files := parsePorcelainZ("## main" + nul)
	if branch != "main" || ahead != nil || behind != nil || len(files) != 0 {
		t.Errorf("clean parse wrong: %q %v %v %+v", branch, ahead, behind, files)
	}
	// empty repo
	b2, _, _, _ := parsePorcelainZ("## No commits yet on main" + nul)
	if b2 != "main" {
		t.Errorf("empty-repo branch=%q", b2)
	}
}

func TestValidGitPath(t *testing.T) {
	ok := []string{"", "src/x.ts", "a/b/c.txt", "./x"}
	bad := []string{"/etc/passwd", "../x", "../../y", "a/../../b", "\x00evil"}
	for _, p := range ok {
		if !validGitPath(p) {
			t.Errorf("should accept %q", p)
		}
	}
	for _, p := range bad {
		if validGitPath(p) {
			t.Errorf("should reject %q", p)
		}
	}
}

func TestCapDiff(t *testing.T) {
	if out, tr := capDiff("short", 100); tr || out != "short" {
		t.Errorf("small should not truncate")
	}
	big := strings.Repeat("a", 1000)
	out, tr := capDiff(big, 100)
	if !tr || len(out) != 100 {
		t.Errorf("expected truncation to 100, got len=%d tr=%v", len(out), tr)
	}
	// UTF-8 boundary back-off: cut in the middle of a 3-byte rune
	s := strings.Repeat("界", 10) // each is 3 bytes
	out2, tr2 := capDiff(s, 10)  // 10 is mid-rune (3*3=9, 4th rune spans 9..12)
	if !tr2 || len(out2)%3 != 0 {
		t.Errorf("expected rune-aligned truncation, got len=%d", len(out2))
	}
}

func TestGitStatusLogic(t *testing.T) {
	ex := &fakeExecer{
		status: docker.ExecResult{Stdout: "## main" + nul + " M a.ts" + nul, ExitCode: 0},
		head:   docker.ExecResult{Stdout: "abc123\n", ExitCode: 0},
	}
	r := gitStatus(context.Background(), ex, "SB")
	if !r.Available || r.Branch != "main" || r.HeadSHA != "abc123" || r.Clean || len(r.Files) != 1 {
		t.Fatalf("status = %+v", r)
	}
	// hardening flags present in the argv
	joined := strings.Join(ex.gotCmd[0], " ")
	for _, want := range []string{"core.fsmonitor=", "safe.directory=*", sandboxAppDir, "--porcelain=v1"} {
		if !strings.Contains(joined, want) {
			t.Errorf("status argv missing %q: %v", want, joined)
		}
	}
}

func TestIsRuntimeFile(t *testing.T) {
	runtime := []string{"sandbox.yaml", "pnpm-lock.yaml", "package-lock.json", "yarn.lock",
		"node_modules/x/y.js", ".astro/settings.json", ".next/cache", ".vite/deps"}
	user := []string{"src/App.tsx", "README.md", "src/sandbox.yaml", "app/pnpm-lock.yaml", "package.json"}
	for _, p := range runtime {
		if !isRuntimeFile(p) {
			t.Errorf("should be runtime-generated: %q", p)
		}
	}
	for _, p := range user {
		if isRuntimeFile(p) {
			t.Errorf("should be a user file: %q", p)
		}
	}
}

// A pristine imported repo whose only changes are runtime-generated files must
// report clean=false (truthful) but user_clean=true, with those files surfaced
// under runtime_files rather than presented as user edits.
func TestPristineImportClassification(t *testing.T) {
	out := "## main" + nul +
		" M sandbox.yaml" + nul +
		" M pnpm-lock.yaml" + nul +
		"?? .astro/settings.json" + nul
	ex := &fakeExecer{
		status: docker.ExecResult{Stdout: out, ExitCode: 0},
		head:   docker.ExecResult{Stdout: "deadbeef\n", ExitCode: 0},
	}
	r := gitStatus(context.Background(), ex, "SB")
	if r.Clean {
		t.Error("raw clean must stay false (status is truthful)")
	}
	if !r.UserClean {
		t.Error("user_clean should be true (only runtime files changed)")
	}
	if len(r.Files) != 0 {
		t.Errorf("no user files expected, got %+v", r.Files)
	}
	if len(r.RuntimeFiles) != 3 {
		t.Errorf("expected 3 runtime files, got %+v", r.RuntimeFiles)
	}
}

func TestMixedUserAndRuntimeClassification(t *testing.T) {
	out := "## main" + nul + " M src/App.tsx" + nul + " M pnpm-lock.yaml" + nul
	ex := &fakeExecer{status: docker.ExecResult{Stdout: out, ExitCode: 0}}
	r := gitStatus(context.Background(), ex, "SB")
	if r.UserClean {
		t.Error("user_clean should be false — src/App.tsx is a user change")
	}
	if len(r.Files) != 1 || r.Files[0].Path != "src/App.tsx" {
		t.Errorf("user files = %+v", r.Files)
	}
	if len(r.RuntimeFiles) != 1 || r.RuntimeFiles[0].Path != "pnpm-lock.yaml" {
		t.Errorf("runtime files = %+v", r.RuntimeFiles)
	}
}

func TestGitStatusNotARepo(t *testing.T) {
	ex := &fakeExecer{status: docker.ExecResult{Stderr: "fatal: not a git repository", ExitCode: 128}}
	r := gitStatus(context.Background(), ex, "SB")
	if r.Available || r.Reason != "not_a_git_repo" {
		t.Errorf("expected not_a_git_repo, got %+v", r)
	}
}

func TestGitDiffLogic(t *testing.T) {
	ex := &fakeExecer{diff: docker.ExecResult{Stdout: "diff --git a/x b/x\n+hi\n", ExitCode: 0}}
	r := gitDiff(context.Background(), ex, "SB", "")
	if !r.Available || r.Truncated || !strings.Contains(r.Diff, "+hi") {
		t.Errorf("diff = %+v", r)
	}
	// Command-ordering regression (Blocker A): --no-ext-diff/--no-textconv are
	// `git diff` options and MUST come AFTER the `diff` subcommand (and HEAD after
	// them), else git rejects them as unknown top-level options.
	argv := ex.gotCmd[0]
	di, ni := indexOf(argv, "diff"), indexOf(argv, "--no-ext-diff")
	ti, hi := indexOf(argv, "--no-textconv"), indexOf(argv, "HEAD")
	if di < 0 || ni < 0 || ti < 0 || hi < 0 || !(di < ni && di < ti && ni < hi && ti < hi) {
		t.Errorf("diff argv mis-ordered (diff must precede its flags, HEAD last): %v", argv)
	}
	// empty repo (no HEAD) -> available, empty diff
	ex2 := &fakeExecer{diff: docker.ExecResult{Stderr: "fatal: bad revision 'HEAD'", ExitCode: 128}}
	r2 := gitDiff(context.Background(), ex2, "SB", "")
	if !r2.Available || r2.Diff != "" {
		t.Errorf("empty-repo diff = %+v", r2)
	}
}

func TestGitDiffTruncationLogic(t *testing.T) {
	ex := &fakeExecer{diff: docker.ExecResult{Stdout: strings.Repeat("x", maxDiffBytes+500), ExitCode: 0}}
	r := gitDiff(context.Background(), ex, "SB", "")
	if !r.Truncated || len(r.Diff) != maxDiffBytes {
		t.Errorf("expected truncation to %d, got len=%d tr=%v", maxDiffBytes, len(r.Diff), r.Truncated)
	}
}

// --- handler guard paths (no Docker needed) ---------------------------

func gitGuardServer(t *testing.T) (*Server, *store.App) {
	t.Helper()
	s := &Server{Store: memStore(t)} // Secrets nil, Docker nil — guards must not touch them
	app := &store.App{ID: newULID(), OwnerToken: "tenantA", Name: "imp"}
	if err := s.Store.CreateApp(context.Background(), app); err != nil {
		t.Fatal(err)
	}
	return s, app
}

func callGit(s *Server, appID, owner, sub string) *httptest.ResponseRecorder {
	r := reqAs("GET", "/v1/apps/"+appID+"/git/"+sub, "", owner)
	r.SetPathValue("id", appID)
	w := httptest.NewRecorder()
	if sub == "status" {
		s.v1GitStatus(w, r)
	} else {
		s.v1GitDiff(w, r)
	}
	return w
}

func TestGitHandlerGuards(t *testing.T) {
	s, app := gitGuardServer(t)

	// cross-owner -> 404
	if w := callGit(s, app.ID, "tenantB", "status"); w.Code != http.StatusNotFound {
		t.Errorf("cross-owner status: got %d", w.Code)
	}
	if w := callGit(s, app.ID, "tenantB", "diff"); w.Code != http.StatusNotFound {
		t.Errorf("cross-owner diff: got %d", w.Code)
	}

	// owner, no sandbox -> 200 available:false reason no_sandbox
	w := callGit(s, app.ID, "tenantA", "status")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "no_sandbox") {
		t.Errorf("no-sandbox: %d %s", w.Code, w.Body.String())
	}

	// add a STOPPED sandbox -> sandbox_not_running
	sb := &store.Sandbox{ID: newULID(), Status: "stopped", AppID: sql.NullString{String: app.ID, Valid: true}}
	if err := s.Store.Create(context.Background(), sb); err != nil {
		t.Fatal(err)
	}
	w2 := callGit(s, app.ID, "tenantA", "status")
	if w2.Code != http.StatusOK || !strings.Contains(w2.Body.String(), "sandbox_not_running") {
		t.Errorf("stopped: %d %s", w2.Code, w2.Body.String())
	}
}

func TestGitDiffRejectsTraversal(t *testing.T) {
	s, app := gitGuardServer(t)
	// running sandbox so we get past the guard to the path check
	sb := &store.Sandbox{ID: newULID(), Status: "running", AppID: sql.NullString{String: app.ID, Valid: true}}
	if err := s.Store.Create(context.Background(), sb); err != nil {
		t.Fatal(err)
	}
	r := reqAs("GET", "/v1/apps/"+app.ID+"/git/diff?path=../../etc/passwd", "", "tenantA")
	r.SetPathValue("id", app.ID)
	w := httptest.NewRecorder()
	s.v1GitDiff(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("traversal path: got %d want 400 (%s)", w.Code, w.Body.String())
	}
}
