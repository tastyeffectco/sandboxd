package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sandboxd/control-plane/internal/manifest"
	"github.com/sandboxd/control-plane/internal/preset"
)

// TestPresetManifestParsersAgree guards against drift between the control-plane
// minimal manifest parser (internal/manifest, used to resolve the preview web
// port) and runtimed's fuller parser (this package). For every built-in preset
// that declares an explicit web.port, both must read the SAME port.
//
// The runtimed default WebPort is a sentinel (9999): if runtimed ever stops
// reading the `port:` tag (e.g. a YAML-tag rename), it would silently fall back
// to that default and this test fails loudly — exactly the silent-3000 drift the
// review flagged. Worker-only presets (no explicit web port) are skipped.
//
// Lives in package main because runtimed's parser is unexported (package main)
// and can't be imported elsewhere; a test here needs no refactor.
func TestPresetManifestParsersAgree(t *testing.T) {
	def := Defaults{
		WebCommand:    "true",
		WebPort:       9999, // sentinel: a drifted runtimed parser would surface this
		BuildCommand:  "true",
		BuildTimeoutS: 60,
		WebHealthPath: "/",
	}
	for _, p := range preset.List() {
		cm, err := manifest.Parse([]byte(p.Manifest))
		if err != nil {
			t.Fatalf("%s: internal/manifest parse: %v", p.ID, err)
		}
		cpPort := cm.WebPort()
		if cpPort == 0 {
			continue // worker-only / no explicit web port — nothing to compare
		}

		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, ManifestFile), []byte(p.Manifest), 0o644); err != nil {
			t.Fatal(err)
		}
		rm, err := LoadManifest(dir, def)
		if err != nil {
			t.Fatalf("%s: runtimed LoadManifest: %v", p.ID, err)
		}
		if rm.Web == nil {
			t.Fatalf("%s: runtimed parsed no web process for a preset with web.port=%d", p.ID, cpPort)
		}
		if rm.Web.Port != cpPort {
			t.Errorf("%s: web.port PARSER DRIFT — internal/manifest=%d, runtimed=%d (sentinel default=%d)",
				p.ID, cpPort, rm.Web.Port, def.WebPort)
		}
	}
}
