package main

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/tastyeffectco/sandboxd/control-plane/internal/preset"
)

func quietLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

var samplePreset = preset.Preset{
	Manifest: "version: 1\nweb:\n  command: \"pnpm dev\"\n  port: 3000\n  health_path: \"/\"\n",
	// Template "" so seeding is a no-op; we only exercise the manifest-write gate.
}

// Blank/scaffold app (empty workspace) + preset: sandbox.yaml IS written.
func TestApplyPresetScaffoldWritesManifest(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, ".gitkeep"), nil, 0o644) // empty-dir placeholder
	applyPreset(dir, samplePreset, quietLog())
	if _, err := os.Stat(filepath.Join(dir, ManifestFile)); err != nil {
		t.Fatalf("blank preset app should get sandbox.yaml: %v", err)
	}
}

// Git import (populated workspace) + preset, no sandbox.yaml: must NOT be written.
func TestApplyPresetImportDoesNotWriteManifest(t *testing.T) {
	dir := t.TempDir()
	// simulate a cloned repo: real files, no sandbox.yaml
	_ = os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"x"}`), 0o644)
	_ = os.MkdirAll(filepath.Join(dir, "src"), 0o755)
	applyPreset(dir, samplePreset, quietLog())
	if _, err := os.Stat(filepath.Join(dir, ManifestFile)); err == nil {
		t.Fatal("imported repo must NOT get an injected sandbox.yaml (advisory model)")
	}
}

// Import that already has sandbox.yaml: it is preserved unchanged.
func TestApplyPresetImportKeepsExisting(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{}`), 0o644)
	existing := "version: 1\nweb:\n  command: \"my own\"\n  port: 4321\n"
	_ = os.WriteFile(filepath.Join(dir, ManifestFile), []byte(existing), 0o644)
	applyPreset(dir, samplePreset, quietLog())
	got, _ := os.ReadFile(filepath.Join(dir, ManifestFile))
	if string(got) != existing {
		t.Errorf("existing sandbox.yaml must be preserved; got %q", got)
	}
}
