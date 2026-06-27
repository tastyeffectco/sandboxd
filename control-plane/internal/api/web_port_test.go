package api

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sandboxd/control-plane/internal/store"
	"github.com/sandboxd/control-plane/internal/traefik"
)

func TestPresetWebPort(t *testing.T) {
	// Built-in presets all declare 3000 today; unknown/empty default to 3000.
	for _, id := range []string{"nextjs", "react-vite", "fastapi", "node-express"} {
		if got := presetWebPort(id); got != 3000 {
			t.Errorf("presetWebPort(%q) = %d; want 3000", id, got)
		}
	}
	if presetWebPort("") != 3000 || presetWebPort("nope") != 3000 {
		t.Error("empty/unknown preset should default to 3000")
	}
}

func TestWorkspaceWebPort(t *testing.T) {
	dir := t.TempDir()
	// no file -> 0
	if got := workspaceWebPort(dir); got != 0 {
		t.Errorf("no manifest = %d; want 0", got)
	}
	// astro-style manifest declaring 4321 -> 4321
	if err := os.WriteFile(filepath.Join(dir, "sandbox.yaml"),
		[]byte("version: 1\nweb:\n  command: \"astro dev --host 0.0.0.0 --port 4321\"\n  port: 4321\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := workspaceWebPort(dir); got != 4321 {
		t.Errorf("astro manifest = %d; want 4321", got)
	}
}

func TestEnsurePortAndWebPortOf(t *testing.T) {
	if got := ensurePort([]int{3000}, 4321); len(got) != 2 || got[1] != 4321 {
		t.Errorf("ensurePort appended wrong: %v", got)
	}
	if got := ensurePort([]int{3000, 4321}, 4321); len(got) != 2 {
		t.Errorf("ensurePort should not duplicate: %v", got)
	}
	// webPortOf: NULL/0 -> 3000; set -> the value
	if webPortOf(&store.Sandbox{}) != 3000 {
		t.Error("unset web_port should default to 3000")
	}
	sb := &store.Sandbox{}
	sb.WebPort.Valid, sb.WebPort.Int64 = true, 4321
	if webPortOf(sb) != 4321 {
		t.Error("set web_port should be honored")
	}
}

// Read path: a sandbox with web_port 4321 produces a preview URL on 4321.
func TestRuntimeViewUsesResolvedPort(t *testing.T) {
	s := &Server{PreviewDomain: "ex.localhost"}
	prev, _ := s.v1RuntimeView("01ABC", "running", nil, 4321)
	if !strings.Contains(prev.URL, "s-01ABC-4321.preview.ex.localhost") {
		t.Errorf("preview URL = %q; want the 4321 host", prev.URL)
	}
}

// Traefik labels include a router + loadbalancer for the resolved (non-3000) port.
func TestTraefikLabelsForResolvedPort(t *testing.T) {
	labels := traefik.Labels("01ABC", []int{3000, 4321}, "ex.localhost", "public", "web", false)
	joined := strings.Join(labels, "\n")
	for _, want := range []string{
		"s-01ABC-4321.preview.ex.localhost",
		"routers.s-01ABC-4321",
		"services.s-01ABC-4321.loadbalancer.server.port=4321",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("labels missing %q", want)
		}
	}
}
