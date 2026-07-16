# `sandbox.yaml` — app runtime manifest

An **optional** file in the workspace root (`~/workspace/app/sandbox.yaml`) that
declares how an app builds, runs, exposes a preview, and reports health — so
sandboxd works beyond the default Vite/React app. The coding agent can write or
edit it like any workspace file.

**No manifest = the built-in defaults** (a Vite/React web app on port 3000), so
existing apps keep working unchanged. runtimed reads the manifest on (re)start.

> **`sandbox.yaml` is NOT Docker Compose.** It declares **one** previewed web
> process (`web:`) and optional background `workers:` — there are no `services:`,
> no `processes:`, no multi-container/multi-port orchestration, and no DB
> provisioning. A top-level `services:`/`processes:` key (or a bare top-level
> `command:`/`port:`) is now a **validation error**, not silently ignored — so a
> compose-style file no longer parses to an empty runtime and runs the default
> template. A single previewed port carries HTTP, **WebSocket**, and SSE (confirmed
> for NiceGUI/Chainlit socket.io, Sanic native WS, Streamlit, Jupyter kernels,
> Gradio SSE).

> **Presets & the process API.** Five runtime presets ship — `react-vite`,
> `nextjs`, `fastapi`, `node-express`, `worker` — each booting to a preview and
> surviving agent tasks + fork/restore. `GET /v1/sandboxes/{id}` includes
> `processes[]`; `GET /v1/sandboxes/{id}/processes/{name}/logs` tails a process's
> log; `GET /v1/presets` lists the presets and `runtime_preset` is accepted on
> app/sandbox create. Manifest view/validate: `GET /v1/apps/{id}/runtime/manifest`
> (effective manifest) and `POST /v1/runtime/manifest/validate`, plus advisory
> detection at `GET /v1/apps/{id}/runtime-inspect`.

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

Resolution rules (how **runtimed** runs a manifest — lenient, the app always boots):
- **no file** → default web app, no workers.
- **file with `web:`** → web app with those fields (missing ones defaulted).
- **file with `workers:` and no `web:`** → worker-only app (no preview).
- **empty file** → default web app (a stray/empty file won't disable the preview).
- **invalid YAML** → logged, falls back to defaults (the app still boots).

> **Validator is stricter than the executor.** The rules above keep an app booting
> even with a bad manifest. The guidance validator
> (`POST /v1/runtime/manifest/validate`, and the `validation` block on
> `GET /v1/apps/{id}/runtime/manifest`) is stricter so it can steer you to a correct,
> forward-compatible manifest: **`version: 1` is required** — a missing or
> unsupported `version` (and therefore an empty manifest) is reported `valid:false`
> with a clear error, and no `effective` runtime is returned for an invalid manifest
> (only the as-declared `parsed` view).

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

## Security
The manifest is **declarative config for processes that already run inside the
hardened sandbox** (cap-drop ALL, no-new-privileges, read-only rootfs, no Docker
socket). Commands run as the unprivileged `sandbox` user via `bash -lc` — exactly
like the agent's own shell. The manifest grants **no new privilege** and does not
add Docker/compose/Kubernetes access (those remain non-goals).
