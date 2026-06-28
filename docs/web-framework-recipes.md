# Web framework recipes (`sandbox.yaml`)

Tested, copy-paste `sandbox.yaml` manifests for common JavaScript web frameworks,
maintained as a small **embedded recipe registry** in
[`control-plane/internal/recipes/data/`](../control-plane/internal/recipes/data).

sandboxd previews any web process that **binds `0.0.0.0` and listens on port
`3000`** (the port the preview routes to). Most framework dev servers default to
`localhost` and/or a non-3000 port, so a Git-imported app with no matching preset
needs a small `sandbox.yaml`. The registry is **advisory data** — `runtime-inspect`
detects the framework and *suggests* the recipe; nothing is auto-applied or
written. You Copy YAML / Ask agent and adopt it yourself.

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

- **No secrets** — recipes are public data; never embed tokens/keys.
- **No databases / services / Docker Compose** — a recipe configures one web
  process; it does not provision infra.
- **No executable scripts/hooks** — a recipe is declarative `sandbox.yaml` text +
  notes. Core never runs it; the agent or user applies it.
- **No auto-apply** — `runtime-inspect` suggests; the console offers Copy YAML /
  Ask agent. There is no write/PUT endpoint.

## Maintainers — candidate presets

`runtime-inspect` detects all ten, but only Next.js + Vite-React are runnable
presets. Gatsby, Vue, Nuxt, SvelteKit, and Eleventy work via the recipes above and
are clean candidates for built-in presets (keyed on their package.json dep) if the
image/template cost is justified later.
