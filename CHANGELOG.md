# Changelog

All notable changes to sandboxd are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/), and the project follows
[Semantic Versioning](https://semver.org/) (pre-1.0: a minor bump adds features,
a patch is fixes only).

## [Unreleased] — v0.4.9 (branch `feat/v0.4.9-runtime-manifest`, stacked on v0.4.8; NOT in v0.4.0)

> Post-v0.4.0, on a feature branch (depends on v0.4.8); not part of the v0.4.0 launch.

### Added
- **`python-asgi` / `nicegui` / `sanic` recipes (advisory).** `python-asgi`
  (uvicorn; detect `litestar`/`starlette` in requirements) — one recipe for the
  FastAPI/Litestar/Starlette ASGI shape (FastAPI keeps its runnable preset). `nicegui`
  and `sanic` tagged `websocket` (both verified `101` + round-trip through the
  preview); `sanic` notes the required `--single-process` (its worker-manager
  otherwise forks processes the supervisor can't track). All tagged `python`.
- **`tanstack-start` + `astro-node-server` recipes (advisory).** `tanstack-start`
  (detect `@tanstack/react-start`) detects ahead of `react-vite` — which now
  `exclude_deps` it — so an SSR/server meta-framework isn't mislabeled a plain SPA;
  dev-runtime oriented (Vite dev + `allowedHosts`), tagged `ssr`/`meta_framework`/
  `needs_config_snippet`, with a note that `vite build` emits a fetch handler (needs
  a Nitro/node-server adapter), not a self-listening server. `astro-node-server`
  (detect `astro` + `@astrojs/node`) is the **build-then-serve** pattern
  (`astro build` → `node ./dist/server/entry.mjs`, `restart_after_task: true`, **no
  `allowedHosts`** — that's Vite-dev-only), tagged `ssr`/`build_then_serve`; the
  dev-HMR `astro` recipe is unchanged. Docs gained a "dev server vs built node
  server" cookbook section.
- **Strapi / Payload / Ghost SQLite-CMS recipes (advisory, deep-tested).** Each
  verified beyond "200" — admin UI + a real DB write + the API: Strapi v5
  (`@strapi/strapi`, better-sqlite3, `POST /admin/register-admin`), Payload v3
  (`payload`, libsql, `POST /api/users/first-register`, REST + GraphQL), Ghost 6
  (`ghost`, ~90-table SQLite, **Node-22-only**). Tagged
  `service`/`cms`/`sqlite_app`/`heavy_install`/`first_boot_slow`; dep-detected;
  advisory recipes (no preset/template, no managed DB). Ghost is marked
  explicitly advisory (needs pnpm ≥10/11 + a writable global npm prefix). App-side
  SQLite only — sandbox/dev state, not managed production hosting.
- **`n8n` service recipe (advisory).** The QA-proven n8n + SQLite manifest
  (port 3000, `N8N_LISTEN_ADDRESS=0.0.0.0`, `N8N_SECURE_COOKIE=false` for the
  plain-HTTP preview, `DB_TYPE=sqlite`) with a **defensive npm-install retry**
  (clean cache + reinstall on a corrupt extraction). Tagged `service` /
  `sqlite_app` / `heavy_install` / `first_boot_slow`. Detected only via the `n8n`
  dependency — never auto-suggested for an empty app. Advisory recipe, **not** a
  built-in preset; surfaced via `runtime-inspect` + `GET /v1/runtime/recipes`.
- **15 extended-stack recipes** (from QA): flask, django, angular, solidstart, bun,
  nestjs, hono, static, streamlit, gradio, jupyter, storybook, qwik, dash, directus
  — advisory data only (no auto-apply, no source edits). Each has the QA-verified
  manifest (port 3000, `0.0.0.0`), `config_snippets` where needed (Django
  `ALLOWED_HOSTS`, Angular host-check, Solid/Qwik/Storybook Vite `allowedHosts`),
  capability `tags` (`needs_config_snippet`, `heavy_install`, `websocket`, `sse`,
  `sqlite_app`, …), and a verified starter. Surfaced via `runtime-inspect` +
  `GET /v1/runtime/recipes`.
- **Generic Python detection** (`detect.requirements_contains`): recipes match a
  token in `requirements.txt`/`pyproject.toml` (case-insensitive), so Python
  frameworks stay representable as data — no framework-specific Go. Recipe schema
  also gained `tags`.

### Improved
- **Manifest validation requires `version: 1`** (QA footgun fix). A manifest with no
  `version` parsed to an empty `{workers:[]}` and returned `valid:true`; now a missing
  `version` → `valid:false` ("must declare 'version: 1'"), an unsupported `version`
  (e.g. `2`) → `valid:false` ("unsupported version"), and an empty manifest is invalid.
  No `effective` runtime is returned for an invalid manifest (only the as-declared
  `parsed`). This is the strict guidance validator; runtimed's executor stays lenient
  (apps still boot on a bad manifest via defaults) — the doc spells out that split.
- **Unknown top-level keys are now validation errors** (QA `processes:`/`services:`
  footgun). The v1 top-level key set is closed: `processes:` → error (did you mean
  `workers:`/`web:`?), `services:` → error (sandbox.yaml is not Docker Compose),
  top-level `command`/`port`/`health_path` → error (belong under `web:`), any other
  unknown key → error. A `version: 1` manifest with **neither `web:` nor `workers:`**
  is invalid ("nothing to run"); worker-only and web manifests stay valid. runtimed
  now refuses to silently run the default `react-standard` template for a present
  manifest with unrecognized keys — it serves **no preview** and logs
  *"sandbox.yaml is invalid — no web process; no preview will be served"* (empty/stray
  files and worker-only manifests are unaffected). All three surfaces agree.
- **runtime-inspect now agrees with the validator.** `existing_manifest` gained
  `valid` + `errors` from the **same** `manifest.Validate` used by
  `POST /v1/runtime/manifest/validate` and `GET /v1/apps/{id}/runtime/manifest`, so all
  three are consistent (a present-but-versionless manifest is `authoritative:true` —
  it exists and takes precedence — but `valid:false` with the version error). The
  `web_*` summary fields are the as-declared parse (non-authoritative when invalid).
- **Manifest validation response** (`POST /v1/runtime/manifest/validate`): now returns
  a `parsed` web/workers view (as-declared, no defaults) **whenever the YAML parses
  — even for an invalid manifest** — so callers can confirm the web command was
  understood; `effective` still appears only when valid. The **"may bind localhost"
  warning no longer false-positives** on all-interface binders (node/express, Nest,
  Bun, uvicorn-with-host) — it now flags only an explicit `localhost`/`127.0.0.1`
  (applied to both the validate endpoint and the runtime-inspect manifest summary).

### Changed
- **Console "Apply sandbox.yaml" CTA (console-only; no core change).** The Runtime
  panel now offers an explicit **Apply sandbox.yaml** button per suggestion,
  alongside Copy YAML / Ask agent. It is **explicit adoption** (button → inline
  confirm), not auto-apply: it **validates** the manifest (`/runtime/manifest/
  validate`) and only then writes `sandbox.yaml` to the workspace via the existing
  generic `PUT /v1/sandboxes/{id}/files` endpoint — **no new endpoint, no core
  change, no framework-config rewrite** (Apply writes only `sandbox.yaml`; config
  edits like Vite `allowedHosts` still go via Ask agent). After writing it shows a
  **restart-the-sandbox** notice (manifest is read at boot). Disabled until a
  sandbox exists.

### Base image
- **Node bumped 20 → 22** (`sandboxd-base`): removes the `worker_threads.markAsUncloneable`
  crash class (undici 8 and similar libs crashed the dev server on boot under Node 20).
  npm 10 + pnpm 9 + bun retained; Python/uv/build tools unchanged.
- **`python3-setuptools` added** so node-gyp can build native modules (better-sqlite3,
  sqlite3, sharp, …) on **Python 3.13** — stdlib `distutils` was removed in 3.12, and
  the vendored shim from setuptools restores it. Previously these failed on arm64 with
  `ModuleNotFoundError: No module named 'distutils'` unless the module shipped a prebuild.
- **`/opt/verify-base.sh`** ships in the image — a self-check for Node 22, npm/pnpm/bun,
  python3/uv/setuptools(+distutils), and make/gcc/g++/git/curl (`docker run --rm <image>
  bash /opt/verify-base.sh`). Native Go/PHP/Ruby/Rust/Deno/Java stay custom-image/roadmap.

### Hardened
- **Per-app `image` field now rejected with a clear 400** instead of being silently
  dropped. All four create bodies (`POST /v1/apps`, `POST /v1/apps/{id}/sandbox`,
  `POST /v1/sandboxes`, `POST /sandbox`) return `per-app image selection is not
  supported; set SANDBOXD_IMAGE instance-wide`. No per-app image selection is
  introduced — the image stays instance-wide.

### Docs
- **Files API path clarified** ([`docs/openapi.yaml`](docs/openapi.yaml) +
  recipes doc): `PUT /v1/sandboxes/{id}/files` `path` is relative to the **workspace
  mount root**, so the app manifest is `path=workspace/app/sandbox.yaml`; a bare
  `path=sandbox.yaml` writes to `/home/sandbox` where runtimed never reads it. The
  OpenAPI request shape is also corrected (query `path` + raw body, not a JSON body).
  No backend change.
- **Custom base images** ([`docs/base-image.md`](docs/base-image.md)): `SANDBOXD_IMAGE`
  is read once at startup (stack recreate required to change it; it's also the
  snapshot seed); native languages (Go/PHP/Ruby/Rust/Java/.NET/Deno) need a custom
  image; **toolchains must be on the login PATH** because runtimed runs commands with
  `bash -lc` (a Dockerfile `ENV PATH` alone is not enough) — use `/etc/profile.d` and/
  or symlink into `/usr/local/bin`.
- **Runtime model** ([`ARCHITECTURE.md`](ARCHITECTURE.md)): documented the **single
  public preview port** per sandbox (HTTP + WebSocket + SSE all work on it; multi-port
  apps run single-port or proxy in-sandbox; multi-port is roadmap), **embedded SQLite**
  (app-bundled engines; part of workspace/snapshot state; external DBs out-of-scope),
  and confirmed **WebSocket-upgrade + SSE** platform capabilities.

### Fixed
- **Apply CTA wrote `sandbox.yaml` to the wrong place (QA).** The files API roots at
  the workspace **mount** (`/home/sandbox`), so `path=sandbox.yaml` landed at
  `/home/sandbox/sandbox.yaml` (200, but runtimed reads
  `/home/sandbox/workspace/app/sandbox.yaml` — no effect). The console Apply CTA now
  writes `path=workspace/app/sandbox.yaml`, so the manifest lands in the app dir
  (visible in git status, picked up by runtimed after a restart). Comments added at
  the call site and in `v1_files_write.go` so the path isn't "simplified" back.
  Console-only fix (plus a backend doc comment); no endpoint or semantics change.
- **Imported repos no longer get a preset `sandbox.yaml` written into them.** When a
  Git import selected a `runtime_preset`, runtimed wrote the preset's `sandbox.yaml`
  into the cloned repo on first boot — a silent mutation that broke the advisory
  model. `applyPreset` now decides scaffold-vs-import by workspace emptiness **before**
  seeding, and writes the preset manifest **only for an empty/scaffold workspace**
  (the "blank from preset" path). A populated workspace (a Git import or a
  snapshot/fork clone) is left untouched; an existing `sandbox.yaml` is always
  preserved. Imported repos that need a manifest get one via the advisory flow
  (`runtime-inspect` suggestion → Copy YAML / Ask agent), not a silent write.

### Added
- **Web framework recipe registry (advisory).** An embedded, in-repo registry
  (`internal/recipes/data/*.yaml`) of per-framework `sandbox.yaml` guidance for
  imported apps with no matching preset — **data only, never executed/written/auto-
  applied**. Seeded with Next.js, Vite+React, Vue+Vite, Astro, Docusaurus, Gatsby,
  Nuxt 3, SvelteKit, Remix v2, Eleventy. Each recipe has detect rules
  (deps/config_files/exclude_deps), a `suggested_manifest`, optional
  `config_snippets` (e.g. Vite `allowedHosts`) + notes, and a verified starter.
  `runtime-inspect` detection is now **data-driven** from this registry (JS
  frameworks) so adding a framework is a one-file change with no core code; the
  Python/express/worker detections stay code. Every recipe's `suggested_manifest`
  is validated by core's `manifest.Validate` at load time (CI gate). New read-only
  `GET /v1/runtime/recipes` lists the registry. Console Runtime panel renders the
  suggested YAML + `config_snippets`, and the **Ask agent** prompt now includes the
  config-file edits — still copy/ask only, no Apply, no task submission. Docs:
  `docs/web-framework-recipes.md` (recipe ≠ preset, how to add one, the Vite
  allowedHosts + install-guard gotchas, no secrets/DB/Compose).
- **QA preset fixes.** The `react-vite` preset now pins `--port 3000` (Vite
  defaults to 5173) and both `react-vite`/`nextjs` presets guard the install on the
  framework binary (`[ -x node_modules/.bin/<bin> ]`) instead of the fragile
  `[ -d node_modules ]` (an interrupted install left `node_modules/` without
  `.bin/` and never reinstalled).
- **Runtime manifest validation & guidance (slice 1).** sandboxd core now owns the
  `sandbox.yaml` contract and validates it, with no framework-specific execution
  logic and nothing auto-applied or written.
  - `POST /v1/runtime/manifest/validate` — stateless validation of a manifest blob,
    returning `{valid, errors, warnings, effective}`. Catches: top-level
    `command`/`port`/`health_path` → **error** (use `web.*`); unknown top-level keys
    → **warning** (forward-compatible — core stays recipe-agnostic, but typos stay
    visible); `web.command` without `web.port` → **error** (a custom command must
    declare its port — avoids preview-mismatch ambiguity); `web.port` out of
    1..65535 → **error**; likely-localhost bind → **warning** ("may"); command/
    `web.port` mismatch → **warning** ("appears to"); worker name/dup/empty-command
    → **error** (mirrors runtimed); invalid YAML → **error**. `effective` (manifest
    after core defaults: port 3000, health_path `/`) is returned **only for a valid
    manifest** — omitted on errors, so invalid config is never shown as runnable.
  - `GET /v1/apps/{id}/runtime/manifest` — owner-scoped, read-only: the app's
    current `sandbox.yaml` validated (`source: sandbox.yaml`), or the selected
    preset's manifest if none on disk (`source: preset`), or `present:false` with a
    reason (no workspace / default). Never 500 for the no-workspace case.
  - `runtime-inspect` now returns an **advisory `suggested_manifest`** (and notes)
    for detect-only stacks (Astro, Docusaurus) — e.g. Astro's
    `pnpm exec astro dev --host 0.0.0.0 --port 3000` plus a note that allowedHosts
    belongs in `astro.config.mjs`, not a CLI flag. Suggestions are never written.
  - Console Runtime panel: sandbox.yaml status (missing / valid / invalid),
    validation errors/warnings, effective command/port/health, suggested YAML with
    **Copy YAML** and **Ask agent** (the latter copies a paste-ready prompt
    explaining the schema + fix — it does **not** submit a task). No Apply button,
    no YAML editor in this slice.
  - `internal/manifest` is now the authoritative validator; runtimed keeps its
    executor-side parser, kept aligned by the existing parser-drift test.

## [Unreleased] — v0.4.8 (branch `feat/v0.4.8-git-push`, stacked on v0.4.7; NOT in v0.4.0)

> Post-v0.4.0, on a feature branch (depends on v0.4.7); not part of the v0.4.0 launch.

### Changed
- **Console Git panel — review-workflow UX (console-only; no backend changes).** The
  Git panel is now a single vertical "Git review" panel, file-list-first: a header
  (branch · short SHA · changed count · **Select all/none**); user files listed and
  checked by default; generated files collapsed and unchecked. Clicking a file
  **lazily loads that file's diff** (`git/diff?path=`, cached, with `+`/`-` line
  coloring and loading/error/binary/truncated states) instead of one whole-repo
  blob. Commit shows the selected count and stays double-submit-guarded. **Push is
  separate and only appears after a commit succeeds in this session** (with helper
  text otherwise), keeps its two-step confirm, and states new-branch-only / no
  force / main untouched. Plain-language states throughout (loading, not running,
  not a Git repo, no changes, nothing to push) and friendly push-reason mapping
  (`branch_exists`, `unsafe_repo_config`, `auth_failed`, …). No new endpoints, no
  diff library, no new framework.

### Docs
- **Git workflow consolidated.** The full Git workflow (credentials → import →
  runtime detect → status/diff → commit → push, spanning v0.4.2–v0.4.8) is
  documented in [`docs/git-workflow.md`](docs/git-workflow.md) — end-to-end flow,
  the host-side-network / in-sandbox-local execution split, security boundaries,
  limitations, and explicit non-goals. `ARCHITECTURE.md` and the README now frame
  Git as an **optional `/v1` workflow on top of sandboxd core**. No code changes.

### Fixed
- **Git commit/push concurrency (QA).** Two commits (or a commit and a push) on
  the same workspace could race: one succeeds and the other failed with a scary
  `git_error` because the selected path was already committed, and a push could
  publish a stale HEAD. Commit and push now take the **same per-workspace
  exclusive lock** (`git:<sandbox-id>` — per workspace, not global; different apps
  never block each other). Commit re-evaluates the changed paths under the lock,
  so a benign race returns `{committed:false, reason:"no_changes"}` instead of
  `git_error`; the same git "did not match / nothing to commit" outputs are now
  classified as `no_changes` (real failures stay `git_error`). Read-only
  status/diff remain unlocked. Console disables Commit while a commit/push is in
  flight (and guards against double-submit).

### Added
- **Git push (B2).** New endpoint `POST /v1/apps/{id}/git/push` pushes locally-
  committed changes to the app's remote, **host-side/control-plane** (never in the
  sandbox). The push target comes from `app.git_repo_url` + `app.git_credential_id`
  metadata — **never `.git/config` origin**. The token is decrypted **only** in the
  push path and reaches git solely via a **host-checking GIT_ASKPASS** (emits the
  token only when the credential prompt resolves to the exact expected host; any
  mismatch/unparsable prompt emits nothing) + a `0600` file — never in argv, env,
  logs, events, `.git/config`, workspace, snapshots, or `docker inspect`. A
  pre-flight **config audit refuses** dangerous repo-local config
  (`insteadOf`/`pushInsteadOf`/`sshCommand`/`fsmonitor`/`hooksPath`/proxy →
  `unsafe_repo_config`). Invocation is hardened: hooks disabled,
  `protocol.ext/file.allow=never`, `GIT_ALLOW_PROTOCOL=https`, clean
  `HOME`/global/system config, no `credential.helper`, argv-only. **New branch
  only** (default `sandboxd/<slug>-<shortsha>`; `main`/`master` and the import
  branch are rejected), **no force**, no PR. Refuses with `no_local_commits` when
  there's nothing to push (computed locally, no network). **Works when the sandbox
  is stopped** (reads the workspace host-side). No fetch/pull/deepen — a shallow-
  update rejection returns `shallow_push_unsupported` (deepen is a later B2.1).
  Console Git panel gains a Push section with an **explicit remote-write confirm**.

## [Unreleased] — v0.4.7 (branch `feat/v0.4.7-git-commit`, stacked on v0.4.6; NOT in v0.4.0)

> Post-v0.4.0, on a feature branch (depends on v0.4.6); not part of the v0.4.0 launch.

### Added
- **Local Git commit (B1).** New endpoint `POST /v1/apps/{id}/git/commit` commits
  selected workspace changes **locally** — no push/fetch/pull, no credentials, no
  network. Runs **in-sandbox** via `docker exec` (uid 1000, locked). It stages
  **only the selected, actually-changed paths** (never `git add -A`) and makes a
  **path-scoped commit** (`commit … -- <paths>`) so anything the agent pre-staged
  is *not* swept in. Defaults to the A2 user `files[]`; `runtime_files` are
  excluded unless named in `runtime_paths`. Uses `--no-verify` (no untrusted repo
  hooks) and an **ephemeral author** via `git -c user.name=… -c user.email=…`
  (default `sandbox-agent`/`agent@sandboxd.local`) — never written to
  `.git/config`. Paths are validated (no absolute/`..`); message required. Returns
  `{committed, sha, branch, files_committed}` or `{committed:false, reason}`
  (`no_changes` | `sandbox_not_running` | `not_a_git_repo` | `empty_repo_unsupported`).
  Requires a running sandbox and an existing HEAD (empty repos unsupported this
  slice). Owner-scoped (cross-owner/unknown → 404). Console Git panel gains a
  commit box (user files checked, runtime unchecked/collapsed, message required,
  shows the resulting sha). **No push UI.**

## [Unreleased] — v0.4.6 (branch `feat/v0.4.6-git-status-diff`, stacked on v0.4.5; NOT in v0.4.0)

> Post-v0.4.0, on a feature branch (depends on v0.4.5); not part of the v0.4.0 launch.

### Added
- **Read-only Git status/diff (A2).** New endpoints `GET /v1/apps/{id}/git/status`
  and `GET /v1/apps/{id}/git/diff?path=` let you inspect an imported repo's
  changes before any commit/push exists. Git runs **in-sandbox via `docker exec`**
  (uid 1000, locked container, **no network**), never host-side — because the
  workspace `.git`/`.gitattributes` are agent-controlled and `git status`/`diff`
  can execute configured programs; the invocation is hardened
  (`-c core.fsmonitor=`, `--no-ext-diff --no-textconv`, `-c safe.directory=*`) and
  argv-only. **No token, no credential lookup, no network, no fetch/pull/commit/
  push.** Status is structured (branch, head_sha, clean, ahead/behind when locally
  known, files[] with status category + staged). Diff is a unified diff, capped
  (256 KiB → `truncated:true`), binary-safe, optional `path` filter (absolute/`..`
  rejected); diff bodies are **not logged** (they can contain secrets). When there
  is no running sandbox / no `.git`, the endpoints return `{available:false,
  reason}` (no auto-wake). Owner-scoped (cross-owner/unknown → 404). Console app
  detail gains a read-only Git panel (status summary, changed files, on-demand
  diff; no commit/push controls).
- **Honest classification of runtime-generated changes (A2, QA follow-up).** A
  pristine import isn't really "dirty" just because sandboxd/the toolchain wrote
  `sandbox.yaml`, a lockfile, or a framework cache (`.astro/`, `.next/`, …). Status
  now keeps `clean` truthful (raw) and adds `user_clean` (ignores runtime files)
  plus a separate `runtime_files[]` list; `files[]` is now user/repo changes only.
  Nothing is auto-committed, `.gitignore` is untouched, and no change is hidden —
  runtime files are surfaced in their own bucket. Console renders them separately
  (collapsed, "not your edits") and shows `user_clean` as the headline state.

### Fixed
- **A2 diff was broken (QA).** `--no-ext-diff`/`--no-textconv` were passed as
  top-level git options *before* the `diff` subcommand, so `git` rejected them
  (`unknown option`). Moved after `diff`
  (`git … diff --no-ext-diff --no-textconv HEAD [-- path]`); added a command-
  ordering regression test.

## [Unreleased] — v0.4.5 (branch `feat/v0.4.5-runtime-inspect`, stacked on v0.4.4; NOT in v0.4.0)

> Post-v0.4.0, on a feature branch (depends on v0.4.4); not part of the v0.4.0 launch.

### Fixed
- **Pre-A2 hardening (review follow-up).** `handleCreate` now **aborts** the
  sandbox create if persisting the resolved `web_port` fails, instead of silently
  continuing (which could route one port via Traefik while `previewURL` later read
  a stale DB port). Added a parser-drift guard (`cmd/runtimed`) asserting the
  control-plane minimal manifest parser and runtimed's parser resolve the same
  `web.port` for every built-in preset (sentinel default catches a silent
  tag-rename fallback).

### Added
- **Advisory runtime detection (A1.5b).** New read-only endpoint
  `GET /v1/apps/{id}/runtime-inspect` inspects the app's workspace **host-side**
  and returns: `existing_manifest` (summary + `authoritative` when a sandbox.yaml
  is present), `suggestions[]` (`preset`, `runnable`, `confidence`, `reasons`,
  `warnings`), `alternatives`, `default_suggestion`, and `warnings`. Detection
  (`internal/detect`) is **purely advisory** — it never runs app code, installs
  deps, reads a Git credential/token, or applies anything; the user always
  overrides manually. Detects **nextjs, react-vite, node-express, fastapi,
  worker** (runnable presets) and **astro, docusaurus** (detect-only — suggested
  with a warning, no built-in preset). An existing `sandbox.yaml` is marked
  authoritative and never overwritten; warnings flag a missing `web.port`, a
  likely-localhost bind, or a referenced missing entry file. `default_suggestion`
  is set only for a single unambiguous high-confidence **runnable** stack. The
  endpoint is owner-scoped (cross-owner/unknown → 404). Console app detail shows
  the result (suggestion + confidence + reasons + warnings); it owns no detection
  logic. **Astro is detect-only in this slice** (no built-in preset/template).

## [Unreleased] — v0.4.4 (branch `feat/v0.4.4-preview-port`, stacked on v0.4.3; NOT in v0.4.0)

> Post-v0.4.0, on a feature branch (depends on v0.4.3); not part of the v0.4.0 launch.

### Fixed
- **Preview port correctness (A1.5a).** The preview URL and Traefik router now use
  the sandbox's **resolved web port** instead of a hardcoded `3000`, so an app that
  serves on a non-3000 port (e.g. an imported Astro repo on 4321) previews
  correctly. The port is resolved at create from (1) the cloned/imported
  workspace `sandbox.yaml` `web.port`, else (2) the selected runtime preset's
  manifest `web.port`, else (3) `3000` (backward compatible), and persisted on the
  sandbox (`web_port`, migration 0021). A shared `internal/manifest` parser lets
  the control plane read `web.port` (runtimed unchanged). The sandbox `preview`
  object now includes `port`. **Multi-port behavior is unchanged** — the resolved
  port is added to the preview routers additively; requested ports are never
  dropped, and a no-port sandbox stays preview-less. No detection/stack/Astro
  preset here (that's A1.5b).

## [Unreleased] — v0.4.3 (branch `feat/v0.4.3-git-import`, stacked on v0.4.2; NOT in v0.4.0)

> Post-v0.4.0, on a feature branch (depends on v0.4.2); not part of the v0.4.0 launch.

### Added
- **Import an app from a private Git repo (Git A1).** `POST /v1/apps` accepts a
  `git: {repo_url, branch, credential_id}` block; on sandbox create the control
  plane clones the HTTPS repo **host-side** into the workspace (`--depth=1`,
  `--no-recurse-submodules`, single branch) using the stored credential via
  `GIT_ASKPASS`, then rewrites the remote tokenless and normalizes ownership to
  uid/gid 1000 — all **before the container starts**, so the **token never enters
  the sandbox, workspace, snapshots, Docker env, logs, events, or task results**
  (it appears in no argv, env, or `.git/config`). Tokenless repo metadata is
  stored on the app; events `git.repo.clone_started|cloned|clone_failed` carry
  only `repo_url`/`branch`/`reason`. `git` added to the control-plane image.
  Console New App gains an "Import from Git URL" mode (repo URL + branch +
  credential dropdown). HTTPS only; no SSH/GitHub App/OAuth; no status/diff/
  commit/push yet.

## [Unreleased] — v0.4.2 (branch `feat/v0.4.2-git-credentials`, NOT in v0.4.0)

> Post-v0.4.0, on a feature branch; not part of the v0.4.0 launch.

### Added
- **Git credential store (Git A0).** Owner-scoped, encrypted-at-rest Git access
  tokens with `POST`/`GET`/`DELETE /v1/git-credentials` and a console Settings
  section to add/list/delete. The token is write-only — sealed with the existing
  secrets cipher and **never returned by the API or shown in the console**.
  **These credentials are stored encrypted and are not used by anything until
  Git import lands in a later v0.4.x release** (clone/diff/commit/push are
  separate, later slices).

## [0.4.0] — 2026-06-25

**Self-hosted control plane for AI-built apps** — adds a web console, runtime
presets, snapshots/fork/restore, observability, process logs, and a read-only
(plus editable lifecycle) settings view on top of the v0.3 app/config/secrets
foundation. Integration of all accepted v0.3 + v0.4 work, validated by a
from-zero VPS release-candidate QA pass.

### Highlights
Web console · runtime presets (React/Vite, Next.js, Node/Express, FastAPI,
Worker) · live preview URLs · agent tasks · app config & secrets (write-only
secrets) · snapshots / fork / restore · activity/events timeline · per-process
logs · settings with editable idle/keepalive lifecycle controls.

### Added
- **Snapshots, fork & restore.** Public `POST/GET/DELETE /v1/snapshots`,
  app-scoped history `GET /v1/apps/{id}/snapshots`, `POST /v1/apps/{id}/restore`,
  and `POST /v1/apps/{id}/fork` (with `source_app_id`). Restore replaces the
  app's current sandbox; fork clones into a new app. Console gets a Snapshots
  panel with confirm-gated restore/fork.
- **Snapshot ignore-list.** Snapshot capture excludes generated/dependency dirs
  (`node_modules`, `.next`, `out`, `.venv`, `__pycache__`, `.cache`) so snapshots
  and forks stay small and free of stale build output (symlink-safe copy).
- **Observability / activity timeline.** Durable `app_events` (centralized
  recorder, monotonic ULIDs) surfaced at `GET /v1/apps/{id}/events` and rendered
  as a newest-first Activity timeline in the console.
- **Runtime manifest (`sandbox.yaml`).** A generalized in-sandbox process model
  (one optional `web` process + N `workers`) supervised by runtimed, with a
  post-task build check. No manifest = the existing default Vite app.
- **Process API + logs.** `GET /v1/sandboxes/{id}` now includes `processes[]`
  (name/kind/running/pid/restarts); `GET /v1/sandboxes/{id}/processes/{name}/logs`
  tails a process's log (read-only, name-validated, tail-only). Console shows a
  Processes panel and per-process logs; the preview pane reads "Preview /
  endpoint" and worker-only apps show preview status `none` (valid, not an error).
- **Runtime presets.** `GET /v1/presets` and `runtime_preset` on
  `POST /v1/apps`, `POST /v1/apps/{id}/sandbox`, `POST /v1/sandboxes`. Five
  accepted presets, each booting and reloading after agent tasks:
  **react-vite** (Vite HMR), **nextjs** (`restart_after_task`, heals an agent
  `next build`), **node-express** (`restart_after_task`), **fastapi**
  (port 3000 + `uvicorn --reload`), **worker** (no preview; editable `worker.sh`
  + `restart_after_task`). A New-App preset picker in the console.
- **Honest build/health semantics.** Task results expose `build_status`
  (`passed`/`failed`/`skipped`), `preview_ok` (omitted for worker-only), and
  `app_healthy`; `build_ok` stays for back-compat (true only when `passed`).
- **`web.restart_after_task` / worker `restart_after_task`.** Per-process
  reload-by-restart for runtimes without live reload.
- **Settings / instance overview.** `GET /v1/settings` — a read-only, safe
  instance summary (version, networking/preview, auth mode, runtime/storage,
  lifecycle, egress mode, agent providers, presets, capabilities; never any
  secret). A console **Settings** page renders it.
- **Editable lifecycle tunables.** `PATCH /v1/settings` edits ONLY the idle-reap
  enable/threshold and keepalive-max (strict allowlist; any other key → 400),
  persisted and **hot-applied** to the running reaper/keepalive; the console
  Settings page edits them. Everything else stays read-only / env-managed.
- **Base image contract.** `docs/base-image.md` documents what a custom base
  image must provide (runtimed, `sandbox` uid/gid 1000, workspace path, no
  privileged/Docker socket) and how to select one via `SANDBOXD_IMAGE`.
- **Shared-host installer & preview port.** `scripts/dev/install-v04-ubuntu.sh`
  with shared-host mode (loopback API, configurable `HTTP_PORT`), and preview
  URLs that include the public port when `HTTP_PORT` ≠ 80.
- **Test foundation.** Public-surface + JSON-shape contract tests (sandboxd is
  the contract), a console unit-test runner (vitest) with API-mirroring fixtures,
  and `docs/release-checklist.md` for manual VPS sign-off.
- **Docs.** `docs/sandbox-manifest.md`, OpenAPI updates (`/v1/presets`,
  `/v1/settings`, process logs, `runtime_preset`, `processes`, task health fields).

### Fixed
- **Explicit `build.command: ""` now skips the build check** (was overwritten by
  the default `pnpm build`), so presets can disable build checks.
- **Next.js post-task `.next` poisoning** — the agent's `next build` no longer
  leaves `next dev` serving 404/500 on `_next/static` (build skipped + `rm -rf
  .next` on start + `restart_after_task`).
- **Fork/restore ownership** — restored/forked workspaces are normalized to the
  sandbox user (uid 1000); apps no longer hit EACCES on `~/.cache`, deps, venv.

### Known limitations (non-blocking; see `docs/sandbox-manifest.md`)
- `DELETE /v1/sandboxes/{id}` **purges** the workspace while the legacy internal
  `DELETE /sandbox/{id}` **stops and keeps** it — same verb, different data outcome.
- `keepalive_until` is set/honored but not surfaced in `GET /v1/sandboxes/{id}`.
- The wake/warming interstitial returns HTTP `200` (a status-only health check
  can't tell "warming" from "ready").
- Per-task `agent.log` transcript can be empty on task timeout — persistence
  needs investigation.
- **Docker backend only** — OCI/containerd/Kata are a future runtime provider.
- **Out of scope for v0.4:** Git/GitHub import, managed databases/sidecars, and
  Docker-Compose-inside-the-sandbox.

---

> v0.4.0 rolls up the v0.3.0 work below (app config & secrets, web console +
> `/v1` OpenAPI) — kept here for detail.

## [Unreleased] — v0.3.0 (integration: `console`)

Backend and console landing together as one incremental release. Tracked on
the `console` integration branch; `main` stays at 0.2.0 until v0.3.0 is cut.

### Added
- **App-scoped config & secrets.** Per-app key/value config under
  `/v1/apps/{id}/config` (`POST` / `GET`) and `/v1/apps/{id}/config/{key}`
  (`PATCH` / `DELETE`). Sensitive values are AES-256-GCM-encrypted at rest and
  write-only over the API (a `GET` returns metadata and `value_set`, never the
  plaintext); non-sensitive values are returned. An `access_policy`
  (`control_plane_only` default) records who may later read a value through the
  broker. Documented in `docs/openapi.yaml`. (#33)
- **Web console + `/v1` OpenAPI spec.** An optional Vite/React console (served
  through Traefik at `console.<domain>`) over the public `/v1` API, plus
  `docs/openapi.yaml` and a contract test that keeps the spec and the routes in
  sync. The app detail screen now includes a **Config & Secrets** panel. (#32)

### Note
- The secrets **broker** (Slice 2 — delivering values to agents/runtimes per
  `access_policy`) is intentionally deferred and tracked separately; it does not
  block this release.

## [0.2.0] — 2026-06-22

Reliability fixes across the core, and durable "apps" as first-class entities
above sandboxes.

### Added
- **Durable app model.** Apps are now first-class entities above sandboxes. An
  app owns the user-facing concept (name, description, tags) and outlives the
  sandbox that is its current running instance. New tenant-scoped `/v1/apps` API
  (`POST` / `GET` / `GET {id}` / `PATCH {id}` / `POST {id}/sandbox`) with optional
  `external_*` integration tags; sandboxes gain a nullable `app_id`. Additive and
  backwards-compatible — the existing sandbox API is unchanged. (#31)
- **Selectable app templates.** A working Vite + React + TypeScript
  `react-standard` scaffold ships in the image at `/opt/templates/<name>` and is
  seeded into a new workspace on first boot (default `react-standard`;
  `template: "blank"` for an empty workspace). The agent now edits a known-good
  app with a passing build and a live preview instead of scaffolding from an
  empty directory. (#29)
- **Per-task timeout.** `timeout_s` on `POST /v1/sandboxes/{id}/tasks` (0 or
  omitted → 10m default, max 24h). The control-plane task watcher now derives its
  streaming window from the task timeout instead of a fixed 15 minutes, so long
  tasks are no longer marked failed prematurely. (#25)
- **Per-sandbox idle policy.** `idle_policy: sleep | always_on`. (#14)
- **End-to-end + image-smoke CI.** A job that builds the base image and drives
  the real create → seed → install → serve → wake lifecycle on a Docker daemon,
  and asserts the agent CLIs and the default template are present on the image.
  Adds `go vet` to the Go job. (#30)

### Fixed
- **Snapshot capture** targeted the old loopback `.img` model and returned 500 on
  the default directory-storage workspaces; it now copies the workspace tree
  crash-consistently and round-trips through `from_snapshot`. (#24)
- **`POST /v1/sandboxes`** returned `400` on a clean install because it forced an
  unseeded `react-standard` template; a no-template create is now provisioned
  cleanly. (#28)
- Four confirmed correctness items from the security/code audit. (#21)

### Changed
- The image installs Claude Code via the official native installer, alongside
  OpenCode. (#18)

### Removed
- The dormant single-token auto-git-push path (undocumented, unused). (#23)

## [0.1.1] — 2026-06-07

- Renamed the project to **sandboxd** and standardized the `SANDBOXD_` env prefix.
- Docs: production-safety checklist; Rancher Desktop / k3s port-80 preview note.

## [0.1.0] — 2026-06-06

- Initial release.

[0.2.0]: https://github.com/tastyeffectco/sandboxd/releases/tag/v0.2.0
[0.1.1]: https://github.com/tastyeffectco/sandboxd/releases/tag/v0.1.1
[0.1.0]: https://github.com/tastyeffectco/sandboxd/releases/tag/v0.1.0
