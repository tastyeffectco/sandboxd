// Package preset is the shared runtime-preset registry: the single source of
// truth for "template + sandbox.yaml + required image capabilities" bundles
// that let a user create a working app of a common type. Imported by both the
// control plane (to validate/store a runtime_preset and list presets) and
// runtimed (to apply the preset's template + manifest on first boot).
//
// Product model: base image = tools available; template = starter files;
// sandbox.yaml = how to run it; preset = template + manifest + capabilities.
package preset

import "sort"

// Preset is one create-time bundle.
type Preset struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description"`
	// Template is the baked /opt/templates/<name> seeded into an empty
	// workspace; "" means no starter files (e.g. worker-only).
	Template string `json:"template"`
	// Manifest is the sandbox.yaml written on first boot when none exists.
	Manifest string `json:"-"`
	// Capabilities the base image must provide for this preset to boot. For
	// now this is metadata (for docs/tests), not enforced at create time.
	Capabilities []string `json:"capabilities"`
}

// order fixes the display order of List().
var order = []string{"react-vite", "nextjs", "node-express", "fastapi", "worker"}

var registry = map[string]Preset{
	"react-vite": {
		ID: "react-vite", Label: "React / Vite",
		Description: "React + Vite single-page app with hot reload.",
		Template:    "react-standard",
		// QA: pin --port 3000 (Vite defaults to 5173) and guard the install on the
		// vite binary, not the dir (an interrupted install leaves node_modules/
		// without .bin/ and a -d check would then skip reinstall forever). The
		// react-standard template sets server.allowedHosts for the preview host.
		Manifest: `version: 1
web:
  command: "[ -x node_modules/.bin/vite ] || pnpm install; pnpm exec vite --host 0.0.0.0 --port 3000"
  port: 3000
  health_path: "/"
build:
  command: "pnpm build"
`,
		Capabilities: []string{"node", "pnpm"},
	},
	"nextjs": {
		ID: "nextjs", Label: "Next.js",
		Description: "Next.js app (App Router).",
		Template:    "nextjs-standard",
		// `rm -rf .next` before dev so a stale/production build (from a snapshot
		// restore or a manual `next build`) can't poison `next dev` with
		// missing _next/static chunks. build is intentionally EMPTY: `next build`
		// writes the SAME .next/ that the long-running `next dev` serves from, so
		// a post-task build check would 500 the live dev server. Skipped until an
		// isolated build check exists (see docs/sandbox-manifest.md follow-ups).
		Manifest: `version: 1
web:
  command: "[ -x node_modules/.bin/next ] || pnpm install; rm -rf .next; pnpm dev --hostname 0.0.0.0"
  port: 3000
  health_path: "/"
  restart_after_task: true
build:
  command: ""
`,
		Capabilities: []string{"node", "pnpm"},
	},
	"node-express": {
		ID: "node-express", Label: "Node / Express API",
		Description: "Node.js REST API with Express. Health at /health.",
		Template:    "node-express-standard",
		// restart_after_task: `node server.js` has no live reload, so the web
		// process is bounced after each task to pick up route/code changes.
		Manifest: `version: 1
web:
  command: "[ -d node_modules ] || pnpm install; node server.js"
  port: 3000
  health_path: "/health"
  restart_after_task: true
build:
  command: ""
`,
		Capabilities: []string{"node"},
	},
	"fastapi": {
		ID: "fastapi", Label: "Python / FastAPI",
		Description: "Python REST API with FastAPI + uvicorn. Health at /health.",
		Template:    "fastapi-standard",
		// Serve on 3000 (the port the public preview routes to — 8000 would
		// 502 externally) and run with --reload so edits made by a coding task
		// are picked up live (watchfiles, from the template's requirements).
		Manifest: `version: 1
web:
  command: "[ -d .venv ] || (python3 -m venv .venv && .venv/bin/pip install -q -r requirements.txt); .venv/bin/uvicorn main:app --host 0.0.0.0 --port 3000 --reload"
  port: 3000
  health_path: "/health"
build:
  command: ""
`,
		Capabilities: []string{"python3", "python3-venv"},
	},
	"worker": {
		ID: "worker", Label: "Worker (no public endpoint)",
		Description: "A background worker process with no web preview.",
		Template:    "worker-standard", // ships an editable worker.sh
		// build.command "" explicitly skips the post-task build check — a worker
		// has no web build to verify. restart_after_task bounces the worker after
		// each task so edits to worker.sh take effect without a manual restart.
		Manifest: `version: 1
build:
  command: ""
workers:
  - name: worker
    command: "bash worker.sh"
    restart_after_task: true
`,
		Capabilities: nil,
	},
}

// Get returns a preset by id.
func Get(id string) (Preset, bool) {
	p, ok := registry[id]
	return p, ok
}

// Valid reports whether id is a known preset.
func Valid(id string) bool {
	_, ok := registry[id]
	return ok
}

// List returns the presets in display order.
func List() []Preset {
	out := make([]Preset, 0, len(registry))
	for _, id := range order {
		if p, ok := registry[id]; ok {
			out = append(out, p)
		}
	}
	// Any preset not in `order` (shouldn't happen) appended sorted.
	if len(out) != len(registry) {
		seen := map[string]bool{}
		for _, p := range out {
			seen[p.ID] = true
		}
		var extra []string
		for id := range registry {
			if !seen[id] {
				extra = append(extra, id)
			}
		}
		sort.Strings(extra)
		for _, id := range extra {
			out = append(out, registry[id])
		}
	}
	return out
}
