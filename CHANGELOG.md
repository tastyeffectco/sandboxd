# Changelog

All notable changes to sandboxd are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/), and the project follows
[Semantic Versioning](https://semver.org/) (pre-1.0: a minor bump adds features,
a patch is fixes only).

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
