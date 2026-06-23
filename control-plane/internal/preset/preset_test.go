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
