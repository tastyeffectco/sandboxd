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

// commitFake routes git subcommands to canned results and records every argv.
type commitFake struct {
	statusOut  string
	statusExit int
	statusErr  string
	hasHEAD    bool
	sha        string
	branch     string
	addExit    int
	commitExit int
	cmds       [][]string
}

func (f *commitFake) Exec(_ context.Context, _ string, cmd []string) (docker.ExecResult, error) {
	f.cmds = append(f.cmds, cmd)
	j := strings.Join(cmd, " ")
	switch {
	case strings.Contains(j, "status"):
		return docker.ExecResult{Stdout: f.statusOut, Stderr: f.statusErr, ExitCode: f.statusExit}, nil
	case strings.Contains(j, "rev-parse --verify HEAD"):
		if f.hasHEAD {
			return docker.ExecResult{Stdout: "x\n", ExitCode: 0}, nil
		}
		return docker.ExecResult{Stderr: "fatal: Needed a single revision", ExitCode: 128}, nil
	case strings.Contains(j, "--abbrev-ref"):
		return docker.ExecResult{Stdout: f.branch + "\n", ExitCode: 0}, nil
	case strings.Contains(j, "rev-parse HEAD"):
		return docker.ExecResult{Stdout: f.sha + "\n", ExitCode: 0}, nil
	case strings.Contains(j, " add "):
		return docker.ExecResult{ExitCode: f.addExit}, nil
	case strings.Contains(j, " commit "):
		return docker.ExecResult{ExitCode: f.commitExit}, nil
	}
	return docker.ExecResult{}, nil
}

func (f *commitFake) cmdContaining(sub string) []string {
	for _, c := range f.cmds {
		if strings.Contains(strings.Join(c, " "), sub) {
			return c
		}
	}
	return nil
}
func (f *commitFake) issued(sub string) bool { return f.cmdContaining(sub) != nil }

// a dirty repo: one user file, one runtime file, one untracked user file
const dirtyStatus = "## main\x00 M src/a.ts\x00 M pnpm-lock.yaml\x00?? note.md\x00"

func TestCommitDefaultsToUserFilesOnly(t *testing.T) {
	f := &commitFake{statusOut: dirtyStatus, hasHEAD: true, sha: "newsha123", branch: "main"}
	r := gitCommit(context.Background(), f, "SB", v1GitCommitReq{Message: "msg"})
	if !r.Committed || r.SHA != "newsha123" || r.Branch != "main" {
		t.Fatalf("commit resp = %+v", r)
	}
	// committed exactly the user files, NOT the runtime lockfile
	got := strings.Join(r.FilesCommitted, ",")
	if !strings.Contains(got, "src/a.ts") || !strings.Contains(got, "note.md") {
		t.Errorf("expected user files committed, got %v", r.FilesCommitted)
	}
	if strings.Contains(got, "pnpm-lock.yaml") {
		t.Errorf("runtime lockfile must NOT be committed by default: %v", r.FilesCommitted)
	}
	add := strings.Join(f.cmdContaining(" add "), " ")
	if strings.Contains(add, "pnpm-lock.yaml") {
		t.Errorf("add staged the runtime file: %s", add)
	}
	if strings.Contains(add, "-A") || strings.Contains(add, "add .") {
		t.Errorf("must never use add -A / add . : %s", add)
	}
}

func TestCommitRuntimeOnlyWhenOptedIn(t *testing.T) {
	// explicit user path + runtime opt-in -> both staged
	f := &commitFake{statusOut: dirtyStatus, hasHEAD: true, sha: "s", branch: "main"}
	r := gitCommit(context.Background(), f, "SB",
		v1GitCommitReq{Message: "m", Paths: []string{"src/a.ts"}, RuntimePaths: []string{"pnpm-lock.yaml"}})
	if !r.Committed || len(r.FilesCommitted) != 2 {
		t.Fatalf("expected 2 files, got %+v", r)
	}
	add := strings.Join(f.cmdContaining(" add "), " ")
	if !strings.Contains(add, "src/a.ts") || !strings.Contains(add, "pnpm-lock.yaml") {
		t.Errorf("add should include both: %s", add)
	}

	// same but WITHOUT opt-in -> runtime excluded
	f2 := &commitFake{statusOut: dirtyStatus, hasHEAD: true, sha: "s", branch: "main"}
	r2 := gitCommit(context.Background(), f2, "SB", v1GitCommitReq{Message: "m", Paths: []string{"src/a.ts"}})
	if len(r2.FilesCommitted) != 1 || r2.FilesCommitted[0] != "src/a.ts" {
		t.Errorf("runtime must be excluded without opt-in: %+v", r2.FilesCommitted)
	}
}

func TestCommitEphemeralAuthorNoConfigWrite(t *testing.T) {
	f := &commitFake{statusOut: dirtyStatus, hasHEAD: true, sha: "s", branch: "main"}
	gitCommit(context.Background(), f, "SB", v1GitCommitReq{Message: "m", AuthorName: "Ann", AuthorEmail: "ann@x.io"})
	commit := strings.Join(f.cmdContaining(" commit "), " ")
	for _, want := range []string{"--no-verify", "user.name=Ann", "user.email=ann@x.io"} {
		if !strings.Contains(commit, want) {
			t.Errorf("commit argv missing %q: %s", want, commit)
		}
	}
	// author must be ephemeral: never a `git config` write
	if f.issued("config") {
		t.Error("must not write author via `git config`")
	}
}

func TestCommitDefaultAuthor(t *testing.T) {
	f := &commitFake{statusOut: dirtyStatus, hasHEAD: true, sha: "s", branch: "main"}
	gitCommit(context.Background(), f, "SB", v1GitCommitReq{Message: "m"})
	commit := strings.Join(f.cmdContaining(" commit "), " ")
	if !strings.Contains(commit, "user.name=sandbox-agent") || !strings.Contains(commit, "user.email=agent@sandboxd.local") {
		t.Errorf("default author not applied: %s", commit)
	}
}

func TestCommitNoChanges(t *testing.T) {
	f := &commitFake{statusOut: dirtyStatus, hasHEAD: true}
	// select a path that isn't actually changed
	r := gitCommit(context.Background(), f, "SB", v1GitCommitReq{Message: "m", Paths: []string{"does/not/exist.ts"}})
	if r.Committed || r.Reason != "no_changes" {
		t.Errorf("expected no_changes, got %+v", r)
	}
	if f.issued(" commit ") {
		t.Error("must not issue a commit when there are no selected changes")
	}
}

func TestCommitEmptyRepoUnsupported(t *testing.T) {
	f := &commitFake{statusOut: "## No commits yet on main\x00?? a.ts\x00", hasHEAD: false}
	r := gitCommit(context.Background(), f, "SB", v1GitCommitReq{Message: "m", Paths: []string{"a.ts"}})
	if r.Committed || r.Reason != "empty_repo_unsupported" {
		t.Errorf("expected empty_repo_unsupported, got %+v", r)
	}
	if f.issued(" add ") || f.issued(" commit ") {
		t.Error("must not stage/commit on an empty repo")
	}
}

func TestCommitNotARepo(t *testing.T) {
	f := &commitFake{statusExit: 128, statusErr: "fatal: not a git repository"}
	r := gitCommit(context.Background(), f, "SB", v1GitCommitReq{Message: "m"})
	if r.Committed || r.Reason != "not_a_git_repo" {
		t.Errorf("expected not_a_git_repo, got %+v", r)
	}
}

// --- handler validation / guards (no Docker needed) -------------------

func commitServer(t *testing.T, running bool) (*Server, string) {
	t.Helper()
	s := &Server{Store: memStore(t)} // Secrets nil, Docker nil
	app := &store.App{ID: newULID(), OwnerToken: "tenantA", Name: "imp"}
	if err := s.Store.CreateApp(context.Background(), app); err != nil {
		t.Fatal(err)
	}
	if running {
		sb := &store.Sandbox{ID: newULID(), Status: "running", AppID: sql.NullString{String: app.ID, Valid: true}}
		if err := s.Store.Create(context.Background(), sb); err != nil {
			t.Fatal(err)
		}
	}
	return s, app.ID
}

func postCommit(s *Server, appID, owner, body string) *httptest.ResponseRecorder {
	r := reqAs("POST", "/v1/apps/"+appID+"/git/commit", body, owner)
	r.SetPathValue("id", appID)
	w := httptest.NewRecorder()
	s.v1GitCommit(w, r)
	return w
}

func TestCommitHandlerValidation(t *testing.T) {
	s, appID := commitServer(t, true) // running sandbox so we reach validation

	// empty message -> 400
	if w := postCommit(s, appID, "tenantA", `{"message":"  "}`); w.Code != http.StatusBadRequest {
		t.Errorf("empty message: got %d", w.Code)
	}
	// path traversal -> 400
	if w := postCommit(s, appID, "tenantA", `{"message":"m","paths":["../../etc/passwd"]}`); w.Code != http.StatusBadRequest {
		t.Errorf("traversal path: got %d", w.Code)
	}
	// traversal in runtime_paths -> 400
	if w := postCommit(s, appID, "tenantA", `{"message":"m","runtime_paths":["/abs"]}`); w.Code != http.StatusBadRequest {
		t.Errorf("absolute runtime path: got %d", w.Code)
	}
}

func TestCommitHandlerGuards(t *testing.T) {
	// stopped sandbox -> committed:false sandbox_not_running
	s, appID := commitServer(t, false)
	sb := &store.Sandbox{ID: newULID(), Status: "stopped", AppID: sql.NullString{String: appID, Valid: true}}
	if err := s.Store.Create(context.Background(), sb); err != nil {
		t.Fatal(err)
	}
	w := postCommit(s, appID, "tenantA", `{"message":"m"}`)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "sandbox_not_running") {
		t.Errorf("stopped: %d %s", w.Code, w.Body.String())
	}

	// cross-owner -> 404 (and Secrets is nil throughout — no credential access)
	if w := postCommit(s, appID, "tenantB", `{"message":"m"}`); w.Code != http.StatusNotFound {
		t.Errorf("cross-owner: got %d", w.Code)
	}
}
