// Command runtimed is the in-sandbox supervisor.
//
// Scope of this slice (slice 1): run as the container's main process,
// start and supervise the Vite dev server, and expose GET /status
// over a Unix domain socket on the workspace loopback. The
// coding-task subsystem (POST /tasks, events, cancel) is a later
// slice — see cmd/runtimed/README.md and ops/design/v1-external-api.md.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/sandboxd/control-plane/internal/preset"
	"github.com/sandboxd/control-plane/internal/runtime"
)

const version = "0.1.0"

// app holds runtimed's live state: the supervised processes (the web dev
// server and any workers from sandbox.yaml), the most recent preview health
// probe, and the one active coding task.
type app struct {
	web           *process   // the previewed process; nil for a worker-only app
	workers       []*process // background processes, no preview
	previewPort   int        // web process's HTTP port
	webHealthPath string     // path probed for web readiness
	defaultWeb    bool       // web is the built-in default => run the Vite asset deep-probe
	webRestart    bool       // restart the web process after every task (manifest web.restart_after_task)
	build         *BuildSpec // post-task build check (from manifest)
	appDir        string
	runtimeDir    string
	log           *slog.Logger
	bootedAt      time.Time

	mu           sync.Mutex
	lastCode     int
	lastAssetErr string // entry-asset compile error, "" when assets transform
	lastChecked  time.Time

	taskMu sync.Mutex // guards task; serializes start (one task at a time)
	task   *task      // the current / last task, or nil
}

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stderr, nil)).With("component", "runtimed")

	appDir := envOr("RUNTIMED_APP_DIR", "/home/sandbox/workspace/app")
	runtimeDir := envOr("RUNTIMED_DIR", "/home/sandbox/.runtimed")
	socketPath := envOr("RUNTIMED_SOCKET", filepath.Join(runtimeDir, "sock"))
	probeInterval := time.Duration(envOrInt("RUNTIMED_PROBE_INTERVAL_SECONDS", 3)) * time.Second

	// Manifest defaults preserve the pre-manifest Vite behavior. The
	// long-standing RUNTIMED_* env vars remain the source of each default, so
	// an operator override still applies when sandbox.yaml doesn't set the
	// field. (Install runs on first boot of a fresh workspace; node_modules
	// then persists across stop/wake. `bash -lc` runs the compound form.)
	manifestDefaults := Defaults{
		WebCommand:    envOr("RUNTIMED_DEV_CMD", "[ -d node_modules ] || pnpm install; pnpm dev"),
		WebPort:       envOrInt("RUNTIMED_PREVIEW_PORT", 3000),
		BuildCommand:  envOr("RUNTIMED_BUILD_CMD", "pnpm build"),
		BuildTimeoutS: envOrInt("RUNTIMED_BUILD_TIMEOUT_SECONDS", 120),
		WebHealthPath: envOr("RUNTIMED_HEALTH_PATH", "/"),
	}

	if err := os.MkdirAll(runtimeDir, 0o755); err != nil {
		log.Error("mkdir runtime dir", "dir", runtimeDir, "err", err.Error())
		os.Exit(1)
	}
	// Ensure the app working directory exists. A fresh workspace ships
	// with only ~/workspace, so ~/workspace/app may not exist yet. Both
	// the managed dev server and the coding-agent runner chdir into
	// appDir — a missing dir makes fork/exec fail with a misleading
	// ENOENT against the binary. Creating it here self-heals every
	// sandbox on start (including ones provisioned before this fix).
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		log.Error("mkdir app dir", "dir", appDir, "err", err.Error())
		os.Exit(1)
	}

	// Seed the app from a baked template on the first boot of an empty
	// workspace. The control plane chooses the template (RUNTIMED_TEMPLATE);
	// the default is react-standard, and "blank"/"none" leaves the app
	// empty. A snapshot/fork clone or an already-populated workspace is
	// left untouched (the dir is non-empty), so this only fires once.
	// Apply a runtime preset (if the create path selected one) on first boot:
	// seed the preset's starter template into an empty workspace and write its
	// sandbox.yaml when none exists. Otherwise fall back to the named template.
	// Both paths only ever touch an EMPTY workspace / a MISSING sandbox.yaml.
	if presetID := envOr("RUNTIMED_RUNTIME_PRESET", ""); presetID != "" {
		if p, ok := preset.Get(presetID); ok {
			applyPreset(appDir, p, log)
		} else {
			log.Warn("unknown runtime preset; using default template", "preset", presetID)
			seedTemplateApp(appDir, "react-standard", log)
		}
	} else {
		seedTemplateApp(appDir, envOr("RUNTIMED_TEMPLATE", "react-standard"), log)
	}

	// Load the app's runtime manifest (sandbox.yaml). Absent => built-in
	// defaults (a Vite web app); a parse error logs and falls back safely.
	m, err := LoadManifest(appDir, manifestDefaults)
	if err != nil {
		log.Warn("manifest load failed; using defaults", "err", err.Error())
	}

	a := &app{
		build:      m.Build,
		appDir:     appDir,
		runtimeDir: runtimeDir,
		log:        log,
		bootedAt:   time.Now(),
	}
	if m.Web != nil {
		a.web = newProcess("web", "web", appDir, m.Web.Command, filepath.Join(runtimeDir, "web.log"), log)
		a.previewPort = m.Web.Port
		a.webHealthPath = m.Web.HealthPath
		a.defaultWeb = m.isDefaultWeb(manifestDefaults)
		a.webRestart = m.Web.RestartAfterTask
	}
	for _, w := range m.Workers {
		a.workers = append(a.workers,
			newProcess(w.Name, "worker", appDir, w.Command, filepath.Join(runtimeDir, w.Name+".log"), log))
	}

	// Finalize any task interrupted by a previous stop/crash before
	// accepting new work — an interrupted task is failed, never resumed.
	recoverInterruptedTasks(filepath.Join(runtimeDir, "tasks"), log)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	if a.web != nil {
		go a.web.supervise(ctx)
		go a.probeLoop(ctx, probeInterval)
	}
	for _, w := range a.workers {
		go w.supervise(ctx)
	}

	log.Info("runtimed started", "version", version, "app_dir", appDir, "socket", socketPath,
		"web", a.web != nil, "workers", len(a.workers))
	if err := serve(ctx, socketPath, a); err != nil {
		log.Error("control server", "err", err.Error())
	}

	// ctx is done — stop all supervised processes cleanly before exiting.
	log.Info("runtimed shutting down — stopping processes")
	if a.web != nil {
		a.web.stop()
	}
	for _, w := range a.workers {
		w.stop()
	}
	log.Info("runtimed stopped")
}

// seedTemplateApp copies a baked app scaffold from /opt/templates/<name>
// into an empty app dir. No-op when the template is blank/none, when the
// app dir is already populated (a snapshot clone or an existing
// workspace), or when the named template isn't present in the image.
func seedTemplateApp(appDir, template string, log *slog.Logger) {
	if template == "" || template == "blank" || template == "none" {
		return
	}
	entries, err := os.ReadDir(appDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		// A lone .gitkeep (the empty-dir placeholder) still counts as
		// empty; anything else is a snapshot clone or a real workspace.
		if e.Name() != ".gitkeep" {
			return
		}
	}
	src := filepath.Join("/opt/templates", template)
	if fi, err := os.Stat(src); err != nil || !fi.IsDir() {
		log.Warn("template not found; leaving app empty", "template", template, "path", src)
		return
	}
	// Trailing /. copies contents into appDir; cp -a preserves perms.
	if out, err := exec.Command("cp", "-a", src+"/.", appDir+"/").CombinedOutput(); err != nil {
		log.Error("seed template failed", "template", template, "err", err.Error(), "out", string(out))
		return
	}
	log.Info("seeded app from template", "template", template)
}

// probeLoop polls the dev server's HTTP port so /status reports a real
// readiness signal rather than just process liveness.
func (a *app) probeLoop(ctx context.Context, interval time.Duration) {
	a.probe()
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			a.probe()
		}
	}
}

func (a *app) probe() {
	if a.web == nil {
		return // worker-only app: no preview to probe
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	code, _ := a.devGet(ctx, a.webHealthPath)
	// The HTML shell can serve 200 while the dev server fails to transform
	// the real entry modules (a blank page). Probe the entry assets once the
	// shell is up — but only for the DEFAULT Vite app; a custom app declares
	// its own health_path and has no Vite entry modules to probe.
	assetErr := ""
	if code == 200 && a.defaultWeb {
		assetErr = a.probeEntryAssets(ctx)
	}
	a.mu.Lock()
	a.lastCode = code
	a.lastAssetErr = assetErr
	a.lastChecked = time.Now()
	a.mu.Unlock()
}

// status derives the runtime.Status snapshot from the supervised processes
// and the latest web health probe.
func (a *app) status() runtime.Status {
	var ps runtime.PreviewState
	var procs []runtime.ProcessState

	if a.web != nil {
		pid, restarts, running := a.web.snapshot()
		a.mu.Lock()
		code, assetErr, checked := a.lastCode, a.lastAssetErr, a.lastChecked
		a.mu.Unlock()

		ps.Restarts = restarts
		ps.LastHTTPStatus = code
		switch {
		case !running:
			ps.Status = runtime.PreviewDown
		case code == 200 && assetErr == "":
			ps.Status = runtime.PreviewReady
			ps.Pid = pid
		case code == 200 && assetErr != "":
			// shell serves but an entry module fails to compile — a blank page.
			ps.Status = runtime.PreviewError
			ps.BuildErrorMessage = assetErr
			ps.Pid = pid
		default:
			ps.Status = runtime.PreviewStarting
			ps.Pid = pid
		}
		if !checked.IsZero() {
			c := checked
			ps.LastCheckedAt = &c
		}
		procs = append(procs, runtime.ProcessState{Name: "web", Kind: "web", Running: running, Pid: pid, Restarts: restarts})
	} else {
		// Worker-only app: there is no web process to preview.
		ps.Status = runtime.PreviewNone
	}

	for _, w := range a.workers {
		pid, restarts, running := w.snapshot()
		procs = append(procs, runtime.ProcessState{Name: w.name, Kind: "worker", Running: running, Pid: pid, Restarts: restarts})
	}

	return runtime.Status{
		Runtimed: runtime.RuntimedInfo{
			Version:  version,
			BootedAt: a.bootedAt,
			UptimeS:  int64(time.Since(a.bootedAt).Seconds()),
		},
		Preview:    ps,
		Processes:  procs,
		ActiveTask: a.activeTaskRef(),
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envOrInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
