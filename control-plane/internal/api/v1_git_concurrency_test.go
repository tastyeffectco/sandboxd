package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tastyeffectco/sandboxd/control-plane/internal/docker"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/gitimport"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/idlock"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/store"
)

// wsFake models a workspace's change set: a commit removes the committed paths,
// so a later status (under the lock) sees them clean. Mutex-guarded for memory
// safety; the per-workspace idlock is what serializes whole commit operations.
type wsFake struct {
	mu      sync.Mutex
	changed map[string]bool
	commits int
}

func newWsFake(paths ...string) *wsFake {
	m := map[string]bool{}
	for _, p := range paths {
		m[p] = true
	}
	return &wsFake{changed: m}
}

func (f *wsFake) Exec(_ context.Context, _ string, cmd []string) (docker.ExecResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	j := strings.Join(cmd, " ")
	switch {
	case strings.Contains(j, "status"):
		var b strings.Builder
		b.WriteString("## main\x00")
		for p := range f.changed {
			b.WriteString(" M " + p + "\x00")
		}
		return docker.ExecResult{Stdout: b.String()}, nil
	case strings.Contains(j, "rev-parse --verify HEAD"):
		return docker.ExecResult{Stdout: "x\n"}, nil
	case strings.Contains(j, "--abbrev-ref"):
		return docker.ExecResult{Stdout: "main\n"}, nil
	case strings.Contains(j, "rev-parse"): // HEAD / --short HEAD
		return docker.ExecResult{Stdout: "sha" + strconv.Itoa(f.commits) + "\n"}, nil
	case strings.Contains(j, " add "):
		return docker.ExecResult{}, nil
	case strings.Contains(j, " commit "):
		for _, p := range pathsAfterDashDash(cmd) {
			delete(f.changed, p)
		}
		f.commits++
		return docker.ExecResult{}, nil
	}
	return docker.ExecResult{}, nil
}

func pathsAfterDashDash(cmd []string) []string {
	for i, a := range cmd {
		if a == "--" {
			return cmd[i+1:]
		}
	}
	return nil
}

// fileStore is a file-backed store so the connection pool's goroutines share one
// database (the in-memory memStore gives each connection its OWN empty DB, which
// breaks genuinely-concurrent reads — production uses a real file DB).
func fileStore(t *testing.T) *store.Store {
	t.Helper()
	dsn := "file:" + filepath.Join(t.TempDir(), "test.db") + "?_fk=1"
	st, err := store.Open(context.Background(), dsn, "../../migrations")
	if err != nil {
		t.Fatal(err)
	}
	return st
}

// concurrencyServer wires real per-workspace locks + an injected workspace fake.
func concurrencyServer(t *testing.T, ex sandboxExecer) (*Server, string, string) {
	t.Helper()
	s := &Server{Store: fileStore(t), Locks: idlock.New(), GitExec: ex}
	app := &store.App{ID: newULID(), OwnerToken: "tenantA", Name: "imp"}
	if err := s.Store.CreateApp(context.Background(), app); err != nil {
		t.Fatal(err)
	}
	sbID := newULID()
	sb := &store.Sandbox{ID: sbID, Status: "running", WorkspaceMnt: t.TempDir(),
		AppID: sql.NullString{String: app.ID, Valid: true}}
	if err := s.Store.Create(context.Background(), sb); err != nil {
		t.Fatal(err)
	}
	return s, app.ID, sbID
}

// Two concurrent commits of the same file: exactly one commits; the other
// re-evaluates under the lock and returns no_changes (not git_error).
func TestConcurrentCommitsOneWins(t *testing.T) {
	f := newWsFake("a.ts")
	s, appID, _ := concurrencyServer(t, f)

	var wg sync.WaitGroup
	results := make([]v1GitCommitResp, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			w := postCommit(s, appID, "tenantA", `{"message":"m","paths":["a.ts"]}`)
			json.Unmarshal(w.Body.Bytes(), &results[i])
		}(i)
	}
	wg.Wait()

	committed, noChanges := 0, 0
	for _, r := range results {
		if r.Committed {
			committed++
		} else if r.Reason == "no_changes" {
			noChanges++
		} else {
			t.Fatalf("unexpected result: %+v", r)
		}
	}
	if committed != 1 || noChanges != 1 {
		t.Fatalf("want exactly one committed + one no_changes; got committed=%d no_changes=%d", committed, noChanges)
	}
	if f.commits != 1 {
		t.Errorf("exactly one commit should have run; got %d", f.commits)
	}
}

// Re-committing an already-committed file sequentially yields no_changes.
func TestSequentialRecommitNoChanges(t *testing.T) {
	f := newWsFake("a.ts")
	s, appID, _ := concurrencyServer(t, f)
	var r1 v1GitCommitResp
	json.Unmarshal(postCommit(s, appID, "tenantA", `{"message":"m","paths":["a.ts"]}`).Body.Bytes(), &r1)
	if !r1.Committed {
		t.Fatalf("first commit should succeed: %+v", r1)
	}
	var r2 v1GitCommitResp
	json.Unmarshal(postCommit(s, appID, "tenantA", `{"message":"m","paths":["a.ts"]}`).Body.Bytes(), &r2)
	if r2.Committed || r2.Reason != "no_changes" {
		t.Errorf("re-commit should be no_changes, got %+v", r2)
	}
}

// A held git lock blocks a push on the SAME workspace, and releasing it unblocks.
func TestPushWaitsForGitLock(t *testing.T) {
	f := &fakePusher{state: okState(), count: 1, outcome: gitimport.PushOutcome{OK: true}}
	s, appID := pushServer(t, f, "running", true)
	sb, _ := s.Store.CurrentSandboxForApp(context.Background(), appID)

	s.Locks.Lock(gitLockKey(sb.ID)) // simulate a commit holding the workspace lock
	done := make(chan int, 1)
	go func() { done <- postPush(s, appID, "tenantA", `{}`).Code }()

	select {
	case <-done:
		t.Fatal("push proceeded while the git lock was held")
	case <-time.After(150 * time.Millisecond): // expected: blocked
	}
	s.Locks.Unlock(gitLockKey(sb.ID))
	select {
	case code := <-done:
		if code != http.StatusOK {
			t.Errorf("push after unlock: code %d", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("push did not proceed after the lock was released")
	}
}

// A lock held for one workspace must NOT block a commit on a DIFFERENT workspace.
func TestDifferentWorkspacesDoNotBlock(t *testing.T) {
	f := newWsFake("a.ts")
	s, appID, _ := concurrencyServer(t, f)

	s.Locks.Lock(gitLockKey("some-other-sandbox")) // unrelated workspace
	defer s.Locks.Unlock(gitLockKey("some-other-sandbox"))

	done := make(chan int, 1)
	go func() { done <- postCommit(s, appID, "tenantA", `{"message":"m","paths":["a.ts"]}`).Code }()
	select {
	case code := <-done:
		if code != http.StatusOK {
			t.Errorf("commit code %d", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("commit blocked by an unrelated workspace lock")
	}
}
