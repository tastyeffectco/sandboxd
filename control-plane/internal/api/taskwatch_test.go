package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sandboxd/control-plane/internal/loopback"
	"github.com/sandboxd/control-plane/internal/runtime"
	"github.com/sandboxd/control-plane/internal/store"
)

// --- the policy: the watch window must outlive the task's timeout -----
//
// The watcher streams runtimed's events under a context deadline. If
// that deadline is shorter than the task's own timeout, the watcher
// aborts the stream and marks a still-running task failed. So the
// window must ALWAYS exceed the effective runtimed task timeout — the
// 10m default, or timeout_s when the caller sets one (up to 24h).
func TestWatchWindowOutlivesTaskTimeout(t *testing.T) {
	cases := []struct {
		name     string
		timeoutS int
		floor    time.Duration // window must exceed this
	}{
		{"default (0 → 10m)", 0, runtime.DefaultTaskTimeout},
		{"one hour", 3600, time.Hour},
		{"max 24h", 86400, 24 * time.Hour},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := watchWindowFor(c.timeoutS)
			if got <= c.floor {
				t.Errorf("watchWindowFor(%d) = %v; must exceed the task timeout %v",
					c.timeoutS, got, c.floor)
			}
		})
	}
}

// --- the mechanism: a fast fake runtimed over a unix socket ----------
//
// No clock mocking: the test injects a small watch window (hundreds of
// ms) instead of minutes, so the timeout vs. terminal-event race plays
// out in real time, instantly.

type fakeEvent struct {
	delay time.Duration
	ev    runtime.Event
}

const (
	testSandboxID = "sbx-test"
	testTaskID    = "task-test"
)

// fakeRuntimed stands up a unix-socket HTTP server at the path
// runtimeClientFor expects (<mnt>/.runtimed/sock) and a Server wired to
// a fresh in-memory store with one running task row. Returns the Server
// plus the sandbox/task ids.
func fakeRuntimed(t *testing.T, events []fakeEvent) (*Server, string, string) {
	t.Helper()
	root := t.TempDir()
	sandboxID, taskID := testSandboxID, testTaskID

	sockDir := filepath.Join(root, sandboxID, ".runtimed")
	if err := os.MkdirAll(sockDir, 0o755); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("unix", filepath.Join(sockDir, "sock"))
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /tasks/{id}/events", func(w http.ResponseWriter, r *http.Request) {
		fl, _ := w.(http.Flusher)
		enc := json.NewEncoder(w)
		for _, e := range events {
			if e.delay > 0 {
				select {
				case <-time.After(e.delay):
				case <-r.Context().Done():
					return
				}
			}
			if enc.Encode(e.ev) != nil {
				return
			}
			if fl != nil {
				fl.Flush()
			}
		}
		<-r.Context().Done() // hold open like a live task until the client gives up
	})
	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })

	st, err := store.Open(context.Background(), "file::memory:?_fk=1", "../../migrations")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.CreateTask(context.Background(), &store.Task{
		TaskID: taskID, SandboxID: sandboxID, Agent: "opencode", Prompt: "x",
	}); err != nil {
		t.Fatalf("create task: %v", err)
	}

	s := &Server{
		Store:    st,
		Loopback: &loopback.Manager{Root: root},
		Log:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	return s, sandboxID, taskID
}

func statusEvent() runtime.Event {
	return runtime.Event{ID: 1, Type: runtime.EventStatus, Time: time.Now().UTC(),
		Data: json.RawMessage(`{"phase":"working"}`)}
}

func doneEvent(taskID string, status runtime.TaskStatus) runtime.Event {
	tr := runtime.TaskResult{ID: taskID, Status: status, FilesChanged: []string{}}
	data, _ := json.Marshal(tr)
	return runtime.Event{ID: 2, Type: runtime.EventDone, Time: time.Now().UTC(), Data: data}
}

// A task that finishes inside its window has its terminal result
// persisted — the case the 15m cap silently broke for long tasks.
func TestWatchTaskPersistsResultWithinWindow(t *testing.T) {
	s, sandboxID, taskID := fakeRuntimed(t, []fakeEvent{
		{0, statusEvent()},
		{30 * time.Millisecond, doneEvent(testTaskID, runtime.TaskSucceeded)},
	})
	s.watchTaskWindow(sandboxID, taskID, 2*time.Second)

	got, err := s.Store.GetTask(context.Background(), taskID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != string(runtime.TaskSucceeded) {
		t.Errorf("task status = %q; want %q", got.Status, runtime.TaskSucceeded)
	}
}

// When the stream stays silent past the window the watcher gives up and
// records a clean internal failure — this is exactly the abort path the
// 15m cap took for any task running longer than 15m.
func TestWatchTaskFailsWhenSilentPastWindow(t *testing.T) {
	s, sandboxID, taskID := fakeRuntimed(t, []fakeEvent{
		{0, statusEvent()}, // ...then the handler holds the stream open, no `done`
	})
	start := time.Now()
	s.watchTaskWindow(sandboxID, taskID, 150*time.Millisecond)
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("watcher did not honor the %v window (took %v)", 150*time.Millisecond, elapsed)
	}
	got, err := s.Store.GetTask(context.Background(), taskID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != string(runtime.TaskFailed) {
		t.Errorf("task status = %q; want %q", got.Status, runtime.TaskFailed)
	}
}
