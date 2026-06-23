package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sandboxd/control-plane/internal/runtime"
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
	if m.buildCommand() != "pnpm build" || m.Build.TimeoutSeconds != 120 {
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
  - name: cron
    command: "python cron.py"
`)
	m, err := LoadManifest(dir, testDefaults)
	if err != nil {
		t.Fatal(err)
	}
	if m.Web.Command != "python -m http.server 5000" || m.Web.Port != 5000 || m.Web.HealthPath != "/healthz" {
		t.Errorf("web wrong: %+v", m.Web)
	}
	if m.buildCommand() != "make build" || m.Build.TimeoutSeconds != 300 {
		t.Errorf("build wrong: %+v", m.Build)
	}
	if len(m.Workers) != 2 || m.Workers[0].Name != "queue" || m.Workers[1].Name != "cron" {
		t.Errorf("workers wrong: %+v", m.Workers)
	}
}

// Build config is never nil — even an absent manifest, a worker-only app, or a
// rejected manifest yields a non-nil Build (so the post-task build check can't
// panic on a.build.Command).
func TestManifestBuildNeverNil(t *testing.T) {
	cases := map[string]string{
		"absent":      "",
		"worker-only": "version: 1\nworkers:\n  - name: w\n    command: \"node w.js\"\n",
		"invalid":     "web: [bad: map",
	}
	for name, body := range cases {
		var dir string
		if body == "" {
			dir = t.TempDir()
		} else {
			dir = writeManifest(t, body)
		}
		m, _ := LoadManifest(dir, testDefaults) // err ignored: must still return a manifest
		if m == nil || m.Build == nil {
			t.Errorf("%s: Build must be non-nil", name)
		}
	}
}

// Invalid worker names are rejected; the manifest falls back to the safe
// default (no workers, default web) so a bad name can never reach a log path.
func TestManifestInvalidWorkerNamesRejected(t *testing.T) {
	bad := []string{
		"workers:\n  - name: \"../escape\"\n    command: x",
		"workers:\n  - name: \"a/b\"\n    command: x",
		"workers:\n  - name: \"..\"\n    command: x",
		"workers:\n  - name: \"\"\n    command: x", // empty
		"workers:\n  - name: \"has space\"\n    command: x",
		"workers:\n  - name: ok\n    command: \"\"", // empty command
	}
	for _, body := range bad {
		dir := writeManifest(t, "version: 1\n"+body+"\n")
		m, err := LoadManifest(dir, testDefaults)
		if err == nil {
			t.Errorf("expected rejection for: %q", body)
		}
		// Fallback is the safe default: a web app with no workers.
		if m.Web == nil || len(m.Workers) != 0 {
			t.Errorf("rejected manifest should fall back to default web/no-workers, got %+v", m)
		}
	}
}

// Duplicate worker names are rejected (they'd collide on the log path).
func TestManifestDuplicateWorkerNames(t *testing.T) {
	dir := writeManifest(t, "version: 1\nworkers:\n  - name: w\n    command: a\n  - name: w\n    command: b\n")
	if _, err := LoadManifest(dir, testDefaults); err == nil {
		t.Error("expected rejection of duplicate worker names")
	}
}

// A worker-only app (web == nil) reports PreviewNone and runs no preview
// probe — and neither status() nor probe() panics with no web process.
func TestWorkerOnlyAppStatusAndProbe(t *testing.T) {
	a := &app{bootedAt: time.Now()} // web nil, no workers
	a.probe()                       // worker-only: must return without touching a web process
	st := a.status()
	if st.Preview.Status != runtime.PreviewNone {
		t.Errorf("worker-only preview status = %q; want %q", st.Preview.Status, runtime.PreviewNone)
	}
	if len(st.Processes) != 0 {
		t.Errorf("expected no processes, got %d", len(st.Processes))
	}
}

// Explicitly out-of-range web ports are rejected. (port: 0 is the YAML int
// zero value = "unset", so it defaults to the standard 3000 rather than being
// an error — there's no way to tell an explicit 0 from an absent field.)
func TestManifestInvalidPort(t *testing.T) {
	for _, p := range []string{"70000", "-1"} {
		dir := writeManifest(t, "version: 1\nweb:\n  port: "+p+"\n")
		if _, err := LoadManifest(dir, testDefaults); err == nil {
			t.Errorf("expected rejection of port %s", p)
		}
	}
	// In-range is accepted.
	dir := writeManifest(t, "version: 1\nweb:\n  port: 65535\n")
	if _, err := LoadManifest(dir, testDefaults); err != nil {
		t.Errorf("port 65535 should be valid: %v", err)
	}
	// port: 0 means "unset" -> defaults to 3000, not an error.
	dir = writeManifest(t, "version: 1\nweb:\n  port: 0\n")
	m, err := LoadManifest(dir, testDefaults)
	if err != nil || m.Web.Port != 3000 {
		t.Errorf("port 0 should default to 3000, got port=%d err=%v", m.Web.Port, err)
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
	// A worker manifest with NO build block still defaults (back-compat); a
	// preset that wants to skip sets build.command: "" explicitly.
	if m.buildCommand() != "pnpm build" {
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

// Build-check resolution: unset vs explicit-empty vs explicit command.
// A preset must be able to DISABLE the build check with build.command: "".
func TestManifestBuildResolution(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want string // resolved build command; "" means skip
	}{
		{"absent build block -> default", "version: 1\nweb:\n  port: 3000\n", "pnpm build"},
		{"empty build block {} -> default", "version: 1\nweb:\n  port: 3000\nbuild: {}\n", "pnpm build"},
		{"explicit empty command -> skip", "version: 1\nweb:\n  port: 3000\nbuild:\n  command: \"\"\n", ""},
		{"explicit command -> run it", "version: 1\nweb:\n  port: 3000\nbuild:\n  command: \"make ci\"\n", "make ci"},
	}
	for _, c := range cases {
		dir := writeManifest(t, c.yaml)
		m, err := LoadManifest(dir, testDefaults)
		if err != nil {
			t.Errorf("%s: load: %v", c.name, err)
			continue
		}
		if got := m.buildCommand(); got != c.want {
			t.Errorf("%s: buildCommand()=%q want %q", c.name, got, c.want)
		}
	}
}
