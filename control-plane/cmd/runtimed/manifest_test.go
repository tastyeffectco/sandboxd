package main

import (
	"os"
	"path/filepath"
	"testing"
)

var testDefaults = Defaults{
	WebCommand:    "[ -d node_modules ] || pnpm install; pnpm dev",
	WebPort:       3000,
	BuildCommand:  "pnpm build",
	BuildTimeoutS: 120,
	WebHealthPath: "/",
}

func writeManifest(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ManifestFile), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// No sandbox.yaml -> the built-in default Vite web app (backward compatible).
func TestManifestAbsentIsDefaultWeb(t *testing.T) {
	m, err := LoadManifest(t.TempDir(), testDefaults)
	if err != nil {
		t.Fatal(err)
	}
	if m.Web == nil {
		t.Fatal("default manifest should have a web process")
	}
	if m.Web.Command != testDefaults.WebCommand || m.Web.Port != 3000 || m.Web.HealthPath != "/" {
		t.Errorf("default web wrong: %+v", m.Web)
	}
	if m.Build.Command != "pnpm build" || m.Build.TimeoutSeconds != 120 {
		t.Errorf("default build wrong: %+v", m.Build)
	}
	if len(m.Workers) != 0 {
		t.Errorf("default has no workers, got %d", len(m.Workers))
	}
	if !m.isDefaultWeb(testDefaults) {
		t.Error("absent manifest should count as default web")
	}
}

// A partial web manifest fills missing fields from defaults.
func TestManifestWebPartialDefaults(t *testing.T) {
	dir := writeManifest(t, "version: 1\nweb:\n  port: 8000\n")
	m, err := LoadManifest(dir, testDefaults)
	if err != nil {
		t.Fatal(err)
	}
	if m.Web.Port != 8000 {
		t.Errorf("port = %d; want 8000", m.Web.Port)
	}
	if m.Web.Command != testDefaults.WebCommand || m.Web.HealthPath != "/" {
		t.Errorf("missing web fields not defaulted: %+v", m.Web)
	}
	if m.isDefaultWeb(testDefaults) {
		t.Error("custom port must not count as default web")
	}
}

// Custom command + health path + build + a worker.
func TestManifestFullCustom(t *testing.T) {
	dir := writeManifest(t, `
version: 1
web:
  command: "python -m http.server 5000"
  port: 5000
  health_path: "/healthz"
build:
  command: "make build"
  timeout_seconds: 300
workers:
  - name: queue
    command: "python worker.py"
  - command: "python cron.py"
`)
	m, err := LoadManifest(dir, testDefaults)
	if err != nil {
		t.Fatal(err)
	}
	if m.Web.Command != "python -m http.server 5000" || m.Web.Port != 5000 || m.Web.HealthPath != "/healthz" {
		t.Errorf("web wrong: %+v", m.Web)
	}
	if m.Build.Command != "make build" || m.Build.TimeoutSeconds != 300 {
		t.Errorf("build wrong: %+v", m.Build)
	}
	if len(m.Workers) != 2 || m.Workers[0].Name != "queue" || m.Workers[1].Name != "worker-2" {
		t.Errorf("workers wrong: %+v", m.Workers)
	}
}

// Workers and no web => worker-only app (no preview).
func TestManifestWorkerOnly(t *testing.T) {
	dir := writeManifest(t, "version: 1\nworkers:\n  - name: q\n    command: \"node w.js\"\n")
	m, err := LoadManifest(dir, testDefaults)
	if err != nil {
		t.Fatal(err)
	}
	if m.Web != nil {
		t.Errorf("worker-only app must have no web process, got %+v", m.Web)
	}
	if len(m.Workers) != 1 {
		t.Fatalf("want 1 worker, got %d", len(m.Workers))
	}
	// Build still defaults (a worker app may still be build-checked).
	if m.Build.Command != "pnpm build" {
		t.Errorf("build default missing: %+v", m.Build)
	}
}

// An empty manifest file is treated as the default web app (a stray/empty
// file must not silently disable the preview).
func TestManifestEmptyIsDefaultWeb(t *testing.T) {
	dir := writeManifest(t, "\n")
	m, err := LoadManifest(dir, testDefaults)
	if err != nil {
		t.Fatal(err)
	}
	if m.Web == nil || m.Web.Port != 3000 {
		t.Errorf("empty manifest should be default web, got %+v", m.Web)
	}
}

// Invalid YAML returns an error AND a safe default (caller falls back).
func TestManifestInvalidFallsBack(t *testing.T) {
	dir := writeManifest(t, "web: [this is not valid: a map")
	m, err := LoadManifest(dir, testDefaults)
	if err == nil {
		t.Error("expected a parse error for invalid YAML")
	}
	if m == nil || m.Web == nil {
		t.Fatal("invalid manifest must still yield a safe default")
	}
}
