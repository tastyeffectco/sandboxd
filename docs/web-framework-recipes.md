# Web framework recipes (`sandbox.yaml`)

Tested, copy-paste `sandbox.yaml` manifests for common JavaScript web frameworks,
maintained as a small **embedded recipe registry** in
[`control-plane/internal/recipes/data/`](../control-plane/internal/recipes/data).

**Runtime recipes standardize on port `3000`.** A custom manifest can declare
another `web.port`, and sandboxd routes the **declared** preview port — the recipes
here use `3000` so they're uniform and copy-paste-ready. The web process must bind
`0.0.0.0` (not `localhost`) so the preview can reach it.

The registry is **advisory data**: `runtime-inspect` detects the framework and
*suggests* the recipe — sandboxd **does not** write it into an imported repo or
rewrite framework config. You adopt it explicitly (Copy YAML / Ask agent / manual
edit), then **restart the sandbox** so runtimed re-reads the new `sandbox.yaml`.

> **Presets vs imports.** When you create an app from a **sandboxd preset**,
> sandboxd writes the runtime files needed to run it immediately (that's the
> point of a preset). For a **Git-imported repo**, detection is advisory — the
> **core** never writes anything. Note that a **client** may act on the
> suggestion: the console auto-applies the detected `sandbox.yaml` on import (and
> writes a per-app `AGENTS.md`), always with an **Undo** and never overwriting a
> repo's existing files. The distinction that matters for the contract: the
> *engine* mutates nothing; a *client* choosing to apply a suggestion is a client
> decision over `/v1`.

See [`sandbox-manifest.md`](./sandbox-manifest.md) for the full schema and
[`git-workflow.md`](./git-workflow.md) for the import flow.

## Recipe ≠ preset

- A **preset** scaffolds a *new* app: starter files baked into an image template +
  Go code + capabilities. Heavyweight; only a handful exist (`react-vite`,
  `nextjs`, `node-express`, `fastapi`, `worker`).
- A **recipe** just configures an *existing* (imported) repo: a `sandbox.yaml` text
  + a detect rule + notes. **Pure data, never executed, never written by core.**

Adding a framework is therefore a data-only change — one YAML file, no core code.

## Three rules that cover every framework

1. **Bind `0.0.0.0` and pin port `3000`.** `web.command` must force the dev server
   onto `--host 0.0.0.0` (or the framework's equivalent) **and** `--port 3000`, and
   `web.port` must be `3000`. Frameworks that default elsewhere (Vite → 5173,
   Gatsby → 8000, Eleventy → 8080, Astro → 4321) will otherwise 502 the preview.
2. **Vite dev servers need `allowedHosts`.** Vite (v4.5+, v5, v6) rejects requests
   with an unknown `Host` header (**HTTP 403 "Blocked request. This host is not
   allowed"**). Because the preview host is `*.preview.<ip>.sslip.io`, any
   Vite-based framework needs `server.allowedHosts: true` set **in the framework's
   config file** — it is *not* a CLI flag. (Astro: under
   `vite.server.allowedHosts` in `astro.config.mjs`.)
3. **Guard the install on the binary, not the directory.** Prefer
   `[ -x node_modules/.bin/<bin> ] || pnpm install` over `[ -d node_modules ]`. If a
   first install is interrupted, `node_modules/` exists without `.bin/`, and a
   directory check then skips reinstall forever.

## When the preview doesn't come up

The loop is meant to feel guided and fast, while staying reviewable — sandboxd
guides, you adopt:

1. **Detected stack** — `GET /v1/apps/{id}/runtime-inspect` (console: Runtime panel)
   names the framework.
2. **Current manifest status** — `GET /v1/apps/{id}/runtime/manifest` reports
   `sandbox.yaml` as missing / valid / invalid, plus the effective command/port/health.
3. **Likely issue** — a non-3000 default port, a `localhost`-only bind, or a Vite
   `allowedHosts` 403 (see the three rules above).
4. **Suggested `sandbox.yaml`** — `runtime-inspect` returns `suggested_manifest`.
5. **Config snippet notes** — e.g. Vite/Astro `allowedHosts` belongs in the
   framework config file, not the CLI.
6. **Adopt it explicitly** — Copy YAML, use the **Ask agent** prompt (paste into
   the task box; the agent writes the file), or edit `sandbox.yaml` by hand. There
   is no auto-apply and no hidden source edit. If you write it via the files API
   (`PUT /v1/sandboxes/{id}/files`), note the path is relative to the **workspace
   mount root**, so the app manifest is **`path=workspace/app/sandbox.yaml`** — a
   bare `path=sandbox.yaml` lands in `/home/sandbox` and runtimed never reads it.
7. **Restart the sandbox.** **When `sandbox.yaml` changes, restart the sandbox so
   runtimed re-reads it** — the manifest is read at boot, not live. Then the
   preview should come up green.

After that: `git diff` → `git commit` → `git push` (new branch) → open a PR.

## Dev server vs built node server (SSR meta-frameworks)

Most recipes run a **dev server** (HMR, Vite `allowedHosts`). SSR meta-frameworks
also have a **production "build-then-serve"** shape that's worth knowing:

- **Dev** (default): e.g. `astro dev` / `vite dev` with `--host 0.0.0.0 --port 3000`
  and the Vite `allowedHosts` config edit. HMR; edits are live.
- **Built node server**: `astro build` → `node ./dist/server/entry.mjs` (`HOST`/`PORT`
  env), `restart_after_task: true`. **No `allowedHosts`** — that's a Vite-dev-only
  concern; the built standalone server accepts any `Host`. Needs the framework's
  node adapter (Astro `@astrojs/node`). The `astro-node-server` recipe encodes this.
- **Other build-then-serve frameworks:** **React Router v7+** → `react-router build`
  then `react-router-serve ./build/server/index.js` (a Node SSR server, not static);
  **TanStack Start** → `vite build` emits a *web-standard fetch handler*
  (`dist/server/server.js`), **not** a self-listening server, so it needs a
  deployment adapter (Nitro / node-server → `.output/server/index.mjs`) to listen.
- **Caveat — not every build is a server**, as TanStack shows. The `react-router`,
  `tanstack-start`, and `astro` recipes all stay **dev-runtime oriented** (the
  `astro-node-server` recipe is the one named built-server variant). sandboxd does
  **not** infer production mode and there is no generic built-server abstraction —
  pick the recipe you want.

## Compatibility matrix

| Framework | `runtime-inspect` | `allowedHosts`? | Verified starter |
|---|---|---|---|
| Next.js | `nextjs` preset | no | theodorusclarence/ts-nextjs-tailwind-starter |
| Vite + React | `react-vite` preset | **yes** | SafdarJamal/vite-template-react |
| Astro | recipe (detect-only) | **yes** | astro blog starter |
| Docusaurus | recipe (detect-only) | no | slorber/docusaurus-starter |
| Gatsby | recipe | no | gatsbyjs/gatsby-starter-blog |
| Vue + Vite | recipe | yes (Vite 5+) | micahlt/vite-vue3-simple-starter |
| Nuxt 3 | recipe | no (here) | nuxt/starter @ v3 |
| SvelteKit | recipe | **yes** | mandrasch/sveltekit-demo-app |
| Remix v2 (Vite) | recipe | **yes** | EvanMarie/remix-vite-tailwind-minimal-template |
| Eleventy / 11ty | recipe | no | 11ty/eleventy-base-blog |

`GET /v1/runtime/recipes` returns the full registry; `runtime-inspect` returns the
matching suggestion (with `suggested_manifest`, `config_snippets`, and `notes`).

## How to add a recipe

1. Add `control-plane/internal/recipes/data/<framework>.yaml`:
   ```yaml
   id: <framework>
   display_name: <Name>
   # preset: <preset-id>      # only if a runnable built-in preset backs it
   detect:
     deps: [<package.json dep>]        # ALL must be present
     config_files: [<config file>]     # ANY present matches
     # exclude_deps: [<dep>]           # skip if any present (e.g. generic vite recipes)
   suggested_manifest: |
     version: 1
     web:
       command: "[ -x node_modules/.bin/<bin> ] || pnpm install; pnpm exec <bin> ... --host 0.0.0.0 --port 3000"
       port: 3000
       health_path: "/"
   config_snippets:                    # optional non-sandbox.yaml edits the user must make
     - file: "vite.config.*"
       note: "Add server.allowedHosts: true …"
   notes: []                           # optional tips
   verified:
     starter: <org/repo>
     version: <vX.Y.Z you verified on>
   ```
2. Run `go test ./internal/recipes/`. Every `suggested_manifest` is run through
   core's `manifest.Validate` — a bad recipe **fails CI**. Add a `Match` case if the
   detection is non-obvious.
3. Verify end-to-end against the listed starter (import → preview 200 → edit) and
   record it under `verified`.

## Constraints (what a recipe may NOT contain)

- **No secrets** — recipes are public data; never embed tokens/keys (service
  recipes use placeholder values like `KEY=replace-me`).
- **No managed databases / Docker Compose / external infra** — a recipe configures
  one web process. A self-contained **service with embedded SQLite** (a file in the
  workspace, e.g. n8n / Directus) is fine — that's still one process plus a file,
  not a provisioned DB server. No separate DB container, no Compose.
- **No executable scripts/hooks** — a recipe is declarative `sandbox.yaml` text +
  notes. Core never runs it; the agent or user applies it.
- **Core never auto-applies** — `runtime-inspect` only suggests; the engine has
  no path that writes a detected manifest. A client (e.g. the console) may apply
  a suggestion by PUT-ing the file over `/v1` — that's a client action, with Undo.

## Service recipes (self-contained app + SQLite)

Some recipes run a whole **service** with an embedded SQLite DB — proven for **n8n**
(workflow automation) and **Directus** (headless CMS). They're tagged
`service` / `sqlite_app` / `heavy_install` / `first_boot_slow`: first boot installs a
large dependency tree (n8n ≈ 2.2 GB, multi-minute) and the SQLite file lives in the
workspace (n8n: `~/.n8n/database.sqlite`). These are **dev/sandbox** recipes, not
production deployment — and snapshots can be large. They are auto-detected only when
there's a real signal (e.g. the `n8n` dependency), never for an empty app.

### SQLite CMS/service recipes (deep-tested)

These were verified beyond "200" — admin UI **plus** a real DB write **plus** the API:

| Recipe | Proven | DB | Node 22 note |
|---|---|---|---|
| **Strapi** v5 (`@strapi/strapi`) | admin 200, `POST /admin/register-admin` → super-admin (id 1), `/_health` 204 | `.tmp/data.db` (better-sqlite3, arm64 prebuilt) | health on `/admin` (`/` 302s) |
| **Payload** v3 (`payload`) | admin 200, `POST /api/users/first-register` → user 1, REST **+** GraphQL live | `payload.db` (libsql, 9 tables incl. `payload_migrations`) | bare `next dev` binds localhost → pinned `-H 0.0.0.0` |
| **Ghost** 6 (`ghost`) | front-end 200, `/ghost/` admin 200, Admin API 200 | `content/data/ghost-local.db` (~90 tables, auto-migrated) | **Node-22-only** (needs ^22.18); `sqlite3`+`sharp` compile from source |

**Ghost is advisory, not turnkey:** Ghost 6's local install needs a modern **pnpm
(≥10/11)** (its tarball uses a pnpm catalog that pnpm 9 rejects) and a **writable
global npm prefix** (the command repoints it to `~/.npm-global`); the recipe encodes
the proven sequence but expect to adjust. Strapi/Payload are smoother (the admin
registration writes the DB on first use). All three persist to **app-side SQLite** —
proving sandboxd runs **self-contained app services with embedded SQLite** end-to-end
(admin UI + DB write/persistence + API). Caveats: this is **sandbox/dev state, not
managed production hosting**; **snapshot size grows** with the DB + `node_modules`;
and **external Postgres/MySQL/Redis remain outside this release** (custom image or an
external service).

## Maintainers — candidate presets

`runtime-inspect` detects all ten, but only Next.js + Vite-React are runnable
presets. Gatsby, Vue, Nuxt, SvelteKit, and Eleventy work via the recipes above and
are clean candidates for built-in presets (keyed on their package.json dep) if the
image/template cost is justified later.
