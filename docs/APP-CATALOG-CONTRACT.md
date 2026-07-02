# App Catalog Contract (v1)

**Goal.** Let the console (or any client) install curated open-source apps into sandboxd with one click —
**without adding a single endpoint or concept to the core engine.** The catalog is a *data layer* that
speaks only the public `/v1` API. Core stays a generic "run a manifest in a hardened sandbox behind a
preview" engine; the open-source-apps catalog is one use case among many.

Validated at scale first: ~150 catalog apps were live-verified through this exact surface
(`qa-reports/selfhosted/WHAT-WORKS.md`, `CHUNK-01..25.md`).

## 1. Layering

```
Console UI  ──┐                  (any client)
              ├──> Catalog layer:  recipes (data) + runner (pure /v1 client)
Other UIs  ───┘
                        │  only public /v1 calls
                        ▼
              sandboxd CORE  (unchanged: apps / sandboxes / files / start / preview)
```

- Core never learns app names. Deleting the catalog leaves core untouched.
- Recipes execute *inside* the sandbox — the same trust boundary as user code today. No new attack surface.
- The two projects can be split later along this exact line (core repo / console+catalog repo).

## 2. The recipe

A recipe is **two files written into the sandbox workspace** plus display metadata:

```ts
interface CatalogRecipe {
  id: string            // 'gitea'
  name: string          // 'Gitea'
  blurb: string         // one-liner for the card
  category: 'dev' | 'productivity' | 'media' | 'data' | 'network' | 'ai' | 'other'
  effort: 'instant' | 'quick' | 'build'   // binary ≈ seconds / pip+npm ≈ 1 min / clone+build ≈ minutes
  script: string        // catalog-run.sh — self-bootstrapping install+run (see §3)
  healthPath: string    // manifest web.health_path — MUST be a 200 route
  entryPath?: string    // UI path if not '/', e.g. '/login' (readeck), '/_/admin/' (trailbase)
  note?: string         // login hints, quirks
}
```

### 3. `catalog-run.sh` — the self-bootstrapping run script (the whole trick)

Install and run live in ONE process command, so the only core features used are
`PUT /v1/sandboxes/{id}/files` and stop/start. Scripts MUST be idempotent (guard file), bind
`0.0.0.0:3000`, keep state in the workspace, and use `TMPDIR` in the workspace (noexec `/tmp`).

```bash
#!/bin/bash
set -e
cd /home/sandbox/workspace/app
export TMPDIR=/home/sandbox/workspace/tmp; mkdir -p "$TMPDIR"
if [ ! -f .catalog-installed ]; then
  # --- install (runs once; resumable on retry) ---
  curl -sL <arm64 release url> -o app.tgz && tar xzf app.tgz && chmod +x <bin>
  touch .catalog-installed
fi
exec ./<bin> --host 0.0.0.0 --port 3000
```

And the manifest the runner writes:

```yaml
version: 1
web:
  command: "exec bash /home/sandbox/workspace/app/catalog-run.sh"
  port: 3000
  health_path: "<healthPath>"
build:
  command: ""
```

### 4. The runner sequence (pure `/v1`)

```
1. POST /v1/apps                      {name: 'gitea', tags: ['catalog','catalog:gitea']}
2. POST /v1/apps/{id}/sandbox         → sandboxId
3. PUT  /v1/sandboxes/{id}/files?path=workspace/app/catalog-run.sh   (recipe.script)
4. PUT  /v1/sandboxes/{id}/files?path=workspace/app/sandbox.yaml     (manifest above)
5. POST /v1/sandboxes/{id}/stop  → poll status=stopped → POST /start   (adopt manifest, evict template)
6. Poll GET /v1/sandboxes/{id} until status=running & web process running & preview.status healthy
7. Done → open preview.url + entryPath
```

Step 5 matters: until the manifest is adopted, the **template dev server holds :3000** and answers 200
with a plausible page — clients must never read that as success (QA finding #2).

## 5. Rules for recipe authors (distilled from 25 QA chunks)

1. **Idempotent installs** with a guard file; every step must tolerate re-run (retry = restart).
2. **Bind `0.0.0.0:3000`.** If the app's port is not configurable (calibre-web 8083, cloudreve 5212),
   run the app + a tiny TCP bridge in the script (see CHUNK-18 recipe).
3. **`health_path` must return 200** (readeck `/login`, superset `/login/`, martin `/catalog`).
4. **Prefer musl/static arm64 assets**; never assume the binary name matches the repo (`trail`, `linux-arm64`).
5. **Validate downloads** (GitHub LFS/raw can return HTML error pages with code 200).
6. Pin versions where "latest" is broken (n8n@1.68.1, deno 1.46 for silverbullet).
7. Python: `uv venv` (3.12/3.13 per `requires-python`); pre-add `setuptools`+`cachetools`.
8. Node: honor `only-allow` PM guards via corepack; `npm i -g --prefix` inside workspace for CLI apps.
9. SQLite/on-disk state only; apps needing managed DBs are out of catalog scope (Tier C).

## 6. What we ask of core (5 generic fixes — none catalog-specific)

| Ask | Why (QA finding) |
|---|---|
| Wake must **recreate** missing containers | host GC bricks idle sandboxes behind an infinite spinner (critical) |
| Boot-grace / activity-aware idle-reaper | slow-boot apps (Java/.NET/builds) reaped mid-boot — #1 failure cause |
| Manifest v2: optional `image:` (allowlisted), `env:`, configurable upstream port | unlocks Ruby image, kills TCP-bridge hacks, cleaner recipes |
| Blank/no-template sandbox option | template squats :3000 and fakes success during installs |
| `DELETE /v1/apps/{id}` + per-sandbox disk quota | catalog lifecycle + the 4.6 GB node_modules lesson |

None block v1: the catalog ships today on the current API.

## 7. Console UI (v1 scope)

- New **App Store** view: card grid (name, blurb, category, effort badge), one-click **Install**.
- Progress: creating → provisioning → writing recipe → restarting → waiting for health → **Open app**.
- Installed catalog apps are ordinary apps (tagged `catalog:<id>`) — full existing AppDetail experience.
- Catalog data lives in `console/src/catalog.ts` (v1). Later: served from a recipes repo/registry endpoint
  when the projects split.

## 8. Base images — operator-curated, never user-built

Decision (validated by the QA sweep): **ship a small official image set; users do not build images.**
User-built images would break the hardened-container trust model and turn the catalog into a Docker
registry. The sweep proved images are barely needed anyway:

| Image | Covers | Status |
|---|---|---|
| `sandboxd-base` | ~90% of the catalog — all Go/Rust binaries, Node, Python, Bun, Deno, static-PHP, portable-JRE, self-contained .NET (the "runtime-drop" pattern) | today's default; every v1 recipe runs on it |
| `sandboxd-ruby` | Rails-family (redmine, docuseal, fizzy, sessy) — native gem compilation needs headers/toolchain | built & verified; blocked on the core `image:` field |
| `sandboxd-media` (future) | chromium/libreoffice/ffmpeg class (gotenberg, browserless, fileflows-full) | optional, add on demand |

Mechanics: recipes carry `image?: string`; the store shows a "requires <image>" badge and refuses to
install when the platform doesn't advertise it (via `/v1/settings` capabilities). Core's only job is the
generic manifest `image:` field validated against an operator allowlist (`SANDBOXD_IMAGES`). Recipe
authors must always prefer a base-image path (portable runtime) before requesting an image.

## 9. Agent tasks on installed apps

Every install writes `workspace/app/AGENTS.md` describing the app, its supervision (`sandbox.yaml` →
`catalog-run.sh`), and the modification model, so `POST /tasks` agents land with context:

- `modifiable: 'source'` (clone-based recipes — dashy, homepage, metube…): agents can edit the app's own
  code, rebuild, restart — the full sandboxd experience.
- `modifiable: 'config'` (release binaries/dists): agents modify configuration files, `catalog-run.sh`
  flags/env, plugins and data — not the app's code. Recipes should name the exact config files in
  `agentNotes` (e.g. glance → `glance.yml`, garage → `garage.toml`).
