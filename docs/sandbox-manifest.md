# `sandbox.yaml` — app runtime manifest

An **optional** file in the workspace root (`~/workspace/app/sandbox.yaml`) that
declares how an app builds, runs, exposes a preview, and reports health — so
sandboxd works beyond the default Vite/React app. The coding agent can write or
edit it like any workspace file.

**No manifest = the built-in defaults** (a Vite/React web app on port 3000), so
existing apps keep working unchanged. runtimed reads the manifest on (re)start.

> **Phase status:** 7A (Runtime Manifest Core), 7B (process API + console), and
> **7C-1 (runtime presets) are all accepted & live-verified.** All five presets
> (react-vite, nextjs, fastapi, node-express, worker) pass preview + agent-task
> reload + fork/restore; the snapshot ignore-list, fork/restore ownership
> normalization, and wake/idle/keepalive edge cases are verified (see
> "Final acceptance" below). `GET /v1/sandboxes/{id}` includes `processes[]`;
> `GET /v1/sandboxes/{id}/processes/{name}/logs` tails a process's log;
> `GET /v1/presets` lists the five presets and `runtime_preset` is accepted on
> app/sandbox create. Remaining items are **non-blocking follow-ups** (see end).
> 7C-2 (manifest view/edit/validate, advanced override, agent-instructions,
> app+DB preset) is **not started**.

## Schema (version 1)

```yaml
version: 1

# The previewed process. Omit `web` entirely for a worker-only app (no preview).
web:
  command: "pnpm dev"     # how to start the web server (run via `bash -lc` in the app dir)
  port: 3000              # the HTTP port the preview routes to
  health_path: "/"        # path probed for readiness; HTTP 200 = ready
  restart_after_task: false  # restart the web process after each task (see below)

# Post-task build check (run after a coding task to catch breakage).
build:
  command: "pnpm build"   # empty string = skip the build check
  timeout_seconds: 120

# Background processes with NO preview (e.g. a queue consumer). Optional.
workers:
  - name: "queue"
    command: "node worker.js"
  - name: "cron"
    command: "python cron.py"
```

Every field is optional and defaulted:

| Field | Default | Notes |
|---|---|---|
| `web.command` | `[ -d node_modules ] \|\| pnpm install; pnpm dev` | also from `RUNTIMED_DEV_CMD` |
| `web.port` | `3000` | also from `RUNTIMED_PREVIEW_PORT` |
| `web.health_path` | `/` | 200 ⇒ ready |
| `web.restart_after_task` | `false` | restart the web process after each task — see "Dev-mode resilience" |
| `build.command` | `pnpm build` | see "Build checks" — explicit `""` skips |
| `build.timeout_seconds` | `120` | |
| `workers` | none | each gets a `worker-N` name if unnamed |

Resolution rules:
- **no file** → default web app, no workers.
- **file with `web:`** → web app with those fields (missing ones defaulted).
- **file with `workers:` and no `web:`** → worker-only app (no preview).
- **empty file** → default web app (a stray/empty file won't disable the preview).
- **invalid YAML** → logged, falls back to defaults (the app still boots).

### Build checks (and how to skip them)
The build check is **runtime verification** — after a coding task, runtimed runs
`build.command` in the workspace to catch obvious breakage before reporting the
result. It is **not a production deployment build**: there is no artifact, no
bundling for release, no deploy. Stacks whose "build" would be meaningless or
harmful as a post-task check (a Next.js dev server, a FastAPI/Express API, a
worker) should **skip** it.

`build.command` distinguishes *unset* from an *explicit empty string*:

| Manifest | Behavior |
|---|---|
| no `build:` block | **default** (`pnpm build`) — backward compatible |
| `build: {}` (block present, no `command`) | **default** (`pnpm build`) |
| `build:`<br>`  command: ""` | **skip** the build check |
| `build:`<br>`  command: "make ci"` | run `make ci` |

A skipped build is reported **honestly**: the task result has
`build_status: "skipped"` (not `passed`), and `build_ok` is `false` (it is `true`
only for `build_status: "passed"`). `app_healthy` still reflects real health
(web preview serving / a worker running), so a skipped-build app is `app_healthy:
true` when it is actually serving.

> Why a pointer/explicit-empty distinction? Earlier, an empty `build.command` was
> silently replaced by the default `pnpm build`, so presets could not disable the
> check — which made the Next.js preset run `next build` after every task and
> poison the live `next dev` server. Presets now set `build.command: ""` to skip.

### Dev-mode resilience (`web.restart_after_task`)
Skipping the platform build check stops *runtimed* from poisoning a dev server,
but the **coding agent itself** can run `next build` (or any production build)
during a task. That writes production `.next/` while `next dev` is live and
poisons it — `_next/static/chunks/*` start returning 404/500.

`restart_after_task: true` makes runtimed **restart that process after every
task** (success or failure; skipped on cancel) so it re-runs its command and
picks up whatever the agent changed. It is a per-process flag on **`web`** and on
each **worker**:
- **web** — runtimed waits (up to 90s) for the restarted server to serve a 200 on
  the health path before reporting the task's final preview/health, so
  `preview_status_after` reflects the restarted server.
- **worker** — no readiness probe, so the worker is simply bounced; the supervisor
  re-runs its command (re-reading any edited script).

Each restart increments the process's `restarts` count in `GET /status`.

It is **opt-in**, used only where the runtime has no live reload of its own:

| Preset | Reload mechanism |
|---|---|
| React/Vite | Vite HMR (no restart) |
| FastAPI | `uvicorn --reload` (no restart) |
| Next.js | `web.restart_after_task: true` (also heals agent `next build` poison) |
| Node/Express | `web.restart_after_task: true` (`node server.js` has no reload) |
| Worker | worker `restart_after_task: true` (re-runs the editable `worker.sh`) |

## Process model
runtimed supervises each declared process — one web process (optional) plus any
workers — restarting on unexpected exit with backoff, and abandoning a process
that fast-fails repeatedly (reported, not crash-looped). Per-process status
(name, kind, running, pid, restarts) and the web preview health are in
`GET /status`; logs are written per process under `~/.runtimed/<name>.log`.

**Worker-only apps have no preview.** When `web` is omitted, the runtime reports
preview status **`none`** (distinct from `down`, which means a web process
exists but isn't serving), no preview probe runs, and there is no preview URL.

**Validation.** A malformed manifest is rejected and the app falls back to the
built-in defaults (so it still boots): worker names must match `[A-Za-z0-9_-]`
(1–64 chars) — no path separators, `..`, empty, or duplicate names, since the
name becomes a log-file path — every worker needs a command, and an explicit
`web.port` must be 1–65535.

## Examples

**Python web app:**
```yaml
version: 1
web:
  command: "pip install -r requirements.txt && python -m flask run --port 8000"
  port: 8000
  health_path: "/healthz"
build:
  command: ""   # interpreted languages: no build step
```

**Worker-only (no preview):**
```yaml
version: 1
workers:
  - name: ingest
    command: "python ingest.py"
```

## Verification status (Phase 7)
Unit-tested **and live-verified end-to-end** (2026-06-23) on a rebuilt base image
(`sandboxd-base:phase7`) driven by a disposable host-run sandboxd — all three
shapes passed:

| Shape | Result |
|---|---|
| **Default Vite** (no manifest) | preview `ready`; `web` process running; `pnpm build` exit 0 (build check works) |
| **Custom web** (python `http.server` on `:5000`, `health_path /healthz`, build skipped) | preview `ready`; web serves `GET /healthz → 200` and `/ → 200`; build intentionally skipped (`build.command: ""`) |
| **Worker-only** (one worker, no `web`) | preview `none` (treated as valid, not an error); worker process running and producing output |

Test sandboxes were **portless** (no Traefik label) so the shared host's
production routing was never touched; verification used runtimed's reported
status plus in-container checks, not Traefik. Re-run after any runtimed change.

### Runtime presets (Phase 7C-1) — ACCEPTED & live-verified (2026-06-23)

**Final acceptance** — all five presets pass the full agent-task + fork flow:

| Preset | Preview | Agent task / reload | Fork/restore |
|---|---|---|---|
| **react-vite** | `/` 200 | task change applies; build→`dist/`, no poisoning | healthy |
| **nextjs** | `/` + chunks 200 | build-provoking task no longer poisons preview (`restart_after_task` heals agent `next build`); chunks stay 200 | healthy |
| **fastapi** | `/health` 200 on **:3000** | edits go live (`uvicorn --reload`) | healthy (`/hello` preserved) |
| **node-express** | `/health` 200 | added route goes live after task (`restart_after_task`) | healthy |
| **worker** | `none` | code/output change reflected in logs after task (`restart_after_task`) | healthy |

Cross-cutting, also verified:
- **Snapshot ignore-list** works (`node_modules`/`.next`/`out`/`.venv`/`__pycache__`/`.cache` excluded from snapshots/forks).
- **Fork/restore ownership normalized to `sandbox:sandbox`** (uid 1000) — forks boot healthy and reinstall deps without EACCES.
- **wake / idle-reaper / keepalive** edge cases pass (wake-on-request ~1s; idle reaped at threshold; keepalive survives past control).

The verification detail behind each row is in the subsections that follow.

#### Boot verification (earlier)
Live e2e (2026-06-23) on a rebuilt image (`sandboxd-base:p7c1`, new templates +
runtimed) via a disposable host-run sandboxd, all sandboxes **portless** (no prod
collision). **All five presets boot.**

| Preset | Result | Ready (warm cache) | Endpoint / worker |
|---|---|---|---|
| **react-vite** | ✅ pass | ~31s | `GET / → 200` |
| **nextjs** | ✅ pass | ~30–39s | `GET / → 200`; `_next/static` asset `→ 200`. *Cold boot may be slower*. See Next.js post-task fix below. |
| **node-express** | ✅ pass | ~30s | `GET /health → 200` |
| **fastapi** | ✅ pass | ~15–37s | `GET /health → 200` on **port 3000** (the preview port); runtime venv/pip install works; `--reload` picks up post-task edits |
| **worker** | ✅ pass | ~28s | preview `none` + worker process running |

Confirmed across all presets:
- each seeds its **expected starter files** (worker seeds none — only `sandbox.yaml`, correct);
- **`sandbox.yaml` is written by runtimed** on first boot (never overwriting an existing one);
- **process status + logs endpoint** work (`GET /v1/sandboxes/{id}/processes/{name}/logs` returned real recent logs);
- install happens **at runtime** on first boot (`node_modules` / `.venv` created then);
- **API rejects unknown presets with 400** (both `POST /v1/apps` and `POST /sandbox`);
- if bypassed with a bad preset env, **runtimed logs loudly** (`WARN "unknown runtime preset; using default template"`) and safely **falls back to `react-standard`**.

Not yet live-tested (still unit-only):
- the **console preset dropdown** is build/typecheck-clean but **not browser-click tested**;
- **app-default preset resolution** (`POST /v1/apps/{id}/sandbox` using the app's stored preset when omitted) is **unit-tested** but not exercised through the public app-sandbox flow live (live runs passed the preset explicitly to stay portless).

**Future image optimization** (later, not 7C-1):
- bake a **warm pnpm store / npm cache** into the image to cut cold-boot installs for react-vite / nextjs / node-express;
- **preinstall FastAPI + uvicorn** (or `uv`) so the FastAPI preset skips pip install on first boot;
- consider a **Next.js-optimized image/layer** (prebaked `next`/`react`) for the heaviest cold boot.

#### Next.js post-task `.next` poisoning — fixed & re-tested (2026-06-23)
**Bug:** the Next.js preset ran `pnpm dev` as the web process and `pnpm build` as
the post-task build check in the same workspace. `next build` writes production
`.next/`, which the long-running `next dev` then serves from → 500s on
`_next/static` (`ENOENT .next/static/chunks/...`). The dev server is not
restarted after the build, so it stays broken.

**Fix (smallest reliable):**
- Next.js preset `build.command` is now **empty** — the build check is the only
  thing that runs `next build`, so skipping it removes the poisoning source.
  Tradeoff: **no post-task build verification for Next.js** until an isolated
  build check exists (build in a temp dir/clone, not the live workspace).
- Web command now `rm -rf .next` before `pnpm dev` — defends a clean dev start
  against a stale/production `.next/` carried in by a snapshot restore.
- Next.js template ships a **`.gitignore`** (`node_modules`, `.next`, `out`,
  `.env`, `.env.local`).
- **`web.restart_after_task: true`** (see "Dev-mode resilience") — the agent can
  run `next build` *during* a task, which `rm -rf .next` at boot can't undo
  mid-session. Restarting the web process after each task re-runs the command
  (incl. `rm -rf .next`) and heals the live dev server. This closes the
  agent-poison gap that skipping the build check alone did not.

**Re-test (rebuilt image `sandboxd-base:p7c1b`, portless):** fresh nextjs ready
**~30s**, `/`+asset `200`; reproduced the bug (`pnpm build` → `/`+asset `500`);
recovery via restart (`rm -rf .next`) → re-ready **~10s**, `/`+asset `200`;
simulated homepage edit → hot-reload `200`, edit visible; checkpoint tracks only
the **6** real files (no `node_modules`/`.next`/`out`).

**Follow-up fix + retest (rebuilt image `sandboxd-base:p7c1d`, 2026-06-23):** a
deeper bug was found — `applyDefaults` replaced an explicit `build.command: ""`
with the default `pnpm build`, so presets *could not* skip the build (Next.js was
still running `next build` after tasks). Fixed by making `build.command` a
pointer (unset vs explicit-empty). Re-tested by actually **running a coding task**
on a Next.js sandbox: the agent itself fails without credentials, but the
post-task build check still runs — and the task result reported
`build_status: "skipped"`, **no `pnpm build`/`next build` executed** (only
`web.log` present), and `/` + four `/_next/static/chunks/*` assets returned
**200** afterward (not poisoned). FastAPI: same — `build_status: "skipped"`, no
`pnpm build`, `/health → 200`.

**Agent-poison fix + retest (rebuilt image `sandboxd-base:p7c1e`, 2026-06-23):**
even with the build check skipped, the *agent* can run `next build` during a
task. Added `web.restart_after_task` and enabled it on the Next.js preset. Live
retest: created a Next.js sandbox, ran `pnpm build` to **simulate the agent
poisoning `.next/` mid-task** (`.next` became a production build with `BUILD_ID`;
`/_next/static/chunks/*` → 404), then ran a coding task. After the task the web
process showed **`restarts: 1`**, `/` + four `/_next/static/chunks/*` returned
**200**, and `.next` was a **clean dev build again** (no `BUILD_ID`) — the
restart healed the live dev server.

#### Honest build / health semantics — implemented (2026-06-23)
The task result no longer reports a skipped build as a pass. `TaskResult` now has:
- **`build_status`**: `passed` | `failed` | `skipped` (skipped = no build command,
  e.g. the Next.js preset);
- **`preview_ok`**: bool for web apps; **omitted/null for worker-only** (no endpoint);
- **`app_healthy`**: build not failed AND (web: preview serving / worker-only: a
  worker process running);
- **`build_ok`** is kept for backward compatibility but is now **true only when
  `build_status == passed`** — a skipped build is never `build_ok=true`.

The console shows "build skipped" / "build passed" / "build failed" (and
"unhealthy" when `app_healthy=false`) instead of the old unconditional "build ok".

#### Snapshot ignore-list — implemented (2026-06-23)
Correction to an earlier note: in the OSS build the workspace is a plain
bind-mounted **directory** (the loopback `.img` model is legacy — `internal/
snapshot`'s zstd-`.img` path is dead in dir mode). The public `/v1`
snapshot/fork/restore subsystem captures by **copying that directory** into the
snapshot store (`captureImage`), so the ignore-list lives right in that copy.
`captureImage` now uses `copyTreeExcluding`, which:
- skips `node_modules`, `.next`, `out`, `.venv`, `__pycache__`, `.cache` by base
  name at **any depth** (conservative generated/dependency dirs only — `dist`/
  `build` are **not** ignored, as the current templates don't treat them as
  generated);
- copies symlinks **verbatim** and never follows them → no path-traversal /
  symlink escape during the staging copy;
- preserves mode + ownership (so the restore path's `cp -a` keeps the sandbox
  user's files writable).

Restored workspaces re-create the ignored dirs on first boot (`[ -d node_modules
] || pnpm install`, `rm -rf .next`, venv install). Tested: exclusion incl. a
nested `node_modules`, source + `sandbox.yaml` preserved, and symlink-escape safety.

#### Fork/restore ownership normalization — implemented (2026-06-23)
`ProvisionFromTemplate` (the single funnel for app **fork**, app **restore**, and
direct **`from_snapshot`** creation) clones the snapshot/template with `cp -a`,
which preserves the *source* ownership, and creates the workspace dir itself as
**root**. So the sandbox user (uid 1000) hit **EACCES** writing `~/.cache`, the
pnpm/npm store, `node_modules`, `.next`, `.venv`, generated files — forks/restores
booted broken. (The fresh seed already chowns; only the template path didn't.)

Fix: after the clone, `ProvisionFromTemplate` **recursively normalizes ownership**
of the whole workspace (incl. the `$HOME` dir itself) to the sandbox uid/gid —
the same result the fresh seed's `chown -R sandbox:sandbox` gives. It never trusts
the snapshot's captured ownership. It uses `Lchown` and **does not follow
symlinks** (a `WalkDir` lstat walk that never descends a symlink), so a symlink
pointing outside the workspace can't redirect the chown. The normalization is
logged. (Host-side numeric chown to 1000:1000 is correct for the OSS no-userns
default; under userns-remap it would move into a container like the seed.)

Live-verified: Next.js snapshot → fork → `$HOME` `1000:1000`, preview `/` + assets
`200`, `node_modules` reinstalled and `~/.cache` writable (no EACCES); FastAPI fork
boots with venv/pip reinstall + `/health 200`.

#### FastAPI preset — port 3000 + live reload (2026-06-23)
Two FastAPI bugs fixed:
- **Port mismatch:** the public preview routes to **3000**, but uvicorn ran on
  8000 — so the external preview was **502** even though the internal probe said
  ready. The preset now serves on 3000 (`--port 3000`, `web.port: 3000`).
- **Stale after task:** uvicorn didn't reload, so an agent-added route 404'd until
  a manual restart. The command now runs with **`--reload`**, and the template's
  `requirements.txt` adds **`watchfiles`** so the reloader works. (No
  `restart_after_task` — reload is reliable; kept for Next.js only.)

`health_path` stays `/health`; build stays skipped.

Live-verified (portless; external check via the container-IP:3000 path Traefik
uses): create → public `/health 200`; agent adds `/hello` → `/hello 200` with **no
manual restart**; snapshot → fork → fork public `/health 200` and **`/hello`
preserved**, venv reinstalled.

#### node-express + worker reload via restart_after_task (2026-06-23)
Closed the last two preset reload gaps (neither has a live reloader):
- **node-express** now sets `web.restart_after_task: true` — `node server.js` is
  bounced after each task so route/code changes go live.
- **worker** now ships an **editable `worker.sh`** (template `worker-standard`,
  command `bash worker.sh`) and sets the worker's `restart_after_task: true`. The
  restart mechanism was extended from web to **worker processes** (a per-process
  flag; workers are bounced without a readiness wait). Not a generic policy
  framework — just the per-process flag.

Live-verified (portless): node-express agent adds `/ping` → `pong` 200 after the
task, no manual restart (web `restarts: 1`); worker `worker.sh` output changed →
`worker.log` shows the new line after the task, no manual restart (worker
`restarts: 1`). React/Vite, FastAPI, Next.js behavior unchanged.

#### Remaining non-blocking follow-ups (documented, not implemented)
These do not block 7C-1 acceptance.
- **Per-task `agent.log` empty on timeout** — agent transcript persistence on task
  timeout still needs investigation (not yet solved).
- **DELETE semantics differ** — v1 `DELETE /v1/sandboxes/{id}` purges the
  workspace, while the legacy internal `DELETE /sandbox/{id}` stops and keeps it.
  Reconcile/clarify (an intentional purge vs stop distinction, but undocumented).
- **`keepalive_until` missing from v1 GET** — the internal row exposes it but
  `GET /v1/sandboxes/{id}` does not surface it.
- **Warming interstitial returns 200** — the wake/warming interstitial responds
  `200`; it should likely be a non-200 (e.g. 503 + Retry-After) so callers don't
  treat "still warming" as "ready".

## Security
The manifest is **declarative config for processes that already run inside the
hardened sandbox** (cap-drop ALL, no-new-privileges, read-only rootfs, no Docker
socket). Commands run as the unprivileged `sandbox` user via `bash -lc` — exactly
like the agent's own shell. The manifest grants **no new privilege** and does not
add Docker/compose/Kubernetes access (those remain non-goals).
