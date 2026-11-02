// Package manifest is the CONTROL-PLANE-side contract for the per-app runtime
// manifest (sandbox.yaml): the shared parser plus the authoritative Validate.
// sandboxd core owns the manifest contract and validation here; runtime
// recipes/presets are advisory and runtimed keeps its own executor-side parser
// (kept aligned by cmd/runtimed's parser-drift test). This package contains NO
// framework-specific execution logic — only schema validation + guidance.
package manifest

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// File is the workspace-relative manifest filename.
const File = "sandbox.yaml"

// Core defaults applied to the "effective" view for fields the contract owns.
const (
	DefaultWebPort    = 3000
	DefaultHealthPath = "/"
)

// Manifest is the parsed sandbox.yaml schema.
type Manifest struct {
	Version int      `yaml:"version"`
	Web     *WebProc `yaml:"web"`
	Workers []Worker `yaml:"workers"`
	Build   *Build   `yaml:"build"`
	// Data lists the app paths (relative to the app dir) whose contents must
	// survive new releases in production: SQLite files, uploads. They live on a
	// persistent volume, are never part of a release bundle, and are what
	// backups copy. Unset means DefaultData in production.
	Data []string `yaml:"data"`
}

// WebProc is the previewed process declaration.
type WebProc struct {
	Command    string `yaml:"command"`
	Port       int    `yaml:"port"`
	HealthPath string `yaml:"health_path"`
	// Start is the production start command (e.g. `next start`), used instead
	// of Command when the app runs in production mode on a server. Optional.
	Start string `yaml:"start"`
}

// Worker is a background process declaration.
type Worker struct {
	Name    string `yaml:"name"`
	Command string `yaml:"command"`
}

// Build is the post-task build check.
type Build struct {
	Command        string `yaml:"command"`
	TimeoutSeconds int    `yaml:"timeout_seconds"`
	// Release is the production build, run once per release before it starts
	// (install dependencies, compile). Optional.
	Release string `yaml:"release"`
}

// DefaultData is the persistent data path used in production when the
// manifest declares none.
var DefaultData = []string{"data"}

// MaxDataPaths bounds the data: list.
const MaxDataPaths = 16

// Parse unmarshals raw sandbox.yaml bytes. No defaults are applied.
func Parse(raw []byte) (*Manifest, error) {
	var m Manifest
	if err := yaml.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// WebPort returns the declared web port, or 0 when there is no web process or no
// port set (worker-only / unset).
func (m *Manifest) WebPort() int {
	if m == nil || m.Web == nil {
		return 0
	}
	return m.Web.Port
}

// --- validation + effective view -------------------------------------

// Result is the validation outcome plus views of the manifest.
type Result struct {
	Valid    bool     `json:"valid"`
	Errors   []string `json:"errors"`
	Warnings []string `json:"warnings"`
	// Parsed is what we read from the manifest AS-DECLARED (no defaults), set
	// whenever the YAML parsed — even for an invalid manifest — so a caller can
	// confirm the web command/port were understood.
	Parsed *Effective `json:"parsed,omitempty"`
	// Effective is the manifest after core defaults. Present ONLY when valid, so an
	// invalid manifest never advertises a (misleading) runnable runtime.
	Effective *Effective `json:"effective,omitempty"`
}

// Effective is the manifest as runtimed would see it after core defaults.
type Effective struct {
	Web     *EffectiveWeb     `json:"web,omitempty"`
	Workers []EffectiveWorker `json:"workers"`
	// Release is the production build command ("" = none).
	Release string `json:"release"`
	// Data is the persistent data paths (DefaultData when unset, in Effective).
	Data []string `json:"data"`
}

type EffectiveWeb struct {
	Command    string `json:"command"`
	Port       int    `json:"port"`
	HealthPath string `json:"health_path"`
	// Start is the production start command ("" = not ready to publish).
	Start string `json:"start"`
}

type EffectiveWorker struct {
	Name    string `json:"name"`
	Command string `json:"command"`
}

var (
	reLocalhost  = regexp.MustCompile(`localhost|127\.0\.0\.1`)
	reCmdPort    = regexp.MustCompile(`(?:--port[ =]|:)(\d{2,5})`)
	reWorkerName = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	reDataPath   = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]*(/[A-Za-z0-9_][A-Za-z0-9._-]*)*$`)
)

// knownTopLevel are the keys the v1 contract understands. The set is CLOSED for
// version 1: an unrecognized top-level key is an ERROR (forward-compat lives in the
// version field — a new key requires a version bump). This catches the common,
// dangerous mistakes (processes:/services:/top-level command) that otherwise parse
// to an empty runtime and silently run the default template.
var knownTopLevel = map[string]bool{"version": true, "web": true, "workers": true, "build": true, "data": true}

// Validate is the authoritative manifest validation. It never executes anything
// and is framework-agnostic. Errors make a manifest invalid; warnings are advice.
func Validate(raw []byte) Result {
	res := Result{Errors: []string{}, Warnings: []string{}}

	// Top-level key inspection (a separate generic parse so we can see keys the
	// typed struct would silently drop).
	var top map[string]any
	if err := yaml.Unmarshal(raw, &top); err != nil {
		res.Errors = append(res.Errors, "invalid YAML: "+oneLine(err.Error()))
		return res
	}
	var m Manifest
	if err := yaml.Unmarshal(raw, &m); err != nil {
		res.Errors = append(res.Errors, "invalid YAML: "+oneLine(err.Error()))
		return res
	}
	// Always expose what we parsed (as-declared), even if validation fails below.
	res.Parsed = parsedOf(&m)

	// version is REQUIRED and must be 1. Without this, a missing/empty manifest
	// parses to an empty {workers:[]} and would misleadingly validate (QA footgun).
	// Forward-compat: a future schema bump declares a new version, so an unknown
	// version is invalid here rather than silently treated as v1.
	switch m.Version {
	case 1:
		// supported
	case 0:
		res.Errors = append(res.Errors, "missing required 'version' — sandbox.yaml must declare 'version: 1'")
	default:
		res.Errors = append(res.Errors, fmt.Sprintf("unsupported version %d — only 'version: 1' is supported", m.Version))
	}

	// Top-level keys are a CLOSED set in v1 (a future schema change bumps version,
	// which is rejected above). An unrecognized top-level key is therefore an ERROR,
	// not a warning — a wrong key (processes:/services:) otherwise parses to zero web
	// and the app silently runs the default template (QA footgun).
	topLevelMistake := false
	for k := range top {
		switch k {
		case "command":
			res.Errors = append(res.Errors, "top-level 'command' is not valid — put it under web.command")
			topLevelMistake = true
		case "port":
			res.Errors = append(res.Errors, "top-level 'port' is not valid — put it under web.port")
			topLevelMistake = true
		case "health_path":
			res.Errors = append(res.Errors, "top-level 'health_path' is not valid — put it under web.health_path")
			topLevelMistake = true
		case "processes":
			res.Errors = append(res.Errors, "top-level 'processes' is not valid — did you mean 'workers:' (background processes) or 'web:' (the previewed process)?")
			topLevelMistake = true
		case "services":
			res.Errors = append(res.Errors, "top-level 'services' is not valid — sandbox.yaml is not Docker Compose; use 'web:' and 'workers:'")
			topLevelMistake = true
		default:
			if !knownTopLevel[k] {
				res.Errors = append(res.Errors, fmt.Sprintf("unknown top-level key %q — sandbox.yaml (v1) allows: version, web, workers, build, data", k))
				topLevelMistake = true
			}
		}
	}

	if m.Web != nil {
		if m.Web.Port != 0 && (m.Web.Port < 1 || m.Web.Port > 65535) {
			res.Errors = append(res.Errors, fmt.Sprintf("web.port %d out of range (1-65535)", m.Web.Port))
		}
		// A custom web.command MUST declare web.port — otherwise preview routing is
		// ambiguous (it would silently assume 3000). This is an error, not a warning.
		if m.Web.Command != "" && m.Web.Port == 0 {
			res.Errors = append(res.Errors, "web.command is set but web.port is missing — a custom web.command must declare web.port")
		}
		if m.Web.Command != "" && bindsLocalhost(m.Web.Command) {
			res.Warnings = append(res.Warnings,
				"web.command may bind localhost only — the preview needs 0.0.0.0 (e.g. --host 0.0.0.0)")
		}
		if cp := commandPort(m.Web.Command); cp != 0 && m.Web.Port != 0 && cp != m.Web.Port {
			res.Warnings = append(res.Warnings,
				fmt.Sprintf("web.command appears to use port %d but web.port is %d", cp, m.Web.Port))
		}
	}

	if m.Web != nil && m.Web.Start != "" && bindsLocalhost(m.Web.Start) {
		res.Warnings = append(res.Warnings,
			"web.start may bind localhost only — a published app needs 0.0.0.0 (e.g. --host 0.0.0.0)")
	}
	if m.Web != nil && m.Web.Start != "" && m.Web.Port != 0 {
		if cp := commandPort(m.Web.Start); cp != 0 && cp != m.Web.Port {
			res.Warnings = append(res.Warnings,
				fmt.Sprintf("web.start appears to use port %d but web.port is %d", cp, m.Web.Port))
		}
	}
	res.Errors = append(res.Errors, DataPathErrors(m.Data)...)

	// Workers — mirror runtimed's rules (kept as errors, like today's behavior).
	seen := map[string]bool{}
	for _, w := range m.Workers {
		if !reWorkerName.MatchString(w.Name) {
			res.Errors = append(res.Errors,
				fmt.Sprintf("invalid worker name %q (allowed: [A-Za-z0-9_-], 1-64 chars)", w.Name))
		}
		if seen[w.Name] {
			res.Errors = append(res.Errors, fmt.Sprintf("duplicate worker name %q", w.Name))
		}
		seen[w.Name] = true
		if w.Command == "" {
			res.Errors = append(res.Errors, fmt.Sprintf("worker %q has no command", w.Name))
		}
	}

	// A manifest must declare something runnable. No web AND no workers is invalid
	// (unless a top-level mistake above already explains the emptiness — avoid a
	// redundant second error for processes:/services:/etc.).
	if m.Web == nil && len(m.Workers) == 0 && !topLevelMistake {
		res.Errors = append(res.Errors, "sandbox.yaml declares neither 'web:' nor 'workers:' — nothing to run")
	}

	res.Valid = len(res.Errors) == 0
	// Only expose the effective view for a VALID manifest, so callers never present
	// invalid config as runnable.
	if res.Valid {
		res.Effective = effectiveOf(&m)
	}
	return res
}

// DataPathErrors checks a data: list. Each path must be relative to the app
// dir, without "." or ".." segments, so it can never point outside the app.
func DataPathErrors(paths []string) []string {
	var errs []string
	if len(paths) > MaxDataPaths {
		errs = append(errs, fmt.Sprintf("data: lists %d paths; at most %d are allowed", len(paths), MaxDataPaths))
	}
	seen := map[string]bool{}
	for _, p := range paths {
		clean := strings.TrimSuffix(p, "/")
		if !reDataPath.MatchString(clean) || strings.Contains("/"+clean+"/", "/../") || len(clean) > 200 {
			errs = append(errs, fmt.Sprintf("invalid data path %q — use a folder or file inside the app, like \"data\" or \"prisma/dev.db\"", p))
			continue
		}
		if seen[clean] {
			errs = append(errs, fmt.Sprintf("duplicate data path %q", p))
		}
		seen[clean] = true
	}
	return errs
}

// DataPaths returns the cleaned data: list, or DefaultData when none is declared.
func (m *Manifest) DataPaths() []string {
	if m == nil || len(m.Data) == 0 {
		return append([]string(nil), DefaultData...)
	}
	out := make([]string, 0, len(m.Data))
	for _, p := range m.Data {
		out = append(out, strings.TrimSuffix(p, "/"))
	}
	return out
}

// ReleaseCommand is the production build: build.release when set.
func (m *Manifest) ReleaseCommand() string {
	if m == nil || m.Build == nil {
		return ""
	}
	return m.Build.Release
}

// StartCommand is the production start command ("" when undeclared).
func (m *Manifest) StartCommand() string {
	if m == nil || m.Web == nil {
		return ""
	}
	return m.Web.Start
}

// parsedOf is the as-declared view (no defaults applied).
func parsedOf(m *Manifest) *Effective {
	e := &Effective{Workers: []EffectiveWorker{}}
	if m.Web != nil {
		e.Web = &EffectiveWeb{Command: m.Web.Command, Port: m.Web.Port, HealthPath: m.Web.HealthPath, Start: m.Web.Start}
	}
	for _, wk := range m.Workers {
		e.Workers = append(e.Workers, EffectiveWorker{Name: wk.Name, Command: wk.Command})
	}
	e.Release = m.ReleaseCommand()
	e.Data = append([]string{}, m.Data...)
	return e
}

func effectiveOf(m *Manifest) *Effective {
	e := &Effective{Workers: []EffectiveWorker{}}
	if m.Web != nil {
		w := &EffectiveWeb{Command: m.Web.Command, Port: m.Web.Port, HealthPath: m.Web.HealthPath, Start: m.Web.Start}
		if w.Port == 0 {
			w.Port = DefaultWebPort
		}
		if w.HealthPath == "" {
			w.HealthPath = DefaultHealthPath
		}
		e.Web = w
	}
	for _, wk := range m.Workers {
		e.Workers = append(e.Workers, EffectiveWorker{Name: wk.Name, Command: wk.Command})
	}
	e.Release = m.ReleaseCommand()
	e.Data = m.DataPaths()
	return e
}

// bindsLocalhost flags ONLY an explicit localhost / 127.0.0.1 in the command. We
// deliberately do NOT guess "dev server without --host" — many servers bind all
// interfaces by default (node/express, Nest, Bun, uvicorn-with-host), and the
// guess produced false positives ("may bind localhost only") on those.
func bindsLocalhost(cmd string) bool {
	return reLocalhost.MatchString(cmd)
}

func commandPort(cmd string) int {
	if mm := reCmdPort.FindStringSubmatch(cmd); mm != nil {
		n, _ := strconv.Atoi(mm[1])
		return n
	}
	return 0
}

func oneLine(s string) string {
	return strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
}
