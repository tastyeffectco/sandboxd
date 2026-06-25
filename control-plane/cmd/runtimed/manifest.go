package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/sandboxd/control-plane/internal/preset"
	"gopkg.in/yaml.v3"
)

// applyPreset prepares an app from a runtime preset on first boot: seed the
// preset's starter template into an EMPTY workspace, then write the preset's
// sandbox.yaml only when none exists. It never overwrites existing app files
// (seedTemplateApp only seeds an empty dir) or an existing sandbox.yaml.
func applyPreset(appDir string, p preset.Preset, log *slog.Logger) {
	seedTemplateApp(appDir, p.Template, log) // no-op if Template=="" or dir non-empty
	writeManifestIfMissing(appDir, p.Manifest, log)
}

// writeManifestIfMissing writes sandbox.yaml only when it doesn't already
// exist — an existing manifest (user/agent/snapshot) is always preserved.
func writeManifestIfMissing(appDir, manifest string, log *slog.Logger) {
	if manifest == "" {
		return
	}
	path := filepath.Join(appDir, ManifestFile)
	if _, err := os.Stat(path); err == nil {
		return // never overwrite an existing sandbox.yaml
	}
	if err := os.WriteFile(path, []byte(manifest), 0o644); err != nil {
		log.Warn("write preset manifest failed", "err", err.Error())
		return
	}
	log.Info("wrote preset sandbox.yaml")
}

// ManifestFile is the optional per-app runtime manifest in the workspace.
// It lets an app declare how it builds, runs, exposes a preview, and reports
// health — so sandboxd works beyond the default Vite/React app. ABSENT is the
// common case and means "use the built-in defaults" (which equal today's
// Vite behavior), so existing apps keep working untouched.
const ManifestFile = "sandbox.yaml"

// Manifest is the parsed sandbox.yaml. All fields are optional; Load fills
// defaults. The agent can edit this file like any workspace file; runtimed
// re-reads it on (re)start.
type Manifest struct {
	Version int        `yaml:"version"`
	Web     *WebProc   `yaml:"web"`     // the previewed process; nil => no preview (worker-only)
	Build   *BuildSpec `yaml:"build"`   // post-task build check
	Workers []Worker   `yaml:"workers"` // background processes, no preview
}

// WebProc is the single previewed process: it serves HTTP on Port and is
// health-probed at HealthPath.
type WebProc struct {
	Command    string `yaml:"command"`
	Port       int    `yaml:"port"`
	HealthPath string `yaml:"health_path"`
	// RestartAfterTask bounces the web process after every coding task so an
	// agent-written production build can't poison a live dev server (e.g. the
	// agent runs `next build`, writing production `.next/` that `next dev`
	// then 500s on). The web command re-runs on restart, so a start-time clean
	// step (e.g. `rm -rf .next`) takes effect. Default false (no restart).
	RestartAfterTask bool `yaml:"restart_after_task"`
}

// BuildSpec is the build check run after a coding task.
type BuildSpec struct {
	// Command is a pointer so we can tell "unset" (nil → use the default
	// build) from an explicit empty string (&"" → SKIP the build check).
	Command        *string `yaml:"command"`
	TimeoutSeconds int     `yaml:"timeout_seconds"`
}

// Worker is a background process with no preview (e.g. a queue consumer).
type Worker struct {
	Name    string `yaml:"name"`
	Command string `yaml:"command"`
	// RestartAfterTask restarts this worker after every coding task so it
	// re-runs its command and picks up code the task changed (a long-running
	// worker otherwise keeps the old behavior until restart). Default false.
	RestartAfterTask bool `yaml:"restart_after_task"`
}

// Defaults preserve the pre-manifest Vite/React behavior. Sourced from the
// long-standing RUNTIMED_* env vars so an operator override still works as the
// default when no manifest sets the field.
type Defaults struct {
	WebCommand    string
	WebPort       int
	BuildCommand  string
	BuildTimeoutS int
	WebHealthPath string
}

// LoadManifest reads <appDir>/sandbox.yaml and returns a fully-defaulted
// manifest. Rules:
//   - no file               -> default web app (Vite), no workers.
//   - file, web present      -> web app with its fields, missing ones defaulted.
//   - file, no web + workers -> worker-only app (no preview).
//   - file, no web, no workers (empty) -> default web app.
//
// A parse error is returned so the caller can log it and fall back to defaults
// rather than booting a misconfigured app silently.
func LoadManifest(appDir string, def Defaults) (*Manifest, error) {
	path := filepath.Join(appDir, ManifestFile)
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return defaultManifest(def), nil
		}
		return defaultManifest(def), fmt.Errorf("read %s: %w", ManifestFile, err)
	}
	var m Manifest
	if err := yaml.Unmarshal(raw, &m); err != nil {
		return defaultManifest(def), fmt.Errorf("parse %s: %w", ManifestFile, err)
	}
	m.applyDefaults(def)
	// Reject an invalid manifest and fall back to the safe default rather than
	// boot a misconfigured (or unsafe) app. The caller logs the error.
	if err := m.validate(); err != nil {
		return defaultManifest(def), fmt.Errorf("invalid %s: %w", ManifestFile, err)
	}
	return &m, nil
}

func defaultManifest(def Defaults) *Manifest {
	m := &Manifest{Version: 1}
	m.applyDefaults(def)
	return m
}

func (m *Manifest) applyDefaults(def Defaults) {
	if m.Version == 0 {
		m.Version = 1
	}
	// An empty manifest (no web, no workers) is a default web app — keeps a
	// stray/empty sandbox.yaml from silently disabling the preview.
	if m.Web == nil && len(m.Workers) == 0 {
		m.Web = &WebProc{}
	}
	if m.Web != nil {
		if m.Web.Command == "" {
			m.Web.Command = def.WebCommand
		}
		if m.Web.Port == 0 {
			m.Web.Port = def.WebPort
		}
		if m.Web.HealthPath == "" {
			m.Web.HealthPath = def.WebHealthPath
		}
	}
	// Build check resolution — distinguish "unset" from an explicit empty
	// command so a preset can DISABLE the post-task build check:
	//   - no build block (m.Build == nil)         -> default build (back-compat)
	//   - build: {} (block present, no command)   -> default build (documented)
	//   - build.command: "" (explicit empty)      -> SKIP the build check
	//   - build.command: "x"                       -> run "x"
	// The build check is runtime verification (does the app still build/start),
	// NOT a production deployment build — presets that have no meaningful build
	// (Next.js dev, FastAPI, workers) skip it with an explicit empty command.
	if m.Build == nil {
		cmd := def.BuildCommand
		m.Build = &BuildSpec{Command: &cmd}
	} else if m.Build.Command == nil {
		cmd := def.BuildCommand
		m.Build.Command = &cmd
	}
	if m.Build.TimeoutSeconds <= 0 {
		m.Build.TimeoutSeconds = def.BuildTimeoutS
	}
}

// buildCommand returns the resolved build command after applyDefaults (always
// non-nil there); "" means "skip the build check".
func (m *Manifest) buildCommand() string {
	if m.Build == nil || m.Build.Command == nil {
		return ""
	}
	return *m.Build.Command
}

// validate rejects unsafe or malformed manifests. Worker names are checked
// strictly because they become a log file path (~/.runtimed/<name>.log), so a
// `/`, `..`, or empty name could escape or collide; ports must be in range.
func (m *Manifest) validate() error {
	if m.Web != nil && (m.Web.Port < 1 || m.Web.Port > 65535) {
		return fmt.Errorf("web.port %d out of range (1-65535)", m.Web.Port)
	}
	seen := map[string]bool{}
	for _, w := range m.Workers {
		if !validWorkerName(w.Name) {
			return fmt.Errorf("invalid worker name %q (allowed: [A-Za-z0-9_-], 1-64 chars)", w.Name)
		}
		if seen[w.Name] {
			return fmt.Errorf("duplicate worker name %q", w.Name)
		}
		seen[w.Name] = true
		if w.Command == "" {
			return fmt.Errorf("worker %q has no command", w.Name)
		}
	}
	return nil
}

// validWorkerName allows only [A-Za-z0-9_-] (1-64 chars). This rejects empty
// names, path separators, and "." / ".." (the dot is not an allowed char), so
// the per-worker log path stays inside the runtime dir.
func validWorkerName(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for _, r := range s {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

// hasDefaultWeb reports whether the web process is the built-in default (no
// sandbox.yaml customization). Used to decide whether to run the Vite-specific
// entry-asset deep probe, which only makes sense for the default React app.
func (m *Manifest) isDefaultWeb(def Defaults) bool {
	return m.Web != nil &&
		m.Web.Command == def.WebCommand &&
		m.Web.Port == def.WebPort &&
		m.Web.HealthPath == def.WebHealthPath
}
