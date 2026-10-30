package main

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/tastyeffectco/sandboxd/control-plane/internal/runtime"
)

const startManifest = "version: 1\nweb:\n  command: \"sleep 300\"\n  port: %PORT%\n"

// reloadApp builds a web app whose sandbox.yaml is in effect, with a stand-in
// preview server answering on its port, and a supervised web process.
func reloadApp(t *testing.T) (*app, string) {
	t.Helper()
	// Stand-in preview: answers 200 except on /missing (an app that is not up).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/missing" {
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	port := srv.Listener.Addr().(*net.TCPAddr).Port
	dir := t.TempDir()
	raw := []byte(strings.ReplaceAll(startManifest, "%PORT%", strconv.Itoa(port)))
	if err := os.WriteFile(filepath.Join(dir, ManifestFile), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := parseManifest(raw, Defaults{WebPort: 3000, WebHealthPath: "/"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	a := &app{appDir: dir, runtimeDir: dir, log: quietLog(), manifestRaw: raw, ctx: ctx,
		defaults: Defaults{WebPort: 3000, WebHealthPath: "/"}, previewPort: port, webHealthPath: "/", build: m.Build}
	a.web = newProcess("web", "web", dir, m.Web.Command, filepath.Join(dir, "web.log"), a.log)
	go a.web.supervise(ctx)
	t.Cleanup(func() { cancel(); a.web.stop() })
	// Running before the test starts, so cleanup always has a process to stop.
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		if _, _, running := a.web.snapshot(); running {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("web process did not start")
		}
	}
	return a, filepath.Join(dir, ManifestFile)
}

func TestRunSettingsUnchangedReportsNothing(t *testing.T) {
	a, _ := reloadApp(t)
	if rs := a.reloadRunSettings(context.Background()); rs != nil {
		t.Fatalf("unchanged sandbox.yaml reported %+v", rs)
	}
}

// The agent switched frameworks: the new command is live without a restart.
func TestRunSettingsAppliesANewStartCommand(t *testing.T) {
	a, path := reloadApp(t)
	next := strings.Replace(string(a.manifestRaw), "sleep 300", "sleep 301", 1) + "  health_path: /health\nbuild:\n  command: \"\"\n"
	if err := os.WriteFile(path, []byte(next), 0o644); err != nil {
		t.Fatal(err)
	}
	rs := a.reloadRunSettings(context.Background())
	if rs == nil || rs.Status != runtime.RunSettingsApplied || !rs.PreviewReady || rs.WebCommand != "sleep 301" {
		t.Fatalf("got %+v; want applied, ready, new command", rs)
	}
	if !strings.Contains(rs.Message, "preview is running") {
		t.Errorf("message %q does not tell the user the preview runs", rs.Message)
	}
	if health, _ := a.webSettings(); health != "/health" {
		t.Errorf("health path %q not applied", health)
	}
	if a.build == nil || a.build.Command == nil || *a.build.Command != "" {
		t.Errorf("build check not applied: %+v", a.build)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		a.web.mu.Lock()
		cmd, running := a.web.command, a.web.running
		a.web.mu.Unlock()
		if cmd == "sleep 301" && running {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("web process not restarted with the new command (command=%q running=%v)", cmd, running)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if rs := a.reloadRunSettings(context.Background()); rs != nil {
		t.Errorf("an applied file is reported again: %+v", rs)
	}
}

// A process the supervisor gave up on starts again with a fixed command.
func TestRunSettingsRestartsAProcessThatGaveUp(t *testing.T) {
	a, path := reloadApp(t)
	a.web.stop()
	a.web.mu.Lock()
	a.web.supervising = false
	a.web.mu.Unlock()
	next := strings.Replace(string(a.manifestRaw), "sleep 300", "sleep 302", 1)
	if err := os.WriteFile(path, []byte(next), 0o644); err != nil {
		t.Fatal(err)
	}
	if rs := a.reloadRunSettings(context.Background()); rs == nil || rs.Status != runtime.RunSettingsApplied {
		t.Fatalf("got %+v", rs)
	}
	deadline := time.Now().Add(10 * time.Second)
	for !func() bool { _, _, running := a.web.snapshot(); return running }() {
		if time.Now().After(deadline) {
			t.Fatal("web process was not started again")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// Changes that would break the preview are undone, in plain words.
func TestRunSettingsKeepsPreviousOnBreakingChanges(t *testing.T) {
	cases := []struct {
		name, content, want string
		remove              bool
	}{
		{"invalid yaml", "web: [unclosed", "not valid", false},
		{"other port", "version: 1\nweb:\n  command: \"pnpm dev\"\n  port: 4321\n", "port 4321", false},
		{"no web app", "version: 1\nworkers:\n  - name: job\n    command: \"sleep 1\"\n", "no preview", false},
		{"unknown keys", "version: 1\nservices:\n  web: x\n", "not valid", false},
		{"removed", "", "removed", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a, path := reloadApp(t)
			before := string(a.manifestRaw)
			if c.remove {
				os.Remove(path)
			} else if err := os.WriteFile(path, []byte(c.content), 0o644); err != nil {
				t.Fatal(err)
			}
			rs := a.reloadRunSettings(context.Background())
			if rs == nil || rs.Status != runtime.RunSettingsKeptPrevious {
				t.Fatalf("got %+v; want kept_previous", rs)
			}
			if !strings.Contains(rs.Message, c.want) || !strings.Contains(rs.Message, "keeps running as before") {
				t.Errorf("message %q", rs.Message)
			}
			if got, _ := os.ReadFile(path); string(got) != before {
				t.Errorf("previous sandbox.yaml not restored: %q", got)
			}
			a.web.mu.Lock()
			cmd := a.web.command
			a.web.mu.Unlock()
			if cmd != "sleep 300" {
				t.Errorf("web command changed to %q", cmd)
			}
		})
	}
}

// A workspace without sandbox.yaml (built-in settings) that gains a broken one
// gets the file removed again rather than a copy of nothing.
func TestRunSettingsRemovesABrokenNewFile(t *testing.T) {
	a, path := reloadApp(t)
	a.manifestRaw = nil
	if err := os.WriteFile(path, []byte("web: [x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if rs := a.reloadRunSettings(context.Background()); rs == nil || rs.Status != runtime.RunSettingsKeptPrevious {
		t.Fatalf("got %+v", rs)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("broken new sandbox.yaml left in place (err=%v)", err)
	}
}

// Worker-only apps cannot gain a preview, and worker edits wait for the next start.
func TestRunSettingsWorkerOnlyApps(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ManifestFile)
	raw := []byte("version: 1\nworkers:\n  - name: job\n    command: \"sleep 1\"\n")
	os.WriteFile(path, raw, 0o644)
	a := &app{appDir: dir, log: quietLog(), manifestRaw: raw, defaults: Defaults{WebPort: 3000, WebHealthPath: "/"}}
	a.workers = []*process{newProcess("job", "worker", dir, "sleep 1", filepath.Join(dir, "job.log"), a.log)}

	os.WriteFile(path, []byte("version: 1\nweb:\n  command: \"pnpm dev\"\n  port: 3000\n"), 0o644)
	if rs := a.reloadRunSettings(context.Background()); rs == nil || rs.Status != runtime.RunSettingsKeptPrevious ||
		!strings.Contains(rs.Message, "new project") {
		t.Fatalf("worker-only app gaining a web app: %+v", rs)
	}
	os.WriteFile(path, []byte("version: 1\nworkers:\n  - name: job\n    command: \"sleep 2\"\n"), 0o644)
	if rs := a.reloadRunSettings(context.Background()); rs == nil || rs.Status != runtime.RunSettingsNextStart {
		t.Fatalf("worker change: %+v", rs)
	}
}

// The new command never answers: applied, but the user is told what to do.
func TestRunSettingsAppliedButPreviewNotAnswering(t *testing.T) {
	a, path := reloadApp(t)
	next := strings.Replace(string(a.manifestRaw), "sleep 300", "sleep 303", 1) + "  health_path: /missing\n"
	if err := os.WriteFile(path, []byte(next), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	rs := a.reloadRunSettings(ctx)
	if rs == nil || rs.Status != runtime.RunSettingsApplied || rs.PreviewReady || !strings.Contains(rs.Message, "Restore") {
		t.Fatalf("got %+v", rs)
	}
}

// A second runtimed in the same workspace (an agent running `runtimed`) must
// not start: it would fail the running task and take over the socket.
func TestRuntimeDirLockAllowsOneSupervisor(t *testing.T) {
	dir := t.TempDir()
	if !lockRuntimeDir(dir) {
		t.Fatal("first supervisor could not take the lock")
	}
	held := runtimedLock
	defer held.Close()
	// flock locks are per open file description: a fresh open is a second process.
	f, err := os.OpenFile(filepath.Join(dir, "runtimed.lock"), os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err == nil {
		t.Fatal("a second supervisor got the lock")
	}
}

// A worker bounced after a task is running again before health is judged.
func TestRestartedWorkerIsRunningBeforeHealth(t *testing.T) {
	dir := t.TempDir()
	a := &app{appDir: dir, log: quietLog()}
	wp := newProcess("job", "worker", dir, "sleep 300", filepath.Join(dir, "job.log"), a.log)
	wp.restartAfterTask = true
	a.workers = []*process{wp}
	ctx, cancel := context.WithCancel(context.Background())
	go wp.supervise(ctx)
	defer func() { cancel(); wp.stop() }()
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		if _, _, running := wp.snapshot(); running {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("worker did not start")
		}
	}
	a.restartWorkersAfterTask()
	if _, _, running := wp.snapshot(); !running {
		t.Fatal("worker reported down right after its restart")
	}
	if healthy, _ := postTaskHealth(true, "skipped", a.status()); !healthy {
		t.Fatal("worker-only app unhealthy right after the worker restarted")
	}
}

// A Restore across a dependency change must not keep the other project's
// installed packages (they made Vite crash after restoring from Astro).
func TestResetStaleDependencies(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755)
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("package.json", `{"dependencies":{"astro":"5"}}`)
	write("node_modules/.bin/vite", "#!/bin/sh")
	write("requirements.txt", "fastapi\n")
	write(".venv/bin/python", "")
	before := dependencyFingerprints(dir)

	if resetStaleDependencies(dir, before, quietLog()) {
		t.Fatal("nothing changed but dependencies were removed")
	}
	write("package.json", `{"devDependencies":{"vite":"5"}}`)
	if !resetStaleDependencies(dir, before, quietLog()) {
		t.Fatal("changed package.json did not reset node_modules")
	}
	if _, err := os.Stat(filepath.Join(dir, "node_modules")); !os.IsNotExist(err) {
		t.Error("node_modules still present")
	}
	if _, err := os.Stat(filepath.Join(dir, ".venv")); err != nil {
		t.Error(".venv removed although Python dependencies did not change")
	}
}
