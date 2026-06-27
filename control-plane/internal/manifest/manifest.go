// Package manifest is a minimal, shared parser for the per-app runtime manifest
// (sandbox.yaml). It exists so the CONTROL PLANE can read the declared web port
// at sandbox-create time (to drive preview routing + the preview URL) — the same
// file runtimed reads to run the app. It is intentionally tiny: only the fields
// the control plane needs (web.port / web.command). runtimed keeps its own
// fuller parser (defaults, build/workers, command building); this package does
// not change runtimed behavior.
package manifest

import "gopkg.in/yaml.v3"

// File is the workspace-relative manifest filename.
const File = "sandbox.yaml"

// Manifest is the subset of sandbox.yaml the control plane cares about.
type Manifest struct {
	Version int      `yaml:"version"`
	Web     *WebProc `yaml:"web"`
}

// WebProc is the previewed process declaration.
type WebProc struct {
	Command    string `yaml:"command"`
	Port       int    `yaml:"port"`
	HealthPath string `yaml:"health_path"`
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
