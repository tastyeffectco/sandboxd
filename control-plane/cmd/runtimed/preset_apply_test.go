package main

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/sandboxd/control-plane/internal/preset"
)

var presetTestLog = slog.New(slog.NewTextHandler(io.Discard, nil))

// Every preset's generated sandbox.yaml parses + validates with the real
// manifest loader, and yields either a web process or workers.
func TestPresetManifestsValidate(t *testing.T) {
	for _, p := range preset.List() {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, ManifestFile), []byte(p.Manifest), 0o644); err != nil {
			t.Fatal(err)
		}
		m, err := LoadManifest(dir, testDefaults)
		if err != nil {
			t.Errorf("preset %s manifest invalid: %v", p.ID, err)
			continue
		}
		if m.Web == nil && len(m.Workers) == 0 {
			t.Errorf("preset %s has neither web nor workers", p.ID)
		}
		if p.ID == "worker" && m.Web != nil {
			t.Errorf("worker preset must have no web process")
		}
	}
}

// writeManifestIfMissing writes when absent and NEVER overwrites an existing
// sandbox.yaml (user/agent/snapshot content is preserved).
func TestWriteManifestIfMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ManifestFile)

	writeManifestIfMissing(dir, "version: 1\nworkers:\n  - name: w\n    command: x\n", presetTestLog)
	if _, err := os.Stat(path); err != nil {
		t.Fatal("manifest not written when missing")
	}
	// Pre-existing content must be preserved.
	if err := os.WriteFile(path, []byte("KEEP ME\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeManifestIfMissing(dir, "version: 1\nweb:\n  port: 9999\n", presetTestLog)
	got, _ := os.ReadFile(path)
	if string(got) != "KEEP ME\n" {
		t.Errorf("existing sandbox.yaml was overwritten: %q", got)
	}
}

// applyPreset on the worker preset (no template) writes a worker-only manifest.
func TestApplyPresetWorkerOnly(t *testing.T) {
	dir := t.TempDir()
	p, _ := preset.Get("worker")
	applyPreset(dir, p, presetTestLog)
	m, err := LoadManifest(dir, testDefaults)
	if err != nil {
		t.Fatal(err)
	}
	if m.Web != nil || len(m.Workers) == 0 {
		t.Errorf("worker preset should yield worker-only manifest: %+v", m)
	}
}

// seedTemplateApp leaves a NON-empty workspace untouched (template applied only
// on first/empty boot).
func TestSeedSkipsNonEmptyWorkspace(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "App.tsx")
	if err := os.WriteFile(existing, []byte("// my code\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	seedTemplateApp(dir, "react-standard", presetTestLog) // template dir absent in tests; non-empty => skip anyway
	got, _ := os.ReadFile(existing)
	if string(got) != "// my code\n" {
		t.Errorf("existing workspace file was modified: %q", got)
	}
}
