# Changelog

All notable changes to sandboxd are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/), and the project follows
[Semantic Versioning](https://semver.org/) (pre-1.0: a minor bump adds features,
a patch is fixes only).

## [Unreleased] — v0.4.8 (branch `feat/v0.4.8-git-push`, stacked on v0.4.7; NOT in v0.4.0)

> Post-v0.4.0, on a feature branch (depends on v0.4.7); not part of the v0.4.0 launch.

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
