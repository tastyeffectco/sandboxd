package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/tastyeffectco/sandboxd/control-plane/internal/runtime"
)

// runSettingsReadyTimeout bounds the wait for an app started with new run
// settings; the new command often installs dependencies first.
const runSettingsReadyTimeout = 4 * time.Minute

// reloadRunSettings applies a sandbox.yaml the agent changed (or a restore
// put back), so the preview follows a change of framework without a workspace
// restart. Guardrails, in plain words for the user:
//   - a file that does not parse or validate is replaced by the previous one;
//   - the web port cannot change (the preview address is fixed to it) and an
//     app cannot switch between a web app and a background-only app — both
//     keep the previous file;
//   - worker changes are valid but only start with the workspace's next start.
//
// It returns nil when sandbox.yaml did not change.
func (a *app) reloadRunSettings(ctx context.Context) *runtime.RunSettings {
	path := filepath.Join(a.appDir, ManifestFile)
	raw, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil
	}
	if bytes.Equal(raw, a.manifestRaw) {
		return nil
	}
	keep := func(why string) *runtime.RunSettings {
		a.restoreManifest(path)
		a.log.Warn("sandbox.yaml change rejected; previous settings restored", "reason", why)
		return &runtime.RunSettings{
			Status:  runtime.RunSettingsKeptPrevious,
			Message: "The agent tried to change how your app starts, but " + why + " Your app keeps running as before.",
		}
	}
	if raw == nil {
		return keep("removed the file that says how to start it.")
	}
	m, err := parseManifest(raw, a.defaults)
	if err != nil {
		return keep("the new settings are not valid (" + err.Error() + ").")
	}
	switch {
	case a.web != nil && m.Web == nil:
		return keep("the new settings have no web app, so there would be no preview.")
	case a.web == nil && m.Web != nil:
		return keep("this project runs in the background only; a web app needs a new project.")
	case m.Web != nil && m.Web.Port != a.previewPort:
		return keep(fmt.Sprintf("they use port %d. The preview only works on port %d.", m.Web.Port, a.previewPort))
	}

	a.manifestRaw = raw
	a.build = m.Build
	workersChanged := !sameWorkers(a.workers, m.Workers)
	if a.web == nil {
		// Worker-only app: only workers can change, at the next start.
		return &runtime.RunSettings{
			Status:  runtime.RunSettingsNextStart,
			Message: "Your app's background tasks changed. They start with the new settings the next time your workspace starts.",
		}
	}
	a.mu.Lock()
	a.webHealthPath = m.Web.HealthPath
	a.defaultWeb = m.isDefaultWeb(a.defaults)
	a.mu.Unlock()
	a.log.Info("applying new run settings", "command", m.Web.Command)
	a.web.replace(a.supervisorContext(), m.Web.Command, m.Web.RestartAfterTask)
	ready := a.waitHealthy(ctx, m.Web.HealthPath, runSettingsReadyTimeout)
	res := &runtime.RunSettings{Status: runtime.RunSettingsApplied, WebCommand: m.Web.Command, PreviewReady: ready}
	if ready {
		res.Message = "Your app now starts with new settings, and the preview is running."
	} else {
		res.Message = "Your app now starts with new settings, but the preview is not answering yet. Ask the agent to fix it, or click Restore to go back."
	}
	if workersChanged {
		res.Message += " Background task changes apply the next time your workspace starts."
	}
	return res
}

// waitHealthy polls the web health path until it answers 200 or the budget
// expires; returns whether the app became ready.
func (a *app) waitHealthy(ctx context.Context, healthPath string, budget time.Duration) bool {
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		if code, _ := a.devGet(ctx, healthPath); code == 200 {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(time.Second):
		}
	}
	return false
}

// restoreManifest puts the settings in effect back on disk (or removes a file
// that was not there before), so the next workspace start uses them too.
func (a *app) restoreManifest(path string) {
	var err error
	if a.manifestRaw == nil {
		err = os.Remove(path)
		if os.IsNotExist(err) {
			err = nil
		}
	} else {
		err = os.WriteFile(path, a.manifestRaw, 0o644)
	}
	if err != nil {
		a.log.Warn("could not restore sandbox.yaml", "err", err.Error())
	}
}

// supervisorContext is the runtimed lifetime context the supervised processes
// run under (a background context in tests that build an app by hand).
func (a *app) supervisorContext() context.Context {
	if a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}

func sameWorkers(running []*process, declared []Worker) bool {
	if len(running) != len(declared) {
		return false
	}
	for i, w := range declared {
		p := running[i]
		p.mu.Lock()
		same := p.name == w.Name && p.command == w.Command
		p.mu.Unlock()
		if !same {
			return false
		}
	}
	return true
}

// dependencyFiles lists, per installed-dependency directory, the files that
// decide what goes in it.
var dependencyFiles = map[string][]string{
	"node_modules": {"package.json", "pnpm-lock.yaml", "package-lock.json", "yarn.lock", "bun.lockb", "bun.lock"},
	".venv":        {"requirements.txt", "pyproject.toml", "uv.lock", "poetry.lock"},
}

// dependencyFingerprints records the dependency files' contents ("" when absent).
func dependencyFingerprints(appDir string) map[string]string {
	out := map[string]string{}
	for _, files := range dependencyFiles {
		for _, f := range files {
			b, _ := os.ReadFile(filepath.Join(appDir, f))
			out[f] = string(b)
		}
	}
	return out
}

// resetStaleDependencies removes an installed-dependency directory whose
// dependency files changed since before (e.g. a Restore across a framework
// switch), so the start command's "install when missing" step reinstalls
// them. Reports whether anything was removed.
func resetStaleDependencies(appDir string, before map[string]string, log interface{ Info(string, ...any) }) bool {
	after := dependencyFingerprints(appDir)
	removed := false
	for dir, files := range dependencyFiles {
		for _, f := range files {
			if before[f] == after[f] {
				continue
			}
			path := filepath.Join(appDir, dir)
			if _, err := os.Stat(path); err == nil {
				if err := os.RemoveAll(path); err == nil {
					log.Info("dependencies changed; removed installed ones to reinstall", "dir", dir, "file", f)
					removed = true
				}
			}
			break
		}
	}
	return removed
}
