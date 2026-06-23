# `sandbox.yaml` — app runtime manifest

An **optional** file in the workspace root (`~/workspace/app/sandbox.yaml`) that
declares how an app builds, runs, exposes a preview, and reports health — so
sandboxd works beyond the default Vite/React app. The coding agent can write or
edit it like any workspace file.

**No manifest = the built-in defaults** (a Vite/React web app on port 3000), so
existing apps keep working unchanged. runtimed reads the manifest on (re)start.

## Schema (version 1)

```yaml
version: 1

# The previewed process. Omit `web` entirely for a worker-only app (no preview).
web:
  command: "pnpm dev"     # how to start the web server (run via `bash -lc` in the app dir)
  port: 3000              # the HTTP port the preview routes to
  health_path: "/"        # path probed for readiness; HTTP 200 = ready

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
| `build.command` | `pnpm build` | empty ⇒ build check skipped |
| `build.timeout_seconds` | `120` | |
| `workers` | none | each gets a `worker-N` name if unnamed |

Resolution rules:
- **no file** → default web app, no workers.
- **file with `web:`** → web app with those fields (missing ones defaulted).
- **file with `workers:` and no `web:`** → worker-only app (no preview).
- **empty file** → default web app (a stray/empty file won't disable the preview).
- **invalid YAML** → logged, falls back to defaults (the app still boots).

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

## Security
The manifest is **declarative config for processes that already run inside the
hardened sandbox** (cap-drop ALL, no-new-privileges, read-only rootfs, no Docker
socket). Commands run as the unprivileged `sandbox` user via `bash -lc` — exactly
like the agent's own shell. The manifest grants **no new privilege** and does not
add Docker/compose/Kubernetes access (those remain non-goals).
