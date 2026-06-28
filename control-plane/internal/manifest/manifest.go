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
}

// WebProc is the previewed process declaration.
type WebProc struct {
	Command    string `yaml:"command"`
	Port       int    `yaml:"port"`
	HealthPath string `yaml:"health_path"`
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
}

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
}

type EffectiveWeb struct {
	Command    string `json:"command"`
	Port       int    `json:"port"`
	HealthPath string `json:"health_path"`
}

type EffectiveWorker struct {
	Name    string `json:"name"`
	Command string `json:"command"`
}

var (
	reLocalhost  = regexp.MustCompile(`localhost|127\.0\.0\.1`)
	reCmdPort    = regexp.MustCompile(`(?:--port[ =]|:)(\d{2,5})`)
	reWorkerName = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
)

// knownTopLevel are the keys the contract understands. Unknown keys WARN (not
// error): core stays forward-compatible and recipe-agnostic — rejecting unknown
// keys would couple core to a fixed key set and break valid-but-newer manifests.
// The warning still makes typos visible. The three web.* fields placed at the top
// level are a common, specific mistake, so they get a pointed ERROR instead.
var knownTopLevel = map[string]bool{"version": true, "web": true, "workers": true, "build": true}

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

	for k := range top {
		switch k {
		case "command":
			res.Errors = append(res.Errors, "top-level 'command' is not valid — put it under web.command")
		case "port":
			res.Errors = append(res.Errors, "top-level 'port' is not valid — put it under web.port")
		case "health_path":
			res.Errors = append(res.Errors, "top-level 'health_path' is not valid — put it under web.health_path")
		default:
			if !knownTopLevel[k] {
				res.Warnings = append(res.Warnings, fmt.Sprintf("unknown top-level key %q (ignored)", k))
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

	res.Valid = len(res.Errors) == 0
	// Only expose the effective view for a VALID manifest, so callers never present
	// invalid config as runnable.
	if res.Valid {
		res.Effective = effectiveOf(&m)
	}
	return res
}

// parsedOf is the as-declared view (no defaults applied).
func parsedOf(m *Manifest) *Effective {
	e := &Effective{Workers: []EffectiveWorker{}}
	if m.Web != nil {
		e.Web = &EffectiveWeb{Command: m.Web.Command, Port: m.Web.Port, HealthPath: m.Web.HealthPath}
	}
	for _, wk := range m.Workers {
		e.Workers = append(e.Workers, EffectiveWorker{Name: wk.Name, Command: wk.Command})
	}
	return e
}

func effectiveOf(m *Manifest) *Effective {
	e := &Effective{Workers: []EffectiveWorker{}}
	if m.Web != nil {
		w := &EffectiveWeb{Command: m.Web.Command, Port: m.Web.Port, HealthPath: m.Web.HealthPath}
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
