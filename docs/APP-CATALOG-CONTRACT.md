# App Catalog Contract (v1)

**Goal.** Let the console (or any client) install curated open-source apps into sandboxd with one click —
**without adding a single endpoint or concept to the core engine.** The catalog is a *data layer* that
speaks only the public `/v1` API. Core stays a generic "run a manifest in a hardened sandbox behind a
preview" engine; the open-source-apps catalog is one use case among many.

Validated at scale first: ~150 catalog apps were live-verified through this exact surface
(`qa-reports/selfhosted/WHAT-WORKS.md`, `CHUNK-01..25.md`).

> **Two products.** The **console UI** (which hosts the catalog) and **sandboxd core** are separate
> products, expected to live in separate repos. This contract is the seam between them: the catalog is
> console/client-side data over the public `/v1` API and **changes nothing in core**. Read it alongside the
> core docs it builds on — it does not restate or override them:
> - [`base-image.md`](base-image.md) — the base-image / custom-image contract, native-language stance, and
>   the **image-profiles + app-level image-selection** roadmap the deferred families depend on.
> - [`sandbox-manifest.md`](sandbox-manifest.md) — the `sandbox.yaml` schema (`version:1` / `web` / `build`
>   / `workers`) every recipe emits, and the "the agent can write the manifest like any workspace file" rule
>   this whole approach relies on.
> - [`ARCHITECTURE.md`](../ARCHITECTURE.md) §"Optional workflows on top of core" — the general principle that
>   apps/agents/git/catalog are optional `/v1`-client layers, not core.
> - [`adr/0001-app-catalog-scope-and-runtimes.md`](adr/0001-app-catalog-scope-and-runtimes.md) — the accepted
>   decision record for scope, runtimes, and images.

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

## 10. Catalog recipes vs core presets

These are two different things and must not be conflated:

| | **Core runtime preset** | **Catalog recipe** |
|---|---|---|
| What | A Go/runtime template bundle — template files + a generated `sandbox.yaml` + capabilities. | Console/client **data** (a `CatalogRecipe` object: `catalog-run.sh` + a standard `sandbox.yaml`). |
| Lives in | **sandboxd core** (`internal/preset`), compiled into the binary; surfaced by `GET /v1/presets` and the `runtime_preset` create field. See [`base-image.md`](base-image.md) and [`sandbox-manifest.md`](sandbox-manifest.md) §7C-1. | The **console** (`console/src/catalog*.ts`), shipped with the UI. |
| Added by | Changing **core** (recompile). | Adding a data object — **no core change**. |
| Applied via | Create-time preset selection. | `PUT /v1/sandboxes/{id}/files` writing `sandbox.yaml` + `catalog-run.sh`, then restart (contract §4). |

A catalog recipe is effectively a **preset-as-client-data**: it produces the same artifact (a valid
`sandbox.yaml`, per the manifest schema) but is written by a `/v1` client instead of registered in core. This
is explicitly sanctioned — `sandbox-manifest.md` states *"the coding agent can write or edit `sandbox.yaml`
like any workspace file."* **The catalog therefore does not add, modify, or depend on any core preset, and
changes nothing in sandboxd core.** If a recipe ever needs to become a first-class, core-blessed preset, that
is a core change and out of scope for the catalog.

## 11. v1 catalog scope

v1 ships **only apps that run on the stock base image with no foreign runtime**, so the catalog needs exactly
one image and zero core changes:

- **Node apps** — `npm`/`pnpm`/`bun` on the base image's Node.
- **Python apps** — `uv venv` + `uv pip` on the base image's Python.
- **Static-binary apps** — download one pinned binary, `chmod`, run (no runtime at all — the simplest class).
- **One base image** (`sandboxd-base`); no per-app image selection (core doesn't offer it — see
  [`base-image.md`](base-image.md)).
- **No Java / PHP / .NET / Deno / Ruby** in the v1 catalog.

Runtime classification is derived from each recipe (`classifyRuntime`); only `node | python | binary` are
surfaced by the store. (The exact shipped count tracks the code; other classes are shelved — see §12.)

## 12. Deferred runtime families

**Java, PHP, .NET, Deno, and Ruby are deferred out of the v1 catalog.** Per [`base-image.md`](base-image.md),
native languages are not in the base and are meant to run via an **operator-scoped image**, with **image
profiles + app-level (allowlisted) image selection** on the roadmap. Until that core capability exists, these
families have no first-class path and are kept shelved (`CATALOG_DEFERRED`), not surfaced.

**The runtime-drop technique used during the QA sweep** — a recipe curling a foreign runtime (portable JRE,
static-php, self-contained .NET, deno binary) into the workspace — was a **QA/discovery bridge to map
feasibility, not a permanent supported architecture.** It trades determinism, supply-chain verification,
disk, and boot-time for zero-core convenience, and it circumvents `base-image.md`'s "native langs need a
custom image" stance. When the core `image:` field / image profiles land, these families move to
**pinned, verified, shared runtime images selected per app from the allowlist** — not runtime-drop.

## 13. Known follow-ups (not fixed here)

- **Wake cannot recreate a *removed* container.** Wake does `docker start <container>`; if the container was
  removed (host `docker prune`, GC, reboot cleanup) it loops `docker start failed` forever behind the
  "Spinning up…" interstitial — the sandbox is bricked though its workspace + DB row are intact. Observed in
  catalog QA. Belongs with the core wake follow-ups (cf. `sandbox-manifest.md` "Remaining non-blocking
  follow-ups"); **documented, not fixed.**
- Also see §6 (core asks) — none block the v1 catalog, which ships on the current API.
