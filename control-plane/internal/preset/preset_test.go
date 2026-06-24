package preset

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The Next.js preset must not run a build check that poisons `next dev`:
// `next build` and `next dev` share the same .next/ dir, so a post-task
// `pnpm build` 500s the live dev server. build must be empty, and the web
// command must clean .next before starting dev (defends against a stale
// build carried in by a snapshot restore).
func TestNextjsPresetNoBuildPoisoning(t *testing.T) {
	p, ok := Get("nextjs")
	if !ok {
		t.Fatal("nextjs preset missing")
	}
	if strings.Contains(p.Manifest, "pnpm build") {
		t.Error("nextjs manifest still runs `pnpm build` — would poison next dev")
	}
	// build.command must be empty.
	if !strings.Contains(p.Manifest, "build:\n  command: \"\"") {
		t.Errorf("nextjs build.command should be empty:\n%s", p.Manifest)
	}
	if !strings.Contains(p.Manifest, "rm -rf .next") {
		t.Error("nextjs web command should `rm -rf .next` before dev")
	}
	// The web process must restart after a task so an agent-run `next build`
	// can't leave the live `next dev` poisoned.
	if !strings.Contains(p.Manifest, "restart_after_task: true") {
		t.Error("nextjs preset should set web.restart_after_task: true")
	}
}

// node-express bounces the web process after each task (Node has no live reload).
func TestNodeExpressRestartsAfterTask(t *testing.T) {
	p, _ := Get("node-express")
	if !strings.Contains(p.Manifest, "restart_after_task: true") {
		t.Errorf("node-express web should set restart_after_task: true:\n%s", p.Manifest)
	}
}

// worker preset ships an editable worker.sh and restarts the worker after each
// task so code edits take effect without a manual restart.
func TestWorkerPresetRestartsAfterTask(t *testing.T) {
	p, _ := Get("worker")
	if p.Template != "worker-standard" {
		t.Errorf("worker preset should use the worker-standard template, got %q", p.Template)
	}
	if !strings.Contains(p.Manifest, "bash worker.sh") {
		t.Error("worker preset should run an editable script (bash worker.sh)")
	}
	if !strings.Contains(p.Manifest, "restart_after_task: true") {
		t.Error("worker preset should set restart_after_task: true on the worker")
	}
}

// Reload-by-restart is opt-in: React/Vite and FastAPI must NOT restart after a
// task (Vite + uvicorn --reload handle live reload), and Next.js keeps its
// restart behavior unchanged.
func TestRestartAfterTaskScoping(t *testing.T) {
	for _, id := range []string{"react-vite", "fastapi"} {
		p, _ := Get(id)
		if strings.Contains(p.Manifest, "restart_after_task") {
			t.Errorf("%s must not use restart_after_task (has live reload):\n%s", id, p.Manifest)
		}
	}
	if nx, _ := Get("nextjs"); !strings.Contains(nx.Manifest, "restart_after_task: true") {
		t.Error("nextjs should still set restart_after_task: true (unchanged)")
	}
}

// FastAPI preset serves on 3000 (the preview port) with --reload, keeps
// /health, and never falls back to a Node build.
func TestFastapiPreset(t *testing.T) {
	p, ok := Get("fastapi")
	if !ok {
		t.Fatal("fastapi preset missing")
	}
	if !strings.Contains(p.Manifest, "port: 3000") {
		t.Errorf("fastapi web.port should be 3000:\n%s", p.Manifest)
	}
	if !strings.Contains(p.Manifest, "--port 3000") {
		t.Error("uvicorn should bind --port 3000 (public preview routes to 3000)")
	}
	if !strings.Contains(p.Manifest, "--reload") {
		t.Error("uvicorn should run with --reload so post-task edits are picked up")
	}
	if !strings.Contains(p.Manifest, `health_path: "/health"`) {
		t.Error("fastapi health_path should be /health")
	}
	if strings.Contains(p.Manifest, "pnpm build") {
		t.Error("fastapi must not fall back to pnpm build")
	}
	if !strings.Contains(p.Manifest, "build:\n  command: \"\"") {
		t.Error("fastapi build should be explicitly skipped")
	}
	if strings.Contains(p.Manifest, "port: 8000") || strings.Contains(p.Manifest, "--port 8000") {
		t.Error("fastapi must not reference the old 8000 port")
	}
}

// The Next.js template ships a .gitignore so node_modules/.next don't become
// checkpoint noise (the workspace git repo relies on the committed .gitignore).
func TestNextjsTemplateHasGitignore(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	// .../control-plane/internal/preset/preset_test.go -> repo root is 3 up.
	repo := filepath.Join(filepath.Dir(thisFile), "..", "..", "..")
	gi := filepath.Join(repo, "image", "templates", "nextjs-standard", ".gitignore")
	data, err := os.ReadFile(gi)
	if err != nil {
		t.Fatalf("nextjs template .gitignore missing: %v", err)
	}
	for _, want := range []string{"node_modules", ".next", "out", ".env", ".env.local"} {
		if !strings.Contains(string(data), want) {
			t.Errorf(".gitignore missing %q", want)
		}
	}
}
