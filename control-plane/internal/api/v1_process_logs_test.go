package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/sandboxd/control-plane/internal/loopback"
	"github.com/sandboxd/control-plane/internal/runtime"
	"github.com/sandboxd/control-plane/internal/store"
)

func newProcLogsServer(t *testing.T) (*Server, string, string) {
	t.Helper()
	st, err := store.Open(context.Background(), "file::memory:?_fk=1", "../../migrations")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	lm := loopback.New()
	lm.Root = t.TempDir()
	s := &Server{Store: st, Loopback: lm, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	id := newULID()
	if err := st.Create(context.Background(), &store.Sandbox{ID: id, Status: "running", ExternalUserID: nullStr("local")}); err != nil {
		t.Fatal(err)
	}
	_, mnt := lm.Paths(id)
	if err := os.MkdirAll(filepath.Join(mnt, ".runtimed"), 0o755); err != nil {
		t.Fatal(err)
	}
	return s, id, mnt
}

func getLogs(s *Server, id, name, tail string) *httptest.ResponseRecorder {
	// Use a benign target and set path values directly — the handler reads
	// PathValue, and some bad names (e.g. a space) can't sit in a URL path.
	target := "/logs"
	if tail != "" {
		target += "?tail=" + tail
	}
	r := httptest.NewRequest("GET", target, nil)
	r.SetPathValue("id", id)
	r.SetPathValue("name", name)
	w := httptest.NewRecorder()
	s.v1ProcessLogs(w, r)
	return w
}

// Tail returns the last N lines of the process log.
func TestProcessLogsTail(t *testing.T) {
	s, id, mnt := newProcLogsServer(t)
	if err := os.WriteFile(filepath.Join(mnt, ".runtimed", "web.log"),
		[]byte("l1\nl2\nl3\nl4\nl5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	w := getLogs(s, id, "web", "2")
	if w.Code != http.StatusOK {
		t.Fatalf("got %d %s", w.Code, w.Body)
	}
	var got struct {
		Process string   `json:"process"`
		Lines   []string `json:"lines"`
	}
	json.Unmarshal(w.Body.Bytes(), &got)
	if got.Process != "web" || len(got.Lines) != 2 || got.Lines[0] != "l4" || got.Lines[1] != "l5" {
		t.Errorf("tail wrong: %+v", got)
	}
}

// Path-traversal / malformed names are rejected (400) — never reach the FS.
func TestProcessLogsRejectsBadNames(t *testing.T) {
	s, id, _ := newProcLogsServer(t)
	for _, name := range []string{"../etc/passwd", "a/b", "..", ".", "web.log", "has space", ""} {
		if w := getLogs(s, id, name, ""); w.Code != http.StatusBadRequest {
			t.Errorf("name %q: got %d, want 400", name, w.Code)
		}
	}
}

// A valid-format but unknown process (no log file) is 404, not an empty 200.
func TestProcessLogsUnknownProcess(t *testing.T) {
	s, id, _ := newProcLogsServer(t)
	if w := getLogs(s, id, "ghost", ""); w.Code != http.StatusNotFound {
		t.Errorf("unknown process: got %d, want 404", w.Code)
	}
}

// An unknown sandbox id is 404 (and never touches the FS).
func TestProcessLogsUnknownSandbox(t *testing.T) {
	s, _, _ := newProcLogsServer(t)
	if w := getLogs(s, newULID(), "web", ""); w.Code != http.StatusNotFound {
		t.Errorf("unknown sandbox: got %d, want 404", w.Code)
	}
}

// v1RuntimeView maps processes and renders a worker-only app as valid: preview
// status "none", NO endpoint URL, and the worker present in processes.
func TestV1RuntimeViewWorkerOnlyAndProcesses(t *testing.T) {
	s := &Server{PreviewDomain: "ex.sslip.io"}

	// Web app: ready, has a URL, web process present.
	web := &runtime.Status{
		Preview:   runtime.PreviewState{Status: runtime.PreviewReady, LastHTTPStatus: 200},
		Processes: []runtime.ProcessState{{Name: "web", Kind: "web", Running: true, Pid: 42, Restarts: 1}},
	}
	prev, procs := s.v1RuntimeView("01ABC", "running", web, 3000)
	if prev.Status != "ready" || prev.URL == "" {
		t.Errorf("web preview wrong: %+v", prev)
	}
	if len(procs) != 1 || procs[0].Name != "web" || procs[0].Kind != "web" || !procs[0].Running {
		t.Errorf("web processes wrong: %+v", procs)
	}

	// Worker-only: status none, NO URL, worker present (valid, not a failure).
	wo := &runtime.Status{
		Preview:   runtime.PreviewState{Status: runtime.PreviewNone},
		Processes: []runtime.ProcessState{{Name: "ticker", Kind: "worker", Running: true}},
	}
	prev, procs = s.v1RuntimeView("01ABC", "running", wo, 3000)
	if prev.Status != "none" {
		t.Errorf("worker-only status = %q; want none", prev.Status)
	}
	if prev.URL != "" {
		t.Errorf("worker-only must have no endpoint URL, got %q", prev.URL)
	}
	if len(procs) != 1 || procs[0].Kind != "worker" || !procs[0].Running {
		t.Errorf("worker process wrong: %+v", procs)
	}

	// runtimed unreachable: reflects the row status.
	if prev, _ := s.v1RuntimeView("01ABC", "running", nil, 3000); prev.Status != "starting" {
		t.Errorf("nil/running = %q; want starting", prev.Status)
	}
	if prev, _ := s.v1RuntimeView("01ABC", "stopped", nil, 3000); prev.Status != "down" {
		t.Errorf("nil/stopped = %q; want down", prev.Status)
	}
}
